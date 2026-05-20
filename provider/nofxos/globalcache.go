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
	maxCallRecords  = 200
)

// ── Call recording ───────────────────────────────────────────────────────────

// CallRecord tracks a single nofxos API call for monitoring.
type CallRecord struct {
	Endpoint   string        `json:"endpoint"`
	TraderID   string        `json:"trader_id"`
	TraderName string        `json:"trader_name"`
	CalledAt   time.Time     `json:"called_at"`
	CacheHit   bool          `json:"cache_hit"`
	Success    bool          `json:"success"`
	DataCount  int           `json:"data_count"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
}

var (
	callRecords   []CallRecord
	callRecordsMu sync.RWMutex
)

func addCallRecord(r CallRecord) {
	callRecordsMu.Lock()
	defer callRecordsMu.Unlock()
	if len(callRecords) >= maxCallRecords {
		callRecords = callRecords[1:]
	}
	callRecords = append(callRecords, r)
}

// GetCallRecords returns the recent call records (newest last).
func GetCallRecords() []CallRecord {
	callRecordsMu.RLock()
	defer callRecordsMu.RUnlock()
	result := make([]CallRecord, len(callRecords))
	copy(result, callRecords)
	return result
}

// CallOption is a functional option for globalCache.Get.
type CallOption struct {
	Endpoint   string
	TraderID   string
	TraderName string
}

func dataCount(v any) int {
	switch val := v.(type) {
	case []CoinData:
		return len(val)
	case *OIRankingData:
		if val == nil {
			return 0
		}
		return len(val.TopPositions) + len(val.LowPositions)
	case *NetFlowRankingData:
		if val == nil {
			return 0
		}
		return len(val.InstitutionFutureTop) + len(val.InstitutionFutureLow) +
			len(val.PersonalFutureTop) + len(val.PersonalFutureLow)
	case *PriceRankingData:
		if val == nil {
			return 0
		}
		return len(val.Durations)
	default:
		return 0
	}
}

// ── Global cache ────────────────────────────────────────────────────────────

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
// Optionally pass CallOption to record the call for monitoring.
func (gc *globalCache[T]) Get(fetch func() (T, error), opts ...CallOption) (T, error) {
	var opt CallOption
	if len(opts) > 0 {
		opt = opts[0]
	}
	start := time.Now()

	// Fast path: cache hit
	if data, ok := gc.tryGet(); ok {
		if opt.Endpoint != "" {
			addCallRecord(CallRecord{
				Endpoint:   opt.Endpoint,
				TraderID:   opt.TraderID,
				TraderName: opt.TraderName,
				CalledAt:   start,
				CacheHit:   true,
				Success:    true,
				DataCount:  dataCount(any(data)),
				Duration:   time.Since(start),
			})
		}
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
			if opt.Endpoint != "" {
				addCallRecord(CallRecord{
					Endpoint:   opt.Endpoint,
					TraderID:   opt.TraderID,
					TraderName: opt.TraderName,
					CalledAt:   start,
					CacheHit:   true,
					Success:    true,
					DataCount:  dataCount(any(data)),
					Duration:   time.Since(start),
				})
			}
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
		if opt.Endpoint != "" {
			addCallRecord(CallRecord{
				Endpoint:   opt.Endpoint,
				TraderID:   opt.TraderID,
				TraderName: opt.TraderName,
				CalledAt:   start,
				CacheHit:   true,
				Success:    true,
				DataCount:  dataCount(any(data)),
				Duration:   time.Since(start),
			})
		}
		return data, nil
	}

	// Fetch fresh data
	result, err := fetch()
	if opt.Endpoint != "" {
		rec := CallRecord{
			Endpoint:   opt.Endpoint,
			TraderID:   opt.TraderID,
			TraderName: opt.TraderName,
			CalledAt:   start,
			CacheHit:   false,
			Success:    err == nil,
			DataCount:  dataCount(any(result)),
			Duration:   time.Since(start),
		}
		if err != nil {
			rec.Error = err.Error()
		}
		addCallRecord(rec)
	}
	if err != nil {
		var zero T
		return zero, err
	}

	// Don't cache empty results — they are likely transient (API glitch, no coins meeting criteria)
	// and would block all users for the full TTL if cached.
	if !gc.shouldCache(result) {
		return result, nil
	}

	// Update cache
	gc.mu.Lock()
	gc.data = result
	gc.valid = true
	gc.stamp = time.Now()
	gc.mu.Unlock()

	return result, nil
}

// shouldCache returns false for empty/zero results that shouldn't be cached.
func (gc *globalCache[T]) shouldCache(v T) bool {
	switch val := any(v).(type) {
	case []CoinData:
		return len(val) > 0
	case *OIRankingData:
		return val != nil && (len(val.TopPositions) > 0 || len(val.LowPositions) > 0)
	case *NetFlowRankingData:
		return val != nil && (len(val.InstitutionFutureTop) > 0 || len(val.InstitutionFutureLow) > 0)
	case *PriceRankingData:
		return val != nil && len(val.Durations) > 0
	default:
		return true
	}
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
