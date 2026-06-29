package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// CoinSupply stores token supply and market-cap metadata sourced from CoinGecko.
// It is used by the small-market-value provider to avoid hitting CoinGecko
// rate limits during trading.
type CoinSupply struct {
	Symbol             string    `gorm:"primaryKey;column:symbol" json:"symbol"`
	CoingeckoID        string    `gorm:"column:coingecko_id" json:"coingecko_id"`
	Name               string    `gorm:"column:name" json:"name"`
	CirculatingSupply  float64   `gorm:"column:circulating_supply" json:"circulating_supply"`
	TotalSupply        float64   `gorm:"column:total_supply" json:"total_supply"`
	MarketCapUSD       float64   `gorm:"column:market_cap_usd" json:"market_cap_usd"`
	Volume24hUSD       float64   `gorm:"column:volume_24h_usd" json:"volume_24h_usd"`
	LastUpdatedAt      time.Time `gorm:"column:last_updated_at" json:"last_updated_at"`
	Source             string    `gorm:"column:source" json:"source"`
}

func (CoinSupply) TableName() string { return "coin_supply" }

// CoinSupplyStore provides CRUD for cached coin supply data.
type CoinSupplyStore struct {
	db *gorm.DB
}

// NewCoinSupplyStore creates a new CoinSupplyStore.
func NewCoinSupplyStore(db *gorm.DB) *CoinSupplyStore {
	return &CoinSupplyStore{db: db}
}

// DB returns the underlying GORM database handle.
func (s *CoinSupplyStore) DB() *gorm.DB {
	return s.db
}

// InitTables creates the coin_supply table if it does not exist.
func (s *CoinSupplyStore) InitTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'coin_supply'`).Scan(&tableExists)
		if tableExists > 0 {
			return nil
		}
	}
	return s.db.AutoMigrate(&CoinSupply{})
}

func (s *CoinSupplyStore) initDefaultData() error {
	return nil
}

// Upsert inserts or updates a batch of coin supply records.
func (s *CoinSupplyStore) Upsert(records []CoinSupply) error {
	if len(records) == 0 {
		return nil
	}
	return s.db.Save(records).Error
}

// GetBySymbol returns a single record by symbol.
func (s *CoinSupplyStore) GetBySymbol(symbol string) (*CoinSupply, error) {
	var record CoinSupply
	err := s.db.Where("symbol = ?", symbol).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// GetBySymbols returns a map of symbol -> CoinSupply for the given symbols.
// Missing symbols are omitted from the result.
func (s *CoinSupplyStore) GetBySymbols(symbols []string) (map[string]CoinSupply, error) {
	result := make(map[string]CoinSupply)
	if len(symbols) == 0 {
		return result, nil
	}

	var records []CoinSupply
	err := s.db.Where("symbol IN ?", symbols).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("query coin_supply: %w", err)
	}

	for _, r := range records {
		result[strings.ToUpper(r.Symbol)] = r
	}
	return result, nil
}

// Count returns the total number of cached records.
func (s *CoinSupplyStore) Count() (int64, error) {
	var count int64
	err := s.db.Model(&CoinSupply{}).Count(&count).Error
	return count, err
}
