package trader

import (
	"nofx/kernel"
	"nofx/store"
	"testing"
)

func makeRecent(symbol, side string, pnl float64) store.RecentTrade {
	return store.RecentTrade{
		Symbol:      symbol,
		Side:        side,
		RealizedPnL: pnl,
	}
}

func TestConsecutiveLossBrakeGlobalScopeBlocksAfter3Losses(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      3,
		CoolDownCycles: 2,
		Scope:          store.ConsecutiveLossScopeGlobal,
	}
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "long", -10),
		makeRecent("ETHUSDT", "short", -5),
		makeRecent("SOLUSDT", "long", -8),
	}
	states := map[string]*CoolingState{}

	blocked, _ := UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if !blocked {
		t.Fatalf("expected block after 3 consecutive losses, got unblocked")
	}
	if _, ok := states[store.ConsecutiveLossScopeGlobal]; !ok {
		t.Fatalf("expected cooling state for global key")
	}
}

func TestConsecutiveLossBrakeGlobalScopeIgnoresNonLossRun(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      3,
		CoolDownCycles: 2,
		Scope:          store.ConsecutiveLossScopeGlobal,
	}
	// Newest first (exit_time DESC): index 0 is the most recent trade.
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "long", -10),
		makeRecent("ETHUSDT", "short", -5),
		makeRecent("SOLUSDT", "long", 8), // oldest in window — irrelevant
	}
	states := map[string]*CoolingState{}

	blocked, _ := UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if blocked {
		t.Fatalf("expected NOT blocked after win, got blocked")
	}
	if len(states) != 0 {
		t.Fatalf("expected no cooling state, got %d", len(states))
	}
}

// TestConsecutiveLossBrakeNewestTradeWinTrailingZero verifies the count
// direction fix: when the most-recent trade is a WIN, the trailing loss
// run (counted from the most-recent side) must be 0, even when older
// trades are losses. The pre-fix iteration walked from the OLDEST end
// of the slice and would incorrectly report the older losses.
func TestConsecutiveLossBrakeNewestTradeWinTrailingZero(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      3,
		CoolDownCycles: 2,
		Scope:          store.ConsecutiveLossScopeGlobal,
	}
	// Most-recent trade is a win; the three older trades are losses.
	// The correct trailing-loss count is 0.
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "long", 10), // newest: win
		makeRecent("ETHUSDT", "short", -5),
		makeRecent("SOLUSDT", "long", -8),
		makeRecent("AVAXUSDT", "long", -12),
	}
	states := map[string]*CoolingState{}

	blocked, _ := UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if blocked {
		t.Fatalf("expected NOT blocked (newest trade is a win → trailing losses = 0), got blocked")
	}
	if _, ok := states[store.ConsecutiveLossScopeGlobal]; ok {
		t.Fatalf("expected no cooling state to be created")
	}
}

func TestConsecutiveLossBrakeDirectionScopeIsolatesShortAndLong(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      2,
		CoolDownCycles: 3,
		Scope:          store.ConsecutiveLossScopeDirection,
	}
	// 2 SHORT losses; LONG has only 1 loss in the trailing window.
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "long", -3),
		makeRecent("BTCUSDT", "short", -4),
		makeRecent("ETHUSDT", "short", -5),
	}
	states := map[string]*CoolingState{}

	shortBlocked, _ := UpdateCoolingState(states, "SHORT", trades, cfg, "en")
	longBlocked, _ := UpdateCoolingState(states, "LONG", trades, cfg, "en")

	if !shortBlocked {
		t.Fatalf("expected SHORT to be blocked after 2 SHORT losses")
	}
	if longBlocked {
		t.Fatalf("expected LONG to NOT be blocked (only 1 LONG loss in trailing window)")
	}
}

func TestConsecutiveLossBrakeSymbolSideScopeIsolates(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      2,
		CoolDownCycles: 3,
		Scope:          store.ConsecutiveLossScopeSymbolSide,
	}
	// BTCUSDT SHORT: 2 losses → block. ETHUSDT SHORT: 1 loss → no block.
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "short", -4),
		makeRecent("ETHUSDT", "short", -3),
		makeRecent("BTCUSDT", "short", -2),
	}
	states := map[string]*CoolingState{}

	btcBlocked, _ := UpdateCoolingState(states, "BTCUSDT_SHORT", trades, cfg, "en")
	ethBlocked, _ := UpdateCoolingState(states, "ETHUSDT_SHORT", trades, cfg, "en")

	if !btcBlocked {
		t.Fatalf("expected BTCUSDT_SHORT to be blocked")
	}
	if ethBlocked {
		t.Fatalf("expected ETHUSDT_SHORT to NOT be blocked")
	}
}

func TestApplyConsecutiveLossGateDirectionScopeOnlyBlocksMatchingSide(t *testing.T) {
	states := map[string]*CoolingState{
		"SHORT": {TotalLosses: 3, CoolDownTotal: 3, CoolDownRemain: 2},
	}
	decisions := []kernel.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "open_short"},
		{Symbol: "BTCUSDT", Action: "close_long"},
	}
	filtered := ApplyConsecutiveLossGate(decisions, states, store.ConsecutiveLossScopeDirection, "test")

	if len(filtered) != 2 {
		t.Fatalf("expected 2 decisions after filter, got %d", len(filtered))
	}
	for _, d := range filtered {
		if d.Action == "open_short" {
			t.Fatalf("open_short should have been blocked")
		}
	}
}

func TestApplyConsecutiveLossGateSymbolSideScopeBlocksOnlyMatchingPair(t *testing.T) {
	states := map[string]*CoolingState{
		"BTCUSDT_SHORT": {TotalLosses: 3, CoolDownTotal: 3, CoolDownRemain: 2},
	}
	decisions := []kernel.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "BTCUSDT", Action: "open_short"},
		{Symbol: "ETHUSDT", Action: "open_short"},
	}
	filtered := ApplyConsecutiveLossGate(decisions, states, store.ConsecutiveLossScopeSymbolSide, "test")

	if len(filtered) != 2 {
		t.Fatalf("expected 2 decisions after filter, got %d", len(filtered))
	}
	for _, d := range filtered {
		if d.Symbol == "BTCUSDT" && d.Action == "open_short" {
			t.Fatalf("BTCUSDT open_short should have been blocked")
		}
	}
}

func TestConsecutiveLossBrakeCoolDownDecrements(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      3,
		CoolDownCycles: 3,
		Scope:          store.ConsecutiveLossScopeGlobal,
	}
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "long", -1),
		makeRecent("ETHUSDT", "short", -1),
		makeRecent("SOLUSDT", "long", -1),
	}
	states := map[string]*CoolingState{}

	// Cycle 1: 3 losses → enter cooling with remain=3
	UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if states[store.ConsecutiveLossScopeGlobal].CoolDownRemain != 3 {
		t.Fatalf("remain = %d, want 3", states[store.ConsecutiveLossScopeGlobal].CoolDownRemain)
	}

	// Cycle 2: losses still 3, but no new loss added to the streak → tick down to 2.
	// This is the fix for the "permanent cooling" bug: when the gate blocks
	// new entries and therefore no new trades flow in, the cooldown must
	// still tick down naturally.
	UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if states[store.ConsecutiveLossScopeGlobal].CoolDownRemain != 2 {
		t.Fatalf("remain = %d, want 2 (steady state must tick down, not reset)", states[store.ConsecutiveLossScopeGlobal].CoolDownRemain)
	}

	// Cycle 3: → remain=1
	UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if states[store.ConsecutiveLossScopeGlobal].CoolDownRemain != 1 {
		t.Fatalf("remain = %d, want 1", states[store.ConsecutiveLossScopeGlobal].CoolDownRemain)
	}

	// Cycle 4: → remain=0 → cleared
	blocked, _ := UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades, cfg, "en")
	if blocked {
		t.Fatalf("expected NOT blocked after cool-down expired")
	}
	if _, ok := states[store.ConsecutiveLossScopeGlobal]; ok {
		t.Fatalf("expected global state to be cleared")
	}
}

// TestConsecutiveLossBrakeCooldownResetsOnFreshLoss verifies that the
// cooldown only resets when a fresh loss is added to the trailing run
// (losses > state.TotalLosses), not when the count merely stays the same.
func TestConsecutiveLossBrakeCooldownResetsOnFreshLoss(t *testing.T) {
	cfg := &store.ConsecutiveLossBrakeConfig{
		Enabled:        true,
		MaxLosses:      3,
		CoolDownCycles: 3,
		Scope:          store.ConsecutiveLossScopeGlobal,
	}
	trades3 := []store.RecentTrade{
		makeRecent("A", "long", -1),
		makeRecent("B", "long", -1),
		makeRecent("C", "long", -1),
	}
	trades4 := []store.RecentTrade{
		makeRecent("A", "long", -1),
		makeRecent("B", "long", -1),
		makeRecent("C", "long", -1),
		makeRecent("D", "long", -1),
	}
	states := map[string]*CoolingState{}

	// Cycle 1: 3 losses → enter cooling with remain=3.
	UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades3, cfg, "en")
	if states[store.ConsecutiveLossScopeGlobal].CoolDownRemain != 3 {
		t.Fatalf("remain = %d, want 3", states[store.ConsecutiveLossScopeGlobal].CoolDownRemain)
	}

	// Cycle 2: losses still 3 → decrement to 2 (no reset).
	UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades3, cfg, "en")
	if states[store.ConsecutiveLossScopeGlobal].CoolDownRemain != 2 {
		t.Fatalf("remain = %d, want 2", states[store.ConsecutiveLossScopeGlobal].CoolDownRemain)
	}

	// Cycle 3: 4 losses (fresh loss added) → reset to 3.
	UpdateCoolingState(states, store.ConsecutiveLossScopeGlobal, trades4, cfg, "en")
	if states[store.ConsecutiveLossScopeGlobal].CoolDownRemain != 3 {
		t.Fatalf("remain = %d, want 3 (reset on fresh loss added to streak)", states[store.ConsecutiveLossScopeGlobal].CoolDownRemain)
	}
	if states[store.ConsecutiveLossScopeGlobal].TotalLosses != 4 {
		t.Fatalf("TotalLosses = %d, want 4", states[store.ConsecutiveLossScopeGlobal].TotalLosses)
	}
}

func TestCollectScopeKeys(t *testing.T) {
	trades := []store.RecentTrade{
		makeRecent("BTCUSDT", "long", -1),
		makeRecent("ETHUSDT", "short", -1),
		makeRecent("BTCUSDT", "long", -2),
	}

	// global
	keys := collectScopeKeys(store.ConsecutiveLossScopeGlobal, trades)
	if len(keys) != 1 || keys[0] != store.ConsecutiveLossScopeGlobal {
		t.Fatalf("global keys = %v", keys)
	}

	// direction
	keys = collectScopeKeys(store.ConsecutiveLossScopeDirection, trades)
	if len(keys) != 2 {
		t.Fatalf("direction keys = %v, want 2", keys)
	}

	// symbol_side
	keys = collectScopeKeys(store.ConsecutiveLossScopeSymbolSide, trades)
	if len(keys) != 2 {
		t.Fatalf("symbol_side keys = %v, want 2", keys)
	}
	hasBTC := false
	hasETH := false
	for _, k := range keys {
		if k == "BTCUSDT_LONG" {
			hasBTC = true
		}
		if k == "ETHUSDT_SHORT" {
			hasETH = true
		}
	}
	if !hasBTC || !hasETH {
		t.Fatalf("symbol_side keys = %v, want BTCUSDT_LONG and ETHUSDT_SHORT", keys)
	}
}
