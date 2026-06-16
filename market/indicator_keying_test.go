package market

import (
	"context"
	"math"
	"testing"
	"time"
)

// TestMultiOutputIndicatorsAddressable guards a real bug: multi-output indicator
// modules (ADX -> adx/plus_di/minus_di, MACD -> macd/macd_signal/macd_histogram)
// must be individually addressable by name. Previously every point was stored
// under the module name, so plus_di/minus_di/macd_signal/macd_histogram were
// unreachable and IndicatorValue("adx") returned whichever sub-point sorted
// last (minus_di) instead of the ADX value. Offline / synthetic, no network.
func TestMultiOutputIndicatorsAddressable(t *testing.T) {
	klines := syntheticTrend(160)
	input := MarketInput{
		Symbol:     "TESTUSDT",
		Timeframes: map[string][]Kline{"15m": klines},
		AsOf:       time.UnixMilli(klines[len(klines)-1].OpenTime).UTC(),
	}
	req := IndicatorRequest{
		EMAPeriods: []int{20, 50},
		ADX:        &ADXSpec{Period: 14},
		MACD:       &MACDSpec{Fast: 12, Slow: 26, Signal: 9},
	}
	snap, err := NewDefaultIndicatorEngine().Calculate(context.Background(), input, req)
	if err != nil {
		t.Fatal(err)
	}

	mustGet := func(name string, period int) float64 {
		v, ok := snap.IndicatorValue(name, "15m", period)
		if !ok {
			t.Fatalf("indicator %q (period %d) is not addressable by name", name, period)
		}
		return v
	}

	adx := mustGet("adx", 14)
	plusDI := mustGet("plus_di", 14)
	minusDI := mustGet("minus_di", 14)
	mustGet("macd", 0)
	mustGet("macd_signal", 0)
	hist := mustGet("macd_histogram", 0)

	// The defining symptom of the old bug: adx == minus_di (the last sub-point).
	if math.Abs(adx-minusDI) < 1e-9 {
		t.Fatalf("adx (%v) equals minus_di (%v): name-collision bug regressed", adx, minusDI)
	}
	// In a clean uptrend +DI should dominate -DI; sanity check the DI wiring.
	if plusDI <= minusDI {
		t.Fatalf("expected +DI > -DI in a synthetic uptrend, got +DI=%v -DI=%v", plusDI, minusDI)
	}
	_ = hist
}

// TestVolumeSpikeIndicatorsAddressable verifies that volume spike outputs are
// stored under their own indicator names and can be retrieved independently.
func TestVolumeSpikeIndicatorsAddressable(t *testing.T) {
	klines := syntheticTrend(160)
	// Inject a past volume spike so last_volume_spike_high is defined.
	// findLastVolumeSpikeHigh excludes the current bar, so the spike must be
	// before the final bar.
	pastSpikeIdx := len(klines) - 5
	klines[pastSpikeIdx].Volume = 100000
	klines[pastSpikeIdx].High = 999.0
	// Inject a current volume spike so volume_spike triggers.
	klines[len(klines)-1].Volume = 100000
	input := MarketInput{
		Symbol:     "TESTUSDT",
		Timeframes: map[string][]Kline{"15m": klines},
		AsOf:       time.UnixMilli(klines[len(klines)-1].OpenTime).UTC(),
	}
	req := IndicatorRequest{
		VolumePeriods:         []int{20},
		VolumeSpikeMultiplier: 4.0,
	}
	snap, err := NewDefaultIndicatorEngine().Calculate(context.Background(), input, req)
	if err != nil {
		t.Fatal(err)
	}

	mustGet := func(name string, period int) float64 {
		v, ok := snap.IndicatorValue(name, "15m", period)
		if !ok {
			t.Fatalf("indicator %q (period %d) is not addressable by name", name, period)
		}
		return v
	}

	mustGet("volume", 0)
	mustGet("volume_avg", 20)
	mustGet("volume_ratio", 20)
	mustGet("volume_spike", 20)
	spikeHigh := mustGet("last_volume_spike_high", 20)
	if math.Abs(spikeHigh-999.0) > 1e-9 {
		t.Fatalf("expected last_volume_spike_high=999.0, got %v", spikeHigh)
	}
	mustGet("break_last_volume_spike_high", 20)
}

// TestMTSIIndicatorsAddressable verifies that MTSI sub-outputs are stored under
// their own names and can be retrieved independently.
func TestMTSIIndicatorsAddressable(t *testing.T) {
	klines := syntheticTrend(160)
	input := MarketInput{
		Symbol:     "TESTUSDT",
		Timeframes: map[string][]Kline{"15m": klines},
		AsOf:       time.UnixMilli(klines[len(klines)-1].OpenTime).UTC(),
	}
	req := IndicatorRequest{
		VWAPPeriods: []int{20},
		EnableMTSI:  true,
	}
	snap, err := NewDefaultIndicatorEngine().Calculate(context.Background(), input, req)
	if err != nil {
		t.Fatal(err)
	}

	mustGet := func(name string, period int) float64 {
		v, ok := snap.IndicatorValue(name, "15m", period)
		if !ok {
			t.Fatalf("indicator %q (period %d) is not addressable by name", name, period)
		}
		return v
	}

	mustGet("mtsi", 20)
	mustGet("mtsi_abs", 20)
	mustGet("close_vwap_distance_pct", 20)
	mustGet("close_above_vwap", 20)
	mustGet("close_below_vwap", 20)
}

// TestPriceChangeNamedWindowsAddressable verifies that named-window returns and
// first-cross signals are stored under their own names.
func TestPriceChangeNamedWindowsAddressable(t *testing.T) {
	klines := syntheticTrend(160)
	input := MarketInput{
		Symbol:     "TESTUSDT",
		Timeframes: map[string][]Kline{"15m": klines},
		AsOf:       time.UnixMilli(klines[len(klines)-1].OpenTime).UTC(),
	}
	req := IndicatorRequest{
		PriceChangeWindows:      []int{12},
		PriceChangeNamedWindows: []string{"1h", "4h"},
	}
	snap, err := NewDefaultIndicatorEngine().Calculate(context.Background(), input, req)
	if err != nil {
		t.Fatal(err)
	}

	mustGet := func(name string, period int) float64 {
		v, ok := snap.IndicatorValue(name, "15m", period)
		if !ok {
			t.Fatalf("indicator %q (period %d) is not addressable by name", name, period)
		}
		return v
	}

	mustGet("price_change", 12)
	mustGet("return_1h", 4)  // 60/15
	mustGet("return_4h", 16) // 240/15
}

// syntheticTrend builds n ascending bars with intrabar range, enough for ADX(14)
// and MACD(26,9) to be well-defined.
func syntheticTrend(n int) []Kline {
	out := make([]Kline, 0, n)
	base := 100.0
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		open := base + float64(i)
		closePx := open + 0.6
		high := closePx + 0.4
		low := open - 0.3
		ot := t0.Add(time.Duration(i) * 15 * time.Minute).UnixMilli()
		out = append(out, Kline{
			OpenTime:  ot,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePx,
			Volume:    1000,
			CloseTime: ot + 15*60*1000 - 1,
		})
	}
	return out
}
