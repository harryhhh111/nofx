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

func TestGuardEventStore_HourlySeries_BucketsByHour(t *testing.T) {
	s := newTestGuardEventStore(t)
	now := time.Now().UTC().Truncate(time.Hour)
	rows := []*GuardEvent{
		{TraderID: "T1", GuardType: GuardEventTypeRRCheck, Action: GuardEventActionBlock, Symbol: "BTCUSDT", TriggeredAt: now.Add(-30 * time.Minute)},
		{TraderID: "T1", GuardType: GuardEventTypeRRCheck, Action: GuardEventActionBlock, Symbol: "ETHUSDT", TriggeredAt: now.Add(-15 * time.Minute)},
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionReduce, Symbol: "BTCUSDT", TriggeredAt: now.Add(-2 * time.Hour)},
		// Out of window
		{TraderID: "T1", GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Symbol: "XRPUSDT", TriggeredAt: now.Add(-72 * time.Hour)},
	}
	if err := s.BulkInsert(rows); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}
	got, err := s.HourlySeries("T1", now.Add(-3*time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("hourly series: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (one per hour bucket)", len(got))
	}
	// 2 hours ago: 1 reduce event.
	if got[0].Action != GuardEventActionReduce || got[0].Count != 1 {
		t.Fatalf("first row = %+v, want reduce/1", got[0])
	}
	// Current hour: 2 block events (aggregated from same guard_type+action).
	if got[1].Count != 2 {
		t.Fatalf("second row count = %d, want 2", got[1].Count)
	}
}

func TestGuardEventStore_TopSymbols_FiltersAndSorts(t *testing.T) {
	s := newTestGuardEventStore(t)
	now := time.Now().UTC()
	rows := []*GuardEvent{
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionBlock, Symbol: "BTCUSDT", TriggeredAt: now},
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionBlock, Symbol: "BTCUSDT", TriggeredAt: now},
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionBlock, Symbol: "ETHUSDT", TriggeredAt: now},
		// Different action — should NOT show up.
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionReduce, Symbol: "BTCUSDT", TriggeredAt: now},
		// Different trader — should NOT show up.
		{TraderID: "T2", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionBlock, Symbol: "BTCUSDT", TriggeredAt: now},
	}
	if err := s.BulkInsert(rows); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}
	got, err := s.TopSymbols("T1", GuardEventTypeEntryRiskGuard, GuardEventActionBlock, now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("top symbols: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Symbol != "BTCUSDT" || got[0].Count != 2 {
		t.Fatalf("top = %+v, want BTCUSDT/2", got[0])
	}
	if got[1].Symbol != "ETHUSDT" || got[1].Count != 1 {
		t.Fatalf("second = %+v, want ETHUSDT/1", got[1])
	}
}

func TestGuardEventStore_AIAgreement_ComputesRate(t *testing.T) {
	s := newTestGuardEventStore(t)
	now := time.Now().UTC()

	both := []byte(`{"ai_self_check":{"hard_block_expected":true}}`)
	aiOnly := []byte(`{"ai_self_check":{"hard_block_expected":true}}`)
	codeOnly := []byte(`{"ai_self_check":{"hard_block_expected":false}}`)
	neither := []byte(`{"ai_self_check":{"hard_block_expected":false}}`)

	rows := []*GuardEvent{
		// 1. Both predicted block → agree
		{TraderID: "T1", GuardType: GuardEventTypeRRCheck, Action: GuardEventActionBlock, Symbol: "BTCUSDT", AIAssessment: both, TriggeredAt: now},
		// 2. AI predicted block, code did reduce → AI only
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionReduce, Symbol: "ETHUSDT", AIAssessment: aiOnly, TriggeredAt: now},
		// 3. AI predicted no block, code blocked → code only
		{TraderID: "T1", GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Symbol: "SOLUSDT", AIAssessment: codeOnly, TriggeredAt: now},
		// 4. AI predicted no block, code did reduce → agree
		{TraderID: "T1", GuardType: GuardEventTypeEntryRiskGuard, Action: GuardEventActionReduce, Symbol: "XRPUSDT", AIAssessment: neither, TriggeredAt: now},
		// 5. No AI assessment at all → excluded from denominator
		{TraderID: "T1", GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Symbol: "DOGEUSDT", TriggeredAt: now},
		// 6. Malformed JSON → excluded
		{TraderID: "T1", GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Symbol: "ADAUSDT", AIAssessment: []byte("not json"), TriggeredAt: now},
		// 7. JSON with no ai_self_check field → excluded
		{TraderID: "T1", GuardType: GuardEventTypeHardSafety, Action: GuardEventActionBlock, Symbol: "AVAXUSDT", AIAssessment: []byte(`{"foo":"bar"}`), TriggeredAt: now},
	}
	if err := s.BulkInsert(rows); err != nil {
		t.Fatalf("bulk insert: %v", err)
	}

	got, err := s.AIAgreement("T1", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("AI agreement: %v", err)
	}
	// Total=4 (excludes #5/#6/#7)
	if got.Total != 4 {
		t.Fatalf("Total = %d, want 4", got.Total)
	}
	// Agreed=2 (rows 1 + 4: AI matches code outcome)
	if got.Agreed != 2 {
		t.Fatalf("Agreed = %d, want 2", got.Agreed)
	}
	// Rate=0.5
	if got.Rate != 0.5 {
		t.Fatalf("Rate = %v, want 0.5", got.Rate)
	}
	// AI predicted block in 2 cases (rows 1 + 2)
	if got.AIBlocked != 2 {
		t.Fatalf("AIBlocked = %d, want 2", got.AIBlocked)
	}
	// Code blocked in 2 cases (rows 1 + 3)
	if got.CodeBlock != 2 {
		t.Fatalf("CodeBlock = %d, want 2", got.CodeBlock)
	}
	// Matrix: BothBlock=1, AIOnly=1, CodeOnly=1
	if got.BothBlock != 1 || got.AIOnly != 1 || got.CodeOnly != 1 {
		t.Fatalf("matrix wrong: both=%d ai=%d code=%d", got.BothBlock, got.AIOnly, got.CodeOnly)
	}
}

func TestGuardEventStore_AIAgreement_Empty(t *testing.T) {
	s := newTestGuardEventStore(t)
	got, err := s.AIAgreement("nobody", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("AI agreement: %v", err)
	}
	if got.Total != 0 || got.Rate != 0 {
		t.Fatalf("expected zero stats, got %+v", got)
	}
}

func TestExtractAIHardBlockExpected(t *testing.T) {
	cases := []struct {
		raw       string
		wantVal   bool
		wantFound bool
	}{
		{`{"ai_self_check":{"hard_block_expected":true}}`, true, true},
		{`{"ai_self_check":{"hard_block_expected":false}}`, false, true},
		{`{"ai_self_check":{}}`, false, false}, // field missing
		{`{"foo":"bar"}`, false, false},        // ai_self_check missing
		{`not json`, false, false},             // parse error
		{`{"ai_self_check":{"hard_block_expected":true,"override_suggested":true}}`, true, true},
	}
	for _, c := range cases {
		gotVal, gotFound := extractAIHardBlockExpected([]byte(c.raw))
		if gotVal != c.wantVal || gotFound != c.wantFound {
			t.Errorf("extractAIHardBlockExpected(%q) = (%v, %v), want (%v, %v)",
				c.raw, gotVal, gotFound, c.wantVal, c.wantFound)
		}
	}
}
