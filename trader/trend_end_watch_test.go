package trader

import (
	"nofx/kernel"
	"nofx/store"
	"testing"
)

func mkClosedPos(symbol, side, closeReason string) *store.TraderPosition {
	return &store.TraderPosition{
		Symbol:      symbol,
		Side:        side,
		CloseReason: closeReason,
		Status:      "CLOSED",
	}
}

func TestIsTrendEndMiss(t *testing.T) {
	cases := []struct {
		reason string
		want   bool
	}{
		{"take_profit", false},
		{"TAKE_PROFIT", false},
		{"stop_loss", true},
		{"manual", true},
		{"risk", true},
		{"drawdown", true},
		{"sync", true},
		{"", true},
	}
	for _, c := range cases {
		pos := mkClosedPos("BTCUSDT", "LONG", c.reason)
		got := IsTrendEndMiss(pos)
		if got != c.want {
			t.Errorf("reason=%q got=%v want=%v", c.reason, got, c.want)
		}
	}
}

func TestUpdateMissCooldownTriggersOnThreeMisses(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         3,
		CoolDownCycles: 3,
		Scope:          store.TrendEndWatchScopeDirection,
	}
	positions := []*store.TraderPosition{
		mkClosedPos("BTCUSDT", "LONG", "stop_loss"),
		mkClosedPos("ETHUSDT", "LONG", "manual"),
		mkClosedPos("SOLUSDT", "LONG", "drawdown"),
	}
	states := map[string]int{}
	updated, blocked := UpdateMissCooldown(states, positions, cfg)
	if !blocked["LONG"] {
		t.Fatalf("expected LONG to be blocked, got blocked=%v", blocked)
	}
	if updated["LONG"] != 3 {
		t.Fatalf("updated[LONG] = %d, want 3", updated["LONG"])
	}
}

func TestUpdateMissCooldownDoesNotTriggerOnTP(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         3,
		CoolDownCycles: 3,
		Scope:          store.TrendEndWatchScopeDirection,
	}
	// Newest first (exit_time DESC): index 0 is the most recent position.
	// The TP break must be in the trailing slice FROM THE NEWEST SIDE.
	positions := []*store.TraderPosition{
		mkClosedPos("BTCUSDT", "LONG", "stop_loss"),
		mkClosedPos("ETHUSDT", "LONG", "take_profit"), // most-recent TP breaks the run
		mkClosedPos("SOLUSDT", "LONG", "stop_loss"),   // oldest — irrelevant
	}
	updated, blocked := UpdateMissCooldown(map[string]int{}, positions, cfg)
	if blocked["LONG"] {
		t.Fatalf("expected LONG NOT blocked (TP breaks run), got blocked=%v", blocked)
	}
	if _, has := updated["LONG"]; has {
		t.Fatalf("expected updated map to not contain LONG, got %v", updated)
	}
}

// TestUpdateMissCooldownNewestTPBreaksRun verifies the count direction
// fix: the trailing miss count must be computed from the most-recent
// end of the slice. Pre-fix iteration walked from the OLDEST end and
// would count the older misses even when the newest position was a TP.
func TestUpdateMissCooldownNewestTPBreaksRun(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         3,
		CoolDownCycles: 3,
		Scope:          store.TrendEndWatchScopeDirection,
	}
	// Most-recent is TP; the three older positions are all misses.
	// Correct trailing miss count is 0 → no trigger.
	positions := []*store.TraderPosition{
		mkClosedPos("BTCUSDT", "LONG", "take_profit"), // most-recent
		mkClosedPos("ETHUSDT", "LONG", "stop_loss"),
		mkClosedPos("SOLUSDT", "LONG", "stop_loss"),
		mkClosedPos("AVAXUSDT", "LONG", "manual"),
	}
	updated, blocked := UpdateMissCooldown(map[string]int{}, positions, cfg)
	if blocked["LONG"] {
		t.Fatalf("expected LONG NOT blocked (most-recent position is TP → trailing misses = 0), got blocked=%v", blocked)
	}
	if _, has := updated["LONG"]; has {
		t.Fatalf("expected no cooldown to be created for LONG, got %v", updated)
	}
}

func TestUpdateMissCooldownOnlyBlocksMatchingDirection(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         2,
		CoolDownCycles: 3,
		Scope:          store.TrendEndWatchScopeDirection,
	}
	// Trailing 3: SHORT miss, SHORT miss, LONG miss.
	// For LONG: trailing run has only 1 LONG miss (current entry was LONG).
	positions := []*store.TraderPosition{
		mkClosedPos("BTCUSDT", "LONG", "stop_loss"),
		mkClosedPos("ETHUSDT", "SHORT", "stop_loss"),
		mkClosedPos("SOLUSDT", "SHORT", "manual"),
	}
	updated, blocked := UpdateMissCooldown(map[string]int{}, positions, cfg)
	if !blocked["SHORT"] {
		t.Fatalf("expected SHORT blocked, got blocked=%v", blocked)
	}
	if blocked["LONG"] {
		t.Fatalf("expected LONG NOT blocked (only 1 trailing LONG miss), got blocked=%v", blocked)
	}
	_ = updated
}

func TestUpdateMissCooldownSymbolSideScope(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         2,
		CoolDownCycles: 2,
		Scope:          store.TrendEndWatchScopeSymbolSide,
	}
	positions := []*store.TraderPosition{
		mkClosedPos("BTCUSDT", "SHORT", "stop_loss"),
		mkClosedPos("ETHUSDT", "SHORT", "manual"),
		mkClosedPos("BTCUSDT", "SHORT", "drawdown"),
	}
	updated, blocked := UpdateMissCooldown(map[string]int{}, positions, cfg)
	if !blocked["BTCUSDT_SHORT"] {
		t.Fatalf("expected BTCUSDT_SHORT blocked, got blocked=%v", blocked)
	}
	if blocked["ETHUSDT_SHORT"] {
		t.Fatalf("expected ETHUSDT_SHORT NOT blocked (only 1 miss), got blocked=%v", blocked)
	}
	_ = updated
}

func TestUpdateMissCooldownDecrementsExistingCooldown(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         3,
		CoolDownCycles: 3,
		Scope:          store.TrendEndWatchScopeDirection,
	}
	states := map[string]int{"LONG": 2}

	updated, _ := UpdateMissCooldown(states, []*store.TraderPosition{}, cfg)
	if updated["LONG"] != 1 {
		t.Fatalf("updated[LONG] = %d, want 1 (decremented)", updated["LONG"])
	}
}

func TestUpdateMissCooldownRemovesExpiredCooldown(t *testing.T) {
	cfg := &store.TrendEndWatchConfig{
		Enabled:        true,
		Misses:         3,
		CoolDownCycles: 3,
		Scope:          store.TrendEndWatchScopeDirection,
	}
	states := map[string]int{"LONG": 1}

	updated, _ := UpdateMissCooldown(states, []*store.TraderPosition{}, cfg)
	if _, has := updated["LONG"]; has {
		t.Fatalf("expected LONG removed after decrementing to 0, got %v", updated)
	}
}

func TestApplyTrendEndWatchGateDirectionBlocksMatchingSide(t *testing.T) {
	states := map[string]int{"SHORT": 2}
	decisions := []kernel.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "ETHUSDT", Action: "open_short"},
		{Symbol: "BTCUSDT", Action: "close_long"},
	}
	filtered := ApplyTrendEndWatchGate(decisions, states, store.TrendEndWatchScopeDirection, "test")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(filtered))
	}
	for _, d := range filtered {
		if d.Action == "open_short" {
			t.Fatalf("open_short should have been blocked")
		}
	}
}

func TestApplyTrendEndWatchGateSymbolSideBlocksMatchingPair(t *testing.T) {
	states := map[string]int{"BTCUSDT_SHORT": 2}
	decisions := []kernel.Decision{
		{Symbol: "BTCUSDT", Action: "open_long"},
		{Symbol: "BTCUSDT", Action: "open_short"},
		{Symbol: "ETHUSDT", Action: "open_short"},
	}
	filtered := ApplyTrendEndWatchGate(decisions, states, store.TrendEndWatchScopeSymbolSide, "test")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(filtered))
	}
	for _, d := range filtered {
		if d.Symbol == "BTCUSDT" && d.Action == "open_short" {
			t.Fatalf("BTCUSDT open_short should have been blocked")
		}
	}
}

func TestUpdateMissCooldownDisabledStillTicksDown(t *testing.T) {
	// Even when disabled, the cooldown should tick down so existing blocks
	// expire naturally.
	cfg := &store.TrendEndWatchConfig{Enabled: false}
	states := map[string]int{"LONG": 2}
	updated, blocked := UpdateMissCooldown(states, nil, cfg)
	if updated["LONG"] != 1 {
		t.Fatalf("expected LONG to decrement to 1, got %d", updated["LONG"])
	}
	if len(blocked) != 0 {
		t.Fatalf("expected no new blocks when disabled, got %v", blocked)
	}
}

func TestFormatTrendEndWatchPrompt(t *testing.T) {
	states := map[string]int{"LONG": 2, "BTCUSDT_SHORT": 1}
	got := FormatTrendEndWatchPrompt(states, store.TrendEndWatchScopeDirection, "en")
	if got == "" {
		t.Fatalf("expected non-empty prompt")
	}
	if !containsString(got, "[TREND_END_WATCH:") {
		t.Fatalf("expected EN prompt, got %q", got)
	}

	zh := FormatTrendEndWatchPrompt(states, store.TrendEndWatchScopeDirection, "zh")
	if !containsString(zh, "【趋势末期观望:") {
		t.Fatalf("expected ZH prompt, got %q", zh)
	}
}

func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
