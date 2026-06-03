package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTraderStoreCreatePreservesShowInCompetitionFalse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Trader{}); err != nil {
		t.Fatalf("migrate trader: %v", err)
	}

	store := NewTraderStore(db)
	trader := &Trader{
		ID:                  "trader-1",
		UserID:              "user-1",
		Name:                "paper test",
		AIModelID:           "model-1",
		ExchangeID:          "exchange-1",
		StrategyID:          "strategy-1",
		InitialBalance:      10000,
		ScanIntervalMinutes: 3,
		IsCrossMargin:       true,
		ShowInCompetition:   false,
	}
	if err := store.Create(trader); err != nil {
		t.Fatalf("create trader: %v", err)
	}

	savedTraders, err := store.List("user-1")
	if err != nil {
		t.Fatalf("list traders: %v", err)
	}
	if len(savedTraders) != 1 {
		t.Fatalf("expected 1 trader, got %d", len(savedTraders))
	}
	if savedTraders[0].ShowInCompetition {
		t.Fatal("expected show_in_competition=false to be persisted")
	}
}
