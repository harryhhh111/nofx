package market

import (
	"context"
	"testing"
	"time"
)

func TestMarketStructureDetectsBreakoutSetup(t *testing.T) {
	klines := testStructureKlines([]float64{
		100, 105, 98, 110, 102, 116, 108, 122, 118, 125,
		121, 127, 123, 130, 126, 134, 129, 136, 132, 140,
	})
	input := MarketInput{
		Symbol: "BTCUSDT",
		Timeframes: map[string][]Kline{
			"15m": klines,
		},
		AsOf: time.Unix(1, 0).UTC(),
	}
	req := StructureRequest{MarketStructure: &MarketStructureRequest{
		Timeframe:           "15m",
		Lookback:            len(klines),
		SwingWindow:         1,
		MinLegBars:          1,
		MinLegATRMultiple:   0.1,
		ZigZagThresholdPct:  0.1,
		BreakoutBufferATR:   0.1,
		RetestToleranceATR:  1,
		ExhaustionRSIPeriod: 14,
	}}

	snapshots, err := NewDefaultStructureEngine().Calculate(context.Background(), input, req)
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	setup, ok := findStructureSnapshot(snapshots, "setup")
	if !ok {
		t.Fatalf("expected setup snapshot, got %+v", snapshots)
	}
	if !setup.Valid || setup.Setup != "breakout_long" || setup.Direction != "long" {
		t.Fatalf("expected breakout_long setup, got %+v", setup)
	}
	structure, ok := findStructureSnapshot(snapshots, "market_structure")
	if !ok || structure.Phase == "" || structure.CurrentLeg == nil {
		t.Fatalf("expected market structure phase and current leg, got %+v", structure)
	}
}

func TestMarketStructureDetectsFailedBreakout(t *testing.T) {
	klines := testStructureKlines([]float64{
		100, 105, 98, 110, 102, 116, 108, 122, 118, 121,
		117, 123, 119, 124, 120, 126, 121, 127, 123, 124,
	})
	klines[len(klines)-2].High = 132
	klines[len(klines)-1].Close = 124
	input := MarketInput{
		Symbol: "BTCUSDT",
		Timeframes: map[string][]Kline{
			"15m": klines,
		},
		AsOf: time.Unix(1, 0).UTC(),
	}
	req := StructureRequest{MarketStructure: &MarketStructureRequest{
		Timeframe:           "15m",
		Lookback:            len(klines),
		SwingWindow:         1,
		MinLegBars:          1,
		MinLegATRMultiple:   0.1,
		ZigZagThresholdPct:  0.1,
		BreakoutBufferATR:   0.1,
		RetestToleranceATR:  1,
		ExhaustionRSIPeriod: 14,
	}}

	snapshots, err := NewDefaultStructureEngine().Calculate(context.Background(), input, req)
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	setup, ok := findStructureSnapshot(snapshots, "setup")
	if !ok {
		t.Fatalf("expected setup snapshot, got %+v", snapshots)
	}
	if !setup.Valid || setup.Setup != "failed_breakout_short" || setup.Direction != "short" {
		t.Fatalf("expected failed_breakout_short setup, got %+v", setup)
	}
}

func TestDetectSetupWaitsForTrendContinuationTrigger(t *testing.T) {
	req := MarketStructureRequest{
		Timeframe:           "15m",
		Lookback:            20,
		SwingWindow:         1,
		MinLegBars:          1,
		MinLegATRMultiple:   1,
		ZigZagThresholdPct:  0.1,
		BreakoutBufferATR:   0.2,
		RetestToleranceATR:  1,
		ExhaustionRSIPeriod: 14,
	}
	klines := testStructureKlines([]float64{113, 112, 111, 112})
	structure := StructureSnapshot{
		Name:      "market_structure",
		Timeframe: "15m",
		Valid:     true,
		Direction: "up",
		Phase:     "middle",
		KeyLevels: map[string]float64{
			"support":    100,
			"resistance": 125,
		},
		Evidence: map[string]float64{"rsi": 55},
		CurrentLeg: &StructureLeg{
			Direction:   "down",
			ATRMultiple: 1,
		},
		Confirmed: true,
	}

	setup := detectSetup(structure, 112, klines, 2, req)
	if setup.Valid || setup.Setup != "no_trade_wait_trigger" || setup.Direction != "long" {
		t.Fatalf("expected trend context to wait for trigger, got %+v", setup)
	}
}

func TestDetectSetupAllowsLightTrendContinuationTrigger(t *testing.T) {
	req := MarketStructureRequest{
		Timeframe:           "15m",
		Lookback:            20,
		SwingWindow:         1,
		MinLegBars:          1,
		MinLegATRMultiple:   1,
		ZigZagThresholdPct:  0.1,
		BreakoutBufferATR:   0.2,
		RetestToleranceATR:  1,
		ExhaustionRSIPeriod: 14,
	}
	klines := testStructureKlines([]float64{109, 110, 111, 112})
	structure := StructureSnapshot{
		Name:      "market_structure",
		Timeframe: "15m",
		Valid:     true,
		Direction: "up",
		Phase:     "middle",
		KeyLevels: map[string]float64{
			"support":    100,
			"resistance": 125,
		},
		Evidence: map[string]float64{"rsi": 55},
		CurrentLeg: &StructureLeg{
			Direction:   "up",
			ATRMultiple: 0.5,
		},
		Confirmed: true,
	}

	setup := detectSetup(structure, 112, klines, 2, req)
	if !setup.Valid || setup.Setup != "trend_continuation_long" || setup.Direction != "long" {
		t.Fatalf("expected light continuation trigger, got %+v", setup)
	}
}

func testStructureKlines(closes []float64) []Kline {
	out := make([]Kline, 0, len(closes))
	start := time.Unix(1, 0).UTC()
	for i, close := range closes {
		ts := start.Add(time.Duration(i) * time.Minute).UnixMilli()
		out = append(out, Kline{
			OpenTime:  ts,
			Open:      close,
			High:      close + 0.5,
			Low:       close - 0.5,
			Close:     close,
			CloseTime: ts,
		})
	}
	return out
}

func findStructureSnapshot(snapshots []StructureSnapshot, name string) (StructureSnapshot, bool) {
	for _, snapshot := range snapshots {
		if snapshot.Name == name {
			return snapshot, true
		}
	}
	return StructureSnapshot{}, false
}
