package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdatePositionExcursionTracksBestAndWorstOpenPerformance(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&TraderPosition{}); err != nil {
		t.Fatalf("migrate positions: %v", err)
	}
	st := NewPositionStore(db)
	if err := st.CreateOpenPosition(&TraderPosition{
		TraderID:   "trader-1",
		Symbol:     "BTCUSDT",
		Side:       "LONG",
		Quantity:   1,
		EntryPrice: 100,
		EntryTime:  1,
		Status:     "OPEN",
	}); err != nil {
		t.Fatalf("create open position: %v", err)
	}

	if err := st.UpdatePositionExcursion("trader-1", "BTCUSDT", "LONG", 105, 5, 10, 1000); err != nil {
		t.Fatalf("record favorable excursion: %v", err)
	}
	if err := st.UpdatePositionExcursion("trader-1", "BTCUSDT", "LONG", 98, -2, -4, 2000); err != nil {
		t.Fatalf("record adverse excursion: %v", err)
	}
	if err := st.UpdatePositionExcursion("trader-1", "BTCUSDT", "LONG", 103, 3, 6, 3000); err != nil {
		t.Fatalf("record smaller favorable excursion: %v", err)
	}

	var got TraderPosition
	if err := db.Where("trader_id = ? AND symbol = ? AND side = ?", "trader-1", "BTCUSDT", "LONG").First(&got).Error; err != nil {
		t.Fatalf("load position: %v", err)
	}
	if got.MaxFavorablePnL != 5 || got.MaxFavorablePnLPct != 10 || got.MaxFavorablePrice != 105 || got.MaxFavorableAt != 1000 {
		t.Fatalf("unexpected favorable excursion: %+v", got)
	}
	if got.MaxAdversePnL != -2 || got.MaxAdversePnLPct != -4 || got.MaxAdversePrice != 98 || got.MaxAdverseAt != 2000 {
		t.Fatalf("unexpected adverse excursion: %+v", got)
	}
}
