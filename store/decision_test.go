package store

import (
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDecisionStore(t *testing.T) *DecisionStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	s := NewDecisionStore(db)
	if err := s.initTables(); err != nil {
		t.Fatalf("init tables: %v", err)
	}
	return s
}

func TestDecisionRecord_GuardAssessmentRoundTrip(t *testing.T) {
	s := newTestDecisionStore(t)
	const ga = `{"market_regime":"trend","ai_self_check":{"hard_block_expected":false,"override_suggested":false,"override_reason":""}}`

	rec := &DecisionRecord{
		TraderID:        "T1",
		CycleNumber:     1,
		Timestamp:       time.Now().UTC(),
		GuardAssessment: ga,
		Success:         true,
		Decisions:       []DecisionAction{},
		CandidateCoins:  []string{"BTCUSDT"},
		ExecutionLog:    []string{"ok"},
	}
	if err := s.LogDecision(rec); err != nil {
		t.Fatalf("log: %v", err)
	}
	if rec.ID == 0 {
		t.Fatalf("expected ID back-filled, got 0")
	}

	got, err := s.GetLatestRecords("T1", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].GuardAssessment != ga {
		t.Fatalf("GuardAssessment mismatch: got %q, want %q", got[0].GuardAssessment, ga)
	}
}

func TestDecisionRecord_GuardAssessmentDefaultsToEmpty(t *testing.T) {
	s := newTestDecisionStore(t)
	rec := &DecisionRecord{
		TraderID:       "T1",
		CycleNumber:    1,
		Timestamp:      time.Now().UTC(),
		Decisions:      []DecisionAction{},
		CandidateCoins: []string{},
		ExecutionLog:   []string{},
	}
	if err := s.LogDecision(rec); err != nil {
		t.Fatalf("log: %v", err)
	}
	got, err := s.GetLatestRecords("T1", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].GuardAssessment != "" {
		t.Fatalf("GuardAssessment = %q, want empty", got[0].GuardAssessment)
	}
}

func TestDecisionRecord_MalformedGuardAssessmentPreserved(t *testing.T) {
	// The DB layer never validates the JSON; it's stored verbatim so
	// post-mortem tooling can diagnose drift. The test pins this
	// contract.
	s := newTestDecisionStore(t)
	rec := &DecisionRecord{
		TraderID:        "T1",
		CycleNumber:     1,
		Timestamp:       time.Now().UTC(),
		GuardAssessment: "{not valid json",
		Decisions:       []DecisionAction{},
	}
	if err := s.LogDecision(rec); err != nil {
		t.Fatalf("log: %v", err)
	}
	got, err := s.GetLatestRecords("T1", 1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if !strings.Contains(got[0].GuardAssessment, "not valid json") {
		t.Fatalf("malformed GuardAssessment not preserved: %q", got[0].GuardAssessment)
	}
}
