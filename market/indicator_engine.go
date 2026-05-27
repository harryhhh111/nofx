package market

import (
	"context"
	"fmt"
	"time"
)

// IndicatorEngine calculates deterministic indicators from already-fetched
// OHLCV data. It does not fetch data and does not call AI.
type IndicatorEngine interface {
	Calculate(ctx context.Context, input MarketInput, req IndicatorRequest) (*FactorSnapshot, error)
}

// DefaultIndicatorEngine is the first implementation boundary. It wraps the
// existing local calculations and can be expanded without changing callers.
type DefaultIndicatorEngine struct{}

func NewDefaultIndicatorEngine() *DefaultIndicatorEngine {
	return &DefaultIndicatorEngine{}
}

func (e *DefaultIndicatorEngine) Calculate(ctx context.Context, input MarketInput, req IndicatorRequest) (*FactorSnapshot, error) {
	if input.Symbol == "" {
		return nil, fmt.Errorf("market input symbol is required")
	}
	if len(input.Timeframes) == 0 {
		return nil, fmt.Errorf("market input timeframes are required")
	}
	if input.AsOf.IsZero() {
		input.AsOf = time.Now().UTC()
	}

	snapshot := &FactorSnapshot{
		Symbol:    input.Symbol,
		AsOf:      input.AsOf,
		Technical: map[string][]IndicatorPoint{},
		External:  map[string]ExternalFactor{},
	}

	for timeframe, klines := range input.Timeframes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if len(klines) == 0 {
			snapshot.Notes = append(snapshot.Notes, fmt.Sprintf("%s has no kline data", timeframe))
			continue
		}
		sourceTime := klineSourceTime(klines[len(klines)-1])
		snapshot.addTechnical("price", IndicatorPoint{
			Name:        "price",
			Timeframe:   timeframe,
			Value:       klines[len(klines)-1].Close,
			SourceTime:  sourceTime,
			AvailableAt: input.AsOf,
		})

		for _, period := range sanitizePeriods(req.EMAPeriods) {
			if len(klines) < period {
				return nil, fmt.Errorf("%s %s requires %d bars, got %d", input.Symbol, indicatorKey("ema", timeframe), period, len(klines))
			}
			snapshot.addTechnical("ema", IndicatorPoint{
				Name:        "ema",
				Timeframe:   timeframe,
				Period:      period,
				Value:       calculateEMA(klines, period),
				SourceTime:  sourceTime,
				AvailableAt: input.AsOf,
			})
		}

		for _, period := range sanitizePeriods(req.RSIPeriods) {
			if len(klines) <= period {
				return nil, fmt.Errorf("%s %s requires more than %d bars, got %d", input.Symbol, indicatorKey("rsi", timeframe), period, len(klines))
			}
			snapshot.addTechnical("rsi", IndicatorPoint{
				Name:        "rsi",
				Timeframe:   timeframe,
				Period:      period,
				Value:       calculateRSI(klines, period),
				SourceTime:  sourceTime,
				AvailableAt: input.AsOf,
			})
		}

		for _, period := range sanitizePeriods(req.ATRPeriods) {
			if len(klines) <= period {
				return nil, fmt.Errorf("%s %s requires more than %d bars, got %d", input.Symbol, indicatorKey("atr", timeframe), period, len(klines))
			}
			snapshot.addTechnical("atr", IndicatorPoint{
				Name:        "atr",
				Timeframe:   timeframe,
				Period:      period,
				Value:       calculateATR(klines, period),
				SourceTime:  sourceTime,
				AvailableAt: input.AsOf,
			})
		}

		for _, spec := range sanitizeBOLLSpecs(req.BOLLPeriods) {
			if len(klines) < spec.Period {
				return nil, fmt.Errorf("%s %s requires %d bars, got %d", input.Symbol, indicatorKey("boll", timeframe), spec.Period, len(klines))
			}
			upper, middle, lower := calculateBOLL(klines, spec.Period, spec.Multiplier)
			params := map[string]float64{"multiplier": spec.Multiplier}
			snapshot.addTechnical("boll_upper", IndicatorPoint{Name: "boll_upper", Timeframe: timeframe, Period: spec.Period, Params: params, Value: upper, SourceTime: sourceTime, AvailableAt: input.AsOf})
			snapshot.addTechnical("boll_middle", IndicatorPoint{Name: "boll_middle", Timeframe: timeframe, Period: spec.Period, Params: params, Value: middle, SourceTime: sourceTime, AvailableAt: input.AsOf})
			snapshot.addTechnical("boll_lower", IndicatorPoint{Name: "boll_lower", Timeframe: timeframe, Period: spec.Period, Params: params, Value: lower, SourceTime: sourceTime, AvailableAt: input.AsOf})
		}

		if req.MACD != nil {
			fast := req.MACD.Fast
			slow := req.MACD.Slow
			signal := req.MACD.Signal
			if fast <= 0 {
				return nil, fmt.Errorf("%s %s missing macd fast period", input.Symbol, indicatorKey("macd", timeframe))
			}
			if slow <= 0 {
				return nil, fmt.Errorf("%s %s missing macd slow period", input.Symbol, indicatorKey("macd", timeframe))
			}
			if signal <= 0 {
				return nil, fmt.Errorf("%s %s missing macd signal period", input.Symbol, indicatorKey("macd", timeframe))
			}
			if fast >= slow {
				return nil, fmt.Errorf("%s %s requires fast < slow", input.Symbol, indicatorKey("macd", timeframe))
			}
			if len(klines) < slow+signal {
				return nil, fmt.Errorf("%s %s requires at least %d bars, got %d", input.Symbol, indicatorKey("macd", timeframe), slow+signal, len(klines))
			}
			macd, signalLine, histogram := calculateMACDFull(klines, fast, slow, signal)
			snapshot.addTechnical("macd", IndicatorPoint{
				Name:      "macd",
				Timeframe: timeframe,
				Params: map[string]float64{
					"fast":   float64(fast),
					"slow":   float64(slow),
					"signal": float64(signal),
				},
				Value:       macd,
				SourceTime:  sourceTime,
				AvailableAt: input.AsOf,
			})
			snapshot.addTechnical("macd_signal", IndicatorPoint{Name: "macd_signal", Timeframe: timeframe, Params: map[string]float64{"fast": float64(fast), "slow": float64(slow), "signal": float64(signal)}, Value: signalLine, SourceTime: sourceTime, AvailableAt: input.AsOf})
			snapshot.addTechnical("macd_histogram", IndicatorPoint{Name: "macd_histogram", Timeframe: timeframe, Params: map[string]float64{"fast": float64(fast), "slow": float64(slow), "signal": float64(signal)}, Value: histogram, SourceTime: sourceTime, AvailableAt: input.AsOf})
		}

		for _, period := range sanitizePeriods(req.VWAPPeriods) {
			if len(klines) < period {
				return nil, fmt.Errorf("%s %s requires %d bars, got %d", input.Symbol, indicatorKey("vwap", timeframe), period, len(klines))
			}
			value := calculateVWAP(klines, period)
			if value <= 0 {
				return nil, fmt.Errorf("%s %s cannot be calculated without positive volume", input.Symbol, indicatorKey("vwap", timeframe))
			}
			snapshot.addTechnical("vwap", IndicatorPoint{Name: "vwap", Timeframe: timeframe, Period: period, Value: value, SourceTime: sourceTime, AvailableAt: input.AsOf})
		}

		for _, period := range sanitizePeriods(req.VolumePeriods) {
			if len(klines) < period {
				return nil, fmt.Errorf("%s %s requires %d bars, got %d", input.Symbol, indicatorKey("volume", timeframe), period, len(klines))
			}
			current := klines[len(klines)-1].Volume
			avg := calculateAverageVolume(klines, period)
			snapshot.addTechnical("volume", IndicatorPoint{Name: "volume", Timeframe: timeframe, Value: current, SourceTime: sourceTime, AvailableAt: input.AsOf})
			snapshot.addTechnical("volume_avg", IndicatorPoint{Name: "volume_avg", Timeframe: timeframe, Period: period, Value: avg, SourceTime: sourceTime, AvailableAt: input.AsOf})
			if avg > 0 {
				snapshot.addTechnical("volume_ratio", IndicatorPoint{Name: "volume_ratio", Timeframe: timeframe, Period: period, Value: current / avg, SourceTime: sourceTime, AvailableAt: input.AsOf})
			}
		}

		for _, period := range sanitizePeriods(req.DonchianPeriods) {
			if len(klines) < period {
				return nil, fmt.Errorf("%s %s requires %d bars, got %d", input.Symbol, indicatorKey("donchian", timeframe), period, len(klines))
			}
			upper, lower := calculateDonchian(klines, period)
			snapshot.addTechnical("donchian_upper", IndicatorPoint{Name: "donchian_upper", Timeframe: timeframe, Period: period, Value: upper, SourceTime: sourceTime, AvailableAt: input.AsOf})
			snapshot.addTechnical("donchian_lower", IndicatorPoint{Name: "donchian_lower", Timeframe: timeframe, Period: period, Value: lower, SourceTime: sourceTime, AvailableAt: input.AsOf})
		}

		for _, period := range sanitizePeriods(req.PriceChangeWindows) {
			if len(klines) <= period {
				return nil, fmt.Errorf("%s %s requires more than %d bars, got %d", input.Symbol, indicatorKey("price_change", timeframe), period, len(klines))
			}
			snapshot.addTechnical("price_change", IndicatorPoint{Name: "price_change", Timeframe: timeframe, Period: period, Params: map[string]float64{"bars": float64(period)}, Value: calculatePriceChangeWindow(klines, period), SourceTime: sourceTime, AvailableAt: input.AsOf})
		}

		for _, period := range sanitizePeriods(req.RealizedVolPeriods) {
			if len(klines) <= period {
				return nil, fmt.Errorf("%s %s requires more than %d bars, got %d", input.Symbol, indicatorKey("realized_vol", timeframe), period, len(klines))
			}
			snapshot.addTechnical("realized_vol", IndicatorPoint{Name: "realized_vol", Timeframe: timeframe, Period: period, Value: calculateRealizedVol(klines, period), SourceTime: sourceTime, AvailableAt: input.AsOf})
		}
	}

	return snapshot, nil
}

func indicatorKey(name, timeframe string) string {
	if timeframe == "" {
		return name
	}
	return name + "[" + timeframe + "]"
}

func (s *FactorSnapshot) addTechnical(name string, point IndicatorPoint) {
	if s.Technical == nil {
		s.Technical = map[string][]IndicatorPoint{}
	}
	s.Technical[name] = append(s.Technical[name], point)
}

func sanitizePeriods(periods []int) []int {
	out := make([]int, 0, len(periods))
	seen := map[int]bool{}
	for _, p := range periods {
		if p <= 0 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func sanitizeBOLLSpecs(specs []BOLLSpec) []BOLLSpec {
	out := make([]BOLLSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Period <= 0 {
			continue
		}
		if spec.Multiplier <= 0 {
			spec.Multiplier = 2
		}
		out = append(out, spec)
	}
	return out
}

func klineSourceTime(k Kline) time.Time {
	if k.CloseTime > 0 {
		return time.UnixMilli(k.CloseTime).UTC()
	}
	if k.OpenTime > 0 {
		return time.UnixMilli(k.OpenTime).UTC()
	}
	return time.Time{}
}
