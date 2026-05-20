package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// AI500 cache TTL (2 hours — data changes slowly)
const ai500CacheTTL = 2 * time.Hour

// Global AI500 cache — shared across all Client instances.
var (
	ai500GlobalCache     []CoinData
	ai500GlobalCacheTime time.Time
	ai500GlobalCacheMu   sync.RWMutex
)

// CoinData represents AI500 coin information
type CoinData struct {
	Pair            string  `json:"pair"`
	Score           float64 `json:"score"`
	StartTime       int64   `json:"start_time"`
	StartPrice      float64 `json:"start_price"`
	LastScore       float64 `json:"last_score"`
	MaxScore        float64 `json:"max_score"`
	MaxPrice        float64 `json:"max_price"`
	IncreasePercent float64 `json:"increase_percent"`
	IsAvailable     bool    `json:"-"`
}

// AI500Response is the API response structure
type AI500Response struct {
	Success bool `json:"success"`
	Data    struct {
		Coins []CoinData `json:"coins"`
		Count int        `json:"count"`
	} `json:"data"`
}

// GetAI500List retrieves AI500 coin list. Results are globally cached for 2 hours.
func (c *Client) GetAI500List() ([]CoinData, error) {
	// Check global cache
	ai500GlobalCacheMu.RLock()
	if ai500GlobalCache != nil && time.Since(ai500GlobalCacheTime) < ai500CacheTTL {
		result := make([]CoinData, len(ai500GlobalCache))
		copy(result, ai500GlobalCache)
		ai500GlobalCacheMu.RUnlock()
		log.Printf("✓ AI500 cache hit (%d coins, cached %v ago)", len(result), time.Since(ai500GlobalCacheTime).Round(time.Second))
		return result, nil
	}
	ai500GlobalCacheMu.RUnlock()

	// Cache miss — fetch using this client's credentials
	coins, err := fetchAI500WithRetry(c)
	if err != nil {
		return nil, err
	}

	// Don't cache empty results
	if len(coins) > 0 {
		ai500GlobalCacheMu.Lock()
		ai500GlobalCache = make([]CoinData, len(coins))
		copy(ai500GlobalCache, coins)
		ai500GlobalCacheTime = time.Now()
		ai500GlobalCacheMu.Unlock()
	}

	return coins, nil
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

// GetTopRatedCoins retrieves top N coins by score from the global cache.
func (c *Client) GetTopRatedCoins(limit int) ([]string, error) {
	coins, err := c.GetAI500List()
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
		log.Printf("⚠️  GetTopRatedCoins: 0 available coins out of %d total", len(coins))
		return []string{}, nil
	}

	// Sort by Score descending
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
		symbols = append(symbols, NormalizeSymbol(availableCoins[i].Pair))
	}

	return symbols, nil
}

// GetAvailableCoins retrieves all available coin symbols from the global cache.
func (c *Client) GetAvailableCoins() ([]string, error) {
	coins, err := c.GetAI500List()
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, coin := range coins {
		if coin.IsAvailable {
			symbols = append(symbols, NormalizeSymbol(coin.Pair))
		}
	}

	return symbols, nil
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
