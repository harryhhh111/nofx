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

// DefaultIndicatorEngine is the modular implementation.
// Each indicator lives in its own file (indicator_*.go) and registers here.
// Adding a new indicator does not change this file.
type DefaultIndicatorEngine struct {
	modules []Module
}

// NewDefaultIndicatorEngine returns the standard engine with all built-in modules.
func NewDefaultIndicatorEngine() *DefaultIndicatorEngine {
	return &DefaultIndicatorEngine{
		modules: []Module{
			&EMAModule{},
			&SMAModule{},
			&RSIModule{},
			&ATRModule{},
			&ADXModule{},
			&SARModule{},
			&BOLLModule{},
			&MACDModule{},
			&VWAPModule{},
			&VolumeModule{},
			&DonchianModule{},
			&PriceChangeModule{},
			&RealizedVolModule{},
			&SessionModule{},
		},
	}
}

// NewIndicatorEngineWithModules allows custom module selection (e.g. for testing).
func NewIndicatorEngineWithModules(modules ...Module) *DefaultIndicatorEngine {
	return &DefaultIndicatorEngine{modules: modules}
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
		addTechnicalToSnapshot(snapshot, "price", IndicatorPoint{
			Name:        "price",
			Timeframe:   timeframe,
			Value:       klines[len(klines)-1].Close,
			SourceTime:  sourceTime,
			AvailableAt: input.AsOf,
		})

		calcCtx := CalcContext{
			Klines:     klines,
			Timeframe:  timeframe,
			AsOf:       input.AsOf,
			SourceTime: sourceTime,
			Symbol:     input.Symbol,
		}

		for _, mod := range e.modules {
			points, err := mod.Calculate(calcCtx, req)
			if err != nil {
				return nil, err
			}
			key := mod.Name()
			for _, p := range points {
				addTechnicalToSnapshot(snapshot, key, p)
			}
		}
	}

	return snapshot, nil
}


