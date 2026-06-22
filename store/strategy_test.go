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
