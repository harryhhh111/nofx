package kernel

import (
	"strings"
	"testing"

	"nofx/store"
)

func TestScoringStrategyVersionUsesConfigFingerprint(t *testing.T) {
	config := versionedScoringConfig()

	scoring := scoringFromStrategyConfig(config)
	if scoring == nil {
		t.Fatal("expected scoring strategy")
	}
	if !strings.HasPrefix(scoring.Version, "cfg_") {
		t.Fatalf("expected config fingerprint version, got %q", scoring.Version)
	}

	first := scoring.Version
	scoringAgain := scoringFromStrategyConfig(config)
	if scoringAgain.Version != first {
		t.Fatalf("expected deterministic fingerprint, got %q then %q", first, scoringAgain.Version)
	}

	config.ScoringConfig.LongThreshold = 68
	changed := scoringFromStrategyConfig(config)
	if changed.Version == first {
		t.Fatalf("expected fingerprint to change after strategy config change, still got %q", changed.Version)
	}
}

func versionedScoringConfig() *store.StrategyConfig {
	return &store.StrategyConfig{
		StrategyMode:      "scoring",
		StrategyArchetype: "trend_following",
		RiskProfile:       "balanced",
		Indicators: store.IndicatorConfig{
			Klines: store.KlineConfig{
				PrimaryTimeframe: "15m",
				EntryTimeframe:   "5m",
			},
			RealizedVolPeriods: []int{20},
		},
		RiskControl: store.RiskControlConfig{
			BTCETHMaxLeverage:  3,
			StopLossATRBuffer:  2,
			MinRiskRewardRatio: 2,
		},
		ScoringConfig: &store.ScoringStrategyConfig{
			Enabled:                 true,
			SelectedFactors:         []string{"trend", "momentum", "structure"},
			FactorWeights:           map[string]float64{"trend": 0.4, "momentum": 0.3, "structure": 0.3},
			LongThreshold:           65,
			ShortThreshold:          -65,
			MinAvailableWeightRatio: 0.5,
			MinConfidence:           55,
			Timeframe:               "15m",
			Execution: store.CompiledRuleExecution{
				Leverage:        2,
				PositionSizeUSD: 100,
				Confidence:      60,
			},
		},
	}
}
