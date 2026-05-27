package kernel

import (
	"fmt"
	"nofx/logger"
)

// ============================================================================
// Decision Validation
// ============================================================================

// validateDecisions validates all decisions in place.
// 1. Invalid action types are rejected for ALL decisions (not just open).
// 2. Open decisions failing risk checks are converted to "wait" so they don't block
//    valid close/hold actions in the same batch.
// Returns the number of rejected decisions (0 = all passed).
func validateDecisions(decisions []Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, marketPrices map[string]float64, minSLDistances map[string]float64) int {
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
		if err := validateDecision(&decisions[i], accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio, marketPrices, minSLDistances); err != nil {
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
func validateDecision(d *Decision, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, marketPrices map[string]float64, minSLDistances map[string]float64) error {
	if d.Action == "open_long" || d.Action == "open_short" {
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
			return fmt.Errorf("%s leverage exceeded limit: %dx > %dx", d.Symbol, d.Leverage, maxLeverage)
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
