package smallcap

import (
	"testing"
	"time"

	"nofx/provider/coinank"
)

func TestCoinAnkProviderCacheKeyIsDeterministic(t *testing.T) {
	p := NewCoinAnkProvider("test-key")

	req1 := SmallMarketValueRequest{
		Exchange:             "Binance",
		SortBy:               SortByMarketCap,
		Limit:                3,
		Min24hQuoteVolumeUSD: 5_000_000,
		MinOpenInterestUSD:   1_000_000,
		MinDepthUSD:          100_000,
	}
	req2 := SmallMarketValueRequest{
		Exchange:             "binance",
		SortBy:               SortByMarketCap,
		Limit:                3,
		Min24hQuoteVolumeUSD: 5_000_000,
		MinOpenInterestUSD:   1_000_000,
		MinDepthUSD:          100_000,
	}

	if p.cacheKey(req1) != p.cacheKey(req2) {
		t.Fatal("cache key should be case-insensitive for exchange")
	}

	req2.MinDepthUSD = 0
	if p.cacheKey(req1) != p.cacheKey(req2) {
		t.Fatal("cache key should ignore depth because CoinAnk depth is unavailable")
	}
}

func TestCoinAnkProviderStoresAndReadsCache(t *testing.T) {
	p := NewCoinAnkProvider("test-key")
	p.ttl = 1 * time.Hour

	req := SmallMarketValueRequest{
		SortBy: SortByMarketCap,
		Limit:  3,
	}
	expected := &SmallMarketValueRankingData{
		Coins: []SmallMarketValueCoin{{Symbol: "PEPEUSDT", MarketCap: 1e9}},
	}

	key := p.cacheKey(req)
	p.mu.Lock()
	p.cache[key] = cacheEntry{data: expected, expiresAt: time.Now().Add(p.ttl)}
	p.mu.Unlock()

	cached, ok := p.cache[key]
	if !ok {
		t.Fatal("expected cache entry to exist")
	}
	if cached.data.Coins[0].Symbol != "PEPEUSDT" {
		t.Fatalf("unexpected cached data: %+v", cached.data)
	}
}

func TestBuildRawCoinsFromRowsLeavesUnknownTotalSupplyZero(t *testing.T) {
	coins := buildRawCoinsFromRows(SmallMarketValueRequest{Exchange: "Binance"}, []coinank.VolumeRankResponse{
		{
			BaseCoin:        "pepe",
			Symbol:          "PEPEUSDT",
			ExchangeName:    "Binance",
			SupportContract: true,
			Price:           0.000001,
			Turnover24H:     10_000_000,
		},
	}, nil)

	if len(coins) != 1 {
		t.Fatalf("expected one raw coin, got %d", len(coins))
	}
	if coins[0].TotalSupply != 0 {
		t.Fatalf("unknown total supply should stay zero, got %f", coins[0].TotalSupply)
	}
}
