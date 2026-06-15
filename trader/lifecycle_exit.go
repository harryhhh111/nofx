package trader

import (
	"fmt"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/logger"
	"nofx/store"
)

// intervalDuration maps a TimeStop bar interval string to its approximate
// duration. The first cut uses fixed durations rather than actual bar counts
// because bar counting would require fetching and aligning kline data.
func intervalDuration(interval string) time.Duration {
	switch strings.ToLower(interval) {
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

// CheckTrailingStop evaluates whether a trailing stop should fire. It
// compares the current leveraged PnL% against the stored peak and the
// configured trigger/retract thresholds. It returns:
//   - trigger: true if the position should be closed/reduced now
//   - newPeak: the updated peak PnL% (caller should persist it)
//   - reason:  human-readable trigger reason, empty when not triggered
func CheckTrailingStop(pos kernel.PositionInfo, peakPct float64, cfg *store.TrailingStopConfig) (trigger bool, newPeak float64, reason string) {
	newPeak = peakPct
	if cfg == nil || !cfg.Enabled {
		return false, newPeak, ""
	}
	triggerPct := cfg.TriggerPct
	if triggerPct <= 0 {
		triggerPct = 2.0
	}
	retractPct := cfg.RetractPct
	if retractPct <= 0 {
		retractPct = 0.5
	}
	minProfitLock := cfg.MinProfitLock
	if minProfitLock < 0 {
		minProfitLock = 0
	}

	current := pos.UnrealizedPnLPct
	// Update peak for both directions: for shorts a "higher" peak means the
	// position is less underwater, but trailing stop logic is symmetric: we
	// want the most favorable PnL% observed.
	if current > newPeak {
		newPeak = current
	}

	// Not armed yet.
	if newPeak < triggerPct {
		return false, newPeak, ""
	}

	// Effective floor is the higher of (peak - retract) and minProfitLock.
	floor := newPeak - retractPct
	if minProfitLock > floor {
		floor = minProfitLock
	}

	if current <= floor {
		side := strings.ToUpper(pos.Side)
		return true, newPeak, fmt.Sprintf("trailing_stop: %s %s peak=%.2f%% current=%.2f%% retracted %.2f%% (floor %.2f%%)",
			pos.Symbol, side, newPeak, current, newPeak-current, floor)
	}
	return false, newPeak, ""
}

// CheckTimeStop evaluates whether a position has been open too long. It uses
// MaxBars * BarInterval as the duration threshold.
func CheckTimeStop(pos kernel.PositionInfo, cfg *store.TimeStopConfig, now time.Time) (trigger bool, reason string) {
	if cfg == nil || !cfg.Enabled {
		return false, ""
	}
	maxBars := cfg.MaxBars
	if maxBars <= 0 {
		maxBars = 12
	}
	interval := cfg.BarInterval
	if interval == "" {
		interval = store.TimeStopBarInterval1h
	}
	threshold := time.Duration(maxBars) * intervalDuration(interval)
	elapsed := now.Sub(time.UnixMilli(pos.UpdateTime))
	if elapsed >= threshold {
		side := strings.ToUpper(pos.Side)
		return true, fmt.Sprintf("time_stop: %s %s open for %s >= %d*%s, requesting exit review",
			pos.Symbol, side, elapsed.Round(time.Minute), maxBars, interval)
	}
	return false, ""
}

// BuildLifecycleCloseDecision builds a close decision for a position that
// has been flagged by a lifecycle exit trigger. The decision asks the AI
// to review the position unless cfg.CloseImmediately is set.
func BuildLifecycleCloseDecision(pos kernel.PositionInfo, guardType, reason string, cfg *store.TimeStopConfig) kernel.Decision {
	action := "close_long"
	if strings.EqualFold(pos.Side, "SHORT") {
		action = "close_short"
	}
	closeImmediately := false
	if cfg != nil && cfg.CloseImmediately {
		closeImmediately = true
	}
	d := kernel.Decision{
		Symbol:     pos.Symbol,
		Action:     action,
		Leverage:   pos.Leverage,
		Reasoning:  fmt.Sprintf("[%s] %s", guardType, reason),
		Confidence: 100,
	}
	if closeImmediately {
		// Mark the decision as force-closed so downstream execution can
		// treat it as mandatory rather than advisory.
		d.Reasoning = fmt.Sprintf("[FORCE_CLOSE] %s", d.Reasoning)
	}
	return d
}

// emitLifecycleExitEvent writes a guard event when a lifecycle exit fires.
// It is defensive: a nil guard context is accepted and simply skipped.
func emitLifecycleExitEvent(gc *kernel.GuardContext, guardType, action, reason string, pos kernel.PositionInfo) {
	if gc == nil {
		return
	}
	var size float64
	if pos.Quantity > 0 && pos.MarkPrice > 0 {
		size = pos.Quantity * pos.MarkPrice
	}
	sideAction := "open_long"
	if strings.EqualFold(pos.Side, "SHORT") {
		sideAction = "open_short"
	}
	evt := gc.NewGuardEvent(guardType, action, reason, &kernel.Decision{
		Symbol: pos.Symbol,
		Action: sideAction,
	})
	if evt == nil {
		return
	}
	evt.Symbol = pos.Symbol
	evt.Side = strings.ToUpper(pos.Side)
	evt.EntryPrice = pos.EntryPrice
	evt.PositionSizeBefore = size
	evt.PositionSizeAfter = 0
	gc.Append(evt)
	logger.Warnf("🛡️ [%s] %s triggered for %s %s: %s", gc.TraderID, guardType, pos.Symbol, strings.ToUpper(pos.Side), reason)
}
