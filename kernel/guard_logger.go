package kernel

import (
	"encoding/json"
	"nofx/store"
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

// aiSelfCheckSubset extracts just the `ai_self_check` field from a
// <guard_assessment> JSON body. Used to store a compact, focused
// snapshot alongside each guard event so the dashboard can compute
// agreement rates without re-parsing the full prompt output.
//
// Returns nil if the input is empty / not JSON / has no ai_self_check
// field — callers must not treat this as an error.
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

func aiSelfCheckSubset(guardAssessment string) []byte {
	if guardAssessment == "" {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(guardAssessment), &raw); err != nil {
		return nil
	}
	sub, ok := raw["ai_self_check"]
	if !ok {
		return nil
	}
	// Re-marshal as a compact JSON object so the column stores
	// {"hard_block_expected":..., "override_suggested":..., "override_reason":...}
	out, err := json.Marshal(map[string]json.RawMessage{"ai_self_check": sub})
	if err != nil {
		return nil
	}
	return out
}
