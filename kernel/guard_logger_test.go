package kernel

import (
	"encoding/json"
	"strings"
	"testing"

	"nofx/market"
	"nofx/store"
)

// eventByType returns the first event in gc.Events matching guardType,
// or nil if none. Used to keep assertions short.
func eventByType(gc *GuardContext, guardType string) *store.GuardEvent {
	for _, e := range gc.Events {
		if e.GuardType == guardType {
			return e
		}
	}
	return nil
}

func TestGuardContext_NilSafeAppend(t *testing.T) {
	// Calling newGuardEvent / append on a nil GuardContext must not panic.
	var gc *GuardContext
	evt := gc.newGuardEvent(store.GuardEventTypeHardSafety, store.GuardEventActionBlock, "x", &Decision{Symbol: "BTCUSDT"})
	if evt != nil {
		t.Fatalf("nil guard context should produce nil event")
	}
	gc.append(nil) // must not panic
}

func TestValidateDecision_EmitsHardSafetyOnInvalidAction(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	// Second decision has invalid action — that's the row we want to assert on.
	decisions := []Decision{
		{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 70000},
		{Symbol: "ETHUSDT", Action: "moon_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 2000, TakeProfit: 3000},
	}

	validateDecisions(decisions, 1000, 5, 5, 10, 5, 1.5, nil, nil, nil, nil, gc, nil, nil)

	if got := eventByType(gc, store.GuardEventTypeHardSafety); got == nil || got.Action != store.GuardEventActionBlock {
		t.Fatalf("expected hard_safety|block event, got %+v", got)
	}
	if got := eventByType(gc, store.GuardEventTypeHardSafety); got != nil && got.Symbol != "ETHUSDT" {
		t.Fatalf("event.Symbol = %q, want ETHUSDT", got.Symbol)
	}
}

func TestValidateDecision_EmitsHardSafetyOnLeverageZero(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	// Use a market price so the entry-price heuristic gives SL/TP enough
	// room to satisfy the SL-distance guard before leverage=0 fires.
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 0, PositionSizeUSD: 1000, StopLoss: 64000, TakeProfit: 68000}
	err := validateDecision(&d, 10000, 5, 5, 10, 5, 1.5, nil, nil, map[string]float64{"BTCUSDT": 65000}, nil, gc, nil, nil)
	if err == nil {
		t.Fatalf("expected leverage error")
	}
	if !contains(err.Error(), "leverage must be greater than 0") {
		t.Fatalf("err = %q, want leverage error", err.Error())
	}
	got := eventByType(gc, store.GuardEventTypeHardSafety)
	if got == nil || got.Reason == "" {
		t.Fatalf("expected hard_safety event, got %+v", got)
	}
}

func TestApplyEntryRiskGuard_EmitsHardSafetyOnTPWrongSide(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 64000}
	err := applyEntryRiskGuard(&d, nil, nil, map[string]float64{"BTCUSDT": 65000}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected wrong-side rejection")
	}
	if !strings.Contains(err.Error(), "wrong side") {
		t.Fatalf("err = %q, want 'wrong side'", err.Error())
	}
	if got := eventByType(gc, store.GuardEventTypeHardSafety); got == nil || got.Action != store.GuardEventActionBlock {
		t.Fatalf("expected hard_safety|block, got %+v", got)
	}
}

func TestApplyEntryRiskGuard_EmitsRRCheckOnHardBlock(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	// entry=61000, SL=60000 (risk=1000), TP=61100 (reward=100), rr=0.1 → hard block at hardFloor=1.2
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 61100}
	err := applyEntryRiskGuard(&d, nil, nil, map[string]float64{"BTCUSDT": 61000}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected R/R hard block")
	}
	if !contains(err.Error(), "risk/reward") {
		t.Fatalf("err = %q, want risk/reward error", err.Error())
	}
	if got := eventByType(gc, store.GuardEventTypeRRCheck); got == nil || got.Action != store.GuardEventActionBlock {
		t.Fatalf("expected rr_check|block, got %+v", got)
	}
}

func TestApplyEntryRiskGuard_EmitsRRCheckReduceOnSoftTier(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	cfg := store.DefaultEntryRiskGuardConfig()
	cfg.Enabled = true
	cfg.BlockLowRiskReward = true
	cfg.RiskRewardSoftFloor = 0.8
	// entry=61000, SL=60000 (risk=1000, risk%=1.639%), need rr in (1.2, 1.5) → reward%=2.13% → TP=62300 → rr=1.30
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 62300}
	err := applyEntryRiskGuard(&d, cfg, nil, map[string]float64{"BTCUSDT": 61000}, 1.5, gc)
	if err != nil {
		t.Fatalf("soft tier should not return err, got %v", err)
	}
	got := eventByType(gc, store.GuardEventTypeRRCheck)
	if got == nil {
		t.Fatalf("expected rr_check event, got events=%+v", gc.Events)
	}
	if got.Action != store.GuardEventActionReduce {
		t.Fatalf("got.Action = %q, want reduce", got.Action)
	}
	if got.PositionSizeAfter >= got.PositionSizeBefore {
		t.Fatalf("size should be reduced: before=%v after=%v", got.PositionSizeBefore, got.PositionSizeAfter)
	}
}

func TestApplyEntryRiskGuard_EmitsTPAnchorOnTPBlock(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	cfg := store.DefaultEntryRiskGuardConfig()
	cfg.Enabled = true
	cfg.Mode = store.EntryRiskGuardModeWarnReduce
	cfg.TakeProfitGuardMode = store.TakeProfitGuardModeHardBlock
	cfg.BlockExtremeRSI = false
	cfg.BlockNearBollBand = false
	cfg.BlockTransitionMarket = false
	cfg.BlockLowRiskReward = false
	// K-lines tight around 65000; TP=70000 extends above recent high. Use
	// market price 65000 so R/R is large enough to pass.
	klines := makeKlines("BTCUSDT", 65000, 30, 0.005)
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 64000, TakeProfit: 70000}
	err := applyEntryRiskGuard(&d, cfg, map[string]*market.Data{
		"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 65000, TimeframeData: map[string]*market.TimeframeSeriesData{"15m": {Timeframe: "15m", ATR14: 100, Klines: klines}}},
	}, map[string]float64{"BTCUSDT": 65000}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected TP-anchor hard block, events=%+v", gc.Events)
	}
	if got := eventByType(gc, store.GuardEventTypeTPAnchor); got == nil || got.Action != store.GuardEventActionBlock {
		t.Fatalf("expected tp_anchor|block, got events=%+v", gc.Events)
	}
}

func TestApplyEntryRiskGuard_EmitsEntryRiskGuardOnNonTPBlock(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	cfg := store.DefaultEntryRiskGuardConfig()
	cfg.Enabled = true
	cfg.Mode = store.EntryRiskGuardModeHardBlock
	cfg.BlockExtendedTakeProfit = false
	cfg.BlockLowRiskReward = false
	// Extreme RSI on the long side (> LongRSI7Max default 70). Use a
	// large enough TP so the R/R check passes.
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 64000, TakeProfit: 70000}
	err := applyEntryRiskGuard(&d, cfg, map[string]*market.Data{
		"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 65000, TimeframeData: map[string]*market.TimeframeSeriesData{"1h": {Timeframe: "1h", RSI7Values: []float64{85.0}, RSI14Values: []float64{82.0}}}},
	}, map[string]float64{"BTCUSDT": 65000}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected RSI hard block")
	}
	if got := eventByType(gc, store.GuardEventTypeEntryRiskGuard); got == nil || got.Action != store.GuardEventActionBlock {
		t.Fatalf("expected entry_risk_guard|block, got events=%+v", gc.Events)
	}
}

func TestGuardContext_NilSinkIsNoOpInPipeline(t *testing.T) {
	// A nil *GuardContext must not change the existing behavior of any
	// function in the validation pipeline. This regression-tests the
	// "test code passes nil everywhere" path.
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 61100}
	err := applyEntryRiskGuard(&d, nil, nil, map[string]float64{"BTCUSDT": 61000}, 1.5, nil)
	if err == nil {
		t.Fatalf("expected R/R hard block with nil guard context")
	}
	// Must not panic
	_ = d
}

func TestConfigSnapshotJSON_NilSafe(t *testing.T) {
	if got := configSnapshotJSON(nil); got != nil {
		t.Fatalf("nil cfg should produce nil snapshot, got %s", string(got))
	}
	cfg := store.DefaultEntryRiskGuardConfig()
	got := configSnapshotJSON(cfg)
	if !json.Valid(got) {
		t.Fatalf("snapshot is not valid JSON: %s", string(got))
	}
}

func TestTPAnchorDiffSuffix(t *testing.T) {
	cases := []struct {
		name      string
		ai        string
		code      string
		wantEmpty bool
	}{
		{"empty assessment", ``, "recent_high_low", true},
		{"missing tp_rationale", `{"ai_self_check":{}}`, "recent_high_low", true},
		{"match recent_high_low", `{"tp_rationale":{"anchor_type":"recent_high_low"}}`, "recent_high_low", true},
		{"match via normalization", `{"tp_rationale":{"anchor_type":"recent_low_high"}}`, "recent_high_low", true},
		{"diff breakout vs recent", `{"tp_rationale":{"anchor_type":"breakout_extension"}}`, "recent_high_low", false},
		{"diff boll vs recent", `{"tp_rationale":{"anchor_type":"boll_band"}}`, "recent_high_low", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gc := &GuardContext{AIAssessment: []byte(c.ai)}
			got := tpAnchorDiffSuffix(gc, c.code)
			if c.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty suffix, got %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected non-empty diff suffix")
			}
			if !strings.Contains(got, "ai-code-diff") {
				t.Fatalf("expected ai-code-diff marker, got %q", got)
			}
		})
	}
}

func TestApplyEntryRiskGuard_TPAnchorDiffInReason(t *testing.T) {
	gc := &GuardContext{
		TraderID:     "t1",
		CycleNumber:  1,
		AIAssessment: []byte(`{"tp_rationale":{"anchor_type":"breakout_extension"}}`),
	}
	cfg := store.DefaultEntryRiskGuardConfig()
	cfg.Enabled = true
	cfg.Mode = store.EntryRiskGuardModeWarnReduce
	cfg.TakeProfitGuardMode = store.TakeProfitGuardModeHardBlock
	cfg.BlockExtremeRSI = false
	cfg.BlockNearBollBand = false
	cfg.BlockTransitionMarket = false
	cfg.BlockLowRiskReward = false

	klines := makeKlines("BTCUSDT", 65000, 30, 0.005)
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 64000, TakeProfit: 70000}
	err := applyEntryRiskGuard(&d, cfg, map[string]*market.Data{
		"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 65000, TimeframeData: map[string]*market.TimeframeSeriesData{"15m": {Timeframe: "15m", ATR14: 100, Klines: klines}}},
	}, map[string]float64{"BTCUSDT": 65000}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected TP-anchor hard block")
	}
	got := eventByType(gc, store.GuardEventTypeTPAnchor)
	if got == nil {
		t.Fatalf("expected tp_anchor event")
	}
	if !strings.Contains(got.Reason, "ai-code-diff") {
		t.Fatalf("expected ai-code-diff marker in reason, got %q", got.Reason)
	}
}

func TestApplyEntryRiskGuard_RecordAllowEvents(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	cfg := store.DefaultEntryRiskGuardConfig()
	cfg.RecordAllowEvents = true
	cfg.BlockLowRiskReward = false
	// Make every soft guard trivially pass while still being "enabled".
	cfg.LongRSI7Max = 100
	cfg.LongRSI14Max = 100
	cfg.BollATRBuffer = 100
	cfg.TransitionADXMin = 100
	cfg.TransitionADXMax = 200

	// TP inside recent range, R/R > minRiskRewardRatio=0.5.
	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 64000, TakeProfit: 65700}
	err := applyEntryRiskGuard(&d, cfg, map[string]*market.Data{
		"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 65000, TimeframeData: map[string]*market.TimeframeSeriesData{"15m": {Timeframe: "15m", ATR14: 100, Klines: makeKlines("BTCUSDT", 65000, 30, 0.005)}}},
	}, map[string]float64{"BTCUSDT": 65000}, 0.5, gc)
	if err != nil {
		t.Fatalf("unexpected block: %v", err)
	}
	got := eventByType(gc, store.GuardEventTypeEntryRiskGuard)
	if got == nil {
		t.Fatalf("expected allow event")
	}
	if got.Action != store.GuardEventActionAllow {
		t.Fatalf("expected action=allow, got %s", got.Action)
	}
}

func TestApplyEntryRiskGuard_SkipAllowEventsWhenDisabled(t *testing.T) {
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	cfg := store.DefaultEntryRiskGuardConfig()
	cfg.RecordAllowEvents = false
	cfg.BlockLowRiskReward = false
	cfg.LongRSI7Max = 100
	cfg.LongRSI14Max = 100
	cfg.BollATRBuffer = 100
	cfg.TransitionADXMin = 100
	cfg.TransitionADXMax = 200

	d := Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 64000, TakeProfit: 65700}
	err := applyEntryRiskGuard(&d, cfg, map[string]*market.Data{
		"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 65000, TimeframeData: map[string]*market.TimeframeSeriesData{"15m": {Timeframe: "15m", ATR14: 100, Klines: makeKlines("BTCUSDT", 65000, 30, 0.005)}}},
	}, map[string]float64{"BTCUSDT": 65000}, 0.5, gc)
	if err != nil {
		t.Fatalf("unexpected block: %v", err)
	}
	if len(gc.Events) != 0 {
		t.Fatalf("expected no events when RecordAllowEvents=false, got %d", len(gc.Events))
	}
}
