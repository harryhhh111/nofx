package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/store"
	"strings"
)

// ── Trend End Watch ──────────────────────────────────────────────────────

// IsTrendEndMiss returns true when a closed position record should be
// counted as "missed TP" for trend-end-watch purposes.
// A position is considered a miss when it closed for any reason other than
// take-profit (e.g. stop-loss, manual, risk-close, drawdown-close, sync).
func IsTrendEndMiss(p *store.TraderPosition) bool {
	if p == nil {
		return false
	}
	if p.Status != "CLOSED" {
		return false
	}
	return !strings.EqualFold(p.CloseReason, "take_profit")
}

// collectTrendEndScopeKeys mirrors collectScopeKeys but limited to the
// scopes supported by TrendEndWatchConfig (direction | symbol_side).
func collectTrendEndScopeKeys(scope string, positions []*store.TraderPosition) []string {
	switch scope {
	case store.TrendEndWatchScopeSymbolSide:
		seen := make(map[string]bool)
		keys := make([]string, 0)
		for _, p := range positions {
			if p == nil {
				continue
			}
			key := p.Symbol + "_" + strings.ToUpper(p.Side)
			if seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
		return keys
	default: // direction
		return []string{"LONG", "SHORT"}
	}
}

// countTrailingMisses returns the length of the trailing run of non-TP
// closes starting from the most-recent closed position (slice is ordered
// most-recent first; callers MUST pass the slice in exit_time DESC order
// as returned by GetClosedPositions). Non-matching positions are skipped
// without breaking the streak.
func countTrailingMisses(positions []*store.TraderPosition, scope, scopeKey string) int {
	count := 0
	for i := 0; i < len(positions); i++ {
		p := positions[i]
		if p == nil {
			continue
		}
		if !trendEndPositionMatchesKey(p, scope, scopeKey) {
			continue
		}
		if IsTrendEndMiss(p) {
			count++
		} else {
			break
		}
	}
	return count
}

// trendEndPositionMatchesKey checks whether a closed position belongs to
// the scopeKey under the given scope. symbol is preserved in upper case;
// side is matched case-insensitively.
func trendEndPositionMatchesKey(p *store.TraderPosition, scope, scopeKey string) bool {
	switch scope {
	case store.TrendEndWatchScopeSymbolSide:
		idx := strings.LastIndex(scopeKey, "_")
		if idx < 0 {
			return false
		}
		sym := scopeKey[:idx]
		sd := scopeKey[idx+1:]
		return p.Symbol == sym && strings.EqualFold(p.Side, sd)
	default: // direction
		return strings.ToUpper(p.Side) == scopeKey
	}
}

// UpdateMissCooldown advances the trend-end-watch state machine for all
// relevant scope keys. Returns the updated cooldown map (which the caller
// should persist on the trader) and the set of blocked keys.
func UpdateMissCooldown(
	states map[string]int,
	positions []*store.TraderPosition,
	cfg *store.TrendEndWatchConfig,
) (updated map[string]int, blockedKeys map[string]bool) {
	if updated == nil {
		updated = make(map[string]int)
	}
	blockedKeys = make(map[string]bool)
	if cfg == nil || !cfg.Enabled {
		// Always tick down existing cooldowns so callers can clear them.
		for k, v := range states {
			v--
			if v > 0 {
				updated[k] = v
			}
		}
		return updated, blockedKeys
	}

	scope := cfg.Scope
	if scope == "" {
		scope = store.TrendEndWatchScopeDirection
	}
	misses := cfg.Misses
	if misses <= 0 {
		misses = 3
	}
	coolDown := cfg.CoolDownCycles
	if coolDown <= 0 {
		coolDown = 3
	}

	scopeKeys := collectTrendEndScopeKeys(scope, positions)
	// Detect newly triggered keys first, then tick down existing cooldowns.
	for _, key := range scopeKeys {
		if _, already := states[key]; already {
			continue
		}
		trailing := countTrailingMisses(positions, scope, key)
		if trailing >= misses {
			updated[key] = coolDown
			blockedKeys[key] = true
			logger.Infof("👀 Trend end watch triggered for scope=%s: %d trailing misses → %d cycle cooldown",
				key, trailing, coolDown)
		}
	}
	// Tick down all existing cooldowns (including newly set ones: leave at
	// configured value on the cycle they were set).
	for k, v := range states {
		if _, newlySet := updated[k]; newlySet {
			// already initialized above; keep as-is
			continue
		}
		v--
		if v > 0 {
			updated[k] = v
		}
	}
	// Re-collect blocked keys after ticking (keys with remain > 0).
	for k, v := range updated {
		if v > 0 {
			blockedKeys[k] = true
		}
	}
	return updated, blockedKeys
}

// ApplyTrendEndWatchGate filters out open decisions whose (symbol, side)
// pair matches any active trend-end-watch scope key. direction scope keys
// map to all symbols on that side; symbol_side keys map to a single pair.
func ApplyTrendEndWatchGate(
	decisions []kernel.Decision,
	states map[string]int,
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
			key := makeTrendEndKey(scope, d.Symbol, side)
			if remain, ok := states[key]; ok && remain > 0 {
				logger.Warnf("👀 [%s] Trend-end watch[%s] (%d cycles left): BLOCKED %s %s",
					traderName, key, remain, d.Action, d.Symbol)
				continue
			}
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// makeTrendEndKey mirrors makeScopeKey but scoped to direction / symbol_side.
func makeTrendEndKey(scope, symbol, side string) string {
	switch scope {
	case store.TrendEndWatchScopeSymbolSide:
		up := strings.ToUpper(side)
		if symbol == "" {
			return up
		}
		return symbol + "_" + up
	default:
		return strings.ToUpper(side)
	}
}

// FormatTrendEndWatchPrompt builds an injected prompt notice for active
// trend-end-watch blocks. Returns empty string when there is nothing to
// inject.
func FormatTrendEndWatchPrompt(states map[string]int, scope, lang string) string {
	if len(states) == 0 {
		return ""
	}
	var keys []string
	for k, v := range states {
		if v > 0 {
			keys = append(keys, fmt.Sprintf("%s(%d)", k, v))
		}
	}
	if len(keys) == 0 {
		return ""
	}
	label := strings.Join(keys, ", ")
	if lang == "zh" || lang == "LangChinese" {
		return fmt.Sprintf("\n\n【趋势末期观望:%s】近期这些方向/币种未达 TP，按风控暂停新开仓。请专注管理现有持仓。\n", label)
	}
	return fmt.Sprintf("\n\n[TREND_END_WATCH:%s] recent closes in these directions/symbols never hit take-profit; new entries paused. Manage existing positions only.\n", label)
}
