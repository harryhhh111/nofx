package store

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrPendingCloseReasonNoOpenPosition is returned when SetPendingCloseReason matches no OPEN row.
var ErrPendingCloseReasonNoOpenPosition = errors.New("set pending close reason: no matching OPEN position")

// adaptivePriceRound rounds a price based on its magnitude to preserve meaningful precision.
// For small prices (like meme coins), it preserves more decimal places.
// It detects the number of decimal places needed from the reference price(s).
func adaptivePriceRound(price float64, referencePrices ...float64) float64 {
	if price == 0 {
		return 0
	}

	// Find the minimum magnitude among all prices (including the price itself)
	minMagnitude := math.Abs(price)
	for _, ref := range referencePrices {
		if ref > 0 && ref < minMagnitude {
			minMagnitude = ref
		}
	}

	// Determine decimal places needed based on price magnitude
	// For price 0.000000541, we need ~15 decimal places
	// For price 0.0001, we need ~8 decimal places
	// For price 1.0, we need ~4 decimal places
	var multiplier float64
	switch {
	case minMagnitude < 0.000001: // Ultra small (meme coins like CHEEMS, SHIB)
		multiplier = 1e15 // 15 decimal places
	case minMagnitude < 0.0001: // Very small (PEPE, FLOKI)
		multiplier = 1e12 // 12 decimal places
	case minMagnitude < 0.01: // Small
		multiplier = 1e10 // 10 decimal places
	case minMagnitude < 1: // Medium
		multiplier = 1e8 // 8 decimal places
	default: // Large
		multiplier = 1e6 // 6 decimal places
	}

	return math.Round(price*multiplier) / multiplier
}

// getPriceDecimalPlaces returns the number of decimal places in a price string
func getPriceDecimalPlaces(price float64) int {
	if price == 0 {
		return 0
	}
	s := strconv.FormatFloat(price, 'f', -1, 64)
	idx := strings.Index(s, ".")
	if idx == -1 {
		return 0
	}
	return len(s) - idx - 1
}

// formatDuration formats a duration
func formatDuration(d time.Duration) string {
	return formatDurationMs(d.Milliseconds())
}

// formatDurationMs formats a duration in milliseconds
func formatDurationMs(ms int64) string {
	seconds := ms / 1000
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24

	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	if hours < 24 {
		remainingMins := minutes % 60
		if remainingMins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, remainingMins)
	}
	remainingHours := hours % 24
	if remainingHours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, remainingHours)
}

// TraderPosition position record
// All time fields use int64 millisecond timestamps (UTC) to avoid timezone issues
type TraderPosition struct {
	ID                 int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID           string  `gorm:"column:trader_id;not null;index:idx_positions_trader" json:"trader_id"`
	ExchangeID         string  `gorm:"column:exchange_id;not null;default:'';index:idx_positions_exchange" json:"exchange_id"`
	ExchangeType       string  `gorm:"column:exchange_type;not null;default:''" json:"exchange_type"`
	ExchangePositionID string  `gorm:"column:exchange_position_id;not null;default:''" json:"exchange_position_id"`
	Symbol             string  `gorm:"column:symbol;not null" json:"symbol"`
	Side               string  `gorm:"column:side;not null" json:"side"`
	EntryQuantity      float64 `gorm:"column:entry_quantity;default:0" json:"entry_quantity"`
	Quantity           float64 `gorm:"column:quantity;not null" json:"quantity"`
	EntryPrice         float64 `gorm:"column:entry_price;not null" json:"entry_price"`
	EntryOrderID       string  `gorm:"column:entry_order_id;default:''" json:"entry_order_id"`
	EntryTime          int64   `gorm:"column:entry_time;not null;index:idx_positions_entry" json:"entry_time"` // Unix milliseconds UTC
	ExitPrice          float64 `gorm:"column:exit_price;default:0" json:"exit_price"`
	ExitOrderID        string  `gorm:"column:exit_order_id;default:''" json:"exit_order_id"`
	ExitTime           int64   `gorm:"column:exit_time;index:idx_positions_exit" json:"exit_time"` // Unix milliseconds UTC, 0 means not set
	RealizedPnL        float64 `gorm:"column:realized_pnl;default:0" json:"realized_pnl"`
	Fee                float64 `gorm:"column:fee;default:0" json:"fee"`
	Leverage           int     `gorm:"column:leverage;default:1" json:"leverage"`
	Status             string  `gorm:"column:status;default:OPEN;index:idx_positions_status" json:"status"`
	CloseReason        string  `gorm:"column:close_reason;default:''" json:"close_reason"`
	// PendingCloseReason tags the next full close (ai / manual / risk) before exchange sync applies.
	PendingCloseReason string `gorm:"column:pending_close_reason;default:''" json:"pending_close_reason,omitempty"`
	// PendingCloseOrderID is the exchange order id for that close (same id across partial fills).
	PendingCloseOrderID    string  `gorm:"column:pending_close_order_id;default:''" json:"pending_close_order_id,omitempty"`
	Source                 string  `gorm:"column:source;default:system" json:"source"`
	OpeningCycle           int     `gorm:"column:opening_cycle;default:0" json:"opening_cycle"`
	OpeningReasoning       string  `gorm:"column:opening_reasoning;default:''" json:"opening_reasoning"`
	OpeningDecisionID      int64   `gorm:"column:opening_decision_id;default:0;index" json:"opening_decision_id,omitempty"`
	OpeningSignalID        string  `gorm:"column:opening_signal_id;default:'';index" json:"opening_signal_id,omitempty"`
	OpeningRuleID          string  `gorm:"column:opening_rule_id;default:'';index" json:"opening_rule_id,omitempty"`
	OpeningSetup           string  `gorm:"column:opening_setup;default:'';index" json:"opening_setup,omitempty"`
	StrategyID             string  `gorm:"column:strategy_id;default:'';index" json:"strategy_id,omitempty"`
	StrategyVersion        string  `gorm:"column:strategy_version;default:'';index" json:"strategy_version,omitempty"`
	StopLossSource         string  `gorm:"column:stop_loss_source;default:''" json:"stop_loss_source,omitempty"`
	StopLossTimeframe      string  `gorm:"column:stop_loss_timeframe;default:''" json:"stop_loss_timeframe,omitempty"`
	StopLossAnchor         float64 `gorm:"column:stop_loss_anchor;default:0" json:"stop_loss_anchor,omitempty"`
	StopLossPolicy         string  `gorm:"column:stop_loss_policy;default:''" json:"stop_loss_policy,omitempty"`
	TakeProfitSource       string  `gorm:"column:take_profit_source;default:''" json:"take_profit_source,omitempty"`
	TakeProfitTimeframe    string  `gorm:"column:take_profit_timeframe;default:''" json:"take_profit_timeframe,omitempty"`
	TakeProfitAnchor       float64 `gorm:"column:take_profit_anchor;default:0" json:"take_profit_anchor,omitempty"`
	TakeProfitPolicy       string  `gorm:"column:take_profit_policy;default:''" json:"take_profit_policy,omitempty"`
	TakeProfitCandidates   int     `gorm:"column:take_profit_candidate_count;default:0" json:"take_profit_candidate_count,omitempty"`
	TakeProfitMinRR        float64 `gorm:"column:take_profit_min_risk_reward;default:0" json:"take_profit_min_risk_reward,omitempty"`
	TakeProfitMinATRs      float64 `gorm:"column:take_profit_min_atr_distance;default:0" json:"take_profit_min_atr_distance,omitempty"`
	TakeProfitSelectedRR   float64 `gorm:"column:take_profit_selected_risk_reward;default:0" json:"take_profit_selected_risk_reward,omitempty"`
	TakeProfitSelectedATRs float64 `gorm:"column:take_profit_selected_atr_distance;default:0" json:"take_profit_selected_atr_distance,omitempty"`
	TakeProfitQualified    bool    `gorm:"column:take_profit_qualified;default:false" json:"take_profit_qualified,omitempty"`
	NearestTakeProfit      float64 `gorm:"column:nearest_take_profit;default:0" json:"nearest_take_profit,omitempty"`
	NearestTakeProfitRR    float64 `gorm:"column:nearest_take_profit_risk_reward;default:0" json:"nearest_take_profit_risk_reward,omitempty"`
	NearestTakeProfitATRs  float64 `gorm:"column:nearest_take_profit_atr_distance;default:0" json:"nearest_take_profit_atr_distance,omitempty"`
	ProtectiveATR          float64 `gorm:"column:protective_atr;default:0" json:"protective_atr,omitempty"`
	ProtectiveATRTimeframe string  `gorm:"column:protective_atr_timeframe;default:''" json:"protective_atr_timeframe,omitempty"`
	ProtectiveATRBuffer    float64 `gorm:"column:protective_atr_buffer;default:0" json:"protective_atr_buffer,omitempty"`
	ProtectiveRiskReward   float64 `gorm:"column:protective_risk_reward;default:0" json:"protective_risk_reward,omitempty"`
	ExecutionRiskReward    float64 `gorm:"column:execution_risk_reward;default:0" json:"execution_risk_reward,omitempty"`
	MaxFavorablePnL        float64 `gorm:"column:max_favorable_pnl;default:0" json:"max_favorable_pnl,omitempty"`
	MaxFavorablePnLPct     float64 `gorm:"column:max_favorable_pnl_pct;default:0" json:"max_favorable_pnl_pct,omitempty"`
	MaxFavorablePrice      float64 `gorm:"column:max_favorable_price;default:0" json:"max_favorable_price,omitempty"`
	MaxFavorableAt         int64   `gorm:"column:max_favorable_at;default:0" json:"max_favorable_at,omitempty"`
	MaxAdversePnL          float64 `gorm:"column:max_adverse_pnl;default:0" json:"max_adverse_pnl,omitempty"`
	MaxAdversePnLPct       float64 `gorm:"column:max_adverse_pnl_pct;default:0" json:"max_adverse_pnl_pct,omitempty"`
	MaxAdversePrice        float64 `gorm:"column:max_adverse_price;default:0" json:"max_adverse_price,omitempty"`
	MaxAdverseAt           int64   `gorm:"column:max_adverse_at;default:0" json:"max_adverse_at,omitempty"`
	LastReviewSummary      string  `gorm:"column:last_review_summary;default:''" json:"last_review_summary"`
	LastReviewCycle        int     `gorm:"column:last_review_cycle;default:0" json:"last_review_cycle"`
	CreatedAt              int64   `gorm:"column:created_at" json:"created_at"` // Unix milliseconds UTC
	UpdatedAt              int64   `gorm:"column:updated_at" json:"updated_at"` // Unix milliseconds UTC
}

// TableName returns the table name
func (TraderPosition) TableName() string {
	return "trader_positions"
}

// PositionStore position storage
type PositionStore struct {
	db *gorm.DB
}

const pendingCloseReasonTTL = 30 * time.Minute

// NewPositionStore creates position storage instance
func NewPositionStore(db *gorm.DB) *PositionStore {
	return &PositionStore{db: db}
}

// isPostgres checks if the database is PostgreSQL
func (s *PositionStore) isPostgres() bool {
	return s.db.Dialector.Name() == "postgres"
}

// InitTables initializes position tables
func (s *PositionStore) InitTables() error {
	// For PostgreSQL with existing table, skip AutoMigrate
	if s.isPostgres() {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'trader_positions'`).Scan(&tableExists)
		if tableExists > 0 {
			// Migrate timestamp columns to bigint (Unix milliseconds UTC)
			// Check if column is still timestamp type before migrating
			timestampColumns := []string{"entry_time", "exit_time", "created_at", "updated_at"}
			for _, col := range timestampColumns {
				var dataType string
				s.db.Raw(`SELECT data_type FROM information_schema.columns WHERE table_name = 'trader_positions' AND column_name = ?`, col).Scan(&dataType)
				if dataType == "timestamp with time zone" || dataType == "timestamp without time zone" {
					// Convert timestamp to Unix milliseconds (bigint)
					s.db.Exec(fmt.Sprintf(`ALTER TABLE trader_positions ALTER COLUMN %s TYPE BIGINT USING EXTRACT(EPOCH FROM %s) * 1000`, col, col))
				}
			}

			// Just ensure index exists
			s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_exchange_pos_unique ON trader_positions(exchange_id, exchange_position_id) WHERE exchange_position_id != ''`)
			// Position memory columns
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS opening_cycle INTEGER DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS opening_reasoning TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS last_review_summary TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS last_review_cycle INTEGER DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS pending_close_reason TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS pending_close_order_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS opening_decision_id BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS opening_signal_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS opening_rule_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS opening_setup TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS strategy_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS strategy_version TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS stop_loss_source TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS stop_loss_timeframe TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS stop_loss_anchor DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS stop_loss_policy TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_source TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_timeframe TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_anchor DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_policy TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_candidate_count INTEGER DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_min_risk_reward DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_min_atr_distance DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_selected_risk_reward DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_selected_atr_distance DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS take_profit_qualified BOOLEAN DEFAULT FALSE`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS nearest_take_profit DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS nearest_take_profit_risk_reward DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS nearest_take_profit_atr_distance DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS protective_atr DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS protective_atr_timeframe TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS protective_atr_buffer DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS protective_risk_reward DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS execution_risk_reward DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_favorable_pnl DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_favorable_pnl_pct DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_favorable_price DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_favorable_at BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_adverse_pnl DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_adverse_pnl_pct DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_adverse_price DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trader_positions ADD COLUMN IF NOT EXISTS max_adverse_at BIGINT DEFAULT 0`)
			s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_positions_opening_signal_id ON trader_positions(opening_signal_id)`)
			s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_positions_strategy_id ON trader_positions(strategy_id)`)
			return nil
		}
	}

	if err := s.db.AutoMigrate(&TraderPosition{}); err != nil {
		return fmt.Errorf("failed to migrate trader_positions table: %w", err)
	}

	// Create unique partial index for exchange position deduplication
	var indexSQL string
	if s.isPostgres() {
		indexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_exchange_pos_unique ON trader_positions(exchange_id, exchange_position_id) WHERE exchange_position_id != ''`
	} else {
		indexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_exchange_pos_unique ON trader_positions(exchange_id, exchange_position_id) WHERE exchange_position_id != ''`
	}
	if err := s.db.Exec(indexSQL).Error; err != nil {
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("failed to create unique index: %w", err)
		}
	}

	return nil
}

// Create creates position record
func (s *PositionStore) Create(pos *TraderPosition) error {
	pos.Status = "OPEN"
	if pos.EntryQuantity == 0 {
		pos.EntryQuantity = pos.Quantity
	}
	return s.db.Create(pos).Error
}

// UpdatePositionOpeningReasoning writes the AI's position-specific reasoning when opening a trade.
func (s *PositionStore) UpdatePositionOpeningReasoning(traderID, symbol, side string, entryNotBefore int64, reasoning string) error {
	query := s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN")
	if entryNotBefore > 0 {
		query = query.Where("entry_time >= ?", entryNotBefore)
	}
	result := query.Update("opening_reasoning", reasoning)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no matching OPEN position found for %s %s %s", traderID, symbol, side)
	}
	return nil
}

type PositionOpeningSignalMetadata struct {
	DecisionID      int64
	SignalID        string
	RuleID          string
	Setup           string
	StrategyID      string
	StrategyVersion string
	EntryNotBefore  int64
}

type PositionProtectiveLevelMetadata struct {
	StopLossSource         string
	StopLossTimeframe      string
	StopLossAnchor         float64
	StopLossPolicy         string
	TakeProfitSource       string
	TakeProfitTimeframe    string
	TakeProfitAnchor       float64
	TakeProfitPolicy       string
	TakeProfitCandidates   int
	TakeProfitMinRR        float64
	TakeProfitMinATRs      float64
	TakeProfitSelectedRR   float64
	TakeProfitSelectedATRs float64
	TakeProfitQualified    bool
	NearestTakeProfit      float64
	NearestTakeProfitRR    float64
	NearestTakeProfitATRs  float64
	ProtectiveATR          float64
	ProtectiveATRTimeframe string
	ProtectiveATRBuffer    float64
	ProtectiveRiskReward   float64
	ExecutionRiskReward    float64
	EntryNotBefore         int64
}

// UpdatePositionOpeningSignalMetadata links an OPEN position to the deterministic
// candidate signal that created it. This is the stable key used by calibration.
func (s *PositionStore) UpdatePositionOpeningSignalMetadata(traderID, symbol, side string, meta PositionOpeningSignalMetadata) error {
	updates := map[string]interface{}{
		"opening_decision_id": meta.DecisionID,
		"opening_signal_id":   meta.SignalID,
		"opening_rule_id":     meta.RuleID,
		"opening_setup":       meta.Setup,
		"strategy_id":         meta.StrategyID,
		"strategy_version":    meta.StrategyVersion,
		"updated_at":          time.Now().UnixMilli(),
	}
	query := s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN")
	if meta.EntryNotBefore > 0 {
		query = query.Where("entry_time >= ?", meta.EntryNotBefore)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no matching OPEN position found for %s %s %s", traderID, symbol, side)
	}
	return nil
}

// UpdatePositionProtectiveLevelMetadata stores how the initial SL/TP levels were derived.
func (s *PositionStore) UpdatePositionProtectiveLevelMetadata(traderID, symbol, side string, meta PositionProtectiveLevelMetadata) error {
	updates := map[string]interface{}{
		"stop_loss_source":                  meta.StopLossSource,
		"stop_loss_timeframe":               meta.StopLossTimeframe,
		"stop_loss_anchor":                  meta.StopLossAnchor,
		"stop_loss_policy":                  meta.StopLossPolicy,
		"take_profit_source":                meta.TakeProfitSource,
		"take_profit_timeframe":             meta.TakeProfitTimeframe,
		"take_profit_anchor":                meta.TakeProfitAnchor,
		"take_profit_policy":                meta.TakeProfitPolicy,
		"take_profit_candidate_count":       meta.TakeProfitCandidates,
		"take_profit_min_risk_reward":       meta.TakeProfitMinRR,
		"take_profit_min_atr_distance":      meta.TakeProfitMinATRs,
		"take_profit_selected_risk_reward":  meta.TakeProfitSelectedRR,
		"take_profit_selected_atr_distance": meta.TakeProfitSelectedATRs,
		"take_profit_qualified":             meta.TakeProfitQualified,
		"nearest_take_profit":               meta.NearestTakeProfit,
		"nearest_take_profit_risk_reward":   meta.NearestTakeProfitRR,
		"nearest_take_profit_atr_distance":  meta.NearestTakeProfitATRs,
		"protective_atr":                    meta.ProtectiveATR,
		"protective_atr_timeframe":          meta.ProtectiveATRTimeframe,
		"protective_atr_buffer":             meta.ProtectiveATRBuffer,
		"protective_risk_reward":            meta.ProtectiveRiskReward,
		"execution_risk_reward":             meta.ExecutionRiskReward,
		"updated_at":                        time.Now().UnixMilli(),
	}
	query := s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN")
	if meta.EntryNotBefore > 0 {
		query = query.Where("entry_time >= ?", meta.EntryNotBefore)
	}
	result := query.Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("no matching OPEN position found for %s %s %s", traderID, symbol, side)
	}
	return nil
}

// UpdatePositionExcursion records the best and worst unrealized performance observed while a position is open.
func (s *PositionStore) UpdatePositionExcursion(traderID, symbol, side string, markPrice, unrealizedPnL, unrealizedPnLPct float64, observedAtMs int64) error {
	if observedAtMs <= 0 {
		observedAtMs = time.Now().UTC().UnixMilli()
	}

	var pos TraderPosition
	err := s.db.Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN").
		Order("entry_time DESC").
		First(&pos).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	updates := map[string]interface{}{}
	if pos.MaxFavorableAt == 0 || unrealizedPnL > pos.MaxFavorablePnL {
		updates["max_favorable_pnl"] = unrealizedPnL
		updates["max_favorable_pnl_pct"] = unrealizedPnLPct
		updates["max_favorable_price"] = markPrice
		updates["max_favorable_at"] = observedAtMs
	}
	if pos.MaxAdverseAt == 0 || unrealizedPnL < pos.MaxAdversePnL {
		updates["max_adverse_pnl"] = unrealizedPnL
		updates["max_adverse_pnl_pct"] = unrealizedPnLPct
		updates["max_adverse_price"] = markPrice
		updates["max_adverse_at"] = observedAtMs
	}
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = time.Now().UTC().UnixMilli()

	return s.db.Model(&TraderPosition{}).Where("id = ?", pos.ID).Updates(updates).Error
}

// UpdatePositionReviewSummary writes the AI's latest hold-decision snapshot to the position.
func (s *PositionStore) UpdatePositionReviewSummary(traderID, symbol, side string, cycle int, summary string) error {
	return s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN").
		Updates(map[string]interface{}{
			"last_review_summary": summary,
			"last_review_cycle":   cycle,
			"updated_at":          time.Now().UnixMilli(),
		}).Error
}

// effectiveCloseReasonFromPending picks close_reason when pending matches this fill.
// Some exchange trade APIs return a fill/trade id here instead of the order id
// that was returned by CloseLong/CloseShort. Keep a recent pending attribution in
// that case so AI/manual/risk closes are not mislabeled as exchange sync closes.
func effectiveCloseReasonFromPending(pos TraderPosition, fillOrderID string, fallback string, fillTimeMs int64) string {
	r := strings.TrimSpace(pos.PendingCloseReason)
	if r == "" {
		return fallback
	}
	pendOrd := strings.TrimSpace(pos.PendingCloseOrderID)
	fill := strings.TrimSpace(fillOrderID)
	if pendOrd != "" && fill != "" && pendOrd != fill && pendingCloseReasonExpired(pos, fillTimeMs) {
		return fallback
	}
	return r
}

func pendingCloseReasonExpired(pos TraderPosition, fillTimeMs int64) bool {
	if pos.UpdatedAt <= 0 || fillTimeMs <= 0 {
		return false
	}
	return time.Duration(fillTimeMs-pos.UpdatedAt)*time.Millisecond > pendingCloseReasonTTL
}

// keepPendingAfterPartialClose is true when this partial fill belongs to the same tagged close order.
func keepPendingAfterPartialClose(pos TraderPosition, fillOrderID string) bool {
	if strings.TrimSpace(pos.PendingCloseReason) == "" {
		return false
	}
	pendOrd := strings.TrimSpace(pos.PendingCloseOrderID)
	fill := strings.TrimSpace(fillOrderID)
	return pendOrd != "" && fill != "" && pendOrd == fill
}

// SetPendingCloseReason marks how the next closing fill should be attributed (consumed on full close).
func (s *PositionStore) SetPendingCloseReason(traderID, symbol, side, reason, closeOrderID string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil
	}
	closeOrderID = strings.TrimSpace(closeOrderID)
	nowMs := time.Now().UTC().UnixMilli()
	updates := map[string]interface{}{
		"pending_close_reason":   reason,
		"pending_close_order_id": closeOrderID,
		"updated_at":             nowMs,
	}
	res := s.db.Model(&TraderPosition{}).
		Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN").
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		res2 := s.db.Model(&TraderPosition{}).
			Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, base, side, "OPEN").
			Updates(updates)
		if res2.Error != nil {
			return res2.Error
		}
		if res2.RowsAffected > 0 {
			return nil
		}
	}
	return ErrPendingCloseReasonNoOpenPosition
}

// ClosePosition closes position
func (s *PositionStore) ClosePosition(id int64, exitPrice float64, exitOrderID string, realizedPnL float64, fee float64, closeReason string) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get position: %w", err)
	}
	nowMs := time.Now().UTC().UnixMilli()
	effective := effectiveCloseReasonFromPending(pos, exitOrderID, closeReason, nowMs)
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"exit_price":             exitPrice,
		"exit_order_id":          exitOrderID,
		"exit_time":              nowMs,
		"realized_pnl":           realizedPnL,
		"fee":                    fee,
		"status":                 "CLOSED",
		"close_reason":           effective,
		"pending_close_reason":   "",
		"pending_close_order_id": "",
		"updated_at":             nowMs,
	}).Error
}

// UpdatePositionQuantityAndPrice updates position quantity and recalculates entry price
func (s *PositionStore) UpdatePositionQuantityAndPrice(id int64, addQty float64, addPrice float64, addFee float64) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get current position: %w", err)
	}

	currentEntryQty := pos.EntryQuantity
	if currentEntryQty == 0 {
		currentEntryQty = pos.Quantity
	}

	newQty := math.Round((pos.Quantity+addQty)*10000) / 10000
	newEntryQty := math.Round((currentEntryQty+addQty)*10000) / 10000
	newEntryPrice := (pos.EntryPrice*pos.Quantity + addPrice*addQty) / newQty
	// Use adaptive precision based on price magnitude (for meme coins with very small prices)
	newEntryPrice = adaptivePriceRound(newEntryPrice, pos.EntryPrice, addPrice)
	newFee := pos.Fee + addFee
	nowMs := time.Now().UTC().UnixMilli()

	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"quantity":       newQty,
		"entry_quantity": newEntryQty,
		"entry_price":    newEntryPrice,
		"fee":            newFee,
		"updated_at":     nowMs,
	}).Error
}

// ReducePositionQuantity reduces position quantity for partial close
// If quantity reaches 0 (or near 0), automatically closes the position.
// fillOrderID is this fill's exchange order id (for pending close correlation).
func (s *PositionStore) ReducePositionQuantity(id int64, reduceQty float64, exitPrice float64, addFee float64, addPnL float64, fillOrderID string) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get current position: %w", err)
	}

	newQty := math.Round((pos.Quantity-reduceQty)*10000) / 10000
	newFee := pos.Fee + addFee
	newPnL := pos.RealizedPnL + addPnL

	closedQty := pos.EntryQuantity - pos.Quantity
	newClosedQty := closedQty + reduceQty

	var newExitPrice float64
	if newClosedQty > 0 {
		newExitPrice = (pos.ExitPrice*closedQty + exitPrice*reduceQty) / newClosedQty
		// Use adaptive precision based on price magnitude (for meme coins with very small prices)
		newExitPrice = adaptivePriceRound(newExitPrice, pos.ExitPrice, exitPrice, pos.EntryPrice)
	}

	nowMs := time.Now().UTC().UnixMilli()

	// Check if position should be fully closed (quantity reduced to ~0)
	const QUANTITY_TOLERANCE = 0.0001
	if newQty <= QUANTITY_TOLERANCE {
		closeReason := effectiveCloseReasonFromPending(pos, fillOrderID, "sync", nowMs)
		return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
			"quantity":               0,
			"fee":                    newFee,
			"exit_price":             newExitPrice,
			"realized_pnl":           newPnL,
			"status":                 "CLOSED",
			"exit_time":              nowMs,
			"close_reason":           closeReason,
			"pending_close_reason":   "",
			"pending_close_order_id": "",
			"updated_at":             nowMs,
		}).Error
	}

	updates := map[string]interface{}{
		"quantity":     newQty,
		"fee":          newFee,
		"exit_price":   newExitPrice,
		"realized_pnl": newPnL,
		"updated_at":   nowMs,
	}
	if !keepPendingAfterPartialClose(pos, fillOrderID) {
		updates["pending_close_reason"] = ""
		updates["pending_close_order_id"] = ""
	}
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(updates).Error
}

// UpdatePositionExchangeInfo updates exchange_id and exchange_type
func (s *PositionStore) UpdatePositionExchangeInfo(id int64, exchangeID, exchangeType string) error {
	nowMs := time.Now().UTC().UnixMilli()
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"exchange_id":   exchangeID,
		"exchange_type": exchangeType,
		"updated_at":    nowMs,
	}).Error
}

// ClosePositionFully marks position as fully closed
// exitTimeMs is Unix milliseconds UTC
func (s *PositionStore) ClosePositionFully(id int64, exitPrice float64, exitOrderID string, exitTimeMs int64, totalRealizedPnL float64, totalFee float64, closeReason string) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get position: %w", err)
	}

	quantity := pos.Quantity
	if pos.EntryQuantity > 0 {
		quantity = pos.EntryQuantity
	}

	effective := effectiveCloseReasonFromPending(pos, exitOrderID, closeReason, exitTimeMs)

	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"quantity":               quantity,
		"exit_price":             exitPrice,
		"exit_order_id":          exitOrderID,
		"exit_time":              exitTimeMs,
		"realized_pnl":           totalRealizedPnL,
		"fee":                    totalFee,
		"status":                 "CLOSED",
		"close_reason":           effective,
		"pending_close_reason":   "",
		"pending_close_order_id": "",
		"updated_at":             time.Now().UTC().UnixMilli(),
	}).Error
}

// DeleteAllOpenPositions deletes all OPEN positions for a trader
func (s *PositionStore) DeleteAllOpenPositions(traderID string) error {
	return s.db.Where("trader_id = ? AND status = ?", traderID, "OPEN").Delete(&TraderPosition{}).Error
}

// GetOpenPositions gets all open positions
func (s *PositionStore) GetOpenPositions(traderID string) ([]*TraderPosition, error) {
	var positions []*TraderPosition
	err := s.db.Where("trader_id = ? AND status = ?", traderID, "OPEN").
		Order("entry_time DESC").
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query open positions: %w", err)
	}

	// Fix EntryQuantity if it's 0
	for _, pos := range positions {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
	}
	return positions, nil
}

// GetOpenPositionBySymbol gets open position for specified symbol and direction
func (s *PositionStore) GetOpenPositionBySymbol(traderID, symbol, side string) (*TraderPosition, error) {
	var pos TraderPosition
	err := s.db.Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, symbol, side, "OPEN").
		Order("entry_time DESC").
		First(&pos).Error

	if err == nil {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
		return &pos, nil
	}

	if err == gorm.ErrRecordNotFound {
		// Try without USDT suffix for backward compatibility
		if strings.HasSuffix(symbol, "USDT") {
			baseSymbol := strings.TrimSuffix(symbol, "USDT")
			err = s.db.Where("trader_id = ? AND symbol = ? AND side = ? AND status = ?", traderID, baseSymbol, side, "OPEN").
				Order("entry_time DESC").
				First(&pos).Error
			if err == nil {
				if pos.EntryQuantity == 0 {
					pos.EntryQuantity = pos.Quantity
				}
				return &pos, nil
			}
		}
		return nil, nil
	}
	return nil, err
}

// GetClosedPositions gets closed positions
func (s *PositionStore) GetClosedPositions(traderID string, limit int) ([]*TraderPosition, error) {
	var positions []*TraderPosition
	err := s.db.Where("trader_id = ? AND status = ?", traderID, "CLOSED").
		Order("exit_time DESC").
		Limit(limit).
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query closed positions: %w", err)
	}

	for _, pos := range positions {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
	}
	return positions, nil
}

// GetAllOpenPositions gets all traders' open positions
func (s *PositionStore) GetAllOpenPositions() ([]*TraderPosition, error) {
	var positions []*TraderPosition
	err := s.db.Where("status = ?", "OPEN").
		Order("trader_id, entry_time DESC").
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query all open positions: %w", err)
	}

	for _, pos := range positions {
		if pos.EntryQuantity == 0 {
			pos.EntryQuantity = pos.Quantity
		}
	}
	return positions, nil
}

// ExistsWithExchangePositionID checks if a position exists
func (s *PositionStore) ExistsWithExchangePositionID(exchangeID, exchangePositionID string) (bool, error) {
	if exchangePositionID == "" {
		return false, nil
	}

	var count int64
	err := s.db.Model(&TraderPosition{}).
		Where("exchange_id = ? AND exchange_position_id = ?", exchangeID, exchangePositionID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check position existence: %w", err)
	}
	return count > 0, nil
}

// GetOpenPositionByExchangePositionID gets an OPEN position by exchange_position_id
func (s *PositionStore) GetOpenPositionByExchangePositionID(exchangeID, exchangePositionID string) (*TraderPosition, error) {
	if exchangePositionID == "" {
		return nil, nil
	}

	var pos TraderPosition
	err := s.db.Where("exchange_id = ? AND exchange_position_id = ? AND status = ?", exchangeID, exchangePositionID, "OPEN").
		First(&pos).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	if pos.EntryQuantity == 0 {
		pos.EntryQuantity = pos.Quantity
	}
	return &pos, nil
}

// CreateOpenPosition creates an open position
func (s *PositionStore) CreateOpenPosition(pos *TraderPosition) error {
	if pos.ExchangePositionID != "" && pos.ExchangeID != "" {
		existingPos, err := s.GetOpenPositionByExchangePositionID(pos.ExchangeID, pos.ExchangePositionID)
		if err != nil {
			return err
		}
		if existingPos != nil {
			return s.UpdatePositionQuantityAndPrice(existingPos.ID, pos.Quantity, pos.EntryPrice, pos.Fee)
		}
		exists, err := s.ExistsWithExchangePositionID(pos.ExchangeID, pos.ExchangePositionID)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	if pos.Status == "" {
		pos.Status = "OPEN"
	}
	if pos.Source == "" {
		pos.Source = "system"
	}
	if pos.EntryQuantity == 0 {
		pos.EntryQuantity = pos.Quantity
	}

	err := s.db.Create(pos).Error
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			existingPos, findErr := s.GetOpenPositionByExchangePositionID(pos.ExchangeID, pos.ExchangePositionID)
			if findErr != nil {
				return findErr
			}
			if existingPos != nil {
				return s.UpdatePositionQuantityAndPrice(existingPos.ID, pos.Quantity, pos.EntryPrice, pos.Fee)
			}
			return nil
		}
		return fmt.Errorf("failed to create open position: %w", err)
	}

	return nil
}

// ClosePositionWithAccurateData closes a position with accurate data from exchange
// exitTimeMs is Unix milliseconds UTC
func (s *PositionStore) ClosePositionWithAccurateData(id int64, exitPrice float64, exitOrderID string, exitTimeMs int64, realizedPnL float64, fee float64, closeReason string) error {
	var pos TraderPosition
	if err := s.db.First(&pos, id).Error; err != nil {
		return fmt.Errorf("failed to get position: %w", err)
	}
	effective := effectiveCloseReasonFromPending(pos, exitOrderID, closeReason, exitTimeMs)
	return s.db.Model(&TraderPosition{}).Where("id = ?", id).Updates(map[string]interface{}{
		"exit_price":             exitPrice,
		"exit_order_id":          exitOrderID,
		"exit_time":              exitTimeMs,
		"realized_pnl":           realizedPnL,
		"fee":                    fee,
		"status":                 "CLOSED",
		"close_reason":           effective,
		"pending_close_reason":   "",
		"pending_close_order_id": "",
		"updated_at":             time.Now().UTC().UnixMilli(),
	}).Error
}
