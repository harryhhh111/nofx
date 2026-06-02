package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ExecutionAnalytics struct {
	ID                  int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID            string  `gorm:"column:trader_id;not null;index:idx_execution_trader_time" json:"trader_id"`
	ExchangeID          string  `gorm:"column:exchange_id;default:''" json:"exchange_id,omitempty"`
	ExchangeType        string  `gorm:"column:exchange_type;default:''" json:"exchange_type,omitempty"`
	Symbol              string  `gorm:"column:symbol;not null;index" json:"symbol"`
	Action              string  `gorm:"column:action;not null;index" json:"action"`
	ExchangeOrderID     string  `gorm:"column:exchange_order_id;default:'';index" json:"exchange_order_id,omitempty"`
	SignalGeneratedAt   int64   `gorm:"column:signal_generated_at;default:0" json:"signal_generated_at,omitempty"`
	OrderSubmittedAt    int64   `gorm:"column:order_submitted_at;default:0;index:idx_execution_trader_time,sort:desc" json:"order_submitted_at,omitempty"`
	FirstFillAt         int64   `gorm:"column:first_fill_at;default:0" json:"first_fill_at,omitempty"`
	FinalFillAt         int64   `gorm:"column:final_fill_at;default:0" json:"final_fill_at,omitempty"`
	IntendedPrice       float64 `gorm:"column:intended_price;default:0" json:"intended_price,omitempty"`
	IntendedQuantity    float64 `gorm:"column:intended_quantity;default:0" json:"intended_quantity,omitempty"`
	SubmittedQuantity   float64 `gorm:"column:submitted_quantity;default:0" json:"submitted_quantity,omitempty"`
	FilledQuantity      float64 `gorm:"column:filled_quantity;default:0" json:"filled_quantity,omitempty"`
	AvgFillPrice        float64 `gorm:"column:avg_fill_price;default:0" json:"avg_fill_price,omitempty"`
	BestBid             float64 `gorm:"column:best_bid;default:0" json:"best_bid,omitempty"`
	BestAsk             float64 `gorm:"column:best_ask;default:0" json:"best_ask,omitempty"`
	SpreadBps           float64 `gorm:"column:spread_bps;default:0" json:"spread_bps,omitempty"`
	ExpectedSlippageBps float64 `gorm:"column:expected_slippage_bps;default:0" json:"expected_slippage_bps,omitempty"`
	RealizedSlippageBps float64 `gorm:"column:realized_slippage_bps;default:0" json:"realized_slippage_bps,omitempty"`
	PartialFillRatio    float64 `gorm:"column:partial_fill_ratio;default:0" json:"partial_fill_ratio,omitempty"`
	Status              string  `gorm:"column:status;not null;default:'submitted';index" json:"status"`
	ErrorMessage        string  `gorm:"column:error_message;default:''" json:"error_message,omitempty"`
	CreatedAt           int64   `gorm:"column:created_at" json:"created_at"`
	UpdatedAt           int64   `gorm:"column:updated_at" json:"updated_at"`
}

func (ExecutionAnalytics) TableName() string { return "execution_analytics" }

type ExecutionAnalyticsStore struct {
	db *gorm.DB
}

func NewExecutionAnalyticsStore(db *gorm.DB) *ExecutionAnalyticsStore {
	return &ExecutionAnalyticsStore{db: db}
}

func (s *ExecutionAnalyticsStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'execution_analytics'`).Scan(&tableExists)
		if tableExists > 0 {
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS exchange_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS exchange_type TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS exchange_order_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS signal_generated_at BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS order_submitted_at BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS first_fill_at BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS final_fill_at BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS intended_price DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS intended_quantity DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS submitted_quantity DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS filled_quantity DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS avg_fill_price DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS best_bid DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS best_ask DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS spread_bps DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS expected_slippage_bps DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS realized_slippage_bps DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS partial_fill_ratio DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'submitted'`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS error_message TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS created_at BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE execution_analytics ADD COLUMN IF NOT EXISTS updated_at BIGINT DEFAULT 0`)
			return nil
		}
	}
	if err := s.db.AutoMigrate(&ExecutionAnalytics{}); err != nil {
		return fmt.Errorf("failed to migrate execution_analytics table: %w", err)
	}
	return nil
}

func (s *ExecutionAnalyticsStore) Create(record *ExecutionAnalytics) error {
	if record == nil {
		return fmt.Errorf("execution analytics record is nil")
	}
	record.TraderID = strings.TrimSpace(record.TraderID)
	record.Symbol = strings.ToUpper(strings.TrimSpace(record.Symbol))
	record.Action = strings.TrimSpace(record.Action)
	if record.TraderID == "" {
		return fmt.Errorf("execution analytics trader_id is required")
	}
	if record.Symbol == "" {
		return fmt.Errorf("execution analytics symbol is required")
	}
	if record.Action == "" {
		return fmt.Errorf("execution analytics action is required")
	}
	if record.Status == "" {
		record.Status = "submitted"
	}
	nowMs := time.Now().UTC().UnixMilli()
	if record.CreatedAt == 0 {
		record.CreatedAt = nowMs
	}
	record.UpdatedAt = nowMs
	return s.db.Create(record).Error
}

func (s *ExecutionAnalyticsStore) Update(id int64, updates map[string]interface{}) error {
	if id <= 0 {
		return nil
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	updates["updated_at"] = time.Now().UTC().UnixMilli()
	return s.db.Model(&ExecutionAnalytics{}).Where("id = ?", id).Updates(updates).Error
}

func (s *ExecutionAnalyticsStore) List(traderID, symbol string, limit int) ([]*ExecutionAnalytics, error) {
	traderID = strings.TrimSpace(traderID)
	if traderID == "" {
		return nil, fmt.Errorf("trader_id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	query := s.db.Where("trader_id = ?", traderID)
	if strings.TrimSpace(symbol) != "" {
		query = query.Where("symbol = ?", strings.ToUpper(strings.TrimSpace(symbol)))
	}
	var records []*ExecutionAnalytics
	err := query.Order("order_submitted_at DESC, created_at DESC").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list execution analytics: %w", err)
	}
	return records, nil
}
