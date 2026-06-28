package smallcap

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nofx/provider/coingecko"
)

func TestCoinGeckoProviderCacheKey(t *testing.T) {
	p := NewCoinGeckoProvider()

	req := SmallMarketValueRequest{
		Exchange:             "Binance",
		SortBy:               SortByMarketCap,
		Limit:                3,
		Min24hQuoteVolumeUSD: 5_000_000,
		MinOpenInterestUSD:   1_000_000,
		MinDepthUSD:          100_000,
	}

	key1 := p.cacheKey(req)
	req.Exchange = "binance"
	key2 := p.cacheKey(req)
	if key1 != key2 {
		t.Fatal("cache key should be case-insensitive for exchange")
	}

	req2 := SmallMarketValueRequest{Limit: 3}
	if p.cacheKey(req) == p.cacheKey(req2) {
		t.Fatal("cache key should differ when liquidity thresholds differ")
	}

	// Depth is not provided by CoinGecko, so it should not affect the cache key.
	req3 := req
	req3.MinDepthUSD = 0
	if p.cacheKey(req) != p.cacheKey(req3) {
		t.Fatal("cache key should ignore MinDepthUSD")
	}
}

func TestCoinGeckoProviderUsesCache(t *testing.T) {
	p := NewCoinGeckoProvider()
	p.ttl = 1 * time.Hour

	req := SmallMarketValueRequest{Limit: 3}
	expected := &SmallMarketValueRankingData{
		Coins: []SmallMarketValueCoin{{Symbol: "PEPEUSDT", MarketCap: 1e9}},
	}

	key := p.cacheKey(req)
	p.mu.Lock()
	p.cache[key] = cacheEntry{data: expected, expiresAt: time.Now().Add(p.ttl)}
	p.mu.Unlock()

	got, err := p.GetSmallMarketValueRanking(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Coins[0].Symbol != "PEPEUSDT" {
		t.Fatalf("unexpected cached result: %+v", got)
	}
}

func TestCoinGeckoProviderFiltersNonTradableCoins(t *testing.T) {
	geckoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":"pepe","symbol":"pepe","name":"Pepe","current_price":0.0001,"market_cap":4200000000,"total_volume":800000000},
			{"id":"unknown-token","symbol":"unknown","name":"Unknown","current_price":0.01,"market_cap":1000000,"total_volume":1000000}
		]`))
	}))
	defer geckoServer.Close()

	binanceServer := newBinanceMockServer(t)
	defer binanceServer.Close()

	p := newTestCoinGeckoProvider(geckoServer.URL, binanceServer.URL)

	req := SmallMarketValueRequest{
		Limit:                2,
		Min24hQuoteVolumeUSD: 500_000_000,
		MinOpenInterestUSD:   100_000,
	}

	data, err := p.GetSmallMarketValueRanking(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Coins) != 1 {
		t.Fatalf("expected 1 tradable coin, got %d", len(data.Coins))
	}
	if data.Coins[0].Symbol != "PEPEUSDT" {
		t.Fatalf("expected PEPEUSDT, got %s", data.Coins[0].Symbol)
	}
}

func TestCoinGeckoProviderMarksOIAvailableFalseWhenAllFail(t *testing.T) {
	geckoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":"pepe","symbol":"pepe","name":"Pepe","current_price":0.0001,"market_cap":4200000000,"total_volume":800000000}
		]`))
	}))
	defer geckoServer.Close()

	binanceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "exchangeInfo") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"symbols":[{"symbol":"PEPEUSDT","status":"TRADING","contractType":"PERPETUAL"}]}`))
			return
		}
		// OI endpoint always fails.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer binanceServer.Close()

	p := newTestCoinGeckoProvider(geckoServer.URL, binanceServer.URL)

	req := SmallMarketValueRequest{
		Limit:                2,
		Min24hQuoteVolumeUSD: 100_000_000,
		MinOpenInterestUSD:   1_000_000,
	}

	data, err := p.GetSmallMarketValueRanking(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if data.OIAvailable {
		t.Fatal("expected OIAvailable=false when OI enrichment fails")
	}
	// When OI is unavailable, the OI filter should be skipped so volume-satisfied
	// coins are still returned.
	if len(data.Coins) != 1 {
		t.Fatalf("expected 1 coin despite OI failure, got %d", len(data.Coins))
	}
}

func TestCoinGeckoProviderStopsPagingWhenOIUnavailable(t *testing.T) {
	var pageRequests atomic.Int32
	geckoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("["))
		for i := 0; i < defaultCoinGeckoFetchSize; i++ {
			if i > 0 {
				w.Write([]byte(","))
			}
			fmt.Fprintf(w, `{"id":"coin%d","symbol":"coin%d","name":"Coin %d","current_price":1,"market_cap":%d,"total_volume":800000000}`,
				i, i, i, 1_000_000+i)
		}
		w.Write([]byte("]"))
	}))
	defer geckoServer.Close()

	binanceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "exchangeInfo") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"symbols":[`))
			for i := 0; i < defaultCoinGeckoFetchSize; i++ {
				if i > 0 {
					w.Write([]byte(","))
				}
				fmt.Fprintf(w, `{"symbol":"COIN%dUSDT","status":"TRADING","contractType":"PERPETUAL"}`, i)
			}
			w.Write([]byte(`]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer binanceServer.Close()

	p := newTestCoinGeckoProvider(geckoServer.URL, binanceServer.URL)
	data, err := p.GetSmallMarketValueRanking(context.Background(), SmallMarketValueRequest{
		Limit:                2,
		Min24hQuoteVolumeUSD: 100_000,
		MinOpenInterestUSD:   1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if data.OIAvailable {
		t.Fatal("expected OIAvailable=false when all OI calls fail")
	}
	if len(data.Coins) != 2 {
		t.Fatalf("expected 2 coins after OI fallback, got %d", len(data.Coins))
	}
	if got := pageRequests.Load(); got != 1 {
		t.Fatalf("expected CoinGecko pagination to stop after first page, got %d page requests", got)
	}
}

func TestCoinGeckoProviderFetchesAndFilters(t *testing.T) {
	geckoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":"pepe","symbol":"pepe","name":"Pepe","current_price":0.0001,"market_cap":4200000000,"total_volume":800000000},
			{"id":"ethereum","symbol":"eth","name":"Ethereum","current_price":3000,"market_cap":360000000000,"total_volume":15000000000},
			{"id":"bitcoin","symbol":"btc","name":"Bitcoin","current_price":60000,"market_cap":1200000000000,"total_volume":30000000000}
		]`))
	}))
	defer geckoServer.Close()

	binanceServer := newBinanceMockServer(t)
	defer binanceServer.Close()

	p := newTestCoinGeckoProvider(geckoServer.URL, binanceServer.URL)

	req := SmallMarketValueRequest{
		Limit:                2,
		Min24hQuoteVolumeUSD: 500_000_000,
		MinOpenInterestUSD:   100_000,
	}

	data, err := p.GetSmallMarketValueRanking(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Coins) != 2 {
		t.Fatalf("expected 2 coins, got %d", len(data.Coins))
	}
	if data.Coins[0].Symbol != "PEPEUSDT" {
		t.Fatalf("expected first coin PEPEUSDT, got %s", data.Coins[0].Symbol)
	}
	if !data.OIAvailable {
		t.Fatal("expected OIAvailable=true")
	}
	if data.Coins[0].OpenInterestUSD <= 0 {
		t.Fatalf("expected OI to be enriched, got %f", data.Coins[0].OpenInterestUSD)
	}
}

func newBinanceMockServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "exchangeInfo") {
			w.Write([]byte(`{"symbols":[
				{"symbol":"PEPEUSDT","status":"TRADING","contractType":"PERPETUAL"},
				{"symbol":"ETHUSDT","status":"TRADING","contractType":"PERPETUAL"},
				{"symbol":"BTCUSDT","status":"TRADING","contractType":"PERPETUAL"}
			]}`))
			return
		}
		symbol := r.URL.Query().Get("symbol")
		oi := "0"
		switch symbol {
		case "PEPEUSDT":
			oi = "1000000000000"
		case "ETHUSDT":
			oi = "10000000000"
		case "BTCUSDT":
			oi = "20000000000"
		}
		w.Write([]byte(`{"openInterest":"` + oi + `"}`))
	}))
}

func newTestCoinGeckoProvider(geckoURL, binanceURL string) *CoinGeckoProvider {
	p := &CoinGeckoProvider{
		client: coingecko.NewClientWithURL(geckoURL),
		ttl:    1 * time.Hour,
		cache:  make(map[string]cacheEntry),
	}
	binanceExchangeInfoURL = binanceURL + "/exchangeInfo"
	binanceOIAPIURL = binanceURL + "/openInterest"
	return p
}
