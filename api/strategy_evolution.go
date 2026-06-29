package api

import (
	"context"
	"net/http"
	"nofx/kernel"
	"nofx/store"
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
	strategyVersion := kernel.StrategyConfigFingerprint(config)
	report, err := s.store.SignalCalibration().BuildReportForVersion(strategyID, strategyVersion, req.Limit)
	if err != nil {
		SafeInternalError(c, "Build strategy calibration report", err)
		return
	}
	if !strategyEvolutionGateAllows(report) {
		c.JSON(http.StatusConflict, gin.H{
			"error":                 "strategy calibration evidence is not sufficient for automated evolution",
			"quality_gate":          report.QualityGate,
			"recommendation":        report.Recommendation,
			"strategy_version":      strategyVersion,
			"sample_count":          report.SampleCount,
			"closed_trade_count":    report.ClosedTradeCount,
			"min_required_samples":  report.MinRequiredSamples,
			"min_required_outcomes": report.MinRequiredOutcomes,
		})
		return
	}
	recentSamples, err := s.store.SignalCalibration().RecentSamplesForVersion(strategyID, strategyVersion, 80)
	if err != nil {
		SafeInternalError(c, "Load strategy calibration samples", err)
		return
	}
	replay, err := kernel.BuildStrategyReplayReport(kernel.StrategyReplayRequest{
		StrategyID:      strategyID,
		StrategyVersion: strategyVersion,
		CurrentConfig:   config,
		Samples:         recentSamples,
		Limit:           80,
	})
	if err != nil {
		SafeInternalError(c, "Build strategy replay report", err)
		return
	}
	recentClosed, err := s.store.SignalCalibration().RecentClosedPositionsForVersion(strategyID, strategyVersion, 50)
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
		StrategyVersion: strategyVersion,
		CurrentConfig:   config,
		Calibration:     report,
		Replay:          replay,
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

func strategyEvolutionGateAllows(report *store.SignalCalibrationReport) bool {
	if report == nil {
		return false
	}
	switch report.QualityGate {
	case "paper_ready", "needs_review":
		return true
	default:
		return false
	}
}
