// Package coinmarketcap provides a lightweight client for the CoinMarketCap API.
package coinmarketcap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBaseURL = "https://pro-api.coinmarketcap.com/v1"
	defaultTimeout = 30 * time.Second

	// CMC free/basic tier is roughly 10,000 credits/month. We target well under
	// that: at most one listings call every 15 minutes.
	requestsPerSecond = 1.0 / 900.0 // one request per 15 minutes
	burst             = 1
)

// Client is a CoinMarketCap API client.
type Client struct {
	baseURL string
	http    *http.Client
	apiKey  string
	limiter *rate.Limiter
}

// NewClient creates a new CoinMarketCap client with the given API key.
func NewClient(apiKey string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: defaultTimeout},
		apiKey:  apiKey,
		limiter: rate.NewLimiter(requestsPerSecond, burst),
	}
}

// ListingsLatestRequest parameters for /cryptocurrency/listings/latest.
type ListingsLatestRequest struct {
	Start  int
	Limit  int
	Convert string
}

// ListingsLatestResponse is the top-level response from CMC.
type ListingsLatestResponse struct {
	Data []CryptoCurrency `json:"data"`
}

// CryptoCurrency represents a single coin from /listings/latest.
type CryptoCurrency struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Symbol            string `json:"symbol"`
	Slug              string `json:"slug"`
	CmcRank           int    `json:"cmc_rank"`
	CirculatingSupply float64          `json:"circulating_supply"`
	TotalSupply       float64          `json:"total_supply"`
	MaxSupply         float64          `json:"max_supply"`
	Quote             map[string]Quote `json:"quote"`
}

// Quote contains USD market data.
type Quote struct {
	Price            float64 `json:"price"`
	Volume24h        float64 `json:"volume_24h"`
	VolumeChange24h  float64 `json:"volume_change_24h"`
	PercentChange24h float64 `json:"percent_change_24h"`
	MarketCap        float64 `json:"market_cap"`
	MarketCapDominance float64 `json:"market_cap_dominance"`
	FullyDilutedMarketCap float64 `json:"fully_diluted_market_cap"`
	LastUpdated      string  `json:"last_updated"`
}

// ListingsLatest fetches the latest cryptocurrency listings.
func (c *Client) ListingsLatest(ctx context.Context, req ListingsLatestRequest) ([]CryptoCurrency, error) {
	if req.Start <= 0 {
		req.Start = 1
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Convert == "" {
		req.Convert = "USD"
	}

	params := url.Values{}
	params.Set("start", strconv.Itoa(req.Start))
	params.Set("limit", strconv.Itoa(req.Limit))
	params.Set("convert", req.Convert)

	fullURL := fmt.Sprintf("%s/cryptocurrency/listings/latest?%s", c.baseURL, params.Encode())

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	hReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	hReq.Header.Set("X-CMC_PRO_API_KEY", c.apiKey)
	hReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(hReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from CoinMarketCap", resp.StatusCode)
	}

	var body ListingsLatestResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return body.Data, nil
}

// SetLimiter replaces the internal rate limiter (for tests).
func (c *Client) SetLimiter(l *rate.Limiter) {
	c.limiter = l
}
