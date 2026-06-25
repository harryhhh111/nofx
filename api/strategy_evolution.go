package api

import (
	"context"
	"net/http"
	"nofx/kernel"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleEvolveStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	strategyID := c.Param("id")
	strategy, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	if strategy.IsDefault {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot evolve system default strategy"})
		return
	}

	var req struct {
		AIModelID       string `json:"ai_model_id" binding:"required"`
		Trigger         string `json:"trigger"`
		UserInstruction string `json:"user_instruction"`
		Limit           int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}
	if strings.TrimSpace(req.Trigger) == "" {
		req.Trigger = "manual"
	}
	if req.Limit <= 0 || req.Limit > 5000 {
		req.Limit = 1000
	}

	config, err := strategy.ParseConfig()
	if err != nil {
		SafeInternalError(c, "Failed to parse strategy config", err)
		return
	}
	report, err := s.store.SignalCalibration().BuildReport(strategyID, req.Limit)
	if err != nil {
		SafeInternalError(c, "Build strategy calibration report", err)
		return
	}
	recentSamples, err := s.store.SignalCalibration().RecentSamples(strategyID, 80)
	if err != nil {
		SafeInternalError(c, "Load strategy calibration samples", err)
		return
	}
	recentClosed, err := s.store.SignalCalibration().RecentClosedPositions(strategyID, 50)
	if err != nil {
		SafeInternalError(c, "Load strategy closed outcomes", err)
		return
	}

	aiClient, err := s.createAIClientForModel(userID, req.AIModelID)
	if err != nil {
		SafeBadRequest(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	evolver := kernel.NewLLMStrategyEvolver(aiClient)
	result, err := evolver.Evolve(ctx, kernel.StrategyEvolutionRequest{
		StrategyID:      strategyID,
		CurrentConfig:   config,
		Calibration:     report,
		RecentSamples:   recentSamples,
		RecentClosed:    recentClosed,
		Language:        config.Language,
		Trigger:         req.Trigger,
		UserInstruction: req.UserInstruction,
	})
	if err != nil {
		SafeBadRequest(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, result)
}
