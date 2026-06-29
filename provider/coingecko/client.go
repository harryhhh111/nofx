// Package coingecko provides a minimal client for the CoinGecko public API.
package coingecko

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBaseURL = "https://api.coingecko.com/api/v3"
	defaultTimeout = 30 * time.Second

	// freeRequestsPerSecond is the conservative rate limit for the free
	// CoinGecko public API. The documented limit is ~10-30 calls/minute;
	// we target 20 calls/minute (1 every 3s) to balance speed and safety.
	freeRequestsPerSecond = 1.0 / 3.0 // one request every 3 seconds
	freeBurst             = 1

	// proRequestsPerSecond is a more permissive rate limit for paid/demo plans.
	proRequestsPerSecond = 30.0 // 30 calls per second
	proBurst             = 10

	// Keep retries low: retries are mainly for transient 429s, and long
	// backoff chains blow through the caller's context deadline.
	maxRetries = 1
)

// Client is a lightweight CoinGecko API client.
type Client struct {
	baseURL string
	http    *http.Client
	apiKey  string // optional, for paid CoinGecko API plans
	limiter *rate.Limiter
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
		limiter: rate.NewLimiter(freeRequestsPerSecond, freeBurst),
	}
}

// NewProClient creates a client for CoinGecko Pro/Demo plans.
// It chooses the correct auth header based on the key prefix:
//   - Demo keys start with "CG-" and use x-cg-demo-api-key
//   - Pro keys use x-cg-pro-api-key
func NewProClient(apiKey string) *Client {
	return &Client{
		baseURL: "https://api.coingecko.com/api/v3",
		http:    &http.Client{Timeout: defaultTimeout},
		apiKey:  apiKey,
		limiter: rate.NewLimiter(proRequestsPerSecond, proBurst),
	}
}

// SetLimiter replaces the internal rate limiter. It is intended for tests that
// need to avoid real rate-limit delays.
func (c *Client) SetLimiter(l *rate.Limiter) {
	c.limiter = l
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
// It respects the client's rate limiter and retries with exponential backoff
// when CoinGecko responds with 429 Too Many Requests.
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

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Short fixed backoff to avoid exceeding caller context deadlines.
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		hReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		if c.apiKey != "" {
			if strings.HasPrefix(c.apiKey, "CG-") {
				hReq.Header.Set("x-cg-demo-api-key", c.apiKey)
			} else {
				hReq.Header.Set("x-cg-pro-api-key", c.apiKey)
			}
		}

		resp, err := c.http.Do(hReq)
		if err != nil {
			return nil, fmt.Errorf("do request: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := resp.Header.Get("Retry-After")
			lastErr = fmt.Errorf("unexpected status %d from CoinGecko (Retry-After: %s)", resp.StatusCode, retryAfter)
			resp.Body.Close()
			if retryAfter != "" {
				if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
					select {
					case <-time.After(time.Duration(seconds) * time.Second):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status %d from CoinGecko", resp.StatusCode)
		}

		var data []MarketData
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()
		return data, nil
	}

	return nil, fmt.Errorf("CoinGecko rate limited after %d retries: %w", maxRetries, lastErr)
}
