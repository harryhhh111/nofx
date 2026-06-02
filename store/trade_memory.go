package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type TradeMemory struct {
	ID              int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID        string     `gorm:"column:trader_id;not null;index:idx_trade_memory_trader_symbol" json:"trader_id"`
	StrategyID      string     `gorm:"column:strategy_id;default:'';index" json:"strategy_id,omitempty"`
	StrategyVersion string     `gorm:"column:strategy_version;default:''" json:"strategy_version,omitempty"`
	Symbol          string     `gorm:"column:symbol;not null;index:idx_trade_memory_trader_symbol" json:"symbol"`
	Side            string     `gorm:"column:side;default:''" json:"side,omitempty"`
	Action          string     `gorm:"column:action;default:''" json:"action,omitempty"`
	Scope           string     `gorm:"column:scope;not null;default:'symbol';index" json:"scope"`
	SourceType      string     `gorm:"column:source_type;not null;default:'trade_outcome'" json:"source_type"`
	SignalID        string     `gorm:"column:signal_id;default:'';index" json:"signal_id,omitempty"`
	DecisionID      int64      `gorm:"column:decision_id;default:0;index" json:"decision_id,omitempty"`
	PositionID      int64      `gorm:"column:position_id;default:0" json:"position_id,omitempty"`
	Result          string     `gorm:"column:result;default:'';index" json:"result,omitempty"`
	OutcomePnL      float64    `gorm:"column:outcome_pnl;default:0" json:"outcome_pnl,omitempty"`
	OutcomePnLPct   float64    `gorm:"column:outcome_pnl_pct;default:0" json:"outcome_pnl_pct,omitempty"`
	Summary         string     `gorm:"column:summary;not null" json:"summary"`
	Evidence        string     `gorm:"column:evidence;default:''" json:"evidence,omitempty"`
	LessonsJSON     string     `gorm:"column:lessons_json;default:'[]'" json:"lessons_json,omitempty"`
	TagsJSON        string     `gorm:"column:tags_json;default:'[]'" json:"tags_json,omitempty"`
	QualityScore    float64    `gorm:"column:quality_score;default:0;index" json:"quality_score"`
	Confidence      float64    `gorm:"column:confidence;default:0" json:"confidence"`
	ExpiresAt       *time.Time `gorm:"column:expires_at;index" json:"expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (TradeMemory) TableName() string { return "trade_memories" }

type TradeMemoryStore struct {
	db *gorm.DB
}

func NewTradeMemoryStore(db *gorm.DB) *TradeMemoryStore {
	return &TradeMemoryStore{db: db}
}

func (s *TradeMemoryStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'trade_memories'`).Scan(&tableExists)
		if tableExists > 0 {
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS strategy_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS strategy_version TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS signal_id TEXT DEFAULT ''`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS decision_id BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS position_id BIGINT DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS outcome_pnl_pct DOUBLE PRECISION DEFAULT 0`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS lessons_json TEXT DEFAULT '[]'`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS tags_json TEXT DEFAULT '[]'`)
			s.db.Exec(`ALTER TABLE trade_memories ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP WITH TIME ZONE`)
			s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_trade_memory_position ON trade_memories(position_id) WHERE position_id > 0`)
			return nil
		}
	}
	if err := s.db.AutoMigrate(&TradeMemory{}); err != nil {
		return fmt.Errorf("failed to migrate trade_memories table: %w", err)
	}
	if err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_trade_memory_position ON trade_memories(position_id) WHERE position_id > 0`).Error; err != nil {
		return fmt.Errorf("failed to create trade memory position index: %w", err)
	}
	return nil
}

func (s *TradeMemoryStore) Create(memory *TradeMemory) error {
	if memory == nil {
		return fmt.Errorf("trade memory is nil")
	}
	memory.TraderID = strings.TrimSpace(memory.TraderID)
	memory.Symbol = strings.TrimSpace(strings.ToUpper(memory.Symbol))
	memory.Summary = strings.TrimSpace(memory.Summary)
	if memory.TraderID == "" {
		return fmt.Errorf("trade memory trader_id is required")
	}
	if memory.Symbol == "" {
		return fmt.Errorf("trade memory symbol is required")
	}
	if memory.Summary == "" {
		return fmt.Errorf("trade memory summary is required")
	}
	if memory.Scope == "" {
		memory.Scope = "symbol"
	}
	if memory.SourceType == "" {
		memory.SourceType = "trade_outcome"
	}
	if memory.QualityScore < 0 || memory.QualityScore > 1 {
		return fmt.Errorf("trade memory quality_score must be between 0 and 1")
	}
	if memory.Confidence < 0 || memory.Confidence > 1 {
		return fmt.Errorf("trade memory confidence must be between 0 and 1")
	}
	if memory.LessonsJSON == "" {
		memory.LessonsJSON = "[]"
	}
	if memory.TagsJSON == "" {
		memory.TagsJSON = "[]"
	}
	return s.db.Create(memory).Error
}

func (s *TradeMemoryStore) ExistsForPosition(traderID string, positionID int64) (bool, error) {
	if strings.TrimSpace(traderID) == "" || positionID <= 0 {
		return false, nil
	}
	var count int64
	err := s.db.Model(&TradeMemory{}).
		Where("trader_id = ? AND position_id = ?", traderID, positionID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *TradeMemoryStore) FindRelevant(traderID string, symbols []string, limit int) ([]*TradeMemory, error) {
	traderID = strings.TrimSpace(traderID)
	if traderID == "" {
		return nil, fmt.Errorf("trader_id is required")
	}
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC()
	query := s.db.Where("trader_id = ?", traderID).
		Where("(expires_at IS NULL OR expires_at > ?)", now).
		Where("quality_score >= ? AND confidence >= ?", 0.3, 0.3)
	normalized := normalizeMemorySymbols(symbols)
	if len(normalized) > 0 {
		query = query.Where("(symbol IN ? OR scope = ?)", normalized, "global")
	}
	var records []*TradeMemory
	err := query.Order("quality_score DESC, updated_at DESC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query trade memories: %w", err)
	}
	return records, nil
}

func (s *TradeMemoryStore) List(traderID, symbol string, limit int) ([]*TradeMemory, error) {
	traderID = strings.TrimSpace(traderID)
	if traderID == "" {
		return nil, fmt.Errorf("trader_id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	query := s.db.Where("trader_id = ?", traderID)
	if strings.TrimSpace(symbol) != "" {
		query = query.Where("symbol = ?", strings.ToUpper(strings.TrimSpace(symbol)))
	}
	var records []*TradeMemory
	err := query.Order("created_at DESC").Limit(limit).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list trade memories: %w", err)
	}
	return records, nil
}

func normalizeMemorySymbols(symbols []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" || seen[symbol] {
			continue
		}
		seen[symbol] = true
		out = append(out, symbol)
	}
	return out
}
