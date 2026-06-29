package smallcap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"nofx/logger"
	"nofx/provider/coinmarketcap"
	"nofx/store"
	"golang.org/x/sync/singleflight"
)

const (
	defaultCMCFetchSize = 5000
	maxCMCPages         = 4 // 4 x 5000 = 20000 coins
)

// CoinMarketCapProvider builds Small Market Value rankings from CoinMarketCap
// listings, validated against Binance futures tradability and enriched with OI.
type CoinMarketCapProvider struct {
	binanceHelper

	client *coinmarketcap.Client
	ttl    time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry

	flight singleflight.Group

	// supplyStore is optional. When set, the provider writes fetched data to
	// the local cache so the CoinGecko/local path can also benefit.
	supplyStore *store.CoinSupplyStore
}

// NewCoinMarketCapProvider creates a provider using the supplied CoinMarketCap client.
func NewCoinMarketCapProvider(client *coinmarketcap.Client) *CoinMarketCapProvider {
	return &CoinMarketCapProvider{
		client: client,
		ttl:    DefaultSmallMarketValueCacheTTL,
		cache:  make(map[string]cacheEntry),
	}
}

// WithSupplyStore enables writing fetched supply data back to the local cache.
func (p *CoinMarketCapProvider) WithSupplyStore(s *store.CoinSupplyStore) *CoinMarketCapProvider {
	p.supplyStore = s
	return p
}

func (p *CoinMarketCapProvider) cacheKey(req SmallMarketValueRequest) string {
	return fmt.Sprintf("cmc:%d:%.0f:%.0f:%s",
		req.Limit, req.Min24hQuoteVolumeUSD,
		req.MinOpenInterestUSD, req.SortBy)
}

// GetSmallMarketValueRanking returns small market value candidates from CoinMarketCap.
func (p *CoinMarketCapProvider) GetSmallMarketValueRanking(ctx context.Context, req SmallMarketValueRequest) (*SmallMarketValueRankingData, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("small market value CoinMarketCap provider is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 3
	}

	cacheKey := p.cacheKey(req)

	// Fast path: fresh cache.
	p.mu.RLock()
	entry, ok := p.cache[cacheKey]
	p.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.data, nil
	}

	// Single-flight: only one goroutine fetches for each cache key.
	v, err, _ := p.flight.Do(cacheKey, func() (interface{}, error) {
		// Double-check cache after winning the race.
		p.mu.RLock()
		entry, ok := p.cache[cacheKey]
		p.mu.RUnlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return entry.data, nil
		}

		result, fetchErr := p.fetchRanking(ctx, req, limit)
		if fetchErr != nil {
			return nil, fetchErr
		}
		p.mu.Lock()
		p.cache[cacheKey] = cacheEntry{
			data:      result,
			expiresAt: time.Now().Add(p.ttl),
		}
		p.mu.Unlock()
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*SmallMarketValueRankingData), nil
}

func (p *CoinMarketCapProvider) fetchRanking(ctx context.Context, req SmallMarketValueRequest, limit int) (*SmallMarketValueRankingData, error) {
	binanceSymbols, err := p.getBinanceSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch Binance tradable symbols: %w", err)
	}

	filterReq := req
	filterReq.Limit = limit
	filterReq.MinDepthUSD = 0

	// Build a deduplicated universe keyed by Binance symbol. CMC can return
	// multiple tokens with the same ticker; keep the one with the largest
	// market cap so we don't accidentally select a worthless duplicate.
	bestBySymbol := make(map[string]SmallMarketValueCoin)

	var oiAttemptCount int
	var oiSuccessCount int

	for page := 1; page <= maxCMCPages; page++ {
		data, err := p.client.ListingsLatest(ctx, coinmarketcap.ListingsLatestRequest{
			Start:   (page-1)*defaultCMCFetchSize + 1,
			Limit:   defaultCMCFetchSize,
			Convert: "USD",
		})
		if err != nil {
			return nil, fmt.Errorf("fetch CoinMarketCap listings page %d: %w", page, err)
		}
		if len(data) == 0 {
			break
		}

		for _, d := range data {
			symbol := normalizeSymbol(d.Symbol, "")
			if symbol == "" {
				continue
			}
			if _, tradable := binanceSymbols[symbol]; !tradable {
				continue
			}

			quote := d.Quote["USD"]
			coin := SmallMarketValueCoin{
				Symbol:            symbol,
				Price:             quote.Price,
				CirculatingSupply: d.CirculatingSupply,
				TotalSupply:       d.TotalSupply,
				MarketCap:         quote.MarketCap,
				FDV:               quote.FullyDilutedMarketCap,
				Volume24hUSD:      quote.Volume24h,
			}

			existing, ok := bestBySymbol[symbol]
			if !ok || coin.MarketCap > existing.MarketCap {
				bestBySymbol[symbol] = coin
			}
		}

		if len(bestBySymbol) >= limit*5 || len(data) < defaultCMCFetchSize {
			break
		}
	}

	allCoins := make([]SmallMarketValueCoin, 0, len(bestBySymbol))
	records := make([]store.CoinSupply, 0, len(bestBySymbol))
	for _, coin := range bestBySymbol {
		allCoins = append(allCoins, coin)
		records = append(records, store.CoinSupply{
			Symbol:            coin.Symbol,
			CoingeckoID:       "",
			Name:              "",
			CirculatingSupply: coin.CirculatingSupply,
			TotalSupply:       coin.TotalSupply,
			MarketCapUSD:      coin.MarketCap,
			Volume24hUSD:      coin.Volume24hUSD,
			LastUpdatedAt:     time.Now().UTC(),
			Source:            "coinmarketcap",
		})
	}

	if len(records) > 0 && p.supplyStore != nil {
		if upsertErr := p.supplyStore.Upsert(records); upsertErr != nil {
			logger.Warnf("CoinMarketCap provider upsert failed: %v", upsertErr)
		}
	}

	raw := make([]*SmallMarketValueCoin, len(allCoins))
	for i := range allCoins {
		raw[i] = &allCoins[i]
	}
	attempts, successes := p.enrichOpenInterest(ctx, raw)
	oiAttemptCount += attempts
	oiSuccessCount += successes
	if attempts > 0 && successes == 0 && oiSuccessCount == 0 {
		filterReq.MinOpenInterestUSD = 0
	}

	filtered := filterAndScore(allCoins, filterReq)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return &SmallMarketValueRankingData{
		Coins:          filtered,
		FilterStats:    computeFilterStats(allCoins, filtered),
		DepthAvailable: false,
		OIAvailable:    oiAttemptCount == 0 || oiSuccessCount > 0,
		FetchedAt:      time.Now().UTC(),
	}, nil
}
