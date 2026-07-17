package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// TraderStore trader storage
type TraderStore struct {
	db *gorm.DB
}

const DeterministicEngineID = "deterministic"

// NewTraderStore creates a new trader store
func NewTraderStore(db *gorm.DB) *TraderStore {
	return &TraderStore{db: db}
}

// Trader trader configuration
type Trader struct {
	ID                  string    `gorm:"primaryKey" json:"id"`
	UserID              string    `gorm:"column:user_id;not null;default:default;index" json:"user_id"`
	Name                string    `gorm:"column:name;not null" json:"name"`
	AIModelID           string    `gorm:"column:ai_model_id;not null;default:'deterministic'" json:"ai_model_id"`
	ExchangeID          string    `gorm:"column:exchange_id;not null" json:"exchange_id"`
	StrategyID          string    `gorm:"column:strategy_id;default:''" json:"strategy_id"`
	DecisionLanguage    string    `gorm:"column:decision_language;default:''" json:"decision_language,omitempty"` // "en"/"zh" overrides strategy language; empty = follow strategy
	InitialBalance      float64   `gorm:"column:initial_balance;not null" json:"initial_balance"`
	ScanIntervalMinutes int       `gorm:"column:scan_interval_minutes;default:3" json:"scan_interval_minutes"`
	IsRunning           bool      `gorm:"column:is_running;default:false" json:"is_running"`
	IsCrossMargin       bool      `gorm:"column:is_cross_margin;default:true" json:"is_cross_margin"`
	ShowInCompetition   bool      `gorm:"column:show_in_competition" json:"show_in_competition"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`

	// Following fields are deprecated, kept only for existing database compatibility.
	BTCETHLeverage  int    `gorm:"column:btc_eth_leverage;default:5" json:"btc_eth_leverage,omitempty"`
	AltcoinLeverage int    `gorm:"column:altcoin_leverage;default:5" json:"altcoin_leverage,omitempty"`
	TradingSymbols  string `gorm:"column:trading_symbols;default:''" json:"trading_symbols,omitempty"`
	UseAI500        bool   `gorm:"column:use_coin_pool;default:false" json:"use_ai500,omitempty"`
	UseOITop        bool   `gorm:"column:use_oi_top;default:false" json:"use_oi_top,omitempty"`
}

// TableName returns the table name for Trader
func (Trader) TableName() string {
	return "traders"
}

// TraderFullConfig contains online runtime dependencies.
type TraderFullConfig struct {
	Trader   *Trader
	Exchange *Exchange
	Strategy *Strategy
}

func (s *TraderStore) initTables() error {
	// For PostgreSQL with existing table, skip AutoMigrate
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'traders'`).Scan(&tableExists)
		if tableExists > 0 {
			if err := s.db.Exec(`ALTER TABLE traders ADD COLUMN IF NOT EXISTS decision_language TEXT DEFAULT ''`).Error; err != nil {
				return fmt.Errorf("failed to migrate traders decision language: %w", err)
			}
			return nil
		}
	}
	// Use GORM AutoMigrate
	if err := s.db.AutoMigrate(&Trader{}); err != nil {
		return fmt.Errorf("failed to migrate traders table: %w", err)
	}
	return nil
}

// Get retrieves a trader by ID for a specific user.
func (s *TraderStore) Get(userID, id string) (*Trader, error) {
	var trader Trader
	err := s.db.Where("id = ? AND user_id = ?", id, userID).First(&trader).Error
	if err != nil {
		return nil, err
	}
	return &trader, nil
}

// GetByName retrieves a trader by name for a specific user
func (s *TraderStore) GetByName(userID, name string) (*Trader, error) {
	var trader Trader
	err := s.db.Where("user_id = ? AND name = ?", userID, name).First(&trader).Error
	if err != nil {
		return nil, err
	}
	return &trader, nil
}

// Create creates trader
func (s *TraderStore) Create(trader *Trader) error {
	// Check for duplicate name within the same user
	var count int64
	if err := s.db.Model(&Trader{}).Where("user_id = ? AND name = ?", trader.UserID, trader.Name).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check duplicate trader name: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("trader name '%s' already exists", trader.Name)
	}
	return s.db.Create(trader).Error
}

// List gets user's trader list
func (s *TraderStore) List(userID string) ([]*Trader, error) {
	var traders []*Trader
	err := s.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&traders).Error
	if err != nil {
		return nil, err
	}
	return traders, nil
}

// UpdateStatus updates trader running status
func (s *TraderStore) UpdateStatus(userID, id string, isRunning bool) error {
	return s.db.Model(&Trader{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("is_running", isRunning).Error
}

// UpdateShowInCompetition updates trader competition visibility
func (s *TraderStore) UpdateShowInCompetition(userID, id string, showInCompetition bool) error {
	return s.db.Model(&Trader{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("show_in_competition", showInCompetition).Error
}

// Update updates trader configuration
func (s *TraderStore) Update(trader *Trader) error {
	fmt.Printf("📝 TraderStore.Update: ID=%s, Name=%s, AIModelID=%s, StrategyID=%s\n",
		trader.ID, trader.Name, trader.AIModelID, trader.StrategyID)

	// Check for duplicate name within the same user (excluding self)
	var count int64
	if err := s.db.Model(&Trader{}).Where("user_id = ? AND name = ? AND id != ?", trader.UserID, trader.Name, trader.ID).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check duplicate trader name: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("trader name '%s' already exists", trader.Name)
	}

	updates := map[string]interface{}{
		"name":                trader.Name,
		"ai_model_id":         trader.AIModelID,
		"exchange_id":         trader.ExchangeID,
		"strategy_id":         trader.StrategyID,
		"decision_language":   trader.DecisionLanguage,
		"is_cross_margin":     trader.IsCrossMargin,
		"show_in_competition": trader.ShowInCompetition,
	}

	// Only update these if > 0
	if trader.InitialBalance > 0 {
		updates["initial_balance"] = trader.InitialBalance
	}
	if trader.ScanIntervalMinutes > 0 {
		updates["scan_interval_minutes"] = trader.ScanIntervalMinutes
		fmt.Printf("📊 TraderStore.Update: scan_interval_minutes=%d will be saved\n", trader.ScanIntervalMinutes)
	} else {
		fmt.Printf("⚠️ TraderStore.Update: scan_interval_minutes=%d (<=0, NOT updating)\n", trader.ScanIntervalMinutes)
	}

	return s.db.Model(&Trader{}).
		Where("id = ? AND user_id = ?", trader.ID, trader.UserID).
		Updates(updates).Error
}

// UpdateInitialBalance updates initial balance
func (s *TraderStore) UpdateInitialBalance(userID, id string, newBalance float64) error {
	return s.db.Model(&Trader{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("initial_balance", newBalance).Error
}

// Delete deletes trader and associated data
func (s *TraderStore) Delete(userID, id string) error {
	// Delete associated equity snapshots first
	s.db.Where("trader_id = ?", id).Delete(&EquitySnapshot{})

	// Delete the trader
	return s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&Trader{}).Error
}

// GetFullConfig gets trader full configuration
func (s *TraderStore) GetFullConfig(userID, traderID string) (*TraderFullConfig, error) {
	var trader Trader
	err := s.db.Where("id = ? AND user_id = ?", traderID, userID).First(&trader).Error
	if err != nil {
		return nil, err
	}

	// Get exchange
	var exchange Exchange
	err = s.db.Where("id = ? AND user_id = ?", trader.ExchangeID, userID).First(&exchange).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange: %w", err)
	}

	// Load associated strategy
	var strategy *Strategy
	if trader.StrategyID != "" {
		strategy, _ = s.getStrategyByID(userID, trader.StrategyID)
	}
	// If no associated strategy, get user's active strategy or default strategy
	if strategy == nil {
		strategy, _ = s.getActiveOrDefaultStrategy(userID)
	}

	return &TraderFullConfig{
		Trader:   &trader,
		Exchange: &exchange,
		Strategy: strategy,
	}, nil
}

// getStrategyByID internal method: gets strategy by ID
func (s *TraderStore) getStrategyByID(userID, strategyID string) (*Strategy, error) {
	var strategy Strategy
	err := s.db.Where("id = ? AND (user_id = ? OR is_default = ?)", strategyID, userID, true).
		First(&strategy).Error
	if err != nil {
		return nil, err
	}
	return &strategy, nil
}

// getActiveOrDefaultStrategy internal method: gets user's active strategy or system default strategy
func (s *TraderStore) getActiveOrDefaultStrategy(userID string) (*Strategy, error) {
	var strategy Strategy

	// First try to get user's active strategy
	err := s.db.Where("user_id = ? AND is_active = ?", userID, true).First(&strategy).Error
	if err == nil {
		return &strategy, nil
	}

	// Fallback to system default strategy
	err = s.db.Where("is_default = ?", true).First(&strategy).Error
	if err != nil {
		return nil, err
	}
	return &strategy, nil
}

// GetByID gets a trader by ID without requiring userID (for public APIs)
func (s *TraderStore) GetByID(traderID string) (*Trader, error) {
	var trader Trader
	err := s.db.Where("id = ?", traderID).First(&trader).Error
	if err != nil {
		return nil, err
	}
	return &trader, nil
}

// ListAll gets all traders
func (s *TraderStore) ListAll() ([]*Trader, error) {
	var traders []*Trader
	err := s.db.Order("created_at DESC").Find(&traders).Error
	if err != nil {
		return nil, err
	}
	return traders, nil
}

// ListByExchangeID gets traders that use a specific exchange
func (s *TraderStore) ListByExchangeID(userID, exchangeID string) ([]*Trader, error) {
	var traders []*Trader
	err := s.db.Where("user_id = ? AND exchange_id = ?", userID, exchangeID).Find(&traders).Error
	if err != nil {
		return nil, err
	}
	return traders, nil
}
