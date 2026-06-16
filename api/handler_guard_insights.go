package api

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nofx/market"
	"nofx/store"
)

// handleGuardInsights computes false-positive / false-negative rates for a
// trader's guard events over a sliding window.
//
// Query params:
//
//	window   (4h|24h|7d, default 24h): how far back to look for events
//	horizon  (4h|24h, default 24h):    how long after a block to wait for price
//
// Response shape:
//
//	{
//	  "trader_id": "...",
//	  "window_hours": 24,
//	  "horizon_hours": 24,
//	  "false_positive": { "count": 3, "total": 10, "rate": 0.30 },
//	  "false_negative": { "count": 2, "total": 8,  "rate": 0.25 }
//	}
//
// False positive: a block/reduce event after which price moved in the
// position's favourable direction by more than 1R (entry − stop loss).
// False negative: an allow event that was followed by an opened position
// which later closed with realized PnL < 0.
func (s *Server) handleGuardInsights(c *gin.Context) {
	_, traderID, err := s.getTraderFromQuery(c)
	if err != nil {
		SafeBadRequest(c, "Invalid trader ID")
		return
	}

	windowHours := 24
	switch c.Query("window") {
	case "4h":
		windowHours = 4
	case "7d":
		windowHours = 24 * 7
	case "24h", "":
		windowHours = 24
	default:
		if parsed, err := strconv.Atoi(c.Query("window")); err == nil && parsed > 0 && parsed <= 24*30 {
			windowHours = parsed
		}
	}

	horizonHours := 24
	switch c.Query("horizon") {
	case "4h":
		horizonHours = 4
	case "24h", "":
		horizonHours = 24
	default:
		if parsed, err := strconv.Atoi(c.Query("horizon")); err == nil && parsed > 0 && parsed <= 24*7 {
			horizonHours = parsed
		}
	}

	since := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour)
	horizon := time.Duration(horizonHours) * time.Hour

	// Cap the number of events we analyze to keep the request bounded.
	events, err := s.store.GuardEvent().GetRecent(traderID, 200)
	if err != nil {
		SafeInternalError(c, "Get guard events", err)
		return
	}

	apiClient := market.NewAPIClient()
	fpCount, fpTotal := 0, 0
	fnCount, fnTotal := 0, 0

	for _, e := range events {
		if e.TriggeredAt.Before(since) {
			continue
		}
		switch e.Action {
		case store.GuardEventActionBlock, store.GuardEventActionReduce:
			if ok := computeFalsePositive(e, horizon, apiClient); ok {
				fpCount++
			}
			fpTotal++
		case store.GuardEventActionAllow:
			if ok := s.computeFalseNegative(e); ok {
				fnCount++
			}
			fnTotal++
		}
	}

	resp := map[string]interface{}{
		"trader_id":     traderID,
		"window_hours":  windowHours,
		"horizon_hours": horizonHours,
		"false_positive": map[string]interface{}{
			"count": fpCount,
			"total": fpTotal,
			"rate":  rate(fpCount, fpTotal),
		},
		"false_negative": map[string]interface{}{
			"count": fnCount,
			"total": fnTotal,
			"rate":  rate(fnCount, fnTotal),
		},
	}
	c.JSON(http.StatusOK, resp)
}

func rate(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) / float64(total)
}

// computeFalsePositive returns true if, after the guard event, price moved
// in the favourable direction by more than 1R.
func computeFalsePositive(e *store.GuardEvent, horizon time.Duration, client *market.APIClient) bool {
	if e.Symbol == "" || e.EntryPrice <= 0 || e.StopLoss <= 0 {
		return false
	}
	interval := "1h"
	limit := int(horizon.Hours()) + 5
	if limit < 10 {
		limit = 10
	}
	if limit > 1000 {
		limit = 1000
	}
	start := e.TriggeredAt.Add(-time.Hour)
	end := e.TriggeredAt.Add(horizon)
	klines, err := client.GetKlinesRange(e.Symbol, interval, start, end, limit)
	if err != nil || len(klines) < 2 {
		return false
	}
	triggerPrice := klinePriceAtOrAfter(klines, e.TriggeredAt)
	horizonPrice := klinePriceAtOrAfter(klines, e.TriggeredAt.Add(horizon))
	if triggerPrice <= 0 || horizonPrice <= 0 {
		return false
	}
	ret := (horizonPrice - triggerPrice) / triggerPrice
	oneR := math.Abs(e.EntryPrice-e.StopLoss) / e.EntryPrice

	side := strings.ToUpper(e.Side)
	favourable := false
	if side == "LONG" && ret > oneR {
		favourable = true
	} else if side == "SHORT" && ret < -oneR {
		favourable = true
	}
	return favourable
}

// computeFalseNegative returns true if an allow event was followed by an
// opened position that later closed with realized PnL < 0.
func (s *Server) computeFalseNegative(e *store.GuardEvent) bool {
	if e.Symbol == "" || e.Side == "" {
		return false
	}
	// Look for a closed position on the same symbol/side that was opened
	// within one hour after the allow event.
	start := e.TriggeredAt
	end := e.TriggeredAt.Add(time.Hour)
	positions, err := s.store.Position().GetClosedPositionsBySymbolAndTimeRange(
		e.TraderID, e.Symbol, e.Side, start, end,
	)
	if err != nil {
		return false
	}
	for _, p := range positions {
		if p.RealizedPnL < 0 {
			return true
		}
	}
	return false
}

// klinePriceAtOrAfter returns the close price of the first kline whose
// close time is at or after t.
func klinePriceAtOrAfter(klines []market.Kline, t time.Time) float64 {
	for _, k := range klines {
		if k.CloseTime >= t.UnixMilli() {
			return k.Close
		}
	}
	return 0
}
