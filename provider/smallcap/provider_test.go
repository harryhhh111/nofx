package smallcap

import (
	"context"
	"errors"
	"testing"
)

func TestMockProviderFiltersAndSortsByMarketCap(t *testing.T) {
	mock := &MockProvider{
		Coins: []SmallMarketValueCoin{
			{Symbol: "BTCUSDT", Price: 60000, CirculatingSupply: 19000000, MarketCap: 1.14e12, Volume24hUSD: 30e9, OpenInterestUSD: 20e9},
			{Symbol: "ETHUSDT", Price: 3000, CirculatingSupply: 120000000, MarketCap: 360e9, Volume24hUSD: 15e9, OpenInterestUSD: 10e9},
			{Symbol: "PEPEUSDT", Price: 0.00001, CirculatingSupply: 420e12, MarketCap: 4.2e9, Volume24hUSD: 800e6, OpenInterestUSD: 300e6},
			{Symbol: "DEADUSDT", Price: 0.001, CirculatingSupply: 1e9, MarketCap: 1e6, Volume24hUSD: 100, OpenInterestUSD: 50},
		},
	}

	req := SmallMarketValueRequest{
		Exchange:             "binance",
		SortBy:               SortByMarketCap,
		Limit:                3,
		Min24hQuoteVolumeUSD: 1_000_000,
		MinOpenInterestUSD:   500_000,
	}

	res, err := mock.GetSmallMarketValueRanking(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Coins) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(res.Coins))
	}

	if res.Coins[0].Symbol != "PEPEUSDT" {
		t.Fatalf("expected first candidate PEPEUSDT, got %s", res.Coins[0].Symbol)
	}
	if res.Coins[1].Symbol != "ETHUSDT" {
		t.Fatalf("expected second candidate ETHUSDT, got %s", res.Coins[1].Symbol)
	}

	if res.Coins[0].MarketCapRank != 1 {
		t.Fatalf("expected market_cap_rank=1, got %d", res.Coins[0].MarketCapRank)
	}
	if res.Coins[0].SmallCapScore <= 0 {
		t.Fatalf("expected positive small_cap_score, got %v", res.Coins[0].SmallCapScore)
	}

	if res.FilterStats.LowVolumeCount != 1 {
		t.Fatalf("expected 1 low_volume filter, got %d", res.FilterStats.LowVolumeCount)
	}
}

func TestMockProviderFiltersMissingLiquidity(t *testing.T) {
	mock := &MockProvider{
		Coins: []SmallMarketValueCoin{
			{Symbol: "EMPTYUSDT", Price: 1, CirculatingSupply: 1_000_000, MarketCap: 1_000_000},
			{Symbol: "OKUSDT", Price: 1, CirculatingSupply: 2_000_000, MarketCap: 2_000_000, Volume24hUSD: 2_000_000, OpenInterestUSD: 1_000_000, DepthUSD: 200_000},
		},
	}

	res, err := mock.GetSmallMarketValueRanking(context.Background(), SmallMarketValueRequest{
		Limit:                2,
		Min24hQuoteVolumeUSD: 1_000_000,
		MinOpenInterestUSD:   500_000,
		MinDepthUSD:          100_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Coins) != 1 || res.Coins[0].Symbol != "OKUSDT" {
		t.Fatalf("expected only OKUSDT to pass, got %+v", res.Coins)
	}
	if res.FilterStats.LowVolumeCount != 1 {
		t.Fatalf("expected 1 low_volume filter, got %d", res.FilterStats.LowVolumeCount)
	}
	if res.FilterStats.LowOICount != 1 {
		t.Fatalf("expected 1 low_oi filter, got %d", res.FilterStats.LowOICount)
	}
	if res.FilterStats.LowDepthCount != 1 {
		t.Fatalf("expected 1 low_depth filter, got %d", res.FilterStats.LowDepthCount)
	}
}

func TestMockProviderReturnsError(t *testing.T) {
	mock := &MockProvider{Err: errors.New("provider down")}
	_, err := mock.GetSmallMarketValueRanking(context.Background(), SmallMarketValueRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNotImplementedProvider(t *testing.T) {
	p := &NotImplementedProvider{}
	_, err := p.GetSmallMarketValueRanking(context.Background(), SmallMarketValueRequest{})
	if err == nil {
		t.Fatal("expected not implemented error")
	}
}
