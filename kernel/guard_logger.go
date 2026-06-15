package kernel

import (
	"encoding/json"
	"nofx/store"
	"strings"
	"time"
)

// GuardContext is the per-cycle bag carried through the validation
// pipeline. When it is nil, guard events are not recorded (the unit
// tests use this for the no-store, no-cycle case). When non-nil, every
// guard branch appends an event to Events. The trader layer bulk-inserts
// Events after LogDecision has populated DecisionRecordID.
//
// TraderID / StrategyID / CycleNumber are stamped into every event so
// post-mortem queries can filter without joining decision_records.
// ConfigSnapshot is captured once at the top of validation and reused
// for every event (saves re-marshaling on the hot path).
//
// AIAssessment is the AI's <guard_assessment> JSON (or its
// ai_self_check subset). It's attached to every event emitted during the
// cycle so the dashboard can compute "AI expected hard block vs code
// hard block" agreement rates. Optional; nil-safe.
type GuardContext struct {
	TraderID       string
	StrategyID     string
	CycleNumber    int
	ConfigSnapshot []byte
	AIAssessment   []byte
	Events         []*store.GuardEvent
}

// newGuardEvent constructs a GuardEvent for the given decision context.
// The caller fills in GuardType, Action, Reason; everything else is
// stamped in by this helper. Snapshot and TriggeredAt are filled in here
// so every emit site is one line.
func (gc *GuardContext) newGuardEvent(
	guardType, action, reason string,
	d *Decision,
) *store.GuardEvent {
	if gc == nil {
		return nil
	}
	evt := &store.GuardEvent{
		TraderID:       gc.TraderID,
		StrategyID:     gc.StrategyID,
		CycleNumber:    gc.CycleNumber,
		GuardType:      guardType,
		Action:         action,
		Reason:         reason,
		ConfigSnapshot: gc.ConfigSnapshot,
		AIAssessment:   cloneBytes(gc.AIAssessment),
		TriggeredAt:    time.Now().UTC(),
	}
	if d != nil {
		evt.Symbol = d.Symbol
		evt.Side = sideFromAction(d.Action)
		evt.StopLoss = d.StopLoss
		evt.TakeProfit = d.TakeProfit
		evt.PositionSizeBefore = d.PositionSizeUSD
	}
	return evt
}

// cloneBytes returns a defensive copy of b so callers can mutate the
// original without affecting stored events.
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// append adds an event to the context. Safe to call on a nil receiver
// (returns immediately) so test code can pass nil everywhere.
func (gc *GuardContext) append(evt *store.GuardEvent) {
	if gc == nil || evt == nil {
		return
	}
	gc.Events = append(gc.Events, evt)
}

// Append is the exported version of append for cross-package callers (e.g.
// lifecycle exit hooks in the trader package).
func (gc *GuardContext) Append(evt *store.GuardEvent) {
	gc.append(evt)
}

// NewGuardEvent is the exported version of newGuardEvent for cross-package
// callers.
func (gc *GuardContext) NewGuardEvent(guardType, action, reason string, d *Decision) *store.GuardEvent {
	return gc.newGuardEvent(guardType, action, reason, d)
}

// sideFromAction converts an open decision action into the canonical
// side string ("LONG" or "SHORT"). Returns "" for non-open actions.
func sideFromAction(action string) string {
	switch action {
	case "open_long", "close_long":
		return "LONG"
	case "open_short", "close_short":
		return "SHORT"
	}
	return ""
}

// configSnapshotJSON marshals a config struct to JSON for the
// config_snapshot column. Returns nil on marshal error so we never
// crash the main decision path.
func configSnapshotJSON(cfg any) []byte {
	if cfg == nil {
		return nil
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	return b
}

// aiSelfCheckHasOverride returns true if the AI explicitly requested an
// override. Used to surface model drift in logs without affecting the
// hard-block outcome.
func aiSelfCheckHasOverride(guardAssessment string) (bool, string) {
	if guardAssessment == "" {
		return false, ""
	}
	var raw struct {
		AISelfCheck struct {
			OverrideSuggested bool   `json:"override_suggested"`
			OverrideReason    string `json:"override_reason"`
		} `json:"ai_self_check"`
	}
	if err := json.Unmarshal([]byte(guardAssessment), &raw); err != nil {
		return false, ""
	}
	return raw.AISelfCheck.OverrideSuggested, raw.AISelfCheck.OverrideReason
}

// aiSelfCheckSubset extracts a compact subset of <guard_assessment>:
// ai_self_check (for agreement metrics) and tp_rationale (for TP anchor
// diff analysis). Returns nil if the input is empty / not JSON / has
// neither field.
func aiSelfCheckSubset(guardAssessment string) []byte {
	if guardAssessment == "" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(guardAssessment), &raw); err != nil {
		return nil
	}
	out := make(map[string]json.RawMessage, 2)
	if sub, ok := raw["ai_self_check"]; ok {
		out["ai_self_check"] = sub
	}
	if sub, ok := raw["tp_rationale"]; ok {
		out["tp_rationale"] = sub
	}
	if len(out) == 0 {
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}

// aiTPAnchorType extracts tp_rationale.anchor_type from a
// <guard_assessment> JSON body. Returns "" if missing / malformed.
func aiTPAnchorType(guardAssessment string) string {
	if guardAssessment == "" {
		return ""
	}
	var raw struct {
		TPRationale struct {
			AnchorType string `json:"anchor_type"`
		} `json:"tp_rationale"`
	}
	if err := json.Unmarshal([]byte(guardAssessment), &raw); err != nil {
		return ""
	}
	return raw.TPRationale.AnchorType
}

// normalizeTPAnchorType maps free-form anchor_type values from the AI
// and from code to a small canonical set so diff comparisons are stable.
func normalizeTPAnchorType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "recent_high_low", "recent-low-high", "recent_low_high":
		return "recent_high_low"
	case "boll_band", "bollinger_band", "boll":
		return "boll_band"
	case "support_resistance", "support/resistance", "sr":
		return "support_resistance"
	case "breakout_extension", "extension", "breakout":
		return "extension"
	case "atr", "atr_band":
		return "atr"
	default:
		return v
	}
}
