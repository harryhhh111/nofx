package api

import (
	"net/http"
	"nofx/kernel"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleStrategyReplayReport(c *gin.Context) {
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
	config, err := strategy.ParseConfig()
	if err != nil {
		SafeInternalError(c, "Failed to parse strategy config", err)
		return
	}
	limit := 500
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	strategyVersion := requestedStrategyVersion(c, config)
	samples, err := s.store.SignalCalibration().RecentSamplesForVersion(strategyID, strategyVersion, limit)
	if err != nil {
		SafeInternalError(c, "Load strategy replay samples", err)
		return
	}
	report, err := kernel.BuildStrategyReplayReport(kernel.StrategyReplayRequest{
		StrategyID:      strategyID,
		StrategyVersion: strategyVersion,
		CurrentConfig:   config,
		Samples:         samples,
		Limit:           limit,
	})
	if err != nil {
		SafeInternalError(c, "Build strategy replay report", err)
		return
	}
	c.JSON(http.StatusOK, report)
}
