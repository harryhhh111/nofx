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
func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, entryRiskGuard *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data, marketPrices map[string]float64, minSLDistances map[string]float64) int {
	rejected := 0
	for i := range decisions {
		// Step 1: action type validation applies to ALL decisions
		validActions := map[string]bool{
			"open_long": true, "open_short": true,
			"close_long": true, "close_short": true,
			"hold": true, "wait": true,
		}
		if !validActions[decisions[i].Action] {
			logger.Infof("⚠️ Decision #%d (%s) invalid action '%s', converting to wait",
				i+1, decisions[i].Symbol, decisions[i].Action)
			decisions[i].Action = "wait"
			decisions[i].Reasoning = fmt.Sprintf("[REJECTED] invalid action | Original: %s", decisions[i].Reasoning)
			rejected++
			continue
		}

		// Step 2: risk validation only for open decisions
		if decisions[i].Action != "open_long" && decisions[i].Action != "open_short" {
			continue
		}
		if err := validateDecision(&decisions[i], accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio, entryRiskGuard, marketDataMap, marketPrices, minSLDistances); err != nil {
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
func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, entryRiskGuard *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data, marketPrices map[string]float64, minSLDistances map[string]float64) error {
	if d.Action == "open_long" || d.Action == "open_short" {
		if err := applyEntryRiskGuard(d, entryRiskGuard, marketDataMap); err != nil {
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
			return fmt.Errorf("leverage must be greater than 0: %d", d.Leverage)
		}
		if d.Leverage > maxLeverage {
			logger.Infof("⚠️  [Leverage Fallback] %s leverage exceeded (%dx > %dx), auto-adjusting to limit %dx",
				d.Symbol, d.Leverage, maxLeverage, maxLeverage)
			d.Leverage = maxLeverage
		}
		if d.PositionSizeUSD <= 0 {
			return fmt.Errorf("position size must be greater than 0: %.2f", d.PositionSizeUSD)
		}

		const minPositionSizeGeneral = 12.0
		const minPositionSizeBTCETH = 60.0

		if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
			if d.PositionSizeUSD < minPositionSizeBTCETH {
				return fmt.Errorf("%s opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.Symbol, d.PositionSizeUSD, minPositionSizeBTCETH)
			}
		} else {
			if d.PositionSizeUSD < minPositionSizeGeneral {
				return fmt.Errorf("opening amount too small (%.2f USDT), must be ≥%.2f USDT", d.PositionSizeUSD, minPositionSizeGeneral)
			}
		}

		tolerance := maxPositionValue * 0.01
		if d.PositionSizeUSD > maxPositionValue+tolerance {
			if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
				return fmt.Errorf("BTC/ETH single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			} else {
				return fmt.Errorf("altcoin single coin position value cannot exceed %.0f USDT (%.1fx account equity), actual: %.0f", maxPositionValue, posRatio, d.PositionSizeUSD)
			}
		}
		if d.StopLoss <= 0 || d.TakeProfit <= 0 {
			return fmt.Errorf("stop loss and take profit must be greater than 0")
		}

		if d.Action == "open_long" {
			if d.StopLoss >= d.TakeProfit {
				return fmt.Errorf("for long positions, stop loss price must be less than take profit price")
			}
		} else {
			if d.StopLoss <= d.TakeProfit {
				return fmt.Errorf("for short positions, stop loss price must be greater than take profit price")
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

		var riskPercent, rewardPercent, riskRewardRatio float64
		if d.Action == "open_long" {
			riskPercent = (entryPrice - d.StopLoss) / entryPrice * 100
			rewardPercent = (d.TakeProfit - entryPrice) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		} else {
			riskPercent = (d.StopLoss - entryPrice) / entryPrice * 100
			rewardPercent = (entryPrice - d.TakeProfit) / entryPrice * 100
			if riskPercent > 0 {
				riskRewardRatio = rewardPercent / riskPercent
			}
		}

		if minRiskRewardRatio <= 0 {
			minRiskRewardRatio = 2.0
		}
		// Match prompt tolerance: AI is told ≥80% of target is acceptable with strong signals
		hardFloor := minRiskRewardRatio * 0.8
		if riskRewardRatio < hardFloor {
			return fmt.Errorf("risk/reward ratio too low (%.2f:1), must be ≥%.1f:1 (hard floor %.1f:1) [entry≈%.2f risk: %.2f%% reward: %.2f%%] [stop loss: %.2f take profit: %.2f]",
				riskRewardRatio, minRiskRewardRatio, hardFloor, entryPrice, riskPercent, rewardPercent, d.StopLoss, d.TakeProfit)
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
				return fmt.Errorf("stop-loss too tight: SL distance %.2f (%.2f%%) < minimum %.2f (%.2f%%). SL must be based on chart structure + ATR, not leverage. Widen SL or skip trade",
					slDistance, slDistPct, minDist, minDistPct)
			}
		}
	}

	return nil
}

func applyEntryRiskGuard(d *Decision, cfg *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data) error {
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
	reasons := evaluateEntryRiskGuard(d, &guard, md)
	if len(reasons) == 0 {
		return nil
	}

	msg := "entry risk guard: " + strings.Join(reasons, "; ")
	if guard.Mode == store.EntryRiskGuardModeHardBlock {
		return fmt.Errorf("%s", msg)
	}

	reducePct := guard.ReducePositionPct
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
	d.Reasoning = fmt.Sprintf("[ENTRY_GUARD_WARNING] %s; position_size_usd reduced %.2f -> %.2f | Original: %s",
		msg, originalSize, d.PositionSizeUSD, d.Reasoning)
	logger.Infof("⚠️ Decision %s %s entry guard warning: %s; reduced position %.2f -> %.2f",
		d.Symbol, d.Action, strings.Join(reasons, "; "), originalSize, d.PositionSizeUSD)
	return nil
}

func evaluateEntryRiskGuard(d *Decision, cfg *store.EntryRiskGuardConfig, md *market.Data) []string {
	var reasons []string
	isShort := d.Action == "open_short"
	isLong := d.Action == "open_long"
	if !isShort && !isLong {
		return reasons
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
			}
			if isLong && high > 0 && d.TakeProfit > high+tolerance {
				reasons = append(reasons, fmt.Sprintf("TP %.4f extends above recent %s high %.4f by > %.2fx ATR", d.TakeProfit, tf.Timeframe, high, cfg.TakeProfitATRTolerance))
			}
		}
	}

	return reasons
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
