package coingecko

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func TestCoinsMarketsRetriesOn429(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":"bitcoin","symbol":"btc","name":"Bitcoin","current_price":60000,"market_cap":1200000000000,"total_volume":30000000000}
		]`))
	}))
	defer server.Close()

	client := NewClientWithURL(server.URL)
	client.SetLimiter(rate.NewLimiter(rate.Inf, 1))
	data, err := client.CoinsMarkets(context.Background(), CoinsMarketsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests (1 failure + 1 success), got %d", requests)
	}
	if len(data) != 1 || data[0].Symbol != "btc" {
		t.Fatalf("unexpected data: %+v", data)
	}
}

func TestCoinsMarketsDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/coins/markets" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":"bitcoin","symbol":"btc","name":"Bitcoin","current_price":60000,"market_cap":1200000000000,"total_volume":30000000000},
			{"id":"ethereum","symbol":"eth","name":"Ethereum","current_price":3000,"market_cap":360000000000,"total_volume":15000000000}
		]`))
	}))
	defer server.Close()

	client := NewClientWithURL(server.URL)
	client.SetLimiter(rate.NewLimiter(rate.Inf, 1))
	data, err := client.CoinsMarkets(context.Background(), CoinsMarketsRequest{
		Order:   "market_cap_asc",
		PerPage: 2,
		Page:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 coins, got %d", len(data))
	}
	if data[0].Symbol != "btc" {
		t.Fatalf("expected btc, got %s", data[0].Symbol)
	}
	if data[0].MarketCap != 1.2e12 {
		t.Fatalf("unexpected market cap %f", data[0].MarketCap)
	}
}
