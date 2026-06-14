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
type GuardContext struct {
	TraderID       string
	StrategyID     string
	CycleNumber    int
	ConfigSnapshot []byte
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
