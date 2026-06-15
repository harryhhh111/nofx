package kernel

import (
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"strings"
)

// ============================================================================
// Decision Validation
// ============================================================================

// validateDecisions validates all decisions in place.
//  1. Invalid action types are rejected for ALL decisions (not just open).
//  2. Open decisions failing risk checks are converted to "wait" so they don't block
//     valid close/hold actions in the same batch.
//
// Returns the number of rejected decisions (0 = all passed).
//
// gc is optional. When non-nil, every guard hit is recorded as a
// GuardEvent under gc.Events for later bulk-insert into guard_events.
//
// candidates is the post-source-filter, post-ranking (if any) list of
// allowed symbols for this cycle. When non-nil, the CandidateRankingFilter
// (Enforce) can block open_* decisions whose symbol is not in the list.
func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, entryRiskGuard *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data, marketPrices map[string]float64, minSLDistances map[string]float64, gc *GuardContext, candidates []CandidateCoin, rankingFilter *store.CandidateRankingFilter) int {
	rejected := 0
	for i := range decisions {
		// Step 1: action type validation applies to ALL decisions
		validActions := map[string]bool{
			"open_long": true, "open_short": true,
			"close_long": true, "close_short": true,
			"hold": true, "wait": true,
		}
		if !validActions[decisions[i].Action] {
			reason := fmt.Sprintf("invalid action '%s'", decisions[i].Action)
			logger.Infof("⚠️ Decision #%d (%s) invalid action '%s', converting to wait",
				i+1, decisions[i].Symbol, decisions[i].Action)
			decisions[i].Action = "wait"
			decisions[i].Reasoning = fmt.Sprintf("[REJECTED] invalid action | Original: %s", decisions[i].Reasoning)
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, reason, &decisions[i]))
			rejected++
			continue
		}

		// Step 2: risk validation only for open decisions
		if decisions[i].Action != "open_long" && decisions[i].Action != "open_short" {
			continue
		}
		if err := validateDecision(&decisions[i], accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio, entryRiskGuard, marketDataMap, marketPrices, minSLDistances, gc, candidates, rankingFilter); err != nil {
			logger.Infof("⚠️ Decision #%d (%s %s) rejected, converting to wait: %v",
				i+1, decisions[i].Symbol, decisions[i].Action, err)
			decisions[i].Action = "wait"
			decisions[i].Reasoning = fmt.Sprintf("[REJECTED] %v | Original: %s", err, decisions[i].Reasoning)
			rejected++
		}
	}
	return rejected
}

// validateDecision validates a single open_long/open_short decision.
// Action type validation is done by the caller (validateDecisions).
//
// gc is optional; pass nil to disable guard-event recording (the unit
// tests use this).
//
// candidates + rankingFilter implement the Phase 3 candidate-pool
// enforcement. When rankingFilter.Enforce=true and the decision symbol
// is not in candidates, the decision is rejected with a candidate_pool
// guard event.
func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, entryRiskGuard *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data, marketPrices map[string]float64, minSLDistances map[string]float64, gc *GuardContext, candidates []CandidateCoin, rankingFilter *store.CandidateRankingFilter) error {
	if d.Action == "open_long" || d.Action == "open_short" {
		// Phase 3: candidate-pool enforcement. Runs FIRST so an
		// out-of-pool symbol doesn't waste cycles on R/R and other
		// hard-safety checks.
		if rankingFilter != nil && rankingFilter.Enforce {
			if !symbolInCandidates(d.Symbol, candidates) {
				err := fmt.Errorf("symbol %s is not in the ranked candidate pool (enforce=true)", d.Symbol)
				gc.append(gc.newGuardEvent(store.GuardEventTypeCandidatePool, store.GuardEventActionBlock, err.Error(), d))
				return err
			}
		}

		if err := applyEntryRiskGuard(d, entryRiskGuard, marketDataMap, marketPrices, minRiskRewardRatio, gc); err != nil {
			return err
		}

		maxLeverage := altcoinLeverage
		posRatio := altcoinPosRatio
		maxPositionValue := accountEquity * posRatio
		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			maxLeverage = btcEthLeverage
			posRatio = btcEthPosRatio
			maxPositionValue = accountEquity * posRatio
		}

		if d.Leverage <= 0 {
			err := fmt.Errorf("leverage must be greater than 0: %d", d.Leverage)
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
			return err
		}
		if d.Leverage > maxLeverage {
			logger.Infof("⚠️  [Leverage Fallback] %s leverage exceeded (%dx > %dx), auto-adjusting to limit %dx",
				d.Symbol, d.Leverage, maxLeverage, maxLeverage)
			d.Leverage = maxLeverage
		}
		if d.PositionSizeUSD <= 0 {
			err := fmt.Errorf("position size must be greater than 0: %.2f", d.PositionSizeUSD)
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
			return err
		}

		const minPositionSizeGeneral = 12.0
		const minPositionSizeBTCETH = 60.0

		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			if d.PositionSizeUSD < minPositionSizeBTCETH {
				err := fmt.Errorf("%s opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.Symbol, d.PositionSizeUSD, minPositionSizeBTCETH)
				gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
				return err
			}
		} else {
			if d.PositionSizeUSD < minPositionSizeGeneral {
				err := fmt.Errorf("opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.PositionSizeUSD, minPositionSizeGeneral)
				gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
				return err
			}
		}

		tolerance := maxPositionValue * 0.01
		if d.PositionSizeUSD > maxPositionValue+tolerance {
			var err error
			if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
				err = fmt.Errorf("BTC/ETH single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			} else {
				err = fmt.Errorf("altcoin single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			}
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
			return err
		}
		if d.StopLoss <= 0 || d.TakeProfit <= 0 {
			err := fmt.Errorf("stop loss and take profit must be greater than 0")
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
			return err
		}

		if d.Action == "open_long" {
			if d.StopLoss >= d.TakeProfit {
				err := fmt.Errorf("for long positions, stop loss price must be less than take profit price")
				gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
				return err
			}
		} else {
			if d.StopLoss <= d.TakeProfit {
				err := fmt.Errorf("for short positions, stop loss price must be greater than take profit price")
				gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
				return err
			}
		}

		// Use real market price as entry estimate; fall back to midpoint if unavailable
		entryPrice := 0.0
		if marketPrices != nil {
			entryPrice = marketPrices[d.Symbol]
		}
		if entryPrice <= 0 {
			entryPrice = (d.StopLoss + d.TakeProfit) / 2
		}

		// Check SL distance ≥ ATR buffer (prevent AI from setting SL too tight)
		if entryPrice > 0 {
			var minDist float64
			if minSLDistances != nil {
				minDist = minSLDistances[d.Symbol]
			}
			// Fallback: if ATR unavailable, use conservative minimum % of entry price
			if minDist <= 0 {
				if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
					minDist = entryPrice * 0.005 // 0.5% for BTC/ETH
				} else {
					minDist = entryPrice * 0.01 // 1.0% for altcoins
				}
			}
			var slDistance float64
			if d.Action == "open_long" {
				slDistance = entryPrice - d.StopLoss
			} else {
				slDistance = d.StopLoss - entryPrice
			}
			if slDistance < minDist {
				slDistPct := slDistance / entryPrice * 100
				minDistPct := minDist / entryPrice * 100
				err := fmt.Errorf("stop-loss too tight: SL distance %.2f (%.2f%%) < minimum %.2f (%.2f%%). SL must be based on chart structure + ATR, not leverage. Widen SL or skip trade",
					slDistance, slDistPct, minDist, minDistPct)
				gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
				return err
			}
		}
	}

	return nil
}

// computeRiskRewardRatio returns (riskPct, rewardPct, rrRatio) using the
// same entry-price heuristic as validateDecision. Returns zero values if
// the SL/TP do not make sense.
func computeRiskRewardRatio(d *Decision, marketPrices map[string]float64) (entryPrice, riskPct, rewardPct, rr float64) {
	if d.StopLoss <= 0 || d.TakeProfit <= 0 {
		return 0, 0, 0, 0
	}
	if d.Action != "open_long" && d.Action != "open_short" {
		return 0, 0, 0, 0
	}
	if marketPrices != nil {
		if p, ok := marketPrices[d.Symbol]; ok && p > 0 {
			entryPrice = p
		}
	}
	if entryPrice <= 0 {
		entryPrice = (d.StopLoss + d.TakeProfit) / 2
	}
	if d.Action == "open_long" {
		riskPct = (entryPrice - d.StopLoss) / entryPrice * 100
		rewardPct = (d.TakeProfit - entryPrice) / entryPrice * 100
	} else {
		riskPct = (d.StopLoss - entryPrice) / entryPrice * 100
		rewardPct = (entryPrice - d.TakeProfit) / entryPrice * 100
	}
	if riskPct > 0 {
		rr = rewardPct / riskPct
	}
	return entryPrice, riskPct, rewardPct, rr
}

// applyEntryRiskGuard runs all enabled guard checks for an open decision.
// When BlockLowRiskReward is true in cfg, the R/R tier check is also
// performed here (replacing the original 0.8× hard-floor magic number).
//
// marketPrices and minRiskRewardRatio are passed in so the R/R check can
// be evaluated against the same entry-price heuristic as the rest of the
// pipeline.
//
// gc is optional. When non-nil, every guard hit is recorded under
// gc.Events for later bulk-insert into guard_events.
//
// Returns a non-nil error only when a guard reason requires hard-blocking
// (e.g. Mode=hard_block and a non-TP reason fired, or
// TakeProfitGuardMode=hard_block and the TP reason fired). warn_reduce
// always returns nil after annotating Reasoning and reducing PositionSizeUSD.
func applyEntryRiskGuard(d *Decision, cfg *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data, marketPrices map[string]float64, minRiskRewardRatio float64, gc *GuardContext) error {
	if d.Action != "open_long" && d.Action != "open_short" {
		return nil
	}
	if minRiskRewardRatio <= 0 {
		minRiskRewardRatio = 2.0
	}

	// ── R/R check (ALWAYS RUNS, independent of cfg.Enabled) ─────────────
	// The R/R floor is a hard contract; turning the entry risk guard off
	// must not silently disable it. BlockLowRiskReward (default ON) routes
	// the hard floor through the configurable RiskRewardSoftFloor and adds a
	// soft tier that triggers warn_reduce. When BlockLowRiskReward is off,
	// fall back to the legacy MinRR × 0.8 hard floor.
	entryPrice, _, rewardPct, rr := computeRiskRewardRatio(d, marketPrices)

	// Reject TP placed on the wrong side of the entry price (e.g. open_long
	// with TP < entry, or open_short with TP > entry). Such trades have a
	// non-positive reward component and would silently skip the R/R check
	// below.
	if d.StopLoss > 0 && d.TakeProfit > 0 {
		if d.Action == "open_long" && d.TakeProfit <= entryPrice {
			err := fmt.Errorf("take profit is on the wrong side of entry price for long: entry≈%.4f, TP=%.4f", entryPrice, d.TakeProfit)
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
			return err
		}
		if d.Action == "open_short" && d.TakeProfit >= entryPrice {
			err := fmt.Errorf("take profit is on the wrong side of entry price for short: entry≈%.4f, TP=%.4f", entryPrice, d.TakeProfit)
			gc.append(gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, err.Error(), d))
			return err
		}
	}

	useTiered := cfg != nil && cfg.Enabled && cfg.BlockLowRiskReward
	if useTiered {
		guard := *cfg
		guard.Clamp()
		soft := guard.RiskRewardSoftFloor
		if soft <= 0 {
			soft = 0.8
		}
		hardFloor := minRiskRewardRatio * soft
		if rr > 0 && rr < hardFloor {
			err := fmt.Errorf("risk/reward ratio too low (%.2f:1), must be ≥%.1f:1 (hard floor %.1f:1) [stop loss: %.2f take profit: %.2f]",
				rr, minRiskRewardRatio, hardFloor, d.StopLoss, d.TakeProfit)
			gc.append(gc.newGuardEvent(store.GuardEventTypeRRCheck, store.GuardEventActionBlock, err.Error(), d))
			return err
		}
		if rr > 0 && rr < minRiskRewardRatio {
			// Soft tier: warn_reduce regardless of the global mode.
			softMsg := fmt.Sprintf(
				"R/R %.2f below target %.1f:1 (soft floor %.2f, hard floor %.1f:1)",
				rr, minRiskRewardRatio, soft, hardFloor,
			)
			sizeBefore := d.PositionSizeUSD
			applyWarnReduce(d, &guard, softMsg)
			evt := gc.newGuardEvent(store.GuardEventTypeRRCheck, store.GuardEventActionReduce, softMsg, d)
			if evt != nil {
				evt.PositionSizeBefore = sizeBefore
				evt.PositionSizeAfter = d.PositionSizeUSD
			}
			gc.append(evt)
			// Fall through: other reasons may still trigger hard-block.
		}
	} else {
		// Legacy: hard block at MinRR × 0.8
		hardFloor := minRiskRewardRatio * 0.8
		if rr > 0 && rr < hardFloor {
			err := fmt.Errorf("risk/reward ratio too low (%.2f:1), must be ≥%.1f:1 (hard floor %.1f:1) [stop loss: %.2f take profit: %.2f]",
				rr, minRiskRewardRatio, hardFloor, d.StopLoss, d.TakeProfit)
			gc.append(gc.newGuardEvent(store.GuardEventTypeRRCheck, store.GuardEventActionBlock, err.Error(), d))
			return err
		}
	}
	_ = rewardPct

	// Everything below is gated by cfg.Enabled — these are the soft guards
	// (RSI / BOLL / transition / extended TP). Returning early here keeps
	// the R/R check above as the only thing that runs when the guard is
	// fully off.
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	guard := *cfg
	guard.Clamp()

	md := (*market.Data)(nil)
	if marketDataMap != nil {
		md = marketDataMap[d.Symbol]
	}
	if md == nil {
		logger.Warnf("⚠️ Entry risk guard enabled but no market data for %s, skipping guard", d.Symbol)
		return nil
	}
	allReasons, tpReasons, otherReasons, tpAnchorType := evaluateEntryRiskGuardSplit(d, &guard, md)
	if len(allReasons) == 0 {
		return nil
	}

	// Effective mode for TP reasons: cfg.TakeProfitGuardMode overrides the
	// global cfg.Mode (only for the extended-TP reason). Empty value falls
	// back to the global Mode.
	tpMode := guard.TakeProfitGuardMode
	if tpMode == "" {
		tpMode = guard.Mode
	}

	// Hard block on TP reason → return error (even if other reasons are soft).
	if len(tpReasons) > 0 && tpMode == store.TakeProfitGuardModeHardBlock {
		reason := "entry risk guard: " + strings.Join(tpReasons, "; ") + tpAnchorDiffSuffix(gc, tpAnchorType)
		err := fmt.Errorf("%s", reason)
		gc.append(gc.newGuardEvent(store.GuardEventTypeTPAnchor, store.GuardEventActionBlock, err.Error(), d))
		return err
	}
	// Hard block on a non-TP reason → return error.
	if len(otherReasons) > 0 && guard.Mode == store.EntryRiskGuardModeHardBlock {
		err := fmt.Errorf("%s", "entry risk guard: "+strings.Join(otherReasons, "; "))
		gc.append(gc.newGuardEvent(store.GuardEventTypeEntryRiskGuard, store.GuardEventActionBlock, err.Error(), d))
		return err
	}

	// Otherwise: warn + reduce. If TP is in warn_reduce mode, prefix the
	// reasoning with [TP_EXTENSION_GUARD] so it shows up in post-mortem.
	msg := "entry risk guard: " + strings.Join(allReasons, "; ")
	prefix := "[ENTRY_GUARD_WARNING]"
	guardType := store.GuardEventTypeEntryRiskGuard
	if len(tpReasons) > 0 {
		prefix = "[TP_EXTENSION_GUARD]"
		guardType = store.GuardEventTypeTPAnchor
		msg += tpAnchorDiffSuffix(gc, tpAnchorType)
	}
	sizeBefore := d.PositionSizeUSD
	applyWarnReduce(d, &guard, fmt.Sprintf("%s %s", prefix, msg))
	evt := gc.newGuardEvent(guardType, store.GuardEventActionReduce, msg, d)
	if evt != nil {
		evt.PositionSizeBefore = sizeBefore
		evt.PositionSizeAfter = d.PositionSizeUSD
	}
	gc.append(evt)
	return nil
}

// applyWarnReduce is the shared helper that mutates a Decision into the
// "warn + reduce" state: shrinks PositionSizeUSD by ReducePositionPct and
// prepends a tagged message to Reasoning.
//
// Public so future callers (e.g. higher-level decision stages) can reuse
// the same shape without duplicating the prefix/format logic.
func applyWarnReduce(d *Decision, cfg *store.EntryRiskGuardConfig, msg string) {
	reducePct := cfg.ReducePositionPct
	if reducePct <= 0 {
		reducePct = 0.5
	}
	if reducePct > 1 {
		reducePct = 1
	}
	if reducePct < 0.1 {
		reducePct = 0.1
	}
	originalSize := d.PositionSizeUSD
	d.PositionSizeUSD *= reducePct
	d.Reasoning = fmt.Sprintf("%s; position_size_usd reduced %.2f -> %.2f | Original: %s",
		msg, originalSize, d.PositionSizeUSD, d.Reasoning)
	logger.Infof("⚠️ Decision %s %s warn_reduce: %s; reduced position %.2f -> %.2f",
		d.Symbol, d.Action, msg, originalSize, d.PositionSizeUSD)
}

func evaluateEntryRiskGuard(d *Decision, cfg *store.EntryRiskGuardConfig, md *market.Data) []string {
	all, _, _, _ := evaluateEntryRiskGuardSplit(d, cfg, md)
	return all
}

// evaluateEntryRiskGuardSplit returns three slices:
//   - allReasons: every triggered guard reason
//   - tpReasons:  the subset that came from the extended-TP check
//   - otherReasons: every triggered reason that did NOT come from TP
//
// Used by applyEntryRiskGuard so the TP reason can be subject to its own
// guard mode (TakeProfitGuardMode) independent of the global Mode.
func evaluateEntryRiskGuardSplit(d *Decision, cfg *store.EntryRiskGuardConfig, md *market.Data) (allReasons, tpReasons, otherReasons []string, tpAnchorType string) {
	var reasons []string
	isShort := d.Action == "open_short"
	isLong := d.Action == "open_long"
	if !isShort && !isLong {
		return reasons, nil, nil, ""
	}

	if cfg.BlockExtremeRSI {
		if tf1h := md.TimeframeData["1h"]; tf1h != nil {
			rsi7 := lastFloat(tf1h.RSI7Values)
			rsi14 := lastFloat(tf1h.RSI14Values)
			if isShort {
				if rsi7 > 0 && rsi7 < cfg.ShortRSI7Min {
					reasons = append(reasons, fmt.Sprintf("1h RSI7 %.2f < %.2f, short chase risk", rsi7, cfg.ShortRSI7Min))
				}
				if rsi14 > 0 && rsi14 < cfg.ShortRSI14Min {
					reasons = append(reasons, fmt.Sprintf("1h RSI14 %.2f < %.2f, short chase risk", rsi14, cfg.ShortRSI14Min))
				}
			}
			if isLong {
				if rsi7 > 0 && rsi7 > cfg.LongRSI7Max {
					reasons = append(reasons, fmt.Sprintf("1h RSI7 %.2f > %.2f, long chase risk", rsi7, cfg.LongRSI7Max))
				}
				if rsi14 > 0 && rsi14 > cfg.LongRSI14Max {
					reasons = append(reasons, fmt.Sprintf("1h RSI14 %.2f > %.2f, long chase risk", rsi14, cfg.LongRSI14Max))
				}
			}
		}
	}

	if cfg.BlockTransitionMarket {
		if tf1h := md.TimeframeData["1h"]; tf1h != nil {
			adx := lastFloat(tf1h.ADXValues)
			if adx >= cfg.TransitionADXMin && adx <= cfg.TransitionADXMax {
				reasons = append(reasons, fmt.Sprintf("1h ADX %.2f is transition market [%.2f, %.2f]", adx, cfg.TransitionADXMin, cfg.TransitionADXMax))
			}
		}
	}

	if cfg.BlockNearBollBand {
		if tf := chooseGuardTimeframe(md); tf != nil && md.CurrentPrice > 0 && tf.ATR14 > 0 {
			upper := lastFloat(tf.BOLLUpper)
			lower := lastFloat(tf.BOLLLower)
			buffer := tf.ATR14 * cfg.BollATRBuffer
			if isShort && lower > 0 && md.CurrentPrice <= lower+buffer {
				reasons = append(reasons, fmt.Sprintf("%s price %.4f near lower BOLL %.4f (+%.4f), short support risk", tf.Timeframe, md.CurrentPrice, lower, buffer))
			}
			if isLong && upper > 0 && md.CurrentPrice >= upper-buffer {
				reasons = append(reasons, fmt.Sprintf("%s price %.4f near upper BOLL %.4f (-%.4f), long resistance risk", tf.Timeframe, md.CurrentPrice, upper, buffer))
			}
		}
	}

	if cfg.BlockExtendedTakeProfit && d.TakeProfit > 0 {
		if tf := chooseGuardTimeframe(md); tf != nil && tf.ATR14 > 0 && len(tf.Klines) > 0 {
			low, high := recentLowHigh(tf.Klines)
			tolerance := tf.ATR14 * cfg.TakeProfitATRTolerance
			if isShort && low > 0 && d.TakeProfit < low-tolerance {
				reasons = append(reasons, fmt.Sprintf("TP %.4f extends below recent %s low %.4f by > %.2fx ATR", d.TakeProfit, tf.Timeframe, low, cfg.TakeProfitATRTolerance))
				if tpAnchorType == "" {
					tpAnchorType = "recent_high_low"
				}
			}
			if isLong && high > 0 && d.TakeProfit > high+tolerance {
				reasons = append(reasons, fmt.Sprintf("TP %.4f extends above recent %s high %.4f by > %.2fx ATR", d.TakeProfit, tf.Timeframe, high, cfg.TakeProfitATRTolerance))
				if tpAnchorType == "" {
					tpAnchorType = "recent_high_low"
				}
			}
		}
	}

	// Partition reasons into TP-related and others. TP reasons are detected
	// by the leading "TP " token (matches the format emitted above).
	tpReasons = make([]string, 0, len(reasons))
	otherReasons = make([]string, 0, len(reasons))
	for _, r := range reasons {
		if strings.HasPrefix(r, "TP ") {
			tpReasons = append(tpReasons, r)
		} else {
			otherReasons = append(otherReasons, r)
		}
	}
	allReasons = reasons
	return allReasons, tpReasons, otherReasons, tpAnchorType
}

// tpAnchorDiffSuffix compares the AI's declared tp_rationale.anchor_type
// with the code-detected anchor type and returns a marker when they
// disagree. Empty values on either side produce no marker.
func tpAnchorDiffSuffix(gc *GuardContext, codeAnchor string) string {
	if gc == nil || codeAnchor == "" {
		return ""
	}
	aiAnchor := aiTPAnchorType(string(gc.AIAssessment))
	if aiAnchor == "" {
		return ""
	}
	if normalizeTPAnchorType(aiAnchor) == normalizeTPAnchorType(codeAnchor) {
		return ""
	}
	return fmt.Sprintf(" [ai-code-diff: ai=%s code=%s]", aiAnchor, codeAnchor)
}

func chooseGuardTimeframe(md *market.Data) *market.TimeframeSeriesData {
	if md == nil || md.TimeframeData == nil {
		return nil
	}
	for _, tf := range []string{"15m", "1h", "30m", "5m"} {
		if data := md.TimeframeData[tf]; data != nil {
			return data
		}
	}
	for _, data := range md.TimeframeData {
		return data
	}
	return nil
}

func lastFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

// recentLowHigh returns the lowest low and highest high over the most recent
// bars. Using only a recent window makes the TP-extension guard react to
// nearby structure rather than ancient extremes from a long series.
func recentLowHigh(klines []market.KlineBar) (float64, float64) {
	if len(klines) == 0 {
		return 0, 0
	}
	const window = 20
	start := len(klines) - window
	if start < 0 {
		start = 0
	}
	low := klines[start].Low
	high := klines[start].High
	for _, kline := range klines[start+1:] {
		if kline.Low < low {
			low = kline.Low
		}
		if kline.High > high {
			high = kline.High
		}
	}
	return low, high
}

// symbolInCandidates is the membership test used by the candidate-pool
// guard. Symbols are matched case-insensitively after market.Normalize
// so "btcusdt" / "BTCUSDT" / "BtcUsdt" all collapse to the same key.
// A nil candidates slice returns false (the enforce check should not
// be invoked with an empty pool — the engine should always have
// produced a non-empty ranked list when Enforce is on).
func symbolInCandidates(symbol string, candidates []CandidateCoin) bool {
	if symbol == "" {
		return false
	}
	sym := market.Normalize(symbol)
	for _, c := range candidates {
		if market.Normalize(c.Symbol) == sym {
			return true
		}
	}
	return false
}
