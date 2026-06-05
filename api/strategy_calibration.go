package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleStrategyCalibrationReport(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	strategyID := c.Param("id")
	if _, err := s.store.Strategy().Get(userID, strategyID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	limit := 1000
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	report, err := s.store.SignalCalibration().BuildReport(strategyID, limit)
	if err != nil {
		SafeInternalError(c, "Build strategy calibration report", err)
		return
	}
	c.JSON(http.StatusOK, report)
}
