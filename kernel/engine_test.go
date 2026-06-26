package kernel

import (
	"errors"
	"testing"

	"nofx/provider/smallcap"
	"nofx/store"
)

func TestGetCandidateCoinsSmallMarketValue(t *testing.T) {
	config := store.GetDefaultStrategyConfig("en")
	config.CoinSource.SourceType = "small_market_value"
	config.CoinSource.UseSmallMarketValue = true
	config.CoinSource.SmallMarketValueLimit = 3
	config.CoinSource.Min24hQuoteVolumeUSD = 1_000_000
	config.CoinSource.MinOpenInterestUSD = 500_000
	config.ClampLimits()

	engine := NewStrategyEngine(&config)
	engine.SetSmallMarketValueProvider(&smallcap.MockProvider{
		Coins: []smallcap.SmallMarketValueCoin{
			{Symbol: "BTCUSDT", Price: 60000, CirculatingSupply: 19000000, MarketCap: 1.14e12, Volume24hUSD: 30e9, OpenInterestUSD: 20e9, DepthUSD: 10e6},
			{Symbol: "ETHUSDT", Price: 3000, CirculatingSupply: 120000000, MarketCap: 360e9, Volume24hUSD: 15e9, OpenInterestUSD: 10e9, DepthUSD: 8e6},
			{Symbol: "PEPEUSDT", Price: 0.00001, CirculatingSupply: 420e12, MarketCap: 4.2e9, Volume24hUSD: 800e6, OpenInterestUSD: 300e6, DepthUSD: 1e6},
			{Symbol: "DEADUSDT", Price: 0.001, CirculatingSupply: 1e9, MarketCap: 1e6, Volume24hUSD: 100, OpenInterestUSD: 50, DepthUSD: 10},
		},
	})

	candidates, err := engine.GetCandidateCoins()
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}

	if candidates[0].Symbol != "PEPEUSDT" {
		t.Fatalf("expected first candidate PEPEUSDT, got %s", candidates[0].Symbol)
	}
	if candidates[1].Symbol != "ETHUSDT" {
		t.Fatalf("expected second candidate ETHUSDT, got %s", candidates[1].Symbol)
	}

	if candidates[0].Metrics == nil {
		t.Fatal("expected metrics on small market value candidates")
	}
	if _, ok := candidates[0].Metrics["small_cap_score"]; !ok {
		t.Fatalf("expected small_cap_score metric, got %+v", candidates[0].Metrics)
	}
}

func TestGetCandidateCoinsSmallMarketValueExcluded(t *testing.T) {
	config := store.GetDefaultStrategyConfig("en")
	config.CoinSource.SourceType = "small_market_value"
	config.CoinSource.UseSmallMarketValue = true
	config.CoinSource.SmallMarketValueLimit = 3
	config.CoinSource.ExcludedCoins = []string{"PEPEUSDT"}
	config.ClampLimits()

	engine := NewStrategyEngine(&config)
	engine.SetSmallMarketValueProvider(&smallcap.MockProvider{
		Coins: []smallcap.SmallMarketValueCoin{
			{Symbol: "PEPEUSDT", Price: 0.00001, CirculatingSupply: 420e12, MarketCap: 4.2e9, Volume24hUSD: 800e6, OpenInterestUSD: 300e6, DepthUSD: 1e6},
			{Symbol: "ETHUSDT", Price: 3000, CirculatingSupply: 120000000, MarketCap: 360e9, Volume24hUSD: 15e9, OpenInterestUSD: 10e9, DepthUSD: 8e6},
		},
	})

	candidates, err := engine.GetCandidateCoins()
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range candidates {
		if c.Symbol == "PEPEUSDT" {
			t.Fatal("excluded coin PEPEUSDT should not appear")
		}
	}
}

func TestGetCandidateCoinsSmallMarketValueProviderError(t *testing.T) {
	config := store.GetDefaultStrategyConfig("en")
	config.CoinSource.SourceType = "small_market_value"
	config.CoinSource.UseSmallMarketValue = true
	config.ClampLimits()

	engine := NewStrategyEngine(&config)
	engine.SetSmallMarketValueProvider(&smallcap.MockProvider{
		Err: errors.New("provider down"),
	})

	_, err := engine.GetCandidateCoins()
	if err == nil {
		t.Fatal("expected provider error")
	}
}

func TestGetCandidateCoinsMixedWithSmallMarketValue(t *testing.T) {
	config := store.GetDefaultStrategyConfig("en")
	config.CoinSource.SourceType = "mixed"
	config.CoinSource.UseAI500 = false
	config.CoinSource.UseSmallMarketValue = true
	config.CoinSource.SmallMarketValueLimit = 2
	config.ClampLimits()

	engine := NewStrategyEngine(&config)
	engine.SetSmallMarketValueProvider(&smallcap.MockProvider{
		Coins: []smallcap.SmallMarketValueCoin{
			{Symbol: "PEPEUSDT", Price: 0.00001, CirculatingSupply: 420e12, MarketCap: 4.2e9, Volume24hUSD: 800e6, OpenInterestUSD: 300e6, DepthUSD: 1e6},
			{Symbol: "DOGEUSDT", Price: 0.1, CirculatingSupply: 140e9, MarketCap: 14e9, Volume24hUSD: 1e9, OpenInterestUSD: 500e6, DepthUSD: 1e6},
		},
	})

	candidates, err := engine.GetCandidateCoins()
	if err != nil {
		t.Fatal(err)
	}

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	sourceOK := false
	for _, s := range candidates[0].Sources {
		if s == "small_market_value" {
			sourceOK = true
			break
		}
	}
	if !sourceOK {
		t.Fatalf("expected small_market_value source, got %+v", candidates[0].Sources)
	}
}
