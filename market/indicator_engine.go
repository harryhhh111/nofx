package market

import (
	"context"
	"fmt"
	"time"
)

// ── IndicatorEngine ───────────────────────────────────────────────────────

// IndicatorEngine calculates indicators from already-fetched OHLCV data.
type IndicatorEngine interface {
	Calculate(ctx context.Context, input MarketInput, req IndicatorRequest) (*FactorSnapshot, error)
}

// DefaultIndicatorEngine is the modular implementation.
// Each indicator lives in its own file and registers via NewDefaultIndicatorEngine.
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
		},
	}
}

// NewIndicatorEngineWithModules allows custom module selection for testing.
func NewIndicatorEngineWithModules(modules ...Module) *DefaultIndicatorEngine {
	return &DefaultIndicatorEngine{modules: modules}
}

// Calculate runs all registered modules for every timeframe and collects results.
func (e *DefaultIndicatorEngine) Calculate(ctx context.Context, input MarketInput, req IndicatorRequest) (*FactorSnapshot, error) {
	if input.Symbol == "" {
		return nil, fmt.Errorf("market input symbol is required")
	}
	if len(input.Timeframes) == 0 {
		return nil, fmt.Errorf("market input timeframes is empty for %s", input.Symbol)
	}
	if input.AsOf.IsZero() {
		input.AsOf = time.Now().UTC()
	}

	snap := &FactorSnapshot{
		Symbol:    input.Symbol,
		AsOf:      input.AsOf,
		Technical: make(map[string][]IndicatorPoint),
	}

	for tf, klines := range input.Timeframes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(klines) == 0 {
			continue
		}

		// Inject current price
		lastClose := klines[len(klines)-1].Close
		sourceTime := klineSourceTime(klines[len(klines)-1])
		addPointToSnapshot(snap, "price", IndicatorPoint{
			Name: "price", Timeframe: tf, Value: lastClose,
		})

		calcCtx := CalcContext{
			Klines:     klines,
			Timeframe:  tf,
			AsOf:       input.AsOf,
			SourceTime: sourceTime,
			Symbol:     input.Symbol,
		}

		for _, mod := range e.modules {
			points, err := mod.Calculate(calcCtx, req)
			if err != nil {
				return nil, fmt.Errorf("%s %s[%s]: %w", input.Symbol, mod.Name(), tf, err)
			}
			for _, p := range points {
				name := p.Name
				if name == "" {
					name = mod.Name()
				}
				addPointToSnapshot(snap, name, p)
			}
		}
	}

	return snap, nil
}

// addPointToSnapshot appends a point to the snapshot's Technical map.
func addPointToSnapshot(snap *FactorSnapshot, name string, p IndicatorPoint) {
	if snap.Technical == nil {
		snap.Technical = make(map[string][]IndicatorPoint)
	}
	snap.Technical[name] = append(snap.Technical[name], p)
}
