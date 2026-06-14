package store

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestGuardEventStore spins up an in-memory SQLite DB and creates the
// guard_events table. Each test gets its own DB so they don't see each
// other's rows.
func newTestGuardEventStore(t *testing.T) *GuardEventStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	s := NewGuardEventStore(db)
	if err := s.initTables(); err != nil {
		t.Fatalf("init tables: %v", err)
	}
	return s
}

func TestGuardEventStore_BulkInsertAndGetRecent(t *testing.T) {
	s := newTestGuardEventStore(t)
	now := time.Now().UTC()

	events := []*GuardEvent{
		{TraderID: "T1", CycleNumber: 1, GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Reason: "invalid action", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now.Add(-2 * time.Hour)},
		{TraderID: "T1", CycleNumber: 1, GuardType: GuardEventTypeRRCheck, Action: GuardEventActionBlock, Reason: "rr too low", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now.Add(-time.Hour)},
		{TraderID: "T1", CycleNumber: 2, GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionReduce, Reason: "extreme rsi", Symbol: "ETHUSDT", Side: "SHORT", TriggeredAt: now},
		// Different trader — must not show up.
		{TraderID: "T2", CycleNumber: 1, GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Reason: "x", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now},
	}
	if err := s.BulkInsert(events); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}

	got, err := s.GetRecent("T1", 10)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (T2 row must be filtered)", len(got))
	}
	// Order: triggered_at DESC → newest first
	if !got[0].TriggeredAt.After(got[1].TriggeredAt) {
		t.Fatalf("expected newest first, got %v before %v", got[0].TriggeredAt, got[1].TriggeredAt)
	}
	if got[0].GuardType != GuardEventTypeEntryRiskGuard {
		t.Fatalf("got[0].GuardType = %q, want %q", got[0].GuardType, GuardEventTypeEntryRiskGuard)
	}
}

func TestGuardEventStore_BulkInsertEmptyIsNoOp(t *testing.T) {
	s := newTestGuardEventStore(t)
	if err := s.BulkInsert(nil); err != nil {
		t.Fatalf("empty insert should be no-op, got %v", err)
	}
	if err := s.BulkInsert([]*GuardEvent{}); err != nil {
		t.Fatalf("zero-len insert should be no-op, got %v", err)
	}
}

func TestGuardEventStore_DecisionRecordIDBackReference(t *testing.T) {
	s := newTestGuardEventStore(t)
	now := time.Now().UTC()
	rid := int64(42)
	events := []*GuardEvent{
		{TraderID: "T1", CycleNumber: 7, DecisionRecordID: &rid, GuardType: GuardEventTypeTPAnchor, Action: GuardEventActionReduce, Reason: "tp extension", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now},
	}
	if err := s.BulkInsert(events); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}
	got, err := s.GetRecent("T1", 1)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if len(got) != 1 || got[0].DecisionRecordID == nil || *got[0].DecisionRecordID != 42 {
		t.Fatalf("decision_record_id round-trip failed: %+v", got)
	}
}

func TestGuardEventStore_CountByTypeAction(t *testing.T) {
	s := newTestGuardEventStore(t)
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	events := []*GuardEvent{
		{TraderID: "T1", CycleNumber: 1, GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Reason: "x", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now},
		{TraderID: "T1", CycleNumber: 2, GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Reason: "x", Symbol: "ETHUSDT", Side: "LONG", TriggeredAt: now},
		{TraderID: "T1", CycleNumber: 3, GuardType: GuardEventTypeRRCheck, Action: GuardEventActionBlock, Reason: "x", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now},
		{TraderID: "T1", CycleNumber: 4, GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionReduce, Reason: "x", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now},
		// Outside the window — must not count.
		{TraderID: "T1", CycleNumber: 5, GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Reason: "x", Symbol: "BTCUSDT", Side: "LONG", TriggeredAt: now.Add(-48 * time.Hour)},
	}
	if err := s.BulkInsert(events); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}
	rows, err := s.CountByTypeAction("T1", cutoff)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	want := map[string]int{
		"hard_safety|block":       2,
		"rr_check|block":          1,
		"entry_risk_guard|reduce": 1,
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d (%+v)", len(rows), len(want), rows)
	}
	for _, r := range rows {
		k := r.GuardType + "|" + r.Action
		if w, ok := want[k]; !ok || w != r.Count {
			t.Fatalf("row %s = %d, want %d (full=%+v)", k, r.Count, w, rows)
		}
	}
}

func TestGuardEventStore_GetRecentLimitClamp(t *testing.T) {
	s := newTestGuardEventStore(t)
	got, err := s.GetRecent("nobody", 0)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if got == nil {
		t.Fatalf("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
