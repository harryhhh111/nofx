package kernel

import (
	"nofx/market"
	"nofx/store"
	"testing"
	"time"
)

func TestBuildStrategyReplayReportUsesPersistedKlineWindows(t *testing.T) {
	config := store.GetDefaultStrategyConfig("en")
	config.Indicators.Klines.PrimaryTimeframe = "15m"
	config.Indicators.Klines.EntryTimeframe = "15m"
	config.Indicators.Klines.ConfirmationTimeframes = nil
	config.Indicators.Klines.SelectedTimeframes = []string{"15m"}
	config.Indicators.Klines.ComputeLookback = 80
	config.Indicators.Klines.PromptDisplayCount = 30
	config.Structure.MarketStructure.Lookback = 80
	config.Structure.MarketStructure.SwingWindow = 1
	config.Structure.MarketStructure.MinLegBars = 1
	config.Structure.MarketStructure.MinLegATRMultiple = 0.1
	config.Structure.MarketStructure.ZigZagThresholdPct = 0.1
	config.Structure.MarketStructure.BreakoutBufferATR = 0.1
	config.Structure.MarketStructure.RetestToleranceATR = 1
	config.ResolvedParameters.Structure.MarketStructure = &config.Structure.MarketStructure
	config.ClampLimits()

	windows := map[string][]market.Kline{
		"15m": replayTestKlines(100, 90),
	}
	samples := []store.SignalCalibrationSample{
		{
			TraderID:         "t1",
			StrategyID:       "strategy-1",
			StrategyVersion:  "v1",
			Symbol:           "BTCUSDT",
			SampleKind:       "setup",
			Setup:            "breakout_long",
			RiskStatus:       "approved",
			KlineWindowsJSON: store.MarshalCalibrationJSON(windows),
			AsOf:             time.Unix(1, 0).UTC(),
		},
		{
			TraderID:        "t1",
			StrategyID:      "strategy-1",
			StrategyVersion: "v1",
			Symbol:          "ETHUSDT",
			SampleKind:      "setup",
			Setup:           "trend_pullback_long",
			RiskStatus:      "no_signal",
			AsOf:            time.Unix(2, 0).UTC(),
		},
	}

	report, err := BuildStrategyReplayReport(StrategyReplayRequest{
		StrategyID:      "strategy-1",
		StrategyVersion: "v1",
		CurrentConfig:   &config,
		Samples:         samples,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("BuildStrategyReplayReport returned error: %v", err)
	}
	if report.SampleCount != 2 || report.ReplayableSampleCount != 1 || report.MissingKlineWindowCount != 1 {
		t.Fatalf("unexpected replay counts: %+v", report)
	}
	if len(report.ParameterScans) < 2 {
		t.Fatalf("expected baseline and parameter variants: %+v", report.ParameterScans)
	}
	if report.ParameterScans[0].VariantID != "baseline" || report.ParameterScans[0].ReplayedCount != 1 {
		t.Fatalf("unexpected baseline scan: %+v", report.ParameterScans[0])
	}
	if len(report.ParameterScans[0].SetupCounts) == 0 {
		t.Fatalf("expected baseline setup counts")
	}
}

func replayTestKlines(start float64, count int) []market.Kline {
	out := make([]market.Kline, 0, count)
	baseTime := time.Unix(1, 0).UTC()
	price := start
	for i := 0; i < count; i++ {
		if i%7 == 0 {
			price -= 2
		} else {
			price += 1.2
		}
		openTime := baseTime.Add(time.Duration(i) * 15 * time.Minute).UnixMilli()
		out = append(out, market.Kline{
			OpenTime:  openTime,
			CloseTime: openTime + int64(15*time.Minute/time.Millisecond),
			Open:      price - 0.4,
			High:      price + 0.8,
			Low:       price - 0.8,
			Close:     price,
			Volume:    1000 + float64(i),
		})
	}
	return out
}
