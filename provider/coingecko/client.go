// Package coingecko provides a minimal client for the CoinGecko public API.
package coingecko

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://api.coingecko.com/api/v3"
	defaultTimeout = 30 * time.Second
)

// Client is a lightweight CoinGecko API client.
type Client struct {
	baseURL string
	http    *http.Client
	apiKey  string // optional, for paid CoinGecko API plans
}

// NewClient creates a new CoinGecko client.
func NewClient() *Client {
	return NewClientWithURL(defaultBaseURL)
}

// NewClientWithURL creates a client targeting a custom CoinGecko base URL.
func NewClientWithURL(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

// NewProClient creates a client for CoinGecko Pro (api.coingecko.com/api/v3 with x-cg-pro-api-key).
func NewProClient(apiKey string) *Client {
	return &Client{
		baseURL: "https://api.coingecko.com/api/v3",
		http:    &http.Client{Timeout: defaultTimeout},
		apiKey:  apiKey,
	}
}

// MarketData represents a single coin returned by /coins/markets.
type MarketData struct {
	ID                       string  `json:"id"`
	Symbol                   string  `json:"symbol"`
	Name                     string  `json:"name"`
	CurrentPrice             float64 `json:"current_price"`
	MarketCap                float64 `json:"market_cap"`
	FullyDilutedValuation    float64 `json:"fully_diluted_valuation"`
	TotalVolume              float64 `json:"total_volume"`
	CirculatingSupply        float64 `json:"circulating_supply"`
	TotalSupply              float64 `json:"total_supply"`
	PriceChangePercentage24h float64 `json:"price_change_percentage_24h"`
}

// CoinsMarketsRequest holds query parameters for /coins/markets.
type CoinsMarketsRequest struct {
	VSCCurrency string
	Order       string // e.g. "market_cap_asc", "market_cap_desc"
	PerPage     int
	Page        int
	Sparkline   bool
}

// CoinsMarkets fetches coin market data from /coins/markets.
func (c *Client) CoinsMarkets(ctx context.Context, req CoinsMarketsRequest) ([]MarketData, error) {
	if req.VSCCurrency == "" {
		req.VSCCurrency = "usd"
	}
	if req.PerPage <= 0 {
		req.PerPage = 250
	}
	if req.PerPage > 250 {
		req.PerPage = 250
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Order == "" {
		req.Order = "market_cap_desc"
	}

	params := url.Values{}
	params.Set("vs_currency", req.VSCCurrency)
	params.Set("order", req.Order)
	params.Set("per_page", strconv.Itoa(req.PerPage))
	params.Set("page", strconv.Itoa(req.Page))
	params.Set("sparkline", "false")
	params.Set("price_change_percentage", "24h")

	fullURL := fmt.Sprintf("%s/coins/markets?%s", c.baseURL, params.Encode())

	hReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if c.apiKey != "" {
		hReq.Header.Set("x-cg-pro-api-key", c.apiKey)
	}

	resp, err := c.http.Do(hReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from CoinGecko", resp.StatusCode)
	}

	var data []MarketData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return data, nil
}
