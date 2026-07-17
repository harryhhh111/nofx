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

func TestClampLimitsEnsuresRealizedVolBaselinePeriod(t *testing.T) {
	config := StrategyConfig{
		StrategyMode: "scoring",
		Indicators: IndicatorConfig{
			RealizedVolPeriods: []int{20},
		},
	}

	config.ClampLimits()

	got := config.Indicators.RealizedVolPeriods
	if len(got) != 2 || got[0] != 20 || got[1] != 60 {
		t.Fatalf("expected realized vol periods [20 60], got %+v", got)
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

func TestClampLimitsSetsSmallMarketValueDefaults(t *testing.T) {
	config := StrategyConfig{
		StrategyMode: "scoring",
		CoinSource: CoinSourceConfig{
			SourceType: "small_market_value",
		},
	}

	config.ClampLimits()

	if config.CoinSource.SourceType != "small_market_value" {
		t.Fatalf("expected source_type to remain small_market_value, got %s", config.CoinSource.SourceType)
	}
	if !config.CoinSource.UseSmallMarketValue {
		t.Fatal("expected UseSmallMarketValue to be true")
	}
	if config.CoinSource.SmallMarketValueLimit != DefaultSmallMarketValueLimit {
		t.Fatalf("expected small market value limit default %d, got %d", DefaultSmallMarketValueLimit, config.CoinSource.SmallMarketValueLimit)
	}
	if config.CoinSource.SmallMarketValueSortBy != "market_cap" {
		t.Fatalf("expected default sort_by market_cap, got %s", config.CoinSource.SmallMarketValueSortBy)
	}
	if config.CoinSource.Min24hQuoteVolumeUSD != DefaultSmallMarketValueMinVolume {
		t.Fatalf("expected default min volume %.0f, got %.0f", DefaultSmallMarketValueMinVolume, config.CoinSource.Min24hQuoteVolumeUSD)
	}
	if config.CoinSource.MinOpenInterestUSD != DefaultSmallMarketValueMinOI {
		t.Fatalf("expected default min OI %.0f, got %.0f", DefaultSmallMarketValueMinOI, config.CoinSource.MinOpenInterestUSD)
	}
	if config.CoinSource.MinDepthUSD != DefaultSmallMarketValueMinDepth {
		t.Fatalf("expected default min depth %.0f, got %.0f", DefaultSmallMarketValueMinDepth, config.CoinSource.MinDepthUSD)
	}
}

func TestClampLimitsClampsSmallMarketValueLimit(t *testing.T) {
	config := StrategyConfig{
		StrategyMode: "scoring",
		CoinSource: CoinSourceConfig{
			SourceType:            "small_market_value",
			SmallMarketValueLimit: MaxCandidateCoins + 10,
		},
	}

	config.ClampLimits()

	if config.CoinSource.SmallMarketValueLimit != MaxCandidateCoins {
		t.Fatalf("expected limit clamped to %d, got %d", MaxCandidateCoins, config.CoinSource.SmallMarketValueLimit)
	}
}

func TestNormalizeCoinSourceFlagsForSmallMarketValue(t *testing.T) {
	config := StrategyConfig{
		CoinSource: CoinSourceConfig{
			SourceType:            "small_market_value",
			UseAI500:              true,
			UseOITop:              true,
			UseSmallMarketValue:   false,
			SmallMarketValueLimit: 5,
		},
	}

	config.normalizeCoinSourceFlags()

	if config.CoinSource.UseAI500 || config.CoinSource.UseOITop {
		t.Fatalf("small_market_value should clear other source flags: %+v", config.CoinSource)
	}
	if !config.CoinSource.UseSmallMarketValue {
		t.Fatal("expected UseSmallMarketValue to be true")
	}
}

func TestGetEffectiveCoinCountForSmallMarketValue(t *testing.T) {
	config := StrategyConfig{
		CoinSource: CoinSourceConfig{
			SourceType:            "small_market_value",
			SmallMarketValueLimit: 5,
		},
	}

	if got := config.getEffectiveCoinCount(); got != 5 {
		t.Fatalf("expected effective coin count 5, got %d", got)
	}
}

func TestGetEffectiveCoinCountForMixedWithSmallMarketValue(t *testing.T) {
	config := StrategyConfig{
		CoinSource: CoinSourceConfig{
			SourceType:            "mixed",
			UseAI500:              true,
			AI500Limit:            2,
			UseSmallMarketValue:   true,
			SmallMarketValueLimit: 3,
		},
	}

	if got := config.getEffectiveCoinCount(); got != 5 {
		t.Fatalf("expected effective coin count 5, got %d", got)
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

func TestClampLimitsDefaultsStopLossATRBuffer(t *testing.T) {
	config := GetDefaultStrategyConfig("zh")
	config.RiskControl.StopLossATRBuffer = 0
	config.ClampLimits()
	if config.RiskControl.StopLossATRBuffer != DefaultStopLossATRBuffer {
		t.Fatalf("expected stop loss ATR buffer to default to %.2f, got %.2f", DefaultStopLossATRBuffer, config.RiskControl.StopLossATRBuffer)
	}

	config.RiskControl.StopLossATRBuffer = -1
	config.ClampLimits()
	if config.RiskControl.StopLossATRBuffer != DefaultStopLossATRBuffer {
		t.Fatalf("expected negative stop loss ATR buffer to default to %.2f, got %.2f", DefaultStopLossATRBuffer, config.RiskControl.StopLossATRBuffer)
	}
}

func TestClampLimitsForcesATR14ForProtectiveStops(t *testing.T) {
	config := GetDefaultStrategyConfig("zh")
	config.Indicators.EnableATR = false
	config.Indicators.ATRPeriods = nil

	config.ClampLimits()

	if !config.Indicators.EnableATR {
		t.Fatal("expected ATR to be forced on because protective stops require ATR14")
	}
	if !testContainsInt(config.Indicators.ATRPeriods, 14) {
		t.Fatalf("expected ATR periods to include 14, got %v", config.Indicators.ATRPeriods)
	}
}

func testContainsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
