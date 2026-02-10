package store

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for testing
// Skips the test gracefully if CGO is disabled (go-sqlite3 requires CGO)
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Skipf("skipping DB test: %v (CGO may be disabled)", err)
	}
	if err := db.AutoMigrate(&DecisionRecordDB{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return db
}

// insertTestRecord inserts a test decision record into the database
func insertTestRecord(t *testing.T, db *gorm.DB, traderID string, cycleNumber int, timestamp time.Time, success bool, decisions []DecisionAction) {
	t.Helper()
	decisionsJSON, _ := json.Marshal(decisions)
	record := &DecisionRecordDB{
		TraderID:            traderID,
		CycleNumber:         cycleNumber,
		Timestamp:           timestamp,
		SystemPrompt:        "system prompt content (should not appear in digest)",
		InputPrompt:         "input prompt content (should not appear in digest)",
		CoTTrace:            "chain of thought trace (should not appear in digest)",
		RawResponse:         "raw AI response (should not appear in digest)",
		Decisions:           string(decisionsJSON),
		Success:             success,
		ErrorMessage:        "",
		AIRequestDurationMs: 3000,
	}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("failed to insert test record: %v", err)
	}
}

// TestToDigest verifies that toDigest correctly converts DB model to DecisionDigest
// and excludes verbose fields (SystemPrompt, InputPrompt, CoTTrace, RawResponse)
func TestToDigest(t *testing.T) {
	decisions := []DecisionAction{
		{
			Action:     "open_long",
			Symbol:     "BTCUSDT",
			Quantity:   0.01,
			Leverage:   5,
			Price:      95000,
			StopLoss:   93000,
			TakeProfit: 100000,
			Confidence: 85,
			Reasoning:  "Bullish momentum detected",
			Success:    true,
		},
	}
	decisionsJSON, _ := json.Marshal(decisions)

	now := time.Now().UTC()
	dbRecord := &DecisionRecordDB{
		ID:                  1,
		TraderID:            "test-trader-001",
		CycleNumber:         10,
		Timestamp:           now,
		SystemPrompt:        "long system prompt...",
		InputPrompt:         "long input prompt...",
		CoTTrace:            "BTC showing strong bullish momentum with EMA20 above EMA50, MACD histogram expanding, RSI at 65 indicating room to run. OI increasing with positive netflow suggesting institutional buying.",
		RawResponse:         "long raw AI response...",
		Decisions:           string(decisionsJSON),
		Success:             true,
		ErrorMessage:        "",
		AIRequestDurationMs: 2500,
	}

	digest := dbRecord.toDigest()

	// Verify key fields are correctly populated
	if digest.TraderID != "test-trader-001" {
		t.Errorf("expected TraderID 'test-trader-001', got '%s'", digest.TraderID)
	}
	if digest.CycleNumber != 10 {
		t.Errorf("expected CycleNumber 10, got %d", digest.CycleNumber)
	}
	if !digest.Timestamp.Equal(now) {
		t.Errorf("expected Timestamp %v, got %v", now, digest.Timestamp)
	}
	if !digest.Success {
		t.Errorf("expected Success true, got false")
	}
	if digest.AIRequestDurationMs != 2500 {
		t.Errorf("expected AIRequestDurationMs 2500, got %d", digest.AIRequestDurationMs)
	}

	// Verify CoTTrace (core AI reasoning) is included in digest
	if digest.CoTTrace == "" {
		t.Errorf("expected CoTTrace to be populated, got empty string")
	}
	if digest.CoTTrace != dbRecord.CoTTrace {
		t.Errorf("expected CoTTrace to match DB record")
	}

	// Verify decisions are correctly deserialized
	if len(digest.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(digest.Decisions))
	}
	if digest.Decisions[0].Action != "open_long" {
		t.Errorf("expected action 'open_long', got '%s'", digest.Decisions[0].Action)
	}
	if digest.Decisions[0].Symbol != "BTCUSDT" {
		t.Errorf("expected symbol 'BTCUSDT', got '%s'", digest.Decisions[0].Symbol)
	}
	if digest.Decisions[0].Confidence != 85 {
		t.Errorf("expected confidence 85, got %d", digest.Decisions[0].Confidence)
	}
}

// TestGetLatestDigest tests GetLatestDigest returns the most recent record for a trader
func TestGetLatestDigest(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()
	traderID := "trader-A"

	// Insert older record
	insertTestRecord(t, db, traderID, 1, now.Add(-10*time.Minute), true,
		[]DecisionAction{{Action: "hold", Symbol: "BTCUSDT"}})

	// Insert newer record (this should be returned)
	insertTestRecord(t, db, traderID, 2, now, true,
		[]DecisionAction{{Action: "open_long", Symbol: "ETHUSDT", Confidence: 90}})

	digest, err := store.GetLatestDigest(traderID)
	if err != nil {
		t.Fatalf("GetLatestDigest failed: %v", err)
	}

	// Verify the latest record is returned
	if digest.CycleNumber != 2 {
		t.Errorf("expected CycleNumber 2 (latest), got %d", digest.CycleNumber)
	}
	if len(digest.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(digest.Decisions))
	}
	if digest.Decisions[0].Action != "open_long" {
		t.Errorf("expected action 'open_long', got '%s'", digest.Decisions[0].Action)
	}
	if digest.Decisions[0].Symbol != "ETHUSDT" {
		t.Errorf("expected symbol 'ETHUSDT', got '%s'", digest.Decisions[0].Symbol)
	}
}

// TestGetLatestDigest_NotFound tests GetLatestDigest when no records exist for the trader
func TestGetLatestDigest_NotFound(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	_, err := store.GetLatestDigest("nonexistent-trader")
	if err == nil {
		t.Errorf("expected error for nonexistent trader, got nil")
	}
}

// TestGetTradersLatestDigests_SortByTimestamp tests the list endpoint sorted by timestamp
func TestGetTradersLatestDigests_SortByTimestamp(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()

	// Trader A: latest at now-5min
	insertTestRecord(t, db, "trader-A", 1, now.Add(-5*time.Minute), true,
		[]DecisionAction{{Action: "hold", Symbol: "BTCUSDT"}})

	// Trader B: latest at now (most recent)
	insertTestRecord(t, db, "trader-B", 1, now, true,
		[]DecisionAction{{Action: "open_long", Symbol: "ETHUSDT"}})

	// Trader C: latest at now-10min (oldest)
	insertTestRecord(t, db, "trader-C", 1, now.Add(-10*time.Minute), false,
		[]DecisionAction{{Action: "wait", Symbol: "ALL"}})

	// Test descending order (default: newest first)
	digests, err := store.GetTradersLatestDigests(nil, "timestamp", "desc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 3 {
		t.Fatalf("expected 3 digests, got %d", len(digests))
	}
	if digests[0].TraderID != "trader-B" {
		t.Errorf("expected first trader to be 'trader-B' (newest), got '%s'", digests[0].TraderID)
	}
	if digests[2].TraderID != "trader-C" {
		t.Errorf("expected last trader to be 'trader-C' (oldest), got '%s'", digests[2].TraderID)
	}

	// Test ascending order (oldest first)
	digestsAsc, err := store.GetTradersLatestDigests(nil, "timestamp", "asc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests (asc) failed: %v", err)
	}
	if digestsAsc[0].TraderID != "trader-C" {
		t.Errorf("expected first trader (asc) to be 'trader-C' (oldest), got '%s'", digestsAsc[0].TraderID)
	}
}

// TestGetTradersLatestDigests_SortByTraderID tests sorting by trader_id
func TestGetTradersLatestDigests_SortByTraderID(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()
	insertTestRecord(t, db, "trader-C", 1, now, true, []DecisionAction{{Action: "hold"}})
	insertTestRecord(t, db, "trader-A", 1, now, true, []DecisionAction{{Action: "hold"}})
	insertTestRecord(t, db, "trader-B", 1, now, true, []DecisionAction{{Action: "hold"}})

	// Ascending by trader_id
	digests, err := store.GetTradersLatestDigests(nil, "trader_id", "asc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 3 {
		t.Fatalf("expected 3, got %d", len(digests))
	}
	if digests[0].TraderID != "trader-A" {
		t.Errorf("expected first 'trader-A', got '%s'", digests[0].TraderID)
	}
	if digests[1].TraderID != "trader-B" {
		t.Errorf("expected second 'trader-B', got '%s'", digests[1].TraderID)
	}
	if digests[2].TraderID != "trader-C" {
		t.Errorf("expected third 'trader-C', got '%s'", digests[2].TraderID)
	}
}

// TestGetTradersLatestDigests_SortBySuccess tests sorting by success status
func TestGetTradersLatestDigests_SortBySuccess(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()
	insertTestRecord(t, db, "trader-fail", 1, now, false, []DecisionAction{{Action: "wait"}})
	insertTestRecord(t, db, "trader-success", 1, now, true, []DecisionAction{{Action: "open_long"}})

	// Descending: success first
	digests, err := store.GetTradersLatestDigests(nil, "success", "desc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 2 {
		t.Fatalf("expected 2, got %d", len(digests))
	}
	if !digests[0].Success {
		t.Errorf("expected first digest to be success=true (desc)")
	}
	if digests[1].Success {
		t.Errorf("expected second digest to be success=false (desc)")
	}
}

// TestGetTradersLatestDigests_Limit tests the limit parameter
func TestGetTradersLatestDigests_Limit(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		traderID := "trader-" + string(rune('A'+i))
		insertTestRecord(t, db, traderID, 1, now.Add(time.Duration(i)*time.Minute), true,
			[]DecisionAction{{Action: "hold"}})
	}

	// Limit to 2 results
	digests, err := store.GetTradersLatestDigests(nil, "timestamp", "desc", 2)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 2 {
		t.Errorf("expected 2 digests with limit=2, got %d", len(digests))
	}
}

// TestGetTradersLatestDigests_FilterByTraderIDs tests filtering by specific trader IDs
func TestGetTradersLatestDigests_FilterByTraderIDs(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()
	insertTestRecord(t, db, "trader-A", 1, now, true, []DecisionAction{{Action: "hold"}})
	insertTestRecord(t, db, "trader-B", 1, now, true, []DecisionAction{{Action: "hold"}})
	insertTestRecord(t, db, "trader-C", 1, now, true, []DecisionAction{{Action: "hold"}})

	// Only request trader-A and trader-C
	digests, err := store.GetTradersLatestDigests([]string{"trader-A", "trader-C"}, "trader_id", "asc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 2 {
		t.Fatalf("expected 2 digests, got %d", len(digests))
	}
	if digests[0].TraderID != "trader-A" {
		t.Errorf("expected first 'trader-A', got '%s'", digests[0].TraderID)
	}
	if digests[1].TraderID != "trader-C" {
		t.Errorf("expected second 'trader-C', got '%s'", digests[1].TraderID)
	}
}

// TestGetTradersLatestDigests_ReturnsLatestPerTrader verifies that only the latest record
// per trader is returned when a trader has multiple records
func TestGetTradersLatestDigests_ReturnsLatestPerTrader(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	now := time.Now().UTC()

	// Trader A has 3 records; only the latest (cycle 3) should appear
	insertTestRecord(t, db, "trader-A", 1, now.Add(-20*time.Minute), true,
		[]DecisionAction{{Action: "wait"}})
	insertTestRecord(t, db, "trader-A", 2, now.Add(-10*time.Minute), true,
		[]DecisionAction{{Action: "hold"}})
	insertTestRecord(t, db, "trader-A", 3, now, true,
		[]DecisionAction{{Action: "open_long", Symbol: "BTCUSDT"}})

	digests, err := store.GetTradersLatestDigests(nil, "timestamp", "desc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 1 {
		t.Fatalf("expected 1 digest (one per trader), got %d", len(digests))
	}
	if digests[0].CycleNumber != 3 {
		t.Errorf("expected latest cycle_number 3, got %d", digests[0].CycleNumber)
	}
	if digests[0].Decisions[0].Action != "open_long" {
		t.Errorf("expected action 'open_long' (latest), got '%s'", digests[0].Decisions[0].Action)
	}
}

// TestGetTradersLatestDigests_Empty tests behavior when no records exist
func TestGetTradersLatestDigests_Empty(t *testing.T) {
	db := setupTestDB(t)
	store := NewDecisionStore(db)

	digests, err := store.GetTradersLatestDigests(nil, "timestamp", "desc", 0)
	if err != nil {
		t.Fatalf("GetTradersLatestDigests failed: %v", err)
	}
	if len(digests) != 0 {
		t.Errorf("expected 0 digests for empty table, got %d", len(digests))
	}
}

// TestDecisionDigestJSON tests that DecisionDigest serializes correctly to JSON
func TestDecisionDigestJSON(t *testing.T) {
	digest := &DecisionDigest{
		TraderID:    "test-trader",
		CycleNumber: 5,
		Timestamp:   time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC),
		Decisions: []DecisionAction{
			{
				Action:     "open_long",
				Symbol:     "BTCUSDT",
				Confidence: 85,
				Reasoning:  "Bullish signal",
				Success:    true,
			},
		},
		Success:             true,
		AIRequestDurationMs: 3500,
	}

	data, err := json.Marshal(digest)
	if err != nil {
		t.Fatalf("failed to marshal DecisionDigest: %v", err)
	}

	// Verify key fields exist in JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	expectedFields := []string{"trader_id", "cycle_number", "timestamp", "cot_trace", "decisions", "success", "ai_request_duration_ms"}
	for _, field := range expectedFields {
		if _, exists := result[field]; !exists {
			t.Errorf("expected JSON field '%s' to exist", field)
		}
	}

	// Verify verbose fields are NOT present (cot_trace IS expected, it's the core reasoning)
	unexpectedFields := []string{"system_prompt", "input_prompt", "raw_response", "decision_json"}
	for _, field := range unexpectedFields {
		if _, exists := result[field]; exists {
			t.Errorf("unexpected verbose field '%s' should not be in DecisionDigest JSON", field)
		}
	}

	// Verify error_message is omitted when empty (omitempty tag)
	if _, exists := result["error_message"]; exists {
		t.Errorf("expected empty error_message to be omitted from JSON")
	}
}
