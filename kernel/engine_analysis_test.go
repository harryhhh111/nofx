package kernel

import (
	"testing"

	"nofx/store"
)

func TestStructureRequestIncludesScoringRoleTimeframes(t *testing.T) {
	config := store.GetDefaultStrategyConfig("zh")
	config.Indicators.Klines.PrimaryTimeframe = "15m"
	config.Indicators.Klines.EntryTimeframe = "5m"
	config.Indicators.Klines.ConfirmationTimeframes = []string{"1h"}
	config.Indicators.Klines.SelectedTimeframes = []string{"5m", "15m", "1h"}
	config.ScoringConfig = &store.ScoringStrategyConfig{}
	config.ScoringConfig.Enabled = true
	config.ScoringConfig.SelectedFactors = []string{"trend", "momentum", "structure", "derivatives"}
	config.ClampLimits()

	req := StructureRequestFromStrategyConfig(&config)

	fibTimeframes := map[string]bool{}
	if req.Fibonacci != nil {
		fibTimeframes[req.Fibonacci.Timeframe] = true
	}
	for _, fib := range req.Fibonaccis {
		fibTimeframes[fib.Timeframe] = true
	}
	supportTimeframes := map[string]bool{}
	if req.Support != nil {
		supportTimeframes[req.Support.Timeframe] = true
	}
	for _, support := range req.Supports {
		supportTimeframes[support.Timeframe] = true
	}

	for _, timeframe := range []string{"15m", "5m", "1h"} {
		if !fibTimeframes[timeframe] {
			t.Fatalf("expected fibonacci structure request for %s, got %+v", timeframe, req)
		}
		if !supportTimeframes[timeframe] {
			t.Fatalf("expected support/resistance structure request for %s, got %+v", timeframe, req)
		}
	}
}
