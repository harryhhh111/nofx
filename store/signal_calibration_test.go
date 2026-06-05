package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSignalCalibrationReportNoData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	st := NewSignalCalibrationStore(db)
	if err := st.initTables(); err != nil {
		t.Fatalf("init tables: %v", err)
	}
	if err := db.AutoMigrate(&TraderPosition{}); err != nil {
		t.Fatalf("migrate positions: %v", err)
	}
	report, err := st.BuildReport("strategy-1", 100)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if report.QualityGate != "no_data" || report.SampleCount != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestSignalCalibrationReportCounts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	st := NewSignalCalibrationStore(db)
	if err := st.initTables(); err != nil {
		t.Fatalf("init tables: %v", err)
	}
	if err := db.AutoMigrate(&TraderPosition{}); err != nil {
		t.Fatalf("migrate positions: %v", err)
	}
	err = st.CreateMany([]*SignalCalibrationSample{
		{TraderID: "t1", StrategyID: "strategy-1", Symbol: "BTCUSDT", SampleKind: "setup", Setup: "trend_continuation_long", Action: "open_long", Eligible: true, RiskStatus: "approved", ReviewStatus: "pass", PrimaryTimeframe: "15m", EntryTimeframe: "5m"},
		{TraderID: "t1", StrategyID: "strategy-1", Symbol: "ETHUSDT", SampleKind: "setup", Setup: "trend_pullback_long", Eligible: false, RiskStatus: "no_signal", ReviewStatus: ""},
		{TraderID: "t1", StrategyID: "strategy-1", Symbol: "SOLUSDT", SampleKind: "signal", Action: "open_long", Eligible: true, RiskStatus: "risk_rejected", ReviewStatus: "warn"},
	})
	if err != nil {
		t.Fatalf("create samples: %v", err)
	}
	report, err := st.BuildReport("strategy-1", 100)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if report.SampleCount != 3 || report.ApprovedCount != 1 || report.NoSignalCount != 1 || report.RiskRejectedCount != 1 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if report.RiskStatusCounts["approved"] != 1 || report.ReviewStatusCounts["none"] != 1 {
		t.Fatalf("unexpected status counts: %+v", report)
	}
}

func TestSignalCalibrationReportIncludesClosedTradeOutcomes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	st := NewSignalCalibrationStore(db)
	if err := st.initTables(); err != nil {
		t.Fatalf("init tables: %v", err)
	}
	if err := db.AutoMigrate(&TraderPosition{}); err != nil {
		t.Fatalf("migrate positions: %v", err)
	}
	if err := st.CreateMany([]*SignalCalibrationSample{
		{TraderID: "t1", StrategyID: "strategy-1", Symbol: "BTCUSDT", SampleKind: "setup", Setup: "trend_continuation_long", Action: "open_long", Eligible: true, RiskStatus: "approved", ReviewStatus: "pass"},
	}); err != nil {
		t.Fatalf("create samples: %v", err)
	}
	err = db.Create([]TraderPosition{
		{TraderID: "t1", StrategyID: "strategy-1", Symbol: "BTCUSDT", Side: "LONG", Status: "CLOSED", OpeningSetup: "trend_continuation_long", EntryPrice: 100, EntryTime: 1, ExitPrice: 110, ExitTime: 2, RealizedPnL: 10},
		{TraderID: "t1", StrategyID: "strategy-1", Symbol: "ETHUSDT", Side: "LONG", Status: "CLOSED", OpeningSetup: "trend_continuation_long", EntryPrice: 100, EntryTime: 1, ExitPrice: 95, ExitTime: 2, RealizedPnL: -5},
	}).Error
	if err != nil {
		t.Fatalf("create positions: %v", err)
	}
	report, err := st.BuildReport("strategy-1", 100)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if report.ClosedTradeCount != 2 || report.WinningTradeCount != 1 || report.LosingTradeCount != 1 {
		t.Fatalf("unexpected outcome counts: %+v", report)
	}
	if report.TotalPnL != 5 || report.AveragePnL != 2.5 || report.WinRate != 0.5 {
		t.Fatalf("unexpected outcome metrics: %+v", report)
	}
	if len(report.SetupStats) == 0 || report.SetupStats[0].ClosedTrades != 2 || report.SetupStats[0].TotalPnL != 5 {
		t.Fatalf("unexpected setup outcome stats: %+v", report.SetupStats)
	}
}
