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
	if config.RiskControl.StopLossTimeframeMode != StopLossTimeframeModeAuto {
		t.Fatalf("expected stop-loss timeframe mode default %s, got %s", StopLossTimeframeModeAuto, config.RiskControl.StopLossTimeframeMode)
	}
}

func TestClampLimitsNormalizesStopLossTimeframeConfig(t *testing.T) {
	config := StrategyConfig{}
	config.Indicators.Klines.PrimaryTimeframe = "15m"
	config.Indicators.Klines.EntryTimeframe = "5m"
	config.Indicators.Klines.SelectedTimeframes = []string{"5m", "15m"}
	config.RiskControl.StopLossTimeframeMode = StopLossTimeframeModeCustom
	config.RiskControl.StopLossTimeframe = "15m"

	config.ClampLimits()

	if config.RiskControl.StopLossTimeframeMode != StopLossTimeframeModeCustom || config.RiskControl.StopLossTimeframe != "15m" {
		t.Fatalf("expected valid custom stop-loss timeframe to be preserved, got %+v", config.RiskControl)
	}

	config.RiskControl.StopLossTimeframe = "2h"
	config.ClampLimits()

	if config.RiskControl.StopLossTimeframeMode != StopLossTimeframeModeAuto || config.RiskControl.StopLossTimeframe != "" {
		t.Fatalf("expected invalid custom stop-loss timeframe to normalize to auto, got %+v", config.RiskControl)
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

func TestDefaultStrategyConfigUsesLowerEntryTimeframe(t *testing.T) {
	config := GetDefaultStrategyConfig("zh")
	config.ClampLimits()

	if config.Indicators.Klines.PrimaryTimeframe != "15m" || config.Indicators.Klines.EntryTimeframe != "5m" {
		t.Fatalf("expected default strategy primary 15m and entry 5m, got primary=%s entry=%s", config.Indicators.Klines.PrimaryTimeframe, config.Indicators.Klines.EntryTimeframe)
	}
	if len(config.Indicators.Klines.SelectedTimeframes) != 3 ||
		config.Indicators.Klines.SelectedTimeframes[0] != "5m" ||
		config.Indicators.Klines.SelectedTimeframes[1] != "15m" ||
		config.Indicators.Klines.SelectedTimeframes[2] != "1h" {
		t.Fatalf("expected default selected timeframes [5m 15m 1h], got %+v", config.Indicators.Klines.SelectedTimeframes)
	}
	if len(config.Indicators.Klines.ConfirmationTimeframes) != 1 || config.Indicators.Klines.ConfirmationTimeframes[0] != "1h" {
		t.Fatalf("expected default confirmation timeframe [1h], got %+v", config.Indicators.Klines.ConfirmationTimeframes)
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
		if config.Indicators.Klines.PrimaryTimeframe != "15m" ||
			config.Indicators.Klines.EntryTimeframe != "5m" ||
			len(config.Indicators.Klines.ConfirmationTimeframes) != 1 ||
			config.Indicators.Klines.ConfirmationTimeframes[0] != "1h" {
			t.Fatalf("template %s should use 15m/5m/1h timeframe roles, got %+v", template.ID, config.Indicators.Klines)
		}
		if config.ScoringConfig.Timeframe != config.Indicators.Klines.PrimaryTimeframe {
			t.Fatalf("template %s scoring timeframe should follow primary timeframe, got %s", template.ID, config.ScoringConfig.Timeframe)
		}
		if config.ScoringConfig.Execution.Leverage != config.RiskControl.BTCETHMaxLeverage {
			t.Fatalf("template %s execution leverage should follow risk control, got execution=%d risk=%d", template.ID, config.ScoringConfig.Execution.Leverage, config.RiskControl.BTCETHMaxLeverage)
		}
		if config.ScoringConfig.Execution.Confidence != config.ScoringConfig.MinConfidence {
			t.Fatalf("template %s execution confidence should follow scoring confidence, got execution=%d scoring=%d", template.ID, config.ScoringConfig.Execution.Confidence, config.ScoringConfig.MinConfidence)
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

func TestClampLimitsPreservesNegativeShortThreshold(t *testing.T) {
	config := GetDefaultStrategyConfig("zh")
	config.ScoringConfig = &ScoringStrategyConfig{Enabled: true}
	config.ScoringConfig.ShortThreshold = -70
	config.ClampLimits()
	if config.ScoringConfig.ShortThreshold != -70 {
		t.Fatalf("expected legal negative short threshold to be preserved, got %.2f", config.ScoringConfig.ShortThreshold)
	}

	config.ScoringConfig.ShortThreshold = 30
	config.ClampLimits()
	if config.ScoringConfig.ShortThreshold != -60 {
		t.Fatalf("expected positive short threshold to fall back to -60, got %.2f", config.ScoringConfig.ShortThreshold)
	}
}

func TestStrategyTemplatesKeepDistinctTradingProfiles(t *testing.T) {
	templates := map[string]StrategyTemplate{}
	for _, template := range ListStrategyTemplates("zh") {
		templates[template.ID] = template
	}

	trend := templates["trend_following_balanced"].Config
	pullback := templates["pullback_balanced"].Config
	rangeReversal := templates["range_reversal_balanced"].Config
	breakout := templates["breakout_balanced"].Config
	volatility := templates["volatility_breakout_aggressive"].Config

	if trend.ScoringConfig.LongThreshold <= pullback.ScoringConfig.LongThreshold {
		t.Fatalf("trend following should require stronger primary evidence than pullback: trend=%.2f pullback=%.2f", trend.ScoringConfig.LongThreshold, pullback.ScoringConfig.LongThreshold)
	}
	if rangeReversal.ScoringConfig.FactorWeights["structure"] <= rangeReversal.ScoringConfig.FactorWeights["trend"] {
		t.Fatalf("range reversal should prioritize structure over trend: %+v", rangeReversal.ScoringConfig.FactorWeights)
	}
	if breakout.ScoringConfig.FactorWeights["trend"] <= breakout.ScoringConfig.FactorWeights["structure"] {
		t.Fatalf("breakout should prioritize directional evidence over structure: %+v", breakout.ScoringConfig.FactorWeights)
	}
	if volatility.RiskControl.RiskPerTradePct >= trend.RiskControl.RiskPerTradePct ||
		volatility.RiskControl.StopLossATRBuffer <= trend.RiskControl.StopLossATRBuffer {
		t.Fatalf("volatility breakout should use smaller risk and wider ATR buffer than trend following: volatility=%+v trend=%+v", volatility.RiskControl, trend.RiskControl)
	}
	if volatility.RiskControl.BTCETHMaxLeverage >= trend.RiskControl.BTCETHMaxLeverage {
		t.Fatalf("volatility breakout should cap leverage below trend following: volatility=%d trend=%d", volatility.RiskControl.BTCETHMaxLeverage, trend.RiskControl.BTCETHMaxLeverage)
	}
}
