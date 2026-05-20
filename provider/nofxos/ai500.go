package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// CoinData represents AI500 coin information
type CoinData struct {
	Pair            string  `json:"pair"`             // Trading pair symbol (e.g.: BTCUSDT)
	Score           float64 `json:"score"`            // Current AI score (0-100)
	StartTime       int64   `json:"start_time"`       // Start time (Unix timestamp)
	StartPrice      float64 `json:"start_price"`      // Start price
	LastScore       float64 `json:"last_score"`       // Latest score
	MaxScore        float64 `json:"max_score"`        // Highest score
	MaxPrice        float64 `json:"max_price"`        // Highest price
	IncreasePercent float64 `json:"increase_percent"` // Increase percentage (already x100)
	IsAvailable     bool    `json:"-"`                // Whether tradable (internal use)
}

// AI500Response is the API response structure
type AI500Response struct {
	Success bool `json:"success"`
	Data    struct {
		Coins []CoinData `json:"coins"`
		Count int        `json:"count"`
	} `json:"data"`
}

// GetAI500ListGlobal retrieves AI500 coin list from the global cache.
func GetAI500ListGlobal(opts ...CallOption) ([]CoinData, error) {
	return ai500Cache.Get(func() ([]CoinData, error) {
		return fetchAI500WithRetry(GetGlobalClient())
	}, opts...)
}

// GetAI500List delegates to the global cache function.
func (c *Client) GetAI500List(opts ...CallOption) ([]CoinData, error) {
	return GetAI500ListGlobal(opts...)
}

func (c *Client) fetchAI500() ([]CoinData, error) {
	log.Printf("🔄 Requesting AI500 data from %s...", c.GetBaseURL())

	body, err := c.doRequest("/api/ai500/list")
	if err != nil {
		return nil, fmt.Errorf("failed to request AI500 API: %w", err)
	}

	var response AI500Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	if len(response.Data.Coins) == 0 {
		log.Printf("ℹ️  AI500 returned empty coin list (no coins meet criteria currently)")
		return []CoinData{}, nil
	}

	coins := response.Data.Coins
	for i := range coins {
		coins[i].IsAvailable = true
	}

	log.Printf("✓ Successfully fetched %d AI500 coins", len(coins))
	return coins, nil
}

func fetchAI500WithRetry(client *Client) ([]CoinData, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			log.Printf("⚠️  Retry attempt %d of %d to fetch AI500 data...", attempt, maxRetries)
			time.Sleep(2 * time.Second)
		}

		coins, err := client.fetchAI500()
		if err == nil {
			if attempt > 1 {
				log.Printf("✓ Retry attempt %d succeeded", attempt)
			}
			return coins, nil
		}

		lastErr = err
		log.Printf("❌ AI500 request attempt %d failed: %v", attempt, err)
	}

	return nil, fmt.Errorf("all AI500 API requests failed: %w", lastErr)
}

// GetTopRatedCoinsGlobal retrieves top N coins by score from the global cache.
func GetTopRatedCoinsGlobal(limit int, opts ...CallOption) ([]string, error) {
	coins, err := GetAI500ListGlobal(opts...)
	if err != nil {
		return nil, err
	}

	var availableCoins []CoinData
	for _, coin := range coins {
		if coin.IsAvailable {
			availableCoins = append(availableCoins, coin)
		}
	}

	if len(availableCoins) == 0 {
		log.Printf("⚠️  GetTopRatedCoinsGlobal: 0 available coins out of %d total", len(coins))
		return []string{}, nil
	}

	// Sort by Score descending (bubble sort)
	for i := 0; i < len(availableCoins); i++ {
		for j := i + 1; j < len(availableCoins); j++ {
			if availableCoins[i].Score < availableCoins[j].Score {
				availableCoins[i], availableCoins[j] = availableCoins[j], availableCoins[i]
			}
		}
	}

	maxCount := limit
	if len(availableCoins) < maxCount {
		maxCount = len(availableCoins)
	}

	var symbols []string
	for i := 0; i < maxCount; i++ {
		symbol := NormalizeSymbol(availableCoins[i].Pair)
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}

// GetTopRatedCoins delegates to the global cache function.
func (c *Client) GetTopRatedCoins(limit int, opts ...CallOption) ([]string, error) {
	return GetTopRatedCoinsGlobal(limit, opts...)
}

// GetAvailableCoinsGlobal retrieves all available coin symbols from the global cache.
func GetAvailableCoinsGlobal() ([]string, error) {
	coins, err := GetAI500ListGlobal()
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, coin := range coins {
		if coin.IsAvailable {
			symbol := NormalizeSymbol(coin.Pair)
			symbols = append(symbols, symbol)
		}
	}

	return symbols, nil
}

// GetAvailableCoins delegates to the global cache function.
func (c *Client) GetAvailableCoins() ([]string, error) {
	return GetAvailableCoinsGlobal()
}

// NormalizeSymbol normalizes coin symbol to XXXUSDT format
func NormalizeSymbol(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	symbol = strings.ToUpper(symbol)
	if !strings.HasSuffix(symbol, "USDT") {
		symbol = symbol + "USDT"
	}
	return symbol
}
