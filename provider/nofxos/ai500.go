package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// AI500 cache TTL - all strategies/users share the same cached data
const ai500CacheTTL = 30 * time.Minute

// Global AI500 cache and request client — shared across all Client instances.
// Any user with a claw402 wallet registers their client here, and all users
// share the cached data (first requester pays, rest get it free).
var (
	ai500GlobalCache     []CoinData
	ai500GlobalCacheTime time.Time
	ai500GlobalCacheMu   sync.RWMutex
	ai500GlobalClient    *Client       // global request client (claw402-enabled preferred)
	ai500GlobalClientMu  sync.Mutex
	ai500Singleflight    singleFlight  // merges concurrent cache-miss requests
)

// simple singleflight: ensures only one in-flight request at a time for AI500
type singleFlight struct {
	mu   sync.Mutex
	call chan struct{} // non-nil while a request is in flight
}

// Do executes fn only if no other call is in flight; otherwise waits and returns the cached result.
func (sf *singleFlight) Do(fn func() ([]CoinData, error)) ([]CoinData, error) {
	sf.mu.Lock()
	if sf.call != nil {
		// Another request is in flight — wait for it, then read cache
		waitCh := sf.call
		sf.mu.Unlock()
		<-waitCh
		// Cache should now be populated; read it
		ai500GlobalCacheMu.RLock()
		if ai500GlobalCache != nil {
			result := make([]CoinData, len(ai500GlobalCache))
			copy(result, ai500GlobalCache)
			ai500GlobalCacheMu.RUnlock()
			return result, nil
		}
		ai500GlobalCacheMu.RUnlock()
		// Cache still empty after wait (the in-flight request failed) — fall through to try ourselves
		sf.mu.Lock()
	}
	// Mark as in-flight
	done := make(chan struct{})
	sf.call = done
	sf.mu.Unlock()

	defer func() {
		sf.mu.Lock()
		sf.call = nil
		close(done)
		sf.mu.Unlock()
	}()

	return fn()
}

// SetAI500GlobalClient registers a Client for global AI500 data fetching.
// A client with claw402 always overrides one without (ensures payment routing
// is upgraded even if a non-claw402 client registered first).
func SetAI500GlobalClient(client *Client) {
	ai500GlobalClientMu.Lock()
	defer ai500GlobalClientMu.Unlock()
	hasClaw402 := client.claw402 != nil
	if ai500GlobalClient == nil || hasClaw402 {
		ai500GlobalClient = client
		log.Printf("🔗 AI500 global client registered (claw402: %v)", hasClaw402)
	}
}

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
// All users share the same cached data. On cache miss, the global client
// (registered via SetAI500GlobalClient) is used to fetch data.
// Concurrent cache-miss calls are merged via singleflight to avoid duplicate requests.
func GetAI500ListGlobal() ([]CoinData, error) {
	// Check global cache first (read lock)
	ai500GlobalCacheMu.RLock()
	if ai500GlobalCache != nil && time.Since(ai500GlobalCacheTime) < ai500CacheTTL {
		result := make([]CoinData, len(ai500GlobalCache))
		copy(result, ai500GlobalCache)
		ai500GlobalCacheMu.RUnlock()
		log.Printf("✓ AI500 global cache hit (%d coins, cached %v ago)", len(result), time.Since(ai500GlobalCacheTime).Round(time.Second))
		return result, nil
	}
	ai500GlobalCacheMu.RUnlock()

	// Cache miss — singleflight ensures only one in-flight request
	return ai500Singleflight.Do(func() ([]CoinData, error) {
		// Double-check cache after winning singleflight (another goroutine might have just filled it)
		ai500GlobalCacheMu.RLock()
		if ai500GlobalCache != nil && time.Since(ai500GlobalCacheTime) < ai500CacheTTL {
			result := make([]CoinData, len(ai500GlobalCache))
			copy(result, ai500GlobalCache)
			ai500GlobalCacheMu.RUnlock()
			return result, nil
		}
		ai500GlobalCacheMu.RUnlock()

		// Use global client or fall back to DefaultClient
		ai500GlobalClientMu.Lock()
		client := ai500GlobalClient
		if client == nil {
			client = DefaultClient()
			log.Printf("⚠️  No global AI500 client registered, using DefaultClient")
		}
		ai500GlobalClientMu.Unlock()

		coins, err := fetchAI500WithRetry(client)
		if err != nil {
			return nil, err
		}

		// Update global cache (write lock)
		ai500GlobalCacheMu.Lock()
		ai500GlobalCache = make([]CoinData, len(coins))
		copy(ai500GlobalCache, coins)
		ai500GlobalCacheTime = time.Now()
		ai500GlobalCacheMu.Unlock()

		return coins, nil
	})
}

// fetchAI500WithRetry fetches AI500 data with retry mechanism
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

// GetAI500List delegates to the global cache function.
func (c *Client) GetAI500List() ([]CoinData, error) {
	return GetAI500ListGlobal()
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

	// Empty list is a normal condition, not an error
	if len(response.Data.Coins) == 0 {
		log.Printf("ℹ️  AI500 returned empty coin list (no coins meet criteria currently)")
		return []CoinData{}, nil
	}

	// Set IsAvailable flag
	coins := response.Data.Coins
	for i := range coins {
		coins[i].IsAvailable = true
	}

	log.Printf("✓ Successfully fetched %d AI500 coins", len(coins))
	return coins, nil
}

// GetTopRatedCoinsGlobal retrieves top N coins by score from the global cache.
func GetTopRatedCoinsGlobal(limit int) ([]string, error) {
	coins, err := GetAI500ListGlobal()
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
func (c *Client) GetTopRatedCoins(limit int) ([]string, error) {
	return GetTopRatedCoinsGlobal(limit)
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
