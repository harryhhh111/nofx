package market

import (
	"fmt"
	"time"
)

// CalcContext holds the input data for a single indicator calculation on one timeframe.
type CalcContext struct {
	Klines     []Kline
	Timeframe  string
	AsOf       time.Time
	SourceTime time.Time
	Symbol     string
}

// Module is the interface that every indicator module must implement.
// Adding a new indicator means creating a new file that implements this interface
// and registering it in indicator_engine.go — no existing files need to change.
type Module interface {
	// Name returns the human-readable indicator name.
	Name() string

	// Calculate computes indicator values for the given context and request.
	// It should return an empty slice (not error) when the indicator is not
	// requested (e.g. config is nil or empty).
	Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error)
}

// ---------------------------------------------------------------------------
// Helper functions used by all modules
// ---------------------------------------------------------------------------

func indicatorKey(name, timeframe string) string {
	if timeframe == "" {
		return name
	}
	return name + "[" + timeframe + "]"
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

// requireBars returns an error when there are fewer bars than needed.
func requireBars(symbol, name string, have, need int) error {
	if have < need {
		return fmt.Errorf("%s %s requires %d bars, got %d", symbol, name, need, have)
	}
	return nil
}

// requireBarsExclusive returns an error when there are need or fewer bars.
func requireBarsExclusive(symbol, name string, have, need int) error {
	if have <= need {
		return fmt.Errorf("%s %s requires more than %d bars, got %d", symbol, name, need, have)
	}
	return nil
}

func addTechnicalToSnapshot(s *FactorSnapshot, name string, point IndicatorPoint) {
	if s.Technical == nil {
		s.Technical = map[string][]IndicatorPoint{}
	}
	s.Technical[name] = append(s.Technical[name], point)
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
