package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nofx/store"
)

// handleGuardEvents returns the most recent guard events for a trader.
// Query params:
//   - limit (1-1000, default 100): max events to return
//   - guard_type (optional): filter by guard_type
//   - action (optional): filter by action (allow / reduce / block)
//   - symbol (optional): filter by symbol
//   - since_hours (optional, default 24): window for the stats summary
//
// Response shape:
//
//	{
//	  "events": [ ...GuardEvent ],
//	  "count":  N,
//	  "stats":  [ {guard_type, action, count}, ... ]
//	}
func (s *Server) handleGuardEvents(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	evtStore := s.store.GuardEvent()
	query := s.store.GormDB().Model(&store.GuardEvent{}).Where("trader_id = ?", traderID)
	if gt := c.Query("guard_type"); gt != "" {
		query = query.Where("guard_type = ?", gt)
	}
	if a := c.Query("action"); a != "" {
		query = query.Where("action = ?", a)
	}
	if sym := c.Query("symbol"); sym != "" {
		query = query.Where("symbol = ?", sym)
	}
	var events []*store.GuardEvent
	if err := query.Order("triggered_at DESC").Limit(limit).Find(&events).Error; err != nil && err != gorm.ErrRecordNotFound {
		SafeInternalError(c, "Get guard events", err)
		return
	}

	// Stats: per-(guard_type, action) count in the last since_hours hours.
	sinceHours := 24
	if h := c.Query("since_hours"); h != "" {
		if parsed, err := strconv.Atoi(h); err == nil && parsed > 0 && parsed <= 24*30 {
			sinceHours = parsed
		}
	}
	stats, err := evtStore.CountByTypeAction(traderID, time.Now().UTC().Add(-time.Duration(sinceHours)*time.Hour))
	if err != nil {
		SafeInternalError(c, "Get guard event stats", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
		"stats":  stats,
	})
}
