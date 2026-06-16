package api

import (
	"net/http"
	"strconv"
	"strings"
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

// handleGuardStats returns aggregated guard-event stats for a trader
// over a sliding window. Designed for the dashboard's "Risk Audit"
// tab: covers the Phase 6 plan metrics that don't require joining
// with trader_positions (误杀率 / 漏杀率 are deferred to a later
// iteration because they need 4h/24h post-block price lookups).
//
// Query params:
//
//	window     (4h|24h|7d, default 24h): sliding window length
//	top_limit  (1-50, default 10):       symbols per top list
//
// Response shape:
//
//	{
//	  "window_hours": 24,
//	  "since":        "2026-06-14T13:00:00Z",
//	  "totals":       { "block": 12, "reduce": 4, "allow": 0 },
//	  "by_type":      [ {guard_type, action, count}, ... ],
//	  "hourly":       [ {hour_start, guard_type, action, count}, ... ],
//	  "top_blocked_symbols": [ {symbol, count}, ... ],
//	  "ai_agreement": { total, agreed, rate, ai_blocked, code_blocked,
//	                    both_block, ai_only, code_only }
//	}
func (s *Server) handleGuardStats(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	// Parse window.
	hours := 24
	switch c.Query("window") {
	case "4h":
		hours = 4
	case "7d":
		hours = 24 * 7
	case "24h", "":
		hours = 24
	default:
		if parsed, err := strconv.Atoi(c.Query("window")); err == nil && parsed > 0 && parsed <= 24*30 {
			hours = parsed
		}
	}

	topLimit := 10
	if l := c.Query("top_limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50 {
			topLimit = parsed
		}
	}

	now := time.Now().UTC()
	since := now.Add(-time.Duration(hours) * time.Hour)
	until := now

	evtStore := s.store.GuardEvent()
	byType, err := evtStore.CountByTypeAction(traderID, since)
	if err != nil {
		SafeInternalError(c, "Get guard event stats", err)
		return
	}
	hourly, err := evtStore.HourlySeries(traderID, since, until)
	if err != nil {
		SafeInternalError(c, "Get guard event hourly series", err)
		return
	}
	topBlocked, err := evtStore.TopSymbols(traderID, store.GuardEventTypeEntryRiskGuard, store.GuardEventActionBlock, since, topLimit)
	if err != nil {
		SafeInternalError(c, "Get top blocked symbols", err)
		return
	}
	agreement, err := evtStore.AIAgreement(traderID, since)
	if err != nil {
		SafeInternalError(c, "Get AI agreement stats", err)
		return
	}

	// Aggregate totals: {block, reduce, allow}. Existing rows in
	// guard_events only use those three actions; if a future guard
	// adds a new action the default branch keeps the response
	// forward-compatible.
	totals := map[string]int{"block": 0, "reduce": 0, "allow": 0}
	for _, r := range byType {
		if _, known := totals[r.Action]; known {
			totals[r.Action] += r.Count
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"trader_id":           traderID,
		"window_hours":        hours,
		"since":               since,
		"until":               until,
		"totals":              totals,
		"by_type":             byType,
		"hourly":              hourly,
		"top_blocked_symbols": topBlocked,
		"ai_agreement":        agreement,
	})
}

// handleGuardGroups returns guard-event counts grouped by arbitrary
// dimensions. Supported dimensions: symbol, side, guard_type, action.
//
// Query params:
//
//	window    (4h|24h|7d, default 24h): sliding window length
//	group_by  (comma-separated, default guard_type,action)
//
// Response shape:
//
//	{
//	  "trader_id": "...",
//	  "window_hours": 24,
//	  "group_by": ["symbol", "side", "guard_type"],
//	  "groups": [
//	    { "symbol": "BTCUSDT", "side": "LONG", "guard_type": "entry_risk_guard", "action": "block", "count": 7 },
//	    ...
//	  ]
//	}
func (s *Server) handleGuardGroups(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	hours := 24
	switch c.Query("window") {
	case "4h":
		hours = 4
	case "7d":
		hours = 24 * 7
	case "24h", "":
		hours = 24
	default:
		if parsed, err := strconv.Atoi(c.Query("window")); err == nil && parsed > 0 && parsed <= 24*30 {
			hours = parsed
		}
	}

	groupBy := []string{"guard_type", "action"}
	if gb := c.Query("group_by"); gb != "" {
		groupBy = strings.Split(gb, ",")
	}

	now := time.Now().UTC()
	since := now.Add(-time.Duration(hours) * time.Hour)

	groups, err := s.store.GuardEvent().GroupedStats(traderID, since, groupBy)
	if err != nil {
		SafeInternalError(c, "Get guard groups", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"trader_id":    traderID,
		"window_hours": hours,
		"group_by":     groupBy,
		"groups":       groups,
	})
}
