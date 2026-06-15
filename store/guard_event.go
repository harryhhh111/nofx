package store

import (
	"bytes"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// GuardEventType enumerates the categories of rules whose outcomes we
// persist. Keep this list in sync with kernel/guard_logger.go and any
// new guard that wants to write a telemetry event.
const (
	GuardEventTypeEntryRiskGuard = "entry_risk_guard"
	GuardEventTypeRRCheck        = "rr_check"
	GuardEventTypeTPAnchor       = "tp_anchor"
	GuardEventTypeCooldown       = "cooldown"
	GuardEventTypeLifecycleExit  = "lifecycle_exit"
	GuardEventTypeHardSafety     = "hard_safety"
	GuardEventTypeCandidatePool  = "candidate_pool"
)

// GuardEventAction enumerates the outcome of a guard check. The pair
// (GuardType, Action) is what the dashboard / stats endpoints group by.
const (
	GuardEventActionAllow  = "allow"
	GuardEventActionReduce = "reduce"
	GuardEventActionBlock  = "block"
)

// GuardEvent records every rule-check outcome for governance and post-mortem.
//
// Events are written by the kernel after validateDecisions runs and by
// lifecycle hooks (cooldown, trailing stop, drawdown close) on the
// trader. Writes are batched and never block the main decision path:
// BulkInsert is called from saveDecision after the decision record has
// been persisted, and the engine treats any insert failure as a warning.
type GuardEvent struct {
	ID                 int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID           string          `gorm:"column:trader_id;not null;index:idx_guard_events_trader_time" json:"trader_id"`
	DecisionRecordID   *int64          `gorm:"column:decision_record_id" json:"decision_record_id"`
	CycleNumber        int             `gorm:"column:cycle_number;not null" json:"cycle_number"`
	StrategyID         string          `gorm:"column:strategy_id" json:"strategy_id"`
	GuardType          string          `gorm:"column:guard_type;not null;index:idx_guard_events_type_time" json:"guard_type"`
	Action             string          `gorm:"column:action;not null" json:"action"`
	Reason             string          `gorm:"column:reason;not null" json:"reason"`
	Symbol             string          `gorm:"column:symbol" json:"symbol"`
	Side               string          `gorm:"column:side" json:"side"`
	EntryPrice         float64         `gorm:"column:entry_price" json:"entry_price"`
	StopLoss           float64         `gorm:"column:stop_loss" json:"stop_loss"`
	TakeProfit         float64         `gorm:"column:take_profit" json:"take_profit"`
	PositionSizeBefore float64         `gorm:"column:position_size_before" json:"position_size_before"`
	PositionSizeAfter  float64         `gorm:"column:position_size_after" json:"position_size_after"`
	ConfigSnapshot     json.RawMessage `gorm:"column:config_snapshot;type:jsonb" json:"config_snapshot"`
	AIAssessment       json.RawMessage `gorm:"column:ai_assessment;type:jsonb" json:"ai_assessment,omitempty"`
	TriggeredAt        time.Time       `gorm:"column:triggered_at;not null;index:idx_guard_events_trader_time,sort:desc;index:idx_guard_events_type_time,sort:desc" json:"triggered_at"`
}

func (GuardEvent) TableName() string { return "guard_events" }

// GuardEventStore handles persistence and queries for guard_events rows.
type GuardEventStore struct {
	db *gorm.DB
}

// NewGuardEventStore creates a new GuardEventStore.
func NewGuardEventStore(db *gorm.DB) *GuardEventStore {
	return &GuardEventStore{db: db}
}

// initTables migrates the guard_events table. For PostgreSQL with an
// existing deployment, also add the AIAssessment JSONB column and the
// composite index lazily so older tables keep working.
func (s *GuardEventStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'guard_events'`).Scan(&tableExists)
		if tableExists > 0 {
			s.db.Exec(`ALTER TABLE guard_events ADD COLUMN IF NOT EXISTS ai_assessment JSONB`)
			s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_guard_events_type_time ON guard_events (guard_type, action, triggered_at DESC)`)
			// Run AutoMigrate as well so any future struct columns are
			// also added to existing tables.
			return s.db.AutoMigrate(&GuardEvent{})
		}
	}
	if err := s.db.AutoMigrate(&GuardEvent{}); err != nil {
		return err
	}
	// AutoMigrate creates the JSON column as TEXT in SQLite, which is
	// sufficient for our access pattern (read as json.RawMessage).
	return nil
}

// BulkInsert writes a batch of guard events. Called after the decision
// record has been persisted so DecisionRecordID can be back-filled.
// The caller is expected to populate DecisionRecordID and TriggeredAt.
func (s *GuardEventStore) BulkInsert(events []*GuardEvent) error {
	if len(events) == 0 {
		return nil
	}
	return s.db.CreateInBatches(events, 100).Error
}

// GetRecent returns the most recent guard events for a trader, newest
// first. limit is clamped to [1, 1000] by the caller.
func (s *GuardEventStore) GetRecent(traderID string, limit int) ([]*GuardEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var out []*GuardEvent
	err := s.db.Where("trader_id = ?", traderID).
		Order("triggered_at DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// CountByTypeAction returns the per-(guard_type, action) hit count for a
// trader. Useful for the dashboard summary card.
type GuardTypeActionCount struct {
	GuardType string `gorm:"column:guard_type" json:"guard_type"`
	Action    string `gorm:"column:action" json:"action"`
	Count     int    `gorm:"column:count" json:"count"`
}

func (s *GuardEventStore) CountByTypeAction(traderID string, since time.Time) ([]GuardTypeActionCount, error) {
	var rows []GuardTypeActionCount
	err := s.db.Model(&GuardEvent{}).
		Select("guard_type, action, COUNT(*) AS count").
		Where("trader_id = ? AND triggered_at >= ?", traderID, since).
		Group("guard_type, action").
		Order("guard_type, action").
		Scan(&rows).Error
	return rows, err
}

// GuardHourlyBucket is one time-bucketed row of guard-event activity
// for the dashboard area chart.
type GuardHourlyBucket struct {
	HourStart time.Time `gorm:"column:hour_start" json:"hour_start"`
	GuardType string    `gorm:"column:guard_type" json:"guard_type"`
	Action    string    `gorm:"column:action" json:"action"`
	Count     int       `gorm:"column:count" json:"count"`
}

// HourlySeries returns per-hour guard-event counts over the [since,
// until] window, broken down by (guard_type, action). Hours with zero
// events are NOT emitted (caller must fill in the gap). SQLite/Postgres
// both support strftime / date_trunc respectively; we go through raw
// SQL because GORM's expression builder makes the per-dialect diff
// noisier than two short statements. The first column comes back as a
// time on PG and a string on SQLite, so we scan into a struct with
// the right per-dialect type and convert.
func (s *GuardEventStore) HourlySeries(traderID string, since, until time.Time) ([]GuardHourlyBucket, error) {
	if s.db.Dialector.Name() == "postgres" {
		var rows []GuardHourlyBucket
		err := s.db.Raw(`
			SELECT
				date_trunc('hour', triggered_at) AS hour_start,
				guard_type, action, COUNT(*) AS count
			FROM guard_events
			WHERE trader_id = ? AND triggered_at >= ? AND triggered_at < ?
			GROUP BY hour_start, guard_type, action
			ORDER BY hour_start, guard_type, action
		`, traderID, since, until).Scan(&rows).Error
		return rows, err
	}
	// SQLite path: hour_start is a TEXT column. Scan into a string and
	// parse so the API stays a single time.Time type.
	var rawRows []struct {
		HourStart string `gorm:"column:hour_start"`
		GuardType string `gorm:"column:guard_type"`
		Action    string `gorm:"column:action"`
		Count     int    `gorm:"column:count"`
	}
	if err := s.db.Raw(`
		SELECT
			strftime('%Y-%m-%d %H:00:00', triggered_at) AS hour_start,
			guard_type, action, COUNT(*) AS count
		FROM guard_events
		WHERE trader_id = ? AND triggered_at >= ? AND triggered_at < ?
		GROUP BY hour_start, guard_type, action
		ORDER BY hour_start, guard_type, action
	`, traderID, since, until).Scan(&rawRows).Error; err != nil {
		return nil, err
	}
	out := make([]GuardHourlyBucket, 0, len(rawRows))
	for _, r := range rawRows {
		// Format produced by strftime above: "2006-01-02 15:00:00".
		ts, perr := time.ParseInLocation("2006-01-02 15:04:05", r.HourStart, time.UTC)
		if perr != nil {
			// Defensive: skip rows we can't parse rather than fail the
			// whole query. Should never happen with strftime output.
			continue
		}
		out = append(out, GuardHourlyBucket{
			HourStart: ts,
			GuardType: r.GuardType,
			Action:    r.Action,
			Count:     r.Count,
		})
	}
	return out, nil
}

// GuardSymbolCount is the result row for TopSymbols.
type GuardSymbolCount struct {
	Symbol string `gorm:"column:symbol" json:"symbol"`
	Count  int    `gorm:"column:count" json:"count"`
}

// TopSymbols returns the symbols most frequently hit by a given
// (guard_type, action) pair, used by the dashboard "recently blocked
// symbols" table.
func (s *GuardEventStore) TopSymbols(traderID string, guardType, action string, since time.Time, limit int) ([]GuardSymbolCount, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	var rows []GuardSymbolCount
	err := s.db.Model(&GuardEvent{}).
		Select("symbol, COUNT(*) AS count").
		Where("trader_id = ? AND guard_type = ? AND action = ? AND triggered_at >= ? AND symbol <> ''", traderID, guardType, action, since).
		Group("symbol").
		Order("count DESC, symbol ASC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// AIAgreementStats summarises how often the AI's self-check matches
// the code's actual outcome.
//
// For each guard event with a non-empty ai_assessment JSON, we parse
// the ai_self_check.hard_block_expected boolean and compare it to
// the event's actual action (block == hard_block_expected=true).
// Events without an AI assessment are EXCLUDED from the denominator
// (we don't know what the AI said, so they can't count as either
// agreement or disagreement).
type AIAgreementStats struct {
	Total     int     `json:"total"`      // events with non-empty AI assessment
	Agreed    int     `json:"agreed"`     // AI predicted correctly
	Rate      float64 `json:"rate"`       // Agreed / Total, 0..1
	AIBlocked int     `json:"ai_blocked"` // AI said hard_block_expected=true
	CodeBlock int     `json:"code_blocked"`
	BothBlock int     `json:"both_block"` // AI said true AND code blocked
	AIOnly    int     `json:"ai_only"`    // AI said true but code did not block
	CodeOnly  int     `json:"code_only"`  // code blocked but AI did not predict
}

// AIAgreement scans ai_assessment JSON for each event in the window
// and computes the agreement metrics defined above. Errors parsing
// individual rows are logged and counted as "unknown" (excluded from
// the denominator) so a single malformed JSON does not poison the
// whole window.
func (s *GuardEventStore) AIAgreement(traderID string, since time.Time) (AIAgreementStats, error) {
	var stats AIAgreementStats
	// Pull all events with non-empty ai_assessment in the window. The
	// result set is bounded by limit guard events: in practice the
	// dashboard caller will pin `since` to the last 24h, capping the
	// window at a few thousand rows even for active traders.
	rows, err := s.db.Model(&GuardEvent{}).
		Select("action, ai_assessment").
		Where("trader_id = ? AND triggered_at >= ? AND ai_assessment IS NOT NULL AND ai_assessment <> ''", traderID, since).
		Rows()
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			action  string
			rawJSON []byte
		)
		if err := rows.Scan(&action, &rawJSON); err != nil {
			continue
		}
		expected, ok := extractAIHardBlockExpected(rawJSON)
		if !ok {
			// Malformed / missing ai_self_check.hard_block_expected
			// → excluded from the denominator.
			continue
		}
		stats.Total++
		actualBlock := action == GuardEventActionBlock
		if actualBlock {
			stats.CodeBlock++
		}
		if expected {
			stats.AIBlocked++
		}
		if expected == actualBlock {
			stats.Agreed++
		}
		switch {
		case expected && actualBlock:
			stats.BothBlock++
		case expected && !actualBlock:
			stats.AIOnly++
		case !expected && actualBlock:
			stats.CodeOnly++
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	if stats.Total > 0 {
		stats.Rate = float64(stats.Agreed) / float64(stats.Total)
	}
	return stats, nil
}

// extractAIHardBlockExpected parses an ai_assessment JSON blob and
// returns (ai_self_check.hard_block_expected, found). The function is
// intentionally narrow: anything we don't recognise (missing field,
// wrong type, unparseable JSON) is treated as "not found" so the
// caller can decide whether to count it.
func extractAIHardBlockExpected(raw []byte) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var outer struct {
		AISelfCheck struct {
			HardBlockExpected bool `json:"hard_block_expected"`
		} `json:"ai_self_check"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil {
		return false, false
	}
	// The zero value of bool is false, so we can't tell "explicitly
	// false" from "field missing" by looking at the struct alone.
	// Instead, peek at the raw JSON to detect a real key.
	needle := []byte(`"hard_block_expected"`)
	if !bytes.Contains(raw, needle) {
		return false, false
	}
	return outer.AISelfCheck.HardBlockExpected, true
}
