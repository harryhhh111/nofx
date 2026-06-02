package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleExecutionAnalytics(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}
	if _, err := s.traderManager.GetTrader(traderID); err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	limit := 100
	if raw := c.DefaultQuery("limit", "100"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	symbol := c.Query("symbol")
	records, err := s.store.ExecutionAnalytics().List(traderID, symbol, limit)
	if err != nil {
		SafeInternalError(c, "List execution analytics", err)
		return
	}
	c.JSON(http.StatusOK, records)
}
