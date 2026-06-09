package trader

import (
	"fmt"

	"nofx/kernel"
	"nofx/logger"
	"nofx/store"
)

// BrakeState represents the trading brake state.
type BrakeState string

const (
	BrakeNormal     BrakeState = "normal"
	BrakeCaution    BrakeState = "caution"
	BrakeRestricted BrakeState = "restricted"
	BrakeHalt       BrakeState = "halt"
)

// BrakeConfig holds thresholds for the brake system.
// MVP: hard-coded defaults. Future: expose via strategy config.
type BrakeConfig struct {
	CautionDrawdownPct     float64 // e.g. 3%
	RestrictDrawdownPct    float64 // e.g. 5%
	HaltDrawdownPct        float64 // e.g. 10%
	HaltConsecutiveDecline int     // e.g. 3 cycles of equity decline
}

// DefaultBrakeConfig returns sensible MVP defaults.
func DefaultBrakeConfig() BrakeConfig {
	return BrakeConfig{
		CautionDrawdownPct:     3.0,
		RestrictDrawdownPct:    5.0,
		HaltDrawdownPct:        10.0,
		HaltConsecutiveDecline: 3,
	}
}

// ComputeBrakeState determines the brake state from equity data.
func ComputeBrakeState(
	currentEquity float64,
	peakEquity float64,
	equityHistory []store.EquitySnapshot,
	config BrakeConfig,
) BrakeState {
	// 1. Drawdown-based states
	if peakEquity > 0 && currentEquity > 0 {
		drawdownPct := (peakEquity - currentEquity) / peakEquity * 100
		if drawdownPct >= config.HaltDrawdownPct {
			return BrakeHalt
		}
		if drawdownPct >= config.RestrictDrawdownPct {
			return BrakeRestricted
		}
		if drawdownPct >= config.CautionDrawdownPct {
			return BrakeCaution
		}
	}

	// 2. Consecutive equity decline -> HALT
	if config.HaltConsecutiveDecline > 0 && len(equityHistory) >= config.HaltConsecutiveDecline {
		declining := true
		n := len(equityHistory)
		for i := n - config.HaltConsecutiveDecline; i < n-1; i++ {
			if i >= 0 && equityHistory[i].TotalEquity <= equityHistory[i+1].TotalEquity {
				declining = false
				break
			}
		}
		if declining {
			return BrakeHalt
		}
	}

	return BrakeNormal
}

// ApplyBrakeGate filters out open decisions when state demands it.
func ApplyBrakeGate(decisions []kernel.Decision, state BrakeState, traderName string) []kernel.Decision {
	if state == BrakeNormal {
		return decisions
	}

	filtered := make([]kernel.Decision, 0, len(decisions))
	for _, d := range decisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			switch state {
			case BrakeHalt:
				logger.Warnf("🛑 [%s] Brake HALT: BLOCKED %s %s", traderName, d.Action, d.Symbol)
			case BrakeRestricted:
				logger.Warnf("🛑 [%s] Brake RESTRICTED: BLOCKED %s %s", traderName, d.Action, d.Symbol)
			case BrakeCaution:
				// Caution allows trades but logs a warning
				logger.Warnf("⚠️ [%s] Brake CAUTION: ALLOWED %s %s (elevated scrutiny)", traderName, d.Action, d.Symbol)
				filtered = append(filtered, d)
				continue
			}
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// BrakePromptLine returns a one-line brake notice for the AI prompt.
func BrakePromptLine(state BrakeState, drawdownPct float64, lang string) string {
	if state == BrakeNormal {
		return ""
	}
	if lang == "zh" {
		switch state {
		case BrakeHalt:
			return fmt.Sprintf("【系统制动】当前回撤 %.1f%%，已强制暂停新开仓，仅允许平仓或持有。", drawdownPct)
		case BrakeRestricted:
			return fmt.Sprintf("【系统制动】当前回撤 %.1f%%，已限制新开仓，仅允许平仓或持有。", drawdownPct)
		case BrakeCaution:
			return fmt.Sprintf("【系统警告】当前回撤 %.1f%%，建议降低交易频率、提高信心度门槛。", drawdownPct)
		}
	} else {
		switch state {
		case BrakeHalt:
			return fmt.Sprintf("[SYSTEM BRAKE] Drawdown %.1f%% — new positions BLOCKED. Close or hold only.", drawdownPct)
		case BrakeRestricted:
			return fmt.Sprintf("[SYSTEM BRAKE] Drawdown %.1f%% — new positions BLOCKED. Close or hold only.", drawdownPct)
		case BrakeCaution:
			return fmt.Sprintf("[SYSTEM CAUTION] Drawdown %.1f%% — reduce frequency and raise confidence threshold.", drawdownPct)
		}
	}
	return ""
}
