package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

// DecisionStore decision log storage
type DecisionStore struct {
	db *gorm.DB
}

// DecisionRecordDB internal GORM model for decision_records table
type DecisionRecordDB struct {
	ID                  int64     `gorm:"primaryKey;autoIncrement"`
	TraderID            string    `gorm:"column:trader_id;not null;index:idx_decision_records_trader_time"`
	CycleNumber         int       `gorm:"column:cycle_number;not null"`
	Timestamp           time.Time `gorm:"not null;index:idx_decision_records_trader_time,sort:desc;index:idx_decision_records_timestamp,sort:desc"`
	SystemPrompt        string    `gorm:"column:system_prompt;default:''"`
	InputPrompt         string    `gorm:"column:input_prompt;default:''"`
	CoTTrace            string    `gorm:"column:cot_trace;default:''"`
	DecisionJSON        string    `gorm:"column:decision_json;default:''"`
	RawResponse         string    `gorm:"column:raw_response;default:''"`
	CandidateCoins      string    `gorm:"column:candidate_coins;default:''"`
	ExecutionLog        string    `gorm:"column:execution_log;default:''"`
	Decisions           string    `gorm:"column:decisions;default:'[]'"`
	Success             bool      `gorm:"default:false"`
	ErrorMessage        string    `gorm:"column:error_message;default:''"`
	AIRequestDurationMs int64     `gorm:"column:ai_request_duration_ms;default:0"`
	CreatedAt           time.Time `json:"created_at"`
}

func (DecisionRecordDB) TableName() string { return "decision_records" }

// DecisionRecord decision record (external API struct)
type DecisionRecord struct {
	ID                  int64              `json:"id"`
	TraderID            string             `json:"trader_id"`
	CycleNumber         int                `json:"cycle_number"`
	Timestamp           time.Time          `json:"timestamp"`
	SystemPrompt        string             `json:"system_prompt"`
	InputPrompt         string             `json:"input_prompt"`
	CoTTrace            string             `json:"cot_trace"`
	DecisionJSON        string             `json:"decision_json"`
	RawResponse         string             `json:"raw_response"` // Raw AI response for debugging
	CandidateCoins      []string           `json:"candidate_coins"`
	ExecutionLog        []string           `json:"execution_log"`
	Success             bool               `json:"success"`
	ErrorMessage        string             `json:"error_message"`
	AIRequestDurationMs int64              `json:"ai_request_duration_ms"`
	AccountState        AccountSnapshot    `json:"account_state"`
	Positions           []PositionSnapshot `json:"positions"`
	Decisions           []DecisionAction   `json:"decisions"`
}

// AccountSnapshot account state snapshot
type AccountSnapshot struct {
	TotalBalance          float64 `json:"total_balance"`
	AvailableBalance      float64 `json:"available_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
	PositionCount         int     `json:"position_count"`
	MarginUsedPct         float64 `json:"margin_used_pct"`
	InitialBalance        float64 `json:"initial_balance"`
}

// PositionSnapshot position snapshot
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	PositionAmt      float64 `json:"position_amt"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidation_price"`
}

// DecisionAction decision action
type DecisionAction struct {
	Action     string    `json:"action"`
	Symbol     string    `json:"symbol"`
	Quantity   float64   `json:"quantity"`
	Leverage   int       `json:"leverage"`
	Price      float64   `json:"price"`
	StopLoss   float64   `json:"stop_loss,omitempty"`   // Stop loss price
	TakeProfit float64   `json:"take_profit,omitempty"` // Take profit price
	Confidence int       `json:"confidence,omitempty"`  // AI confidence (0-100)
	Reasoning  string    `json:"reasoning,omitempty"`   // Brief reasoning
	OrderID    int64     `json:"order_id"`
	Timestamp  time.Time `json:"timestamp"`
	Success    bool      `json:"success"`
	Error      string    `json:"error"`
}

// Statistics statistics information
type Statistics struct {
	TotalCycles         int `json:"total_cycles"`
	SuccessfulCycles    int `json:"successful_cycles"`
	FailedCycles        int `json:"failed_cycles"`
	TotalOpenPositions  int `json:"total_open_positions"`
	TotalClosePositions int `json:"total_close_positions"`
}

// CoTTraceEntry 单条思考过程，用于 recent_cot_traces（按时间从新到旧）
type CoTTraceEntry struct {
	CycleNumber int       `json:"cycle_number"`
	Timestamp   time.Time `json:"timestamp"`
	CoTTrace    string    `json:"cot_trace"`
}

// DecisionDigest AI decision digest (key decision information with core AI reasoning)
// Includes the AI's core thinking analysis (CoTTrace from <reasoning> tag) and final decisions,
// but excludes verbose system prompt, input prompt, and raw AI response.
// RecentCoTTraces: 该 trader 最近 3 条决策的思考过程，单条 digest 与 list 一致，上游无需改。
type DecisionDigest struct {
	TraderID            string            `json:"trader_id"`
	CycleNumber         int               `json:"cycle_number"`
	Timestamp           time.Time         `json:"timestamp"`
	CoTTrace            string            `json:"cot_trace"`           // 当前记录的思考过程
	RecentCoTTraces     []CoTTraceEntry   `json:"recent_cot_traces"`   // 最后 3 条思考过程（从新到旧）
	Decisions           []DecisionAction  `json:"decisions"`
	Success             bool              `json:"success"`
	ErrorMessage        string            `json:"error_message,omitempty"`
	AIRequestDurationMs int64             `json:"ai_request_duration_ms"`
}

// toDigest converts DB model to DecisionDigest (includes core reasoning, excludes prompts and raw response)
func (db *DecisionRecordDB) toDigest() *DecisionDigest {
	digest := &DecisionDigest{
		TraderID:            db.TraderID,
		CycleNumber:         db.CycleNumber,
		Timestamp:           db.Timestamp,
		CoTTrace:            db.CoTTrace, // AI's core thinking analysis
		Success:             db.Success,
		ErrorMessage:        db.ErrorMessage,
		AIRequestDurationMs: db.AIRequestDurationMs,
	}
	json.Unmarshal([]byte(db.Decisions), &digest.Decisions)
	return digest
}

// NewDecisionStore creates a new DecisionStore
func NewDecisionStore(db *gorm.DB) *DecisionStore {
	return &DecisionStore{db: db}
}

// initTables initializes AI decision log tables
func (s *DecisionStore) initTables() error {
	// For PostgreSQL with existing table, skip AutoMigrate
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'decision_records'`).Scan(&tableExists)
		if tableExists > 0 {
			return nil
		}
	}
	return s.db.AutoMigrate(&DecisionRecordDB{})
}

// toRecord converts DB model to API struct
func (db *DecisionRecordDB) toRecord() *DecisionRecord {
	record := &DecisionRecord{
		ID:                  db.ID,
		TraderID:            db.TraderID,
		CycleNumber:         db.CycleNumber,
		Timestamp:           db.Timestamp,
		SystemPrompt:        db.SystemPrompt,
		InputPrompt:         db.InputPrompt,
		CoTTrace:            db.CoTTrace,
		DecisionJSON:        db.DecisionJSON,
		RawResponse:         db.RawResponse,
		Success:             db.Success,
		ErrorMessage:        db.ErrorMessage,
		AIRequestDurationMs: db.AIRequestDurationMs,
	}
	json.Unmarshal([]byte(db.CandidateCoins), &record.CandidateCoins)
	json.Unmarshal([]byte(db.ExecutionLog), &record.ExecutionLog)
	json.Unmarshal([]byte(db.Decisions), &record.Decisions)
	return record
}

// LogDecision logs decision
func (s *DecisionStore) LogDecision(record *DecisionRecord) error {
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	} else {
		record.Timestamp = record.Timestamp.UTC()
	}

	// Serialize arrays to JSON
	candidateCoinsJSON, _ := json.Marshal(record.CandidateCoins)
	executionLogJSON, _ := json.Marshal(record.ExecutionLog)
	decisionsJSON, _ := json.Marshal(record.Decisions)

	dbRecord := &DecisionRecordDB{
		TraderID:            record.TraderID,
		CycleNumber:         record.CycleNumber,
		Timestamp:           record.Timestamp,
		SystemPrompt:        record.SystemPrompt,
		InputPrompt:         record.InputPrompt,
		CoTTrace:            record.CoTTrace,
		DecisionJSON:        record.DecisionJSON,
		RawResponse:         record.RawResponse,
		CandidateCoins:      string(candidateCoinsJSON),
		ExecutionLog:        string(executionLogJSON),
		Decisions:           string(decisionsJSON),
		Success:             record.Success,
		ErrorMessage:        record.ErrorMessage,
		AIRequestDurationMs: record.AIRequestDurationMs,
	}

	if err := s.db.Create(dbRecord).Error; err != nil {
		return fmt.Errorf("failed to insert decision record: %w", err)
	}
	record.ID = dbRecord.ID
	return nil
}

// GetLatestRecords gets the latest N records for specified trader (sorted by time in ascending order: old to new)
func (s *DecisionStore) GetLatestRecords(traderID string, n int) ([]*DecisionRecord, error) {
	var dbRecords []*DecisionRecordDB
	err := s.db.Where("trader_id = ?", traderID).
		Order("timestamp DESC").
		Limit(n).
		Find(&dbRecords).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query decision records: %w", err)
	}

	records := make([]*DecisionRecord, len(dbRecords))
	for i, db := range dbRecords {
		records[i] = db.toRecord()
	}

	// Reverse array to sort time from old to new
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, nil
}

// GetAllLatestRecords gets the latest N records for all traders
func (s *DecisionStore) GetAllLatestRecords(n int) ([]*DecisionRecord, error) {
	var dbRecords []*DecisionRecordDB
	err := s.db.Order("timestamp DESC").Limit(n).Find(&dbRecords).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query decision records: %w", err)
	}

	records := make([]*DecisionRecord, len(dbRecords))
	for i, db := range dbRecords {
		records[i] = db.toRecord()
	}

	// Reverse array
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, nil
}

// GetRecordsByDate gets all records for a specified trader on a specified date
func (s *DecisionStore) GetRecordsByDate(traderID string, date time.Time) ([]*DecisionRecord, error) {
	dateStr := date.Format("2006-01-02")

	var dbRecords []*DecisionRecordDB
	err := s.db.Where("trader_id = ? AND DATE(timestamp) = ?", traderID, dateStr).
		Order("timestamp ASC").
		Find(&dbRecords).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query decision records: %w", err)
	}

	records := make([]*DecisionRecord, len(dbRecords))
	for i, db := range dbRecords {
		records[i] = db.toRecord()
	}

	return records, nil
}

// CleanOldRecords cleans old records from N days ago
func (s *DecisionStore) CleanOldRecords(traderID string, days int) (int64, error) {
	cutoffTime := time.Now().AddDate(0, 0, -days)

	result := s.db.Where("trader_id = ? AND timestamp < ?", traderID, cutoffTime).
		Delete(&DecisionRecordDB{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to clean old records: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// GetStatistics gets statistics information for specified trader
func (s *DecisionStore) GetStatistics(traderID string) (*Statistics, error) {
	stats := &Statistics{}

	var totalCount, successCount int64
	s.db.Model(&DecisionRecordDB{}).Where("trader_id = ?", traderID).Count(&totalCount)
	s.db.Model(&DecisionRecordDB{}).Where("trader_id = ? AND success = ?", traderID, true).Count(&successCount)

	stats.TotalCycles = int(totalCount)
	stats.SuccessfulCycles = int(successCount)
	stats.FailedCycles = stats.TotalCycles - stats.SuccessfulCycles

	// Count from trader_positions table using raw query for cross-table
	s.db.Raw("SELECT COUNT(*) FROM trader_positions WHERE trader_id = ?", traderID).Scan(&stats.TotalOpenPositions)
	s.db.Raw("SELECT COUNT(*) FROM trader_positions WHERE trader_id = ? AND status = 'CLOSED'", traderID).Scan(&stats.TotalClosePositions)

	return stats, nil
}

// GetAllStatistics gets statistics information for all traders
func (s *DecisionStore) GetAllStatistics() (*Statistics, error) {
	stats := &Statistics{}

	var totalCount, successCount int64
	s.db.Model(&DecisionRecordDB{}).Count(&totalCount)
	s.db.Model(&DecisionRecordDB{}).Where("success = ?", true).Count(&successCount)

	stats.TotalCycles = int(totalCount)
	stats.SuccessfulCycles = int(successCount)
	stats.FailedCycles = stats.TotalCycles - stats.SuccessfulCycles

	// Count from trader_positions table
	s.db.Raw("SELECT COUNT(*) FROM trader_positions").Scan(&stats.TotalOpenPositions)
	s.db.Raw("SELECT COUNT(*) FROM trader_positions WHERE status = 'CLOSED'").Scan(&stats.TotalClosePositions)

	return stats, nil
}

// GetLastCycleNumber gets the last cycle number for specified trader
func (s *DecisionStore) GetLastCycleNumber(traderID string) (int, error) {
	var cycleNumber *int
	err := s.db.Model(&DecisionRecordDB{}).
		Where("trader_id = ?", traderID).
		Select("MAX(cycle_number)").
		Scan(&cycleNumber).Error
	if err != nil {
		return 0, err
	}
	if cycleNumber == nil {
		return 0, nil
	}
	return *cycleNumber, nil
}

// getLatestCoTTraces 获取指定 trader 最近 n 条决策的思考过程（按时间从新到旧）
func (s *DecisionStore) getLatestCoTTraces(traderID string, n int) ([]CoTTraceEntry, error) {
	if n <= 0 {
		return nil, nil
	}
	var dbRecords []DecisionRecordDB
	err := s.db.Where("trader_id = ?", traderID).
		Order("timestamp DESC").
		Limit(n).
		Select("cycle_number", "timestamp", "cot_trace").
		Find(&dbRecords).Error
	if err != nil {
		return nil, err
	}
	out := make([]CoTTraceEntry, 0, len(dbRecords))
	for _, r := range dbRecords {
		out = append(out, CoTTraceEntry{CycleNumber: r.CycleNumber, Timestamp: r.Timestamp, CoTTrace: r.CoTTrace})
	}
	return out, nil
}

// GetLatestDigest gets the latest decision digest for a specific trader (lightweight version).
// 附带该 trader 最近 3 条思考过程（RecentCoTTraces），与 list 一致。
func (s *DecisionStore) GetLatestDigest(traderID string) (*DecisionDigest, error) {
	var dbRecord DecisionRecordDB
	err := s.db.Where("trader_id = ?", traderID).
		Order("timestamp DESC").
		First(&dbRecord).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query latest decision digest for trader %s: %w", traderID, err)
	}
	digest := dbRecord.toDigest()
	digest.RecentCoTTraces, _ = s.getLatestCoTTraces(traderID, 3)
	return digest, nil
}

// GetTradersLatestDigests gets the latest decision digest for each specified trader, with sorting
// traderIDs: list of trader IDs to query; if empty, queries all traders
// sortBy: sorting field - "timestamp" (default), "trader_id", "success"
// order: sorting direction - "desc" (default), "asc"
// limit: max number of results; 0 means no limit
func (s *DecisionStore) GetTradersLatestDigests(traderIDs []string, sortBy, order string, limit int) ([]*DecisionDigest, error) {
	// Step 1: Determine which trader IDs to query
	queryIDs := traderIDs
	if len(queryIDs) == 0 {
		// Get all distinct trader IDs from the decision_records table
		if err := s.db.Model(&DecisionRecordDB{}).Distinct("trader_id").Pluck("trader_id", &queryIDs).Error; err != nil {
			return nil, fmt.Errorf("failed to get distinct trader IDs: %w", err)
		}
	}

	// Step 2: For each trader, get the latest record and convert to digest，并附带最近 3 条思考过程（与单条 digest 一致，不新增参数）
	digests := make([]*DecisionDigest, 0, len(queryIDs))
	for _, tid := range queryIDs {
		var dbRecord DecisionRecordDB
		err := s.db.Where("trader_id = ?", tid).
			Order("timestamp DESC").
			First(&dbRecord).Error
		if err != nil {
			continue // Skip traders with no records
		}
		d := dbRecord.toDigest()
		d.RecentCoTTraces, _ = s.getLatestCoTTraces(tid, 3)
		digests = append(digests, d)
	}

	// Step 3: Sort results by the specified field and order
	isAsc := order == "asc"
	sort.Slice(digests, func(i, j int) bool {
		switch sortBy {
		case "trader_id":
			if isAsc {
				return digests[i].TraderID < digests[j].TraderID
			}
			return digests[i].TraderID > digests[j].TraderID
		case "success":
			if isAsc {
				// false (0) before true (1) in ascending
				return !digests[i].Success && digests[j].Success
			}
			// true (1) before false (0) in descending
			return digests[i].Success && !digests[j].Success
		default: // "timestamp"
			if isAsc {
				return digests[i].Timestamp.Before(digests[j].Timestamp)
			}
			return digests[i].Timestamp.After(digests[j].Timestamp)
		}
	})

	// Step 4: Apply limit
	if limit > 0 && len(digests) > limit {
		digests = digests[:limit]
	}

	return digests, nil
}
