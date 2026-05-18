package nofxos

import (
	"log"
	"sync"
	"time"
)

// Default TTLs for nofxos data caches
const (
	defaultCacheTTL = 30 * time.Minute
	ai500CacheTTL   = 2 * time.Hour
)

// globalCache provides a thread-safe TTL cache with singleflight deduplication.
type globalCache[T any] struct {
	mu     sync.RWMutex
	data   T
	valid  bool
	stamp  time.Time
	ttl    time.Duration
	sfMu   sync.Mutex
	sfCall chan struct{}
}

// Get returns cached data if still valid, otherwise calls fetch to refresh.
// Concurrent cache-miss calls are merged via singleflight.
func (gc *globalCache[T]) Get(fetch func() (T, error)) (T, error) {
	// Fast path: cache hit
	if data, ok := gc.tryGet(); ok {
		return data, nil
	}

	// Singleflight: only one goroutine fetches at a time
	gc.sfMu.Lock()
	if gc.sfCall != nil {
		waitCh := gc.sfCall
		gc.sfMu.Unlock()
		<-waitCh
		// Re-read cache after waiting
		if data, ok := gc.tryGet(); ok {
			return data, nil
		}
		// Cache still stale (fetch failed) — fall through to try ourselves
		gc.sfMu.Lock()
	}

	done := make(chan struct{})
	gc.sfCall = done
	gc.sfMu.Unlock()

	defer func() {
		gc.sfMu.Lock()
		gc.sfCall = nil
		close(done)
		gc.sfMu.Unlock()
	}()

	// Double-check after winning singleflight
	if data, ok := gc.tryGet(); ok {
		return data, nil
	}

	// Fetch fresh data
	result, err := fetch()
	if err != nil {
		var zero T
		return zero, err
	}

	// Update cache
	gc.mu.Lock()
	gc.data = result
	gc.valid = true
	gc.stamp = time.Now()
	gc.mu.Unlock()

	return result, nil
}

// tryGet attempts a cache read under RLock. Returns (data, true) if cache is valid.
func (gc *globalCache[T]) tryGet() (T, bool) {
	gc.mu.RLock()
	defer gc.mu.RUnlock()
	if gc.valid && !gc.stamp.IsZero() && time.Since(gc.stamp) < gc.ttl {
		return gc.data, true
	}
	var zero T
	return zero, false
}

// ── Global client registry ──────────────────────────────────────────────────

var (
	ai500GlobalClient   *Client
	ai500GlobalClientMu sync.Mutex
)

// SetAI500GlobalClient registers a Client for global nofxos data fetching.
// A client with claw402 always overrides one without (ensures payment routing
// is upgraded even if a non-claw402 client registered first).
func SetAI500GlobalClient(client *Client) {
	ai500GlobalClientMu.Lock()
	defer ai500GlobalClientMu.Unlock()
	hasClaw402 := client.claw402 != nil
	if ai500GlobalClient == nil || hasClaw402 {
		ai500GlobalClient = client
		log.Printf("🔗 NofxOS global client registered (claw402: %v)", hasClaw402)
	}
}

// GetGlobalClient returns the registered global client or DefaultClient.
func GetGlobalClient() *Client {
	ai500GlobalClientMu.Lock()
	client := ai500GlobalClient
	if client == nil {
		client = DefaultClient()
		log.Printf("⚠️  No global nofxos client registered, using DefaultClient")
	}
	ai500GlobalClientMu.Unlock()
	return client
}

// ── Global cache instances ──────────────────────────────────────────────────

var (
	ai500Cache = globalCache[[]CoinData]{ttl: ai500CacheTTL}
)
