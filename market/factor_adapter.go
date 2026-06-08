package market

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BuildFactorSnapshotFromData converts legacy *Data into a structured FactorSnapshot.
// It is purely read-only and does not modify Data.
func BuildFactorSnapshotFromData(data *Data) *FactorSnapshot {
	if data == nil {
		return nil
	}

	snap := &FactorSnapshot{
		Symbol:    data.Symbol,
		AsOf:      time.Now().UTC(),
		Technical: make(map[string][]IndicatorPoint),
	}

	// ── Symbol-level scalars ──
	addPoint(snap, "price", data.CurrentPrice, "", 0)
	if data.CurrentEMA20 > 0 {
		addPoint(snap, "ema20", data.CurrentEMA20, "", 0)
	}
	if data.CurrentMACD != 0 {
		addPoint(snap, "macd", data.CurrentMACD, "", 0)
	}
	if data.CurrentRSI7 > 0 {
		addPoint(snap, "rsi7", data.CurrentRSI7, "", 0)
	}
	if data.CurrentADX > 0 {
		addPoint(snap, "adx", data.CurrentADX, "", 0)
		addPoint(snap, "plus_di", data.CurrentPlusDI, "", 0)
		addPoint(snap, "minus_di", data.CurrentMinusDI, "", 0)
	}
	if data.CurrentSAR > 0 {
		addPoint(snap, "sar", data.CurrentSAR, "", 0)
		addPointBool(snap, "sar_uptrend", data.SARIsUptrend, "")
		addPointBool(snap, "sar_flip_up", data.SARFlipUp, "")
		addPointBool(snap, "sar_flip_down", data.SARFlipDown, "")
	}
	for period, val := range data.CurrentSMA {
		addPoint(snap, "sma", val, "", period)
	}

	// ── Per-timeframe indicator last values ──
	for tf, ts := range data.TimeframeData {
		if ts == nil {
			continue
		}
		if len(ts.EMA20Values) > 0 {
			addPoint(snap, "ema20", ts.EMA20Values[len(ts.EMA20Values)-1], tf, 20)
		}
		if len(ts.EMA50Values) > 0 {
			addPoint(snap, "ema50", ts.EMA50Values[len(ts.EMA50Values)-1], tf, 50)
		}
		if len(ts.ADXValues) > 0 {
			addPoint(snap, "adx", ts.ADXValues[len(ts.ADXValues)-1], tf, 0)
			addPoint(snap, "plus_di", ts.PlusDIValues[len(ts.PlusDIValues)-1], tf, 0)
			addPoint(snap, "minus_di", ts.MinusDIValues[len(ts.MinusDIValues)-1], tf, 0)
		}
		if len(ts.SARValues) > 0 {
			addPoint(snap, "sar", ts.SARValues[len(ts.SARValues)-1], tf, 0)
			if len(ts.SARUptrend) > 0 {
				addPointBool(snap, "sar_uptrend", ts.SARUptrend[len(ts.SARUptrend)-1], tf)
			}
			if len(ts.SARFlipUp) > 0 {
				addPointBool(snap, "sar_flip_up", ts.SARFlipUp[len(ts.SARFlipUp)-1], tf)
			}
			if len(ts.SARFlipDown) > 0 {
				addPointBool(snap, "sar_flip_down", ts.SARFlipDown[len(ts.SARFlipDown)-1], tf)
			}
			addPoint(snap, "sar_af", ts.SARAF, tf, 0)
		}
		if len(ts.MACDValues) > 0 {
			addPoint(snap, "macd", ts.MACDValues[len(ts.MACDValues)-1], tf, 0)
		}
		if len(ts.RSI7Values) > 0 {
			addPoint(snap, "rsi7", ts.RSI7Values[len(ts.RSI7Values)-1], tf, 7)
		}
		if len(ts.RSI14Values) > 0 {
			addPoint(snap, "rsi14", ts.RSI14Values[len(ts.RSI14Values)-1], tf, 14)
		}
		for period, vals := range ts.SMAValues {
			if len(vals) > 0 {
				addPoint(snap, "sma", vals[len(vals)-1], tf, period)
			}
		}
		if ts.ATR14 > 0 {
			addPoint(snap, "atr14", ts.ATR14, tf, 14)
		}
		if len(ts.BOLLUpper) > 0 && len(ts.BOLLMiddle) > 0 && len(ts.BOLLLower) > 0 {
			n := len(ts.BOLLUpper) - 1
			addPoint(snap, "boll_upper", ts.BOLLUpper[n], tf, 0)
			addPoint(snap, "boll_middle", ts.BOLLMiddle[n], tf, 0)
			addPoint(snap, "boll_lower", ts.BOLLLower[n], tf, 0)
		}
	}

	return snap
}

func addPoint(snap *FactorSnapshot, name string, val float64, tf string, period int) {
	key := indicatorPointKey(name, tf, period)
	snap.Technical[key] = append(snap.Technical[key], IndicatorPoint{
		Name:      name,
		Timeframe: tf,
		Period:    period,
		Value:     val,
	})
}

func addPointBool(snap *FactorSnapshot, name string, b bool, tf string) {
	v := 0.0
	if b {
		v = 1.0
	}
	key := indicatorPointKey(name, tf, 0)
	snap.Technical[key] = append(snap.Technical[key], IndicatorPoint{
		Name:      name,
		Timeframe: tf,
		Value:     v,
	})
}

// indicatorPointKey builds a lookup key: name[_period][_timeframe].
// Symbol-level scalars (tf=""): "ema20", "sma_50".
// Per-timeframe values: "ema20_5m", "sma_50_1h".
func indicatorPointKey(name, tf string, period int) string {
	if period > 0 && tf != "" {
		return fmt.Sprintf("%s_%d_%s", name, period, tf)
	}
	if tf != "" {
		return name + "_" + tf
	}
	if period > 0 {
		return fmt.Sprintf("%s_%d", name, period)
	}
	return name
}

// IndicatorValue looks up a single indicator value from the snapshot.
// timeframe="" matches any timeframe (first found); period=0 matches any period.
func (s *FactorSnapshot) IndicatorValue(name, timeframe string, period int) (float64, bool) {
	if s == nil || s.Technical == nil {
		return 0, false
	}
	for key, points := range s.Technical {
		// Match by name or by key prefix
		if key != name && !strings.HasPrefix(key, name+"_") && !strings.HasPrefix(key, name) {
			continue
		}
		for _, p := range points {
			if timeframe != "" && p.Timeframe != timeframe {
				continue
			}
			if period > 0 && p.Period != period && p.Period != 0 {
				continue
			}
			return p.Value, true
		}
		// If no timeframe match, return the first empty-timeframe match
		for _, p := range points {
			if p.Timeframe == "" {
				if period > 0 && p.Period != period && p.Period != 0 {
					continue
				}
				return p.Value, true
			}
		}
	}
	return 0, false
}

// ── Engine-based adapter ──────────────────────────────────────────────────

// BuildFactorSnapshotWithEngine uses the modular IndicatorEngine to compute
// indicators from raw klines instead of extracting last values from arrays.
func BuildFactorSnapshotWithEngine(data *Data, req IndicatorRequest) *FactorSnapshot {
	if data == nil {
		return nil
	}

	input := MarketInput{
		Symbol:     data.Symbol,
		Timeframes: make(map[string][]Kline),
		AsOf:       time.Now().UTC(),
	}
	for tf, ts := range data.TimeframeData {
		if ts == nil {
			continue
		}
		if len(ts.ComputeBars) > 0 {
			input.Timeframes[tf] = ts.ComputeBars
		} else {
			input.Timeframes[tf] = klineBarsToKlines(ts.Klines)
		}
	}
	if len(input.Timeframes) == 0 {
		return nil
	}

	snap, err := NewDefaultIndicatorEngine().Calculate(context.Background(), input, req)
	if err != nil {
		return nil
	}

	// Inject non-OHLCV factors
	snap.AsOf = time.Now().UTC()
	if data.CurrentPrice > 0 {
		addPoint(snap, "price", data.CurrentPrice, "", 0)
	}
	if data.OpenInterest != nil {
		addPoint(snap, "oi_latest", data.OpenInterest.Latest, "", 0)
	}
	if data.FundingRate != 0 {
		addPoint(snap, "funding_rate", data.FundingRate, "", 0)
	}

	return snap
}

// klineBarsToKlines converts []KlineBar (prompt-ready) to []Kline (calc-ready).
func klineBarsToKlines(bars []KlineBar) []Kline {
	klines := make([]Kline, len(bars))
	for i, bar := range bars {
		klines[i] = Kline{
			OpenTime: bar.Time, Open: bar.Open,
			High: bar.High, Low: bar.Low,
			Close: bar.Close, Volume: bar.Volume,
		}
	}
	return klines
}
