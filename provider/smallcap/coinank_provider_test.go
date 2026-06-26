package smallcap

import (
	"testing"
	"time"
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
