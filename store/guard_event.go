package store

import (
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

// initTables migrates the guard_events table.
func (s *GuardEventStore) initTables() error {
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
