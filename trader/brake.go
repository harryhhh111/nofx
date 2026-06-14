package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/store"
	"strings"
)

// ── Consecutive Loss Brake ─────────────────────────────────────────────────

// CoolingState tracks the cooling-off period after consecutive losses.
type CoolingState struct {
	TotalLosses    int // Consecutive losses that triggered this cooling period
	CoolDownTotal  int // Total cool-down cycles (K from config)
	CoolDownRemain int // Remaining cool-down cycles
}

// makeScopeKey returns the scopeKey used to index coolingStates.
//   - global:     "global"
//   - direction:  "LONG" | "SHORT"
//   - symbol_side:"BTCUSDT_LONG"
func makeScopeKey(scope, symbol, side string) string {
	switch scope {
	case store.ConsecutiveLossScopeDirection:
		up := strings.ToUpper(side)
		if up != "" {
			return up
		}
		return store.ConsecutiveLossScopeGlobal
	case store.ConsecutiveLossScopeSymbolSide:
		up := strings.ToUpper(side)
		if symbol == "" {
			return up
		}
		return symbol + "_" + up
	default:
		return store.ConsecutiveLossScopeGlobal
	}
}

// collectScopeKeys returns the deduplicated set of scopeKeys to maintain
// based on recentTrades. For global scope only "global" is returned; for
// direction scope both LONG and SHORT keys are always included so the
// counter can settle to zero on first usage. For symbol_side scope we
// include every distinct (symbol, side) pair from recentTrades.
func collectScopeKeys(scope string, recentTrades []store.RecentTrade) []string {
	switch scope {
	case store.ConsecutiveLossScopeDirection:
		return []string{"LONG", "SHORT"}
	case store.ConsecutiveLossScopeSymbolSide:
		seen := make(map[string]bool)
		keys := make([]string, 0)
		for _, t := range recentTrades {
			key := makeScopeKey(scope, t.Symbol, t.Side)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
		return keys
	default:
		return []string{store.ConsecutiveLossScopeGlobal}
	}
}

// countConsecutiveLosses returns the length of the trailing run of losses
// starting from the most-recent trade (slice is ordered most-recent first;
// callers MUST pass the slice in exit_time DESC order as returned by
// GetRecentTrades). When scope is symbol_side or direction, only trades
// matching the symbol/side filter are counted; non-matching trades are
// skipped without breaking the streak.
func countConsecutiveLosses(trades []store.RecentTrade, scope, symbol, side string) int {
	filterKey := makeScopeKey(scope, symbol, side)
	count := 0
	for i := 0; i < len(trades); i++ {
		if !tradeMatchesKey(trades[i], scope, filterKey) {
			continue
		}
		if trades[i].RealizedPnL < 0 {
			count++
		} else {
			break
		}
	}
	return count
}

// tradeMatchesKey checks if a trade belongs to the scopeKey for the
// given scope type.
func tradeMatchesKey(t store.RecentTrade, scope, scopeKey string) bool {
	switch scope {
	case store.ConsecutiveLossScopeDirection:
		return strings.ToUpper(t.Side) == scopeKey
	case store.ConsecutiveLossScopeSymbolSide:
		// scopeKey format: "SYMBOL_SIDE"
		idx := strings.LastIndex(scopeKey, "_")
		if idx < 0 {
			return false
		}
		sym := scopeKey[:idx]
		sd := scopeKey[idx+1:]
		return t.Symbol == sym && strings.ToUpper(t.Side) == sd
	default:
		return true // global scope: every trade counts
	}
}

// UpdateCoolingState advances the brake state machine for a single scope key.
// Called once per (scopeKey) per trading cycle. Returns true if entries for
// that scopeKey should be blocked.
//
// The caller is responsible for invoking this function once per scope key
// (see collectScopeKeys) and then calling ApplyConsecutiveLossGate to
// filter decisions.
func UpdateCoolingState(
	states map[string]*CoolingState,
	scopeKey string,
	recentTrades []store.RecentTrade,
	cfg *store.ConsecutiveLossBrakeConfig,
	lang string,
) (blockEntries bool, promptMsg string) {
	if cfg == nil || !cfg.Enabled {
		return false, ""
	}

	scope := cfg.Scope
	if scope == "" {
		scope = store.ConsecutiveLossScopeGlobal
	}

	// Decode scopeKey into (symbol, side) for filtering trades.
	symbol, side := decodeScopeKey(scope, scopeKey)

	losses := countConsecutiveLosses(recentTrades, scope, symbol, side)
	maxLosses, coolDown := cfg.MaxLosses, cfg.CoolDownCycles
	if maxLosses <= 0 {
		maxLosses = 3
	}
	if coolDown <= 0 {
		coolDown = 5
	}

	state := states[scopeKey]
	if state != nil {
		// Only reset when the trailing run GREW (a new loss was actually
		// added to the streak). If losses stay at the same level because
		// the brake is blocking new entries — and therefore no new trades
		// are flowing in — the cooldown must still tick down naturally,
		// otherwise the brake becomes a near-permanent lock.
		if losses > state.TotalLosses {
			*state = CoolingState{TotalLosses: losses, CoolDownTotal: coolDown, CoolDownRemain: coolDown}
		} else {
			state.CoolDownRemain--
		}
	} else {
		if losses >= maxLosses {
			state = &CoolingState{TotalLosses: losses, CoolDownTotal: coolDown, CoolDownRemain: coolDown}
			states[scopeKey] = state
		} else {
			return false, ""
		}
	}

	if state.CoolDownRemain <= 0 {
		delete(states, scopeKey)
		return false, ""
	}

	return true, formatCoolingPrompt(state, scopeKey, lang)
}

// decodeScopeKey parses a scopeKey back into (symbol, side).
// For "global" both are empty. For direction scope (e.g. "LONG")
// symbol is empty and side is "LONG". For symbol_side (e.g.
// "BTCUSDT_LONG") symbol is "BTCUSDT" and side is "LONG".
func decodeScopeKey(scope, scopeKey string) (string, string) {
	switch scope {
	case store.ConsecutiveLossScopeDirection:
		return "", scopeKey
	case store.ConsecutiveLossScopeSymbolSide:
		idx := strings.LastIndex(scopeKey, "_")
		if idx < 0 {
			return scopeKey, ""
		}
		return scopeKey[:idx], scopeKey[idx+1:]
	default:
		return "", ""
	}
}

// ApplyConsecutiveLossGate filters out open decisions whose (symbol, side)
// pair matches any active scopeKey. direction scope keys map to all symbols
// on that side; symbol_side keys map to a single pair.
func ApplyConsecutiveLossGate(
	decisions []kernel.Decision,
	states map[string]*CoolingState,
	scope string,
	traderName string,
) []kernel.Decision {
	if len(states) == 0 {
		return decisions
	}
	filtered := make([]kernel.Decision, 0, len(decisions))
	for _, d := range decisions {
		if d.Action == "open_long" || d.Action == "open_short" {
			side := "LONG"
			if d.Action == "open_short" {
				side = "SHORT"
			}
			key := makeScopeKey(scope, d.Symbol, side)
			if _, blocked := states[key]; blocked {
				logger.Warnf("🧊 [%s] Cooling[%s]: BLOCKED %s %s", traderName, key, d.Action, d.Symbol)
				continue
			}
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// ── Internal helpers ──────────────────────────────────────────────────────

func formatCoolingPrompt(s *CoolingState, scopeKey, lang string) string {
	label := scopeKey
	if scopeKey == store.ConsecutiveLossScopeGlobal {
		if lang == "zh" || lang == "LangChinese" {
			label = "全局"
		} else {
			label = "global"
		}
	}
	if lang == "zh" || lang == "LangChinese" {
		return fmt.Sprintf("\n\n【冷却期:%s】已连续 %d 笔亏损，禁止开仓 %d 个轮次。请专注管理现有持仓。剩余 %d 轮。\n",
			label, s.TotalLosses, s.CoolDownTotal, s.CoolDownRemain)
	}
	return fmt.Sprintf("\n\n[COOLING:%s] %d consecutive losses — no new positions for %d cycles. Manage existing positions only. %d cycles remaining.\n",
		label, s.TotalLosses, s.CoolDownTotal, s.CoolDownRemain)
}
