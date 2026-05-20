package nofxos

import (
	"log"
	"sync"
	"time"
)

// Default TTL for nofxos data caches (30 minutes).
const (
	defaultCacheTTL = 30 * time.Minute
	maxCallRecords  = 200
)

// simpleCache is a minimal TTL cache used by OI/NetFlow/Price.
type simpleCache[T any] struct {
	mu    sync.RWMutex
	data  T
	stamp time.Time
	ttl   time.Duration
}

func (sc *simpleCache[T]) get() (T, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if !sc.stamp.IsZero() && time.Since(sc.stamp) < sc.ttl {
		return sc.data, true
	}
	var zero T
	return zero, false
}

func (sc *simpleCache[T]) set(data T) {
	sc.mu.Lock()
	sc.data = data
	sc.stamp = time.Now()
	sc.mu.Unlock()
}

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

// ── Global client registry ──────────────────────────────────────────────────

var (
	ai500GlobalClient   *Client
	ai500GlobalClientMu sync.Mutex
)

// SetAI500GlobalClient registers a Client for global nofxos data fetching.
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
	}
	ai500GlobalClientMu.Unlock()
	return client
}
