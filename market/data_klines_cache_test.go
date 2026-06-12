package market

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// resetKlineCache clears the package-level cache and singleflight between
// tests so each test starts from a clean slate.
func resetKlineCache() {
	klineCacheMu.Lock()
	for k := range klineCache {
		delete(klineCache, k)
	}
	klineCacheMu.Unlock()
	// singleflight.Group has no public reset; reusing a stale key across
	// tests is fine because we always use unique keys per test.
}

// makeKlines builds a deterministic series of `n` klines with the given bar
// duration (in ms). OpenTimes start at `startMs` and step by `stepMs`.
func makeKlines(n int, startMs, stepMs int64) []Kline {
	out := make([]Kline, n)
	for i := 0; i < n; i++ {
		t0 := startMs + int64(i)*stepMs
		out[i] = Kline{
			OpenTime:  t0,
			Open:      100 + float64(i),
			High:      101 + float64(i),
			Low:       99 + float64(i),
			Close:     100.5 + float64(i),
			Volume:    float64(i + 1),
			CloseTime: t0 + stepMs - 1,
		}
	}
	return out
}

// countingFetcher wraps a fetcher and counts how many times it was invoked.
type countingFetcher struct {
	mu        sync.Mutex
	calls     int
	lastLimit int
	lastKey   string
	// fetch returns the klines for the requested (limit, exchange).
	fetch func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error)
}

func (c *countingFetcher) Fetcher() klineFetcher {
	return func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
		c.mu.Lock()
		c.calls++
		c.lastLimit = limit
		c.lastKey = symbol + ":" + exchange + ":" + interval
		c.mu.Unlock()
		return c.fetch(ctx, symbol, interval, limit, exchange)
	}
}

func (c *countingFetcher) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestKlineCache_HitOnSecondCallWithinTTL(t *testing.T) {
	resetKlineCache()
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	// First call -> miss, populates cache.
	got, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("len=%d, want 100", len(got))
	}
	if cf.Calls() != 1 {
		t.Fatalf("after first call: calls=%d, want 1", cf.Calls())
	}

	// Second call within TTL -> hit, no extra fetch.
	got2, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(got2) != 100 {
		t.Fatalf("second len=%d, want 100", len(got2))
	}
	if cf.Calls() != 1 {
		t.Fatalf("after second call: calls=%d, want 1 (cache should hit)", cf.Calls())
	}
}

func TestKlineCache_InvalidateOnBarBoundary(t *testing.T) {
	resetKlineCache()

	// Build klines whose newest CloseTime is older than one bar -> the
	// bar-boundary check should mark the entry stale on the next call.
	oldCloseMs := time.Now().Add(-2 * time.Minute).UnixMilli()
	freshKlines := makeKlines(100, oldCloseMs-int64(100)*60_000, 60_000)
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return freshKlines, nil
		},
	}
	ctx := context.Background()

	if _, err := getKlinesCached(ctx, "ETHUSDT", "binance", "1m", 100, cf.Fetcher()); err != nil {
		t.Fatalf("first: %v", err)
	}
	if cf.Calls() != 1 {
		t.Fatalf("calls=%d, want 1", cf.Calls())
	}

	// Second call: base TTL is 30s, but the newest bar is >1m old -> stale.
	if _, err := getKlinesCached(ctx, "ETHUSDT", "binance", "1m", 100, cf.Fetcher()); err != nil {
		t.Fatalf("second: %v", err)
	}
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2 (bar boundary should invalidate)", cf.Calls())
	}
}

func TestKlineCache_RequestLimitLessThanCached(t *testing.T) {
	resetKlineCache()
	// Cache holds 500; request 100 -> tail of 100, no extra fetch.
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	if _, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 500, cf.Fetcher()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	cf.mu.Lock()
	if cf.lastLimit != 500 {
		cf.mu.Unlock()
		t.Fatalf("warm fetch limit=%d, want 500", cf.lastLimit)
	}
	cf.mu.Unlock()

	got, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher())
	if err != nil {
		t.Fatalf("small req: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("len=%d, want 100", len(got))
	}
	if cf.Calls() != 1 {
		t.Fatalf("calls=%d, want 1 (small req should hit cache)", cf.Calls())
	}
}

func TestKlineCache_RequestLimitGreaterThanCached(t *testing.T) {
	resetKlineCache()
	// Cache holds 100; request 500 -> re-fetch with 500.
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	if _, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher()); err != nil {
		t.Fatalf("warm: %v", err)
	}

	got, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 500, cf.Fetcher())
	if err != nil {
		t.Fatalf("big req: %v", err)
	}
	if len(got) != 500 {
		t.Fatalf("len=%d, want 500", len(got))
	}
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2 (bigger req should refetch)", cf.Calls())
	}
	cf.mu.Lock()
	if cf.lastLimit != 500 {
		cf.mu.Unlock()
		t.Fatalf("refetch limit=%d, want 500", cf.lastLimit)
	}
	cf.mu.Unlock()
}

func TestKlineCache_SingleflightCoalescesConcurrent(t *testing.T) {
	resetKlineCache()
	// 3 concurrent goroutines hit the same key+limit -> only 1 fetch.
	var inFlight int32
	var maxInFlight int32
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			now := atomic.AddInt32(&inFlight, 1)
			for {
				old := atomic.LoadInt32(&maxInFlight)
				if now <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, now) {
					break
				}
			}
			// Tiny sleep to widen the window for races.
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	const goroutines = 3
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := getKlinesCached(ctx, "SOLUSDT", "binance", "1m", 200, cf.Fetcher()); err != nil {
				t.Errorf("concurrent: %v", err)
			}
		}()
	}
	wg.Wait()

	if cf.Calls() != 1 {
		t.Fatalf("calls=%d, want 1 (singleflight should coalesce)", cf.Calls())
	}
	if atomic.LoadInt32(&maxInFlight) > 1 {
		t.Fatalf("maxInFlight=%d, want <=1", maxInFlight)
	}
}

func TestKlineCache_DifferentLimitsDoNotInterfere(t *testing.T) {
	resetKlineCache()
	// Concurrent limit=100 and limit=500 on the same key -> 2 fetches
	// (one per singleflight key), and the 500 must not get clobbered by
	// the 100's cacheSet.
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher()); err != nil {
			t.Errorf("100: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 500, cf.Fetcher()); err != nil {
			t.Errorf("500: %v", err)
		}
	}()
	wg.Wait()

	// Two distinct singleflight keys -> exactly 2 fetches.
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2", cf.Calls())
	}

	// Both must be served from cache on re-request, including the bigger
	// one (i.e. the 100-fetch did NOT shrink the 500 entry).
	got, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 500, cf.Fetcher())
	if err != nil {
		t.Fatalf("re-500: %v", err)
	}
	if len(got) != 500 {
		t.Fatalf("re-500 len=%d, want 500 (cacheSet guard failed)", len(got))
	}
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2 (re-500 should hit cache)", cf.Calls())
	}
}

func TestKlineCache_FallbackDoesNotPolluteOriginalKey(t *testing.T) {
	resetKlineCache()

	// Simulate the production fallback flow at the cache layer: a request
	// for "bybit" fails; the wrapper then requests "binance" via the same
	// cached path. The cache must only ever see successful (key, exchange)
	// pairs, so the bybit key MUST stay empty even though the overall
	// request succeeded.
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			if exchange == "bybit" {
				return nil, fmt.Errorf("bybit unavailable")
			}
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	// Step 1: bybit fetch via the cache layer -> failure, not cached.
	_, err := getKlinesCached(ctx, "BTCUSDT", "bybit", "1m", 100, cf.Fetcher())
	if err == nil {
		t.Fatalf("bybit fetch should have failed")
	}
	if _, ok := cacheGet(cacheKey("BTCUSDT", "bybit", "1m")); ok {
		t.Fatalf("bybit cache should be empty after failure")
	}

	// Step 2: simulate the wrapper falling back to binance via the cache
	// layer. (We do it manually so the mock fetcher is the one in use.)
	got, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher())
	if err != nil {
		t.Fatalf("binance fetch: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("len=%d, want 100", len(got))
	}

	// bybit still empty; binance populated.
	if _, ok := cacheGet(cacheKey("BTCUSDT", "bybit", "1m")); ok {
		t.Fatalf("bybit cache should still be empty")
	}
	if _, ok := cacheGet(cacheKey("BTCUSDT", "binance", "1m")); !ok {
		t.Fatalf("binance cache should be populated by fallback")
	}

	// Exactly 2 fetches: 1 bybit fail + 1 binance success.
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2 (bybit fail + binance success)", cf.Calls())
	}
}

func TestKlineCache_DifferentKeysAreIndependent(t *testing.T) {
	resetKlineCache()
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*60_000, 60_000), nil
		},
	}
	ctx := context.Background()

	// 3 distinct (symbol, exchange, interval) tuples.
	keys := []struct{ sym, ex, tf string }{
		{"BTCUSDT", "binance", "1m"},
		{"BTCUSDT", "bybit", "1m"},
		{"ETHUSDT", "binance", "1m"},
	}
	for _, k := range keys {
		if _, err := getKlinesCached(ctx, k.sym, k.ex, k.tf, 50, cf.Fetcher()); err != nil {
			t.Fatalf("%v: %v", k, err)
		}
	}
	if cf.Calls() != len(keys) {
		t.Fatalf("calls=%d, want %d", cf.Calls(), len(keys))
	}
}

func TestKlineCache_StaleAfterBaseTTL(t *testing.T) {
	resetKlineCache()
	// To exercise the TTL path without sleeping, we use a 3m interval
	// (base TTL = 90s) and manually age the entry past the TTL.
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return makeKlines(limit, time.Now().UnixMilli()-int64(limit)*3*60_000, 3*60_000), nil
		},
	}
	ctx := context.Background()

	if _, err := getKlinesCached(ctx, "BTCUSDT", "binance", "3m", 100, cf.Fetcher()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	// Manually age the entry past the 3m base TTL.
	klineCacheMu.Lock()
	entry := klineCache[cacheKey("BTCUSDT", "binance", "3m")]
	entry.fetchedAt = time.Now().Add(-2 * time.Minute)
	klineCacheMu.Unlock()

	if _, err := getKlinesCached(ctx, "BTCUSDT", "binance", "3m", 100, cf.Fetcher()); err != nil {
		t.Fatalf("re: %v", err)
	}
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2 (TTL expired -> refetch)", cf.Calls())
	}
}

func TestKlineCache_ErrorIsNotCached(t *testing.T) {
	resetKlineCache()
	boom := errors.New("upstream boom")
	cf := &countingFetcher{
		fetch: func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
			return nil, boom
		},
	}
	ctx := context.Background()

	_, err := getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher())
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want %v", err, boom)
	}
	// Error must NOT be cached; a second call must hit the fetcher again.
	_, err = getKlinesCached(ctx, "BTCUSDT", "binance", "1m", 100, cf.Fetcher())
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want %v", err, boom)
	}
	if cf.Calls() != 2 {
		t.Fatalf("calls=%d, want 2 (errors must not be cached)", cf.Calls())
	}
}
