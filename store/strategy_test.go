package store

import "testing"

func TestClampLimitsNormalizesSingleCoinSourceFlags(t *testing.T) {
	config := StrategyConfig{
		StrategyMode: "scoring",
		CoinSource: CoinSourceConfig{
			SourceType:   "static",
			StaticCoins:  []string{"BTCUSDT"},
			UseAI500:     true,
			UseOITop:     true,
			UseOILow:     true,
			UseHyperAll:  true,
			UseHyperMain: true,
		},
	}

	config.ClampLimits()

	if config.CoinSource.UseAI500 || config.CoinSource.UseOITop || config.CoinSource.UseOILow ||
		config.CoinSource.UseHyperAll || config.CoinSource.UseHyperMain {
		t.Fatalf("static source should clear external source flags: %+v", config.CoinSource)
	}
}

func TestClampLimitsPreservesMixedCoinSourceFlags(t *testing.T) {
	config := StrategyConfig{
		StrategyMode: "scoring",
		CoinSource: CoinSourceConfig{
			SourceType: "mixed",
			UseAI500:   true,
			UseOITop:   true,
			UseOILow:   false,
		},
	}

	config.ClampLimits()

	if !config.CoinSource.UseAI500 || !config.CoinSource.UseOITop || config.CoinSource.UseOILow {
		t.Fatalf("mixed source should preserve selected source flags: %+v", config.CoinSource)
	}
}

func TestClampLimitsBackfillsRiskControlDefaults(t *testing.T) {
	config := StrategyConfig{}

	config.ClampLimits()

	if config.RiskControl.MaxPositions != MaxPositions {
		t.Fatalf("expected max positions default %d, got %d", MaxPositions, config.RiskControl.MaxPositions)
	}
	if config.RiskControl.MinRiskRewardRatio != DefaultMinRiskRewardRatio {
		t.Fatalf("expected min risk reward default %.2f, got %.2f", DefaultMinRiskRewardRatio, config.RiskControl.MinRiskRewardRatio)
	}
}

func TestClampLimitsDefaultsEntryTimeframeToPrimary(t *testing.T) {
	config := StrategyConfig{}
	config.Indicators.Klines.PrimaryTimeframe = "15m"
	config.Indicators.Klines.SelectedTimeframes = []string{"5m", "15m", "1h"}

	config.ClampLimits()

	if config.Indicators.Klines.EntryTimeframe != "15m" {
		t.Fatalf("expected entry timeframe to default to primary 15m, got %s", config.Indicators.Klines.EntryTimeframe)
	}
	if len(config.Indicators.Klines.ConfirmationTimeframes) != 1 || config.Indicators.Klines.ConfirmationTimeframes[0] != "1h" {
		t.Fatalf("expected default confirmations to use higher timeframe only, got %+v", config.Indicators.Klines.ConfirmationTimeframes)
	}
}

func TestDefaultStrategyConfigUsesPrimaryAsEntryTimeframe(t *testing.T) {
	config := GetDefaultStrategyConfig("zh")
	config.ClampLimits()

	if config.Indicators.Klines.PrimaryTimeframe != "15m" || config.Indicators.Klines.EntryTimeframe != "15m" {
		t.Fatalf("expected default strategy primary/entry 15m, got primary=%s entry=%s", config.Indicators.Klines.PrimaryTimeframe, config.Indicators.Klines.EntryTimeframe)
	}
	if len(config.Indicators.Klines.SelectedTimeframes) != 2 ||
		config.Indicators.Klines.SelectedTimeframes[0] != "15m" ||
		config.Indicators.Klines.SelectedTimeframes[1] != "1h" {
		t.Fatalf("expected default selected timeframes [15m 1h], got %+v", config.Indicators.Klines.SelectedTimeframes)
	}
}

func TestStrategyTemplatesAreExecutableScoringConfigs(t *testing.T) {
	templates := ListStrategyTemplates("zh")
	if len(templates) == 0 {
		t.Fatal("expected strategy templates")
	}

	for _, template := range templates {
		config := template.Config
		if template.ID == "" || template.Name == "" || template.Archetype == "" || template.RiskProfile == "" {
			t.Fatalf("template metadata is incomplete: %+v", template)
		}
		if config.StrategyArchetype != template.Archetype || config.RiskProfile != template.RiskProfile {
			t.Fatalf("template config metadata mismatch for %s: %+v", template.ID, config)
		}
		if config.StrategyMode != "scoring" || !config.ScoringConfig.Enabled {
			t.Fatalf("template %s should be an enabled scoring strategy: %+v", template.ID, config.ScoringConfig)
		}
		if config.ScoringConfig.LongThreshold <= 0 || config.ScoringConfig.ShortThreshold >= 0 {
			t.Fatalf("template %s has invalid thresholds: %+v", template.ID, config.ScoringConfig)
		}
		weightSum := 0.0
		for _, factor := range config.ScoringConfig.SelectedFactors {
			weightSum += config.ScoringConfig.FactorWeights[factor]
		}
		if weightSum < 0.99 || weightSum > 1.01 {
			t.Fatalf("template %s factor weights should sum to 1, got %.2f", template.ID, weightSum)
		}
	}
}
