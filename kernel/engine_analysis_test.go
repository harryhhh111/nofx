package kernel

import (
	"encoding/json"
	"strings"
	"testing"

	"nofx/store"
)

func TestValidateJSONFormat_AllowsThousandsSeparatorInsideStrings(t *testing.T) {
	jsonStr := `[{"symbol":"BTCUSDT","action":"hold","reasoning":"Price rejected $76,400 and remains below $76,150."}]`

	if err := validateJSONFormat(jsonStr); err != nil {
		t.Fatalf("validateJSONFormat() error = %v, want nil", err)
	}
}

func TestValidateJSONFormat_RejectsThousandsSeparatorOutsideStrings(t *testing.T) {
	jsonStr := `[{"symbol":"ETHUSDT","action":"open_short","position_size_usd":6,400,"reasoning":"test"}]`

	err := validateJSONFormat(jsonStr)
	if err == nil {
		t.Fatal("validateJSONFormat() error = nil, want thousand separator error")
	}
	if !strings.Contains(err.Error(), "thousand separator") {
		t.Fatalf("validateJSONFormat() error = %v, want thousand separator error", err)
	}
}

// ----------------------------------------------------------------------
// Phase 2: <guard_assessment> parsing
// ----------------------------------------------------------------------

func TestExtractGuardAssessment_HappyPath(t *testing.T) {
	resp := `<reasoning>ok</reasoning>
<decision>[{"symbol":"BTCUSDT","action":"hold"}]</decision>
<guard_assessment>
{
  "market_regime": "trend",
  "risk_signals": ["extreme_rsi"],
  "tp_rationale": {"anchor_type":"recent_high_low","anchor_price":65000,"breakout_evidence":[]},
  "ai_self_check": {"hard_block_expected": true, "override_suggested": false, "override_reason": ""}
}
</guard_assessment>`

	got := extractGuardAssessment(resp)
	if got == "" {
		t.Fatal("extractGuardAssessment returned empty")
	}
	if !strings.Contains(got, "hard_block_expected") {
		t.Fatalf("missing ai_self_check, got: %s", got)
	}
}

func TestExtractGuardAssessment_MissingIsEmpty(t *testing.T) {
	resp := `<reasoning>ok</reasoning>
<decision>[{"symbol":"BTCUSDT","action":"hold"}]</decision>`

	if got := extractGuardAssessment(resp); got != "" {
		t.Fatalf("expected empty for missing tag, got: %s", got)
	}
}

func TestExtractGuardAssessment_MalformedJSONPreserved(t *testing.T) {
	// We don't validate; we preserve whatever the AI emitted so the
	// post-mortem pipeline can diagnose drift.
	resp := `<decision>[{"symbol":"BTCUSDT","action":"hold"}]</decision>
<guard_assessment>{not valid json at all}</guard_assessment>`

	got := extractGuardAssessment(resp)
	if got == "" {
		t.Fatal("expected malformed content to be preserved")
	}
}

func TestParseFullDecisionResponse_OverrideSuggestedDoesNotAffectCode(t *testing.T) {
	// AI says "hard block expected=false, override_suggested=true". Code
	// should still hard-block the bad R:R — override_suggested is purely
	// a post-mortem signal, not a bypass.
	aiResponse := `<reasoning>this is risky but trust me</reasoning>
<reasoning_summary>trying to override the guard</reasoning_summary>
<decision>[{"symbol":"BTCUSDT","action":"open_long","leverage":3,"position_size_usd":1000,"stop_loss":60000,"take_profit":60100,"reasoning":"override please"}]</decision>
<guard_assessment>
{
  "market_regime": "trend",
  "risk_signals": [],
  "tp_rationale": {"anchor_type":"none","anchor_price":0,"breakout_evidence":[]},
  "ai_self_check": {"hard_block_expected": false, "override_suggested": true, "override_reason": "feels right"}
}
</guard_assessment>`

	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	dec, err := parseFullDecisionResponse(
		aiResponse,
		1000, 5, 5, 10, 5, 1.5, // minRR=1.5, hardFloor=1.2
		nil,
		nil,
		map[string]float64{"BTCUSDT": 60050},
		nil,
		gc, nil, nil,
	)
	// parseFullDecisionResponse converts bad decisions to "wait" and
	// returns nil err — the test must look at the emitted guard event
	// and the converted decision to verify the hard block fired.
	_ = err
	if dec == nil || len(dec.Decisions) == 0 {
		t.Fatalf("expected at least one decision in the result")
	}
	if dec.Decisions[0].Action != "wait" {
		t.Fatalf("expected decision converted to wait, got %q", dec.Decisions[0].Action)
	}
	// Code path produced a hard block.
	if got := eventByType(gc, store.GuardEventTypeRRCheck); got == nil || got.Action != store.GuardEventActionBlock {
		t.Fatalf("expected rr_check|block, got %+v", got)
	}
	// But the AI's override_suggested is preserved in the decision.
	if !strings.Contains(dec.GuardAssessment, "override_suggested") {
		t.Fatalf("GuardAssessment = %q, want it preserved", dec.GuardAssessment)
	}
	if !strings.Contains(dec.GuardAssessment, `"override_suggested": true`) {
		t.Fatalf("GuardAssessment missing override_suggested=true, got: %s", dec.GuardAssessment)
	}
}

func TestParseFullDecisionResponse_MissingGuardAssessmentDoesNotError(t *testing.T) {
	aiResponse := `<reasoning>just a normal call</reasoning>
<reasoning_summary>all clear</reasoning_summary>
<decision>[{"symbol":"BTCUSDT","action":"hold","reasoning":"watching"}]</decision>`

	dec, err := parseFullDecisionResponse(
		aiResponse, 1000, 5, 5, 10, 5, 1.5,
		nil, nil, nil, nil,
		&GuardContext{TraderID: "t1", CycleNumber: 1},
		nil, nil,
	)
	if err != nil {
		t.Fatalf("missing <guard_assessment> should not error, got %v", err)
	}
	if dec.GuardAssessment != "" {
		t.Fatalf("GuardAssessment should be empty, got %q", dec.GuardAssessment)
	}
	if len(dec.Decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(dec.Decisions))
	}
}

func TestAISelfCheckSubset_ExtractsOnlyAiSelfCheck(t *testing.T) {
	in := `{
  "market_regime": "trend",
  "risk_signals": ["extreme_rsi"],
  "tp_rationale": {"anchor_type":"recent_high_low"},
  "ai_self_check": {"hard_block_expected": true, "override_suggested": false, "override_reason": ""}
}`
	out := aiSelfCheckSubset(in)
	if out == nil {
		t.Fatal("aiSelfCheckSubset returned nil for valid input")
	}
	var parsed struct {
		Ai map[string]any `json:"ai_self_check"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, _ := parsed.Ai["hard_block_expected"].(bool); !v {
		t.Fatalf("ai_self_check.hard_block_expected not preserved: %s", string(out))
	}
	// Other fields should not be present at top level.
	if strings.Contains(string(out), "market_regime") {
		t.Fatalf("subset should not contain market_regime: %s", string(out))
	}
}

func TestAISelfCheckSubset_NilSafe(t *testing.T) {
	cases := []string{"", "{", "not json", `{"foo":"bar"}`}
	for _, c := range cases {
		if got := aiSelfCheckSubset(c); got != nil {
			t.Fatalf("aiSelfCheckSubset(%q) = %s, want nil", c, string(got))
		}
	}
}

func TestGuardContext_AIAssessmentAttachedToEvents(t *testing.T) {
	gc := &GuardContext{
		TraderID:     "t1",
		CycleNumber:  1,
		AIAssessment: []byte(`{"ai_self_check":{"hard_block_expected":true}}`),
	}
	d := &Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 60100}
	err := applyEntryRiskGuard(d, nil, nil, map[string]float64{"BTCUSDT": 60050}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected R/R block")
	}
	evt := eventByType(gc, store.GuardEventTypeRRCheck)
	if evt == nil {
		t.Fatalf("expected rr_check event")
	}
	if string(evt.AIAssessment) != string(gc.AIAssessment) {
		t.Fatalf("event.AIAssessment = %s, want %s", string(evt.AIAssessment), string(gc.AIAssessment))
	}
}

func TestGuardContext_AIAssessmentNilIsFine(t *testing.T) {
	// No AI assessment (older model, or section missing). Event still
	// records the rule hit; only AIAssessment column is empty.
	gc := &GuardContext{TraderID: "t1", CycleNumber: 1}
	d := &Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 60100}
	err := applyEntryRiskGuard(d, nil, nil, map[string]float64{"BTCUSDT": 60050}, 1.5, gc)
	if err == nil {
		t.Fatalf("expected R/R block")
	}
	evt := eventByType(gc, store.GuardEventTypeRRCheck)
	if evt == nil || evt.AIAssessment != nil {
		t.Fatalf("expected event with nil AIAssessment, got %+v", evt)
	}
}

func TestGuardContext_AIAssessmentNotAliasedAcrossEvents(t *testing.T) {
	// Defensive: mutating one event's AIAssessment should not affect
	// another. cloneBytes ensures the slice is independent.
	src := []byte(`{"ai_self_check":{"x":1}}`)
	gc := &GuardContext{TraderID: "t1", AIAssessment: src}
	d1 := &Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 60000, TakeProfit: 60100}
	d2 := &Decision{Symbol: "ETHUSDT", Action: "open_long", Leverage: 3, PositionSizeUSD: 1000, StopLoss: 2000, TakeProfit: 2001}
	if err := applyEntryRiskGuard(d1, nil, nil, map[string]float64{"BTCUSDT": 60050}, 1.5, gc); err == nil {
		t.Fatalf("expected R/R block for BTCUSDT")
	}
	if err := applyEntryRiskGuard(d2, nil, nil, map[string]float64{"ETHUSDT": 2000.5}, 1.5, gc); err == nil {
		t.Fatalf("expected R/R block for ETHUSDT")
	}
	if len(gc.Events) < 2 {
		t.Fatalf("expected 2 events, got %d", len(gc.Events))
	}
	gc.Events[0].AIAssessment = []byte("mutated")
	if string(gc.Events[1].AIAssessment) == "mutated" {
		t.Fatalf("events aliased: mutating one affected the other")
	}
}
