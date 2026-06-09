package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/store"
)

// ── Consecutive Loss Brake ─────────────────────────────────────────────────

// CoolingState tracks the cooling-off period after consecutive losses.
type CoolingState struct {
	TotalLosses     int // Consecutive losses that triggered this cooling period
	CoolDownTotal   int // Total cool-down cycles (K from config)
	CoolDownRemain  int // Remaining cool-down cycles
}

// UpdateCoolingState advances the brake state machine.
// Called once per trading cycle. Returns true if entries should be blocked.
func UpdateCoolingState(
	state **CoolingState,
	recentTrades []store.RecentTrade,
	cfg *store.ConsecutiveLossBrakeConfig,
	lang string,
) (blockEntries bool, promptMsg string) {
	if cfg == nil || !cfg.Enabled {
		return false, ""
	}

	losses := countConsecutiveLosses(recentTrades)
	maxLosses, coolDown := cfg.MaxLosses, cfg.CoolDownCycles
	if maxLosses <= 0 {
		maxLosses = 3
	}
	if coolDown <= 0 {
		coolDown = 5
	}

	// Already cooling?
	if *state != nil {
		// New loss during cooling → reset timer
		if losses >= maxLosses {
			**state = CoolingState{TotalLosses: losses, CoolDownTotal: coolDown, CoolDownRemain: coolDown}
		} else {
			(*state).CoolDownRemain--
		}
	} else {
		// Not cooling, check if triggered
		if losses >= maxLosses {
			*state = &CoolingState{TotalLosses: losses, CoolDownTotal: coolDown, CoolDownRemain: coolDown}
		} else {
			return false, ""
		}
	}

	s := *state
	if s.CoolDownRemain <= 0 {
		*state = nil // cooled down
		return false, ""
	}

	return true, formatCoolingPrompt(s, lang)
}

// ApplyConsecutiveLossGate filters out open decisions during cooling.
func ApplyConsecutiveLossGate(decisions []kernel.Decision, traderName string) []kernel.Decision {
	filtered := make([]kernel.Decision, 0, len(decisions))
	for _, d := range decisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			logger.Warnf("🧊 [%s] Cooling: BLOCKED %s %s", traderName, d.Action, d.Symbol)
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// ── Internal helpers ──────────────────────────────────────────────────────

func countConsecutiveLosses(trades []store.RecentTrade) int {
	if len(trades) == 0 {
		return 0
	}
	count := 0
	for i := len(trades) - 1; i >= 0; i-- {
		if trades[i].RealizedPnL < 0 {
			count++
		} else {
			break
		}
	}
	return count
}

func formatCoolingPrompt(s *CoolingState, lang string) string {
	if lang == "zh" || lang == "LangChinese" {
		return fmt.Sprintf("\n\n【冷却期】已连续 %d 笔亏损，禁止开仓 %d 个轮次。请专注管理现有持仓。剩余 %d 轮。\n", s.TotalLosses, s.CoolDownTotal, s.CoolDownRemain)
	}
	return fmt.Sprintf("\n\n[COOLING] %d consecutive losses — no new positions for %d cycles. Manage existing positions only. %d cycles remaining.\n", s.TotalLosses, s.CoolDownTotal, s.CoolDownRemain)
}
