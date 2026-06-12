package market

import (
	"context"
	"fmt"
	"nofx/logger"
	"nofx/provider/coinank"
	"nofx/provider/coinank/coinank_api"
	"nofx/provider/coinank/coinank_enum"
	"nofx/provider/hyperliquid"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Note: Kline data now uses free/open API (coinank_api.Kline) which doesn't require authentication

// mapCoinAnkInterval maps an interval string to the CoinAnk enum.
func mapCoinAnkInterval(interval string) (coinank_enum.Interval, error) {
	switch interval {
	case "1m":
		return coinank_enum.Minute1, nil
	case "3m":
		return coinank_enum.Minute3, nil
	case "5m":
		return coinank_enum.Minute5, nil
	case "15m":
		return coinank_enum.Minute15, nil
	case "30m":
		return coinank_enum.Minute30, nil
	case "1h":
		return coinank_enum.Hour1, nil
	case "2h":
		return coinank_enum.Hour2, nil
	case "4h":
		return coinank_enum.Hour4, nil
	case "6h":
		return coinank_enum.Hour6, nil
	case "8h":
		return coinank_enum.Hour8, nil
	case "12h":
		return coinank_enum.Hour12, nil
	case "1d":
		return coinank_enum.Day1, nil
	case "3d":
		return coinank_enum.Day3, nil
	case "1w":
		return coinank_enum.Week1, nil
	default:
		return "", fmt.Errorf("unsupported interval: %s", interval)
	}
}

// mapCoinAnkExchange maps an exchange string to the CoinAnk enum.
func mapCoinAnkExchange(exchange string) coinank_enum.Exchange {
	switch strings.ToLower(exchange) {
	case "binance":
		return coinank_enum.Binance
	case "bybit":
		return coinank_enum.Bybit
	case "okx":
		return coinank_enum.Okex
	case "bitget":
		return coinank_enum.Bitget
	case "gate":
		return coinank_enum.Gate
	case "hyperliquid":
		return coinank_enum.Hyperliquid
	case "aster":
		return coinank_enum.Aster
	default:
		// Note: unknown exchanges silently fall back to Binance. This means
		// Binance data will be cached under the original exchange key, which
		// is a pre-existing trade-off carried over from the legacy fetcher.
		return coinank_enum.Binance
	}
}

// convertCoinAnkKlines converts coinank kline format to market.Kline format.
func convertCoinAnkKlines(coinankKlines []coinank.KlineResult) []Kline {
	klines := make([]Kline, len(coinankKlines))
	for i, ck := range coinankKlines {
		klines[i] = Kline{
			OpenTime:  ck.StartTime,
			Open:      ck.Open,
			High:      ck.High,
			Low:       ck.Low,
			Close:     ck.Close,
			Volume:    ck.Volume,
			CloseTime: ck.EndTime,
		}
	}
	return klines
}

// fetchKlinesCoinAnkRaw performs a single CoinAnk API request for the given
// exchange, with NO fallback. The returned data is authoritative for the
// (symbol, exchange, interval) key. Used as the underlying fetcher for the
// cache layer, which must never silently substitute another exchange's data.
func fetchKlinesCoinAnkRaw(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
	coinankInterval, err := mapCoinAnkInterval(interval)
	if err != nil {
		return nil, err
	}
	coinankExchange := mapCoinAnkExchange(exchange)

	ts := time.Now().UnixMilli()
	coinankKlines, err := coinank_api.Kline(ctx, symbol, coinankExchange, ts, coinank_enum.To, limit, coinankInterval)
	if err != nil {
		return nil, fmt.Errorf("CoinAnk %s %s %s: %w", exchange, symbol, interval, err)
	}
	if len(coinankKlines) == 0 {
		// Binance + empty data: likely silent rate-limiting
		// (CoinAnk free API returns {"success":true, "data":[]} when throttled).
		if coinankExchange == coinank_enum.Binance {
			logger.Warnf("⚠️ CoinAnk Binance %s %s returned empty data (possible rate limiting)", symbol, interval)
			return nil, fmt.Errorf("CoinAnk Binance %s %s returned empty kline data (possible rate limiting)", symbol, interval)
		}
		return nil, fmt.Errorf("CoinAnk %s %s %s returned empty data", exchange, symbol, interval)
	}
	return convertCoinAnkKlines(coinankKlines), nil
}

// getKlinesFromCoinAnkContext is the public fallback wrapper. It first tries
// the requested exchange; on failure or empty data, it falls back to Binance
// (and that fallback also goes through the cache, so a Binance outage does
// not amplify the load).
func getKlinesFromCoinAnkContext(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
	klines, err := getKlinesCached(ctx, symbol, exchange, interval, limit, fetchKlinesCoinAnkRaw)
	if err == nil {
		return klines, nil
	}
	if strings.EqualFold(exchange, "binance") {
		return nil, err
	}
	logger.Warnf("⚠️ CoinAnk %s data failed, falling back to Binance: %v", exchange, err)
	return getKlinesCached(ctx, symbol, "binance", interval, limit, fetchKlinesCoinAnkRaw)
}

// getKlinesFromCoinAnk is the legacy no-context entry point. It delegates to
// the context-aware version with a background context.
func getKlinesFromCoinAnk(symbol, interval, exchange string, limit int) ([]Kline, error) {
	return getKlinesFromCoinAnkContext(context.Background(), symbol, interval, limit, exchange)
}

// fetchKlinesHyperliquidRaw performs a single Hyperliquid API request with NO
// fallback. Used as the underlying fetcher for the cache layer.
func fetchKlinesHyperliquidRaw(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
	// Remove xyz: prefix if present for the API call
	baseCoin := strings.TrimPrefix(symbol, "xyz:")

	hlInterval := hyperliquid.MapTimeframe(interval)

	client := hyperliquid.NewClient()
	candles, err := client.GetCandles(ctx, baseCoin, hlInterval, limit)
	if err != nil {
		return nil, fmt.Errorf("Hyperliquid API error: %w", err)
	}

	klines := make([]Kline, len(candles))
	for i, c := range candles {
		open, _ := strconv.ParseFloat(c.Open, 64)
		high, _ := strconv.ParseFloat(c.High, 64)
		low, _ := strconv.ParseFloat(c.Low, 64)
		closePrice, _ := strconv.ParseFloat(c.Close, 64)
		volume, _ := strconv.ParseFloat(c.Volume, 64)

		klines[i] = Kline{
			OpenTime:  c.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: c.CloseTime,
		}
	}
	return klines, nil
}

// getKlinesFromHyperliquidContext is the context-aware Hyperliquid fetcher.
// Hyperliquid has no fallback exchange, so this just goes through the cache
// directly.
func getKlinesFromHyperliquidContext(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	return getKlinesCached(ctx, symbol, "hyperliquid", interval, limit, fetchKlinesHyperliquidRaw)
}

// getKlinesFromHyperliquid is the legacy no-context entry point. It delegates
// to the context-aware version with a background context.
func getKlinesFromHyperliquid(symbol, interval string, limit int) ([]Kline, error) {
	return getKlinesFromHyperliquidContext(context.Background(), symbol, interval, limit)
}

// ---------------------------------------------------------------------------
// Kline cache layer (see docs/plans/2026-06-12-kline-cache-design.md)
// ---------------------------------------------------------------------------

// klineFetcher is the function signature used by getKlinesCached. It performs
// a single, side-effect-free fetch for a (symbol, interval, limit, exchange)
// tuple. The cache layer treats the returned data as authoritative for that
// tuple; the fetcher must NOT transparently fall back to a different
// exchange, or the cache would store data under a misleading key.
type klineFetcher func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error)

type klineCacheEntry struct {
	klines    []Kline
	fetchedAt time.Time
	limit     int
}

var (
	klineCache   = make(map[string]*klineCacheEntry)
	klineCacheMu sync.RWMutex
	klineSF      singleflight.Group
)

func init() {
	go cleanExpiredCacheLoop(5 * time.Minute)
}

// cacheKey returns the storage key for a (symbol, exchange, interval) tuple.
func cacheKey(symbol, exchange, interval string) string {
	return symbol + ":" + strings.ToLower(exchange) + ":" + interval
}

// baseTTL returns the base freshness window for a given interval. A new bar
// (when it closes) is the actual signal that data has changed; the TTL is
// just a backstop so we never serve indefinitely stale data.
func baseTTL(interval string) time.Duration {
	switch interval {
	case "1m":
		return 30 * time.Second
	case "3m":
		return 90 * time.Second
	case "5m":
		return 150 * time.Second
	default:
		// 15m and above: short TTL is enough; a single decision cycle reuses it.
		return 30 * time.Second
	}
}

// intervalDuration returns the duration of a single bar for the given
// interval. Returns 0 for unknown intervals.
func intervalDuration(interval string) time.Duration {
	mins := parseTimeframeToMinutes(interval)
	if mins <= 0 {
		return 0
	}
	return time.Duration(mins) * time.Minute
}

// cacheValid reports whether the cached entry is still fresh enough to serve.
// It enforces both the base TTL and a "bar boundary" check: if at least one
// full bar has closed since the newest cached bar, we have definitely missed
// a bar and must refetch.
func cacheValid(entry *klineCacheEntry, interval string) bool {
	if entry == nil {
		return false
	}
	if time.Since(entry.fetchedAt) >= baseTTL(interval) {
		return false
	}
	if len(entry.klines) == 0 {
		return true
	}
	barDuration := intervalDuration(interval)
	if barDuration <= 0 {
		return true
	}
	newestBar := entry.klines[len(entry.klines)-1]
	if newestBar.CloseTime <= 0 {
		// No CloseTime: degrade to base TTL only.
		return true
	}
	if time.Since(time.UnixMilli(newestBar.CloseTime)) > barDuration {
		return false
	}
	return true
}

// cacheGet returns a copy of the cache entry pointer (not a deep copy of the
// kline slice) under the read lock. Callers must not mutate the kline slice.
func cacheGet(key string) (*klineCacheEntry, bool) {
	klineCacheMu.RLock()
	defer klineCacheMu.RUnlock()
	entry, ok := klineCache[key]
	return entry, ok
}

// cacheSet stores klines under the given key. The entry is only overwritten
// if the new fetch's limit is >= the existing entry's limit, so a concurrent
// "small" request can't shrink a "big" request's cache.
func cacheSet(key string, klines []Kline, limit int) {
	klineCacheMu.Lock()
	defer klineCacheMu.Unlock()
	if existing, ok := klineCache[key]; ok && existing.limit > limit {
		return
	}
	klineCache[key] = &klineCacheEntry{
		klines:    klines,
		fetchedAt: time.Now(),
		limit:     limit,
	}
}

// sliceTail returns the last `limit` klines (or all of them if shorter).
func sliceTail(klines []Kline, limit int) []Kline {
	if limit <= 0 || len(klines) <= limit {
		// Return a copy so callers can't mutate the cache.
		out := make([]Kline, len(klines))
		copy(out, klines)
		return out
	}
	out := make([]Kline, limit)
	copy(out, klines[len(klines)-limit:])
	return out
}

// getKlinesCached returns klines for (symbol, exchange, interval) with a
// per-process in-memory cache. Concurrent requests for the same key+limit
// share a single underlying fetch via singleflight. The fetcher is injected
// so callers (and tests) can swap in mocks.
func getKlinesCached(ctx context.Context, symbol, exchange, interval string, limit int, fetch klineFetcher) ([]Kline, error) {
	if fetch == nil {
		return nil, fmt.Errorf("getKlinesCached: nil fetcher")
	}
	cKey := cacheKey(symbol, exchange, interval)

	// Fast path: cache hit, fully covers the requested limit, no singleflight.
	if entry, ok := cacheGet(cKey); ok && cacheValid(entry, interval) && entry.limit >= limit {
		logger.Debugf("kline cache hit: %s limit=%d", cKey, limit)
		return sliceTail(entry.klines, limit), nil
	}

	// Slow path: singleflight-coalesce fetches per (key, limit).
	sfKey := cKey + ":" + strconv.Itoa(limit)
	v, err, _ := klineSF.Do(sfKey, func() (interface{}, error) {
		// Double-check: another goroutine may have filled the cache while we
		// were waiting on singleflight.
		if entry, ok := cacheGet(cKey); ok && cacheValid(entry, interval) && entry.limit >= limit {
			return sliceTail(entry.klines, limit), nil
		}

		// If a "bigger" entry exists but is stale, we re-fetch with the
		// bigger limit to amortize future requests; otherwise we fetch
		// exactly what was asked for.
		fetchLimit := limit
		if entry, ok := cacheGet(cKey); ok && entry.limit > fetchLimit {
			fetchLimit = entry.limit
		}

		klines, err := fetch(ctx, symbol, interval, fetchLimit, exchange)
		if err != nil {
			return nil, err
		}
		cacheSet(cKey, klines, fetchLimit)
		return klines, nil
	})
	if err != nil {
		return nil, err
	}
	return sliceTail(v.([]Kline), limit), nil
}

// cleanExpiredCacheLoop periodically drops entries whose fetchedAt + TTL is
// already in the past. Prevents stale residue from symbols that have been
// removed from the candidate set. The TTL is computed per-entry by extracting
// the interval from the cache key (format: symbol:exchange:interval).
func cleanExpiredCacheLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		klineCacheMu.Lock()
		for key, entry := range klineCache {
			// Extract interval from the last colon-separated segment.
			// Fall back to 5 minutes for keys that don't match the expected format.
			ttl := 5 * time.Minute
			if idx := strings.LastIndex(key, ":"); idx >= 0 && idx+1 < len(key) {
				if entryTTL := baseTTL(key[idx+1:]); entryTTL > 0 {
					ttl = entryTTL + 30*time.Second // small grace period
				}
			}
			if now.Sub(entry.fetchedAt) >= ttl {
				delete(klineCache, key)
			}
		}
		klineCacheMu.Unlock()
	}
}

// calculateTimeframeSeries calculates series data for a single timeframe
func calculateTimeframeSeries(klines []Kline, timeframe string, count int, adxPeriod int, smaPeriods ...int) *TimeframeSeriesData {
	if count <= 0 {
		count = 10 // default
	}

	data := &TimeframeSeriesData{
		Timeframe:   timeframe,
		ComputeBars: klines, // full calc window, never serialized
		Klines:      make([]KlineBar, 0, count),
		MidPrices:   make([]float64, 0, count),
		EMA20Values: make([]float64, 0, count),
		EMA50Values: make([]float64, 0, count),
		SMAValues:     make(map[int][]float64),
		ADXValues:     make([]float64, 0, count),
		PlusDIValues:  make([]float64, 0, count),
		MinusDIValues: make([]float64, 0, count),
		SARValues:     make([]float64, 0, count),
		SARUptrend:    make([]bool, 0, count),
		SARFlipUp:     make([]bool, 0, count),
		SARFlipDown:   make([]bool, 0, count),
		MACDValues:    make([]float64, 0, count),
		RSI7Values:    make([]float64, 0, count),
		RSI14Values:   make([]float64, 0, count),
		Volume:        make([]float64, 0, count),
		BOLLUpper:     make([]float64, 0, count),
		BOLLMiddle:    make([]float64, 0, count),
		BOLLLower:     make([]float64, 0, count),
	}

	// Get latest N data points based on count from config
	start := len(klines) - count
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		// Store full OHLCV kline data
		data.Klines = append(data.Klines, KlineBar{
			Time:   klines[i].OpenTime,
			Open:   klines[i].Open,
			High:   klines[i].High,
			Low:    klines[i].Low,
			Close:  klines[i].Close,
			Volume: klines[i].Volume,
		})

		// Keep MidPrices and Volume for backward compatibility
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate EMA50 for each point
		if i >= 49 {
			ema50 := calculateEMA(klines[:i+1], 50)
			data.EMA50Values = append(data.EMA50Values, ema50)
		}

		// Calculate SMA for configured periods
		for _, p := range smaPeriods {
			if p > 0 && i >= p-1 {
				sma := calculateSMA(klines[:i+1], p)
				data.SMAValues[p] = append(data.SMAValues[p], sma)
			}
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}

		// Calculate Bollinger Bands (period 20, std dev multiplier 2)
		if i >= 19 {
			upper, middle, lower := calculateBOLL(klines[:i+1], 20, 2.0)
			data.BOLLUpper = append(data.BOLLUpper, upper)
			data.BOLLMiddle = append(data.BOLLMiddle, middle)
			data.BOLLLower = append(data.BOLLLower, lower)
		}

		// Calculate ADX for each point
		if adxPeriod > 0 && i >= 2*adxPeriod {
			adx, plusDI, minusDI := calculateADX(klines[:i+1], adxPeriod)
			data.ADXValues = append(data.ADXValues, adx)
			data.PlusDIValues = append(data.PlusDIValues, plusDI)
			data.MinusDIValues = append(data.MinusDIValues, minusDI)
		}

		// Calculate Parabolic SAR for each point
		if i >= 1 {
			sar, isUp, flipUp, flipDown, af := calculateParabolicSAR(klines[:i+1])
			data.SARValues = append(data.SARValues, sar)
			data.SARUptrend = append(data.SARUptrend, isUp)
			data.SARFlipUp = append(data.SARFlipUp, flipUp)
			data.SARFlipDown = append(data.SARFlipDown, flipDown)
				data.SARAF = af
		}
	}

	// Calculate ATR14
	data.ATR14 = calculateATR(klines, 14)
	data.BBMACD = calculateBBMACD(klines)

	return data
}

// calculatePriceChangeByBars calculates how many K-lines to look back for price change based on timeframe
func calculatePriceChangeByBars(klines []Kline, timeframe string, targetMinutes int) float64 {
	if len(klines) < 2 {
		return 0
	}

	// Parse timeframe to minutes
	tfMinutes := parseTimeframeToMinutes(timeframe)
	if tfMinutes <= 0 {
		return 0
	}

	// Calculate how many K-lines to look back
	barsBack := targetMinutes / tfMinutes
	if barsBack < 1 {
		barsBack = 1
	}

	currentPrice := klines[len(klines)-1].Close
	idx := len(klines) - 1 - barsBack
	if idx < 0 {
		idx = 0
	}

	oldPrice := klines[idx].Close
	if oldPrice > 0 {
		return ((currentPrice - oldPrice) / oldPrice) * 100
	}
	return 0
}

// parseTimeframeToMinutes parses timeframe string to minutes
func parseTimeframeToMinutes(tf string) int {
	switch tf {
	case "1m":
		return 1
	case "3m":
		return 3
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "6h":
		return 360
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "3d":
		return 4320
	case "1w":
		return 10080
	default:
		return 0
	}
}

// calculateIntradaySeries calculates intraday series data
func calculateIntradaySeries(klines []Kline, adxPeriod int, smaPeriods ...int) *IntradayData {
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		SMAValues:     make(map[int][]float64),
		ADXValues:     make([]float64, 0, 10),
		PlusDIValues:  make([]float64, 0, 10),
		MinusDIValues: make([]float64, 0, 10),
		SARValues:     make([]float64, 0, 10),
		SARUptrend:    make([]bool, 0, 10),
		SARFlipUp:     make([]bool, 0, 10),
		SARFlipDown:   make([]bool, 0, 10),
		MACDValues:    make([]float64, 0, 10),
		RSI7Values:    make([]float64, 0, 10),
		RSI14Values:   make([]float64, 0, 10),
		Volume:        make([]float64, 0, 10),
	}

	// Get latest 10 data points
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate SMA for configured periods
		for _, p := range smaPeriods {
			if p > 0 && i >= p-1 {
				sma := calculateSMA(klines[:i+1], p)
				data.SMAValues[p] = append(data.SMAValues[p], sma)
			}
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}

		// Calculate ADX for each point
		if adxPeriod > 0 && i >= 2*adxPeriod {
			adx, plusDI, minusDI := calculateADX(klines[:i+1], adxPeriod)
			data.ADXValues = append(data.ADXValues, adx)
			data.PlusDIValues = append(data.PlusDIValues, plusDI)
			data.MinusDIValues = append(data.MinusDIValues, minusDI)
		}

		// Calculate Parabolic SAR for each point
		if i >= 1 {
			sar, isUp, flipUp, flipDown, _ := calculateParabolicSAR(klines[:i+1])
			data.SARValues = append(data.SARValues, sar)
			data.SARUptrend = append(data.SARUptrend, isUp)
			data.SARFlipUp = append(data.SARFlipUp, flipUp)
			data.SARFlipDown = append(data.SARFlipDown, flipDown)
		}
	}

	// Calculate 3m ATR14
	data.ATR14 = calculateATR(klines, 14)

	return data
}

// calculateLongerTermData calculates longer-term data
func calculateLongerTermData(klines []Kline, adxPeriod int, smaPeriods ...int) *LongerTermData {
	data := &LongerTermData{
		SMA:         make(map[int]float64),
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Calculate EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// Calculate ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// Calculate ADX
	if adxPeriod > 0 && len(klines) >= 2*adxPeriod+1 {
		data.ADX, data.PlusDI, data.MinusDI = calculateADX(klines, adxPeriod)
	}

	// Calculate Parabolic SAR
	if len(klines) >= 2 {
		data.SAR, data.SARIsUptrend, data.SARFlipUp, data.SARFlipDown, _ = calculateParabolicSAR(klines)
	}

	// Calculate volume
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// Calculate average volume
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// Calculate MACD and RSI series
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// GetBoxData fetches 1h klines and calculates box data for a symbol
func GetBoxData(symbol string) (*BoxData, error) {
	symbol = Normalize(symbol)

	// Fetch 500 1h klines
	var klines []Kline
	var err error

	if IsXyzDexAsset(symbol) {
		klines, err = getKlinesFromHyperliquidContext(context.Background(), symbol, "1h", LongBoxPeriod)
	} else {
		klines, err = getKlinesFromCoinAnkContext(context.Background(), symbol, "1h", LongBoxPeriod, "binance")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get 1h klines: %w", err)
	}

	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data available")
	}

	currentPrice := klines[len(klines)-1].Close

	return calculateBoxData(klines, currentPrice), nil
}
