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
