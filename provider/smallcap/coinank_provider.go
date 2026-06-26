package smallcap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"nofx/provider/coinank"
	"nofx/provider/coinank/coinank_enum"
)

const (
	defaultCoinAnkFetchSize = 50
	minCoinAnkFetchSize     = 20
	maxCoinAnkFetchSize     = 100
	maxCoinAnkPages         = 5
	coinAnkEnrichWorkers    = 8
	// DefaultSmallMarketValueCacheTTL is the default time-to-live for cached
	// small market value rankings. Multiple strategies/traders can share the
	// same cached result to avoid hammering the upstream API.
	DefaultSmallMarketValueCacheTTL = 2 * time.Hour
)

// cacheEntry stores a cached ranking result and its expiration time.
type cacheEntry struct {
	data       *SmallMarketValueRankingData
	expiresAt  time.Time
}

// CoinAnkProvider builds Small Market Value rankings from CoinAnk aggregate data.
type CoinAnkProvider struct {
	client    *coinank.CoinankClient
	fetchSize int
	ttl       time.Duration

	mu     sync.RWMutex
	cache  map[string]cacheEntry
}

// NewCoinAnkProvider creates the default CoinAnk-backed small market value provider.
func NewCoinAnkProvider(apiKey string) *CoinAnkProvider {
	return &CoinAnkProvider{
		client:    coinank.NewCoinankClient(coinank_enum.MainUrl, apiKey),
		fetchSize: defaultCoinAnkFetchSize,
		ttl:       DefaultSmallMarketValueCacheTTL,
		cache:     make(map[string]cacheEntry),
	}
}

func (p *CoinAnkProvider) GetSmallMarketValueRanking(ctx context.Context, req SmallMarketValueRequest) (*SmallMarketValueRankingData, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("small market value CoinAnk provider is not configured")
	}

	cacheKey := p.cacheKey(req)
	p.mu.RLock()
	entry, ok := p.cache[cacheKey]
	p.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.data, nil
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 3
	}
	fetchSize := p.fetchSize
	if fetchSize < limit*5 {
		fetchSize = limit * 5
	}
	if fetchSize < minCoinAnkFetchSize {
		fetchSize = minCoinAnkFetchSize
	}
	if fetchSize > maxCoinAnkFetchSize {
		fetchSize = maxCoinAnkFetchSize
	}

	// Paginate through market-cap ascending pages until we have enough raw
	// candidates or hit the safety page limit.
	var volumeRows []coinank.VolumeRankResponse
	var oiRows []coinank.OiRankResponse
	for page := 1; page <= maxCoinAnkPages; page++ {
		vRows, err := p.client.VolumeRank(ctx, coinank_enum.MarketCap, coinank_enum.Asc, page, fetchSize)
		if err != nil {
			return nil, fmt.Errorf("fetch CoinAnk volume ranking page %d: %w", page, err)
		}
		oRows, err := p.client.OiRank(ctx, coinank_enum.MarketCap, coinank_enum.Asc, page, fetchSize)
		if err != nil {
			return nil, fmt.Errorf("fetch CoinAnk open interest ranking page %d: %w", page, err)
		}
		volumeRows = append(volumeRows, vRows...)
		oiRows = append(oiRows, oRows...)
		if len(vRows) < fetchSize {
			break
		}
	}

	oiBySymbol := make(map[string]coinank.OiRankResponse, len(oiRows))
	oiByBase := make(map[string]coinank.OiRankResponse, len(oiRows))
	for _, row := range oiRows {
		symbol := normalizeSymbol(row.Symbol, row.BaseCoin)
		if symbol != "" {
			oiBySymbol[symbol] = row
		}
		if row.BaseCoin != "" {
			oiByBase[strings.ToUpper(row.BaseCoin)] = row
		}
	}

	rawCoins := make([]*SmallMarketValueCoin, 0, len(volumeRows))
	for _, row := range volumeRows {
		if !matchesExchange(req.Exchange, row.ExchangeName) {
			continue
		}
		if !row.SupportContract {
			continue
		}

		baseCoin := strings.ToUpper(strings.TrimSpace(row.BaseCoin))
		symbol := normalizeSymbol(row.Symbol, baseCoin)
		if symbol == "" {
			continue
		}
		if baseCoin == "" {
			baseCoin = baseFromSymbol(symbol)
		}

		coin := &SmallMarketValueCoin{
			Symbol:            symbol,
			Price:             row.Price,
			CirculatingSupply: float64(row.CirculatingSupply),
			Volume24hUSD:      row.Turnover24H,
		}

		if oi, ok := oiBySymbol[symbol]; ok {
			coin.OpenInterestUSD = oi.OpenInterest
			if coin.Price <= 0 {
				coin.Price = oi.Price
			}
			if coin.CirculatingSupply <= 0 {
				coin.CirculatingSupply = float64(oi.CirculatingSupply)
			}
		} else if oi, ok := oiByBase[baseCoin]; ok {
			coin.OpenInterestUSD = oi.OpenInterest
			if coin.Price <= 0 {
				coin.Price = oi.Price
			}
			if coin.CirculatingSupply <= 0 {
				coin.CirculatingSupply = float64(oi.CirculatingSupply)
			}
		}

		// Defer market cap enrichment to concurrent workers below.
		coin.TotalSupply = -1 // sentinel: not yet fetched
		rawCoins = append(rawCoins, coin)
	}

	// Concurrently enrich coins with detailed market cap / supply data.
	if err := p.enrichCoins(ctx, rawCoins); err != nil {
		return nil, fmt.Errorf("enrich coins: %w", err)
	}

	coins := make([]SmallMarketValueCoin, 0, len(rawCoins))
	for _, c := range rawCoins {
		if c.Price > 0 && c.CirculatingSupply > 0 {
			c.MarketCap = c.Price * c.CirculatingSupply
		}
		if c.FDV <= 0 && c.Price > 0 && c.TotalSupply > 0 {
			c.FDV = c.Price * c.TotalSupply
		}
		coins = append(coins, *c)
	}

	filterReq := req
	filterReq.Limit = limit
	// CoinAnk aggregate ranking does not expose order book depth, so depth is
	// explicitly marked unavailable instead of treating missing depth as liquid.
	filterReq.MinDepthUSD = 0

	filtered := filterAndScore(coins, filterReq)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	result := &SmallMarketValueRankingData{
		Coins:          filtered,
		FilterStats:    computeFilterStats(coins, filtered),
		DepthAvailable: false,
		FetchedAt:      time.Now().UTC(),
	}

	p.mu.Lock()
	p.cache[cacheKey] = cacheEntry{
		data:      result,
		expiresAt: time.Now().Add(p.ttl),
	}
	p.mu.Unlock()

	return result, nil
}

func (p *CoinAnkProvider) cacheKey(req SmallMarketValueRequest) string {
	return fmt.Sprintf("%s|%s|%d|%.0f|%.0f|%.0f",
		strings.ToLower(strings.TrimSpace(req.Exchange)),
		req.SortBy,
		req.Limit,
		req.Min24hQuoteVolumeUSD,
		req.MinOpenInterestUSD,
		req.MinDepthUSD,
	)
}

func (p *CoinAnkProvider) enrichCoins(ctx context.Context, coins []*SmallMarketValueCoin) error {
	if len(coins) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	workCh := make(chan *SmallMarketValueCoin)
	errCh := make(chan error, len(coins))

	for i := 0; i < coinAnkEnrichWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for coin := range workCh {
				baseCoin := baseFromSymbol(coin.Symbol)
				if baseCoin == "" {
					continue
				}
				market, err := p.client.GetCoinMarketCap(ctx, baseCoin)
				if err != nil {
					// Enrichment is best-effort; missing data does not fail the request.
					errCh <- fmt.Errorf("market cap for %s: %w", baseCoin, err)
					continue
				}
				if market == nil {
					continue
				}
				if !market.SupportContract {
					// Mark coin for removal by zeroing price.
					coin.Price = 0
					continue
				}
				if market.Price > 0 {
					coin.Price = market.Price
				}
				if market.CirculatingSupply > 0 {
					coin.CirculatingSupply = market.CirculatingSupply
				}
				if market.TotalSupply > 0 {
					coin.TotalSupply = market.TotalSupply
				}
				if market.MarketCap > 0 {
					coin.MarketCap = market.MarketCap
				}
			}
		}()
	}

	for _, coin := range coins {
		workCh <- coin
	}
	close(workCh)
	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 && len(errs) == len(coins) {
		return errs[0]
	}
	return nil
}

func matchesExchange(requested, actual string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" || strings.EqualFold(requested, "auto") || strings.EqualFold(requested, "all") {
		return true
	}
	actual = strings.TrimSpace(actual)
	return actual == "" || strings.EqualFold(actual, requested)
}

func normalizeSymbol(symbol, baseCoin string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol != "" {
		return strings.ReplaceAll(symbol, "-", "")
	}
	baseCoin = strings.ToUpper(strings.TrimSpace(baseCoin))
	if baseCoin == "" {
		return ""
	}
	return baseCoin + "USDT"
}

func baseFromSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	for _, suffix := range []string{"USDT", "USDC", "USD"} {
		if strings.HasSuffix(symbol, suffix) && len(symbol) > len(suffix) {
			return strings.TrimSuffix(symbol, suffix)
		}
	}
	return symbol
}
