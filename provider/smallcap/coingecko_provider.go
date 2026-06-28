package smallcap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"nofx/provider/coingecko"
)

const (
	defaultCoinGeckoFetchSize = 250
	maxCoinGeckoPages         = 5
	coinGeckoEnrichWorkers    = 8
	binanceSymbolsCacheTTL    = 1 * time.Hour
)

var (
	binanceExchangeInfoURL = "https://fapi.binance.com/fapi/v1/exchangeInfo"
	binanceOIAPIURL        = "https://fapi.binance.com/fapi/v1/openInterest"
)

// CoinGeckoProvider builds Small Market Value rankings from CoinGecko market data
// with Binance futures tradability validation and optional open-interest enrichment.
type CoinGeckoProvider struct {
	client *coingecko.Client
	ttl    time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry

	symbolsMu        sync.RWMutex
	binanceSymbols   map[string]struct{}
	symbolsFetchedAt time.Time
}

// NewCoinGeckoProvider creates the default free small market value provider.
func NewCoinGeckoProvider() *CoinGeckoProvider {
	return &CoinGeckoProvider{
		client: coingecko.NewClient(),
		ttl:    DefaultSmallMarketValueCacheTTL,
		cache:  make(map[string]cacheEntry),
	}
}

func (p *CoinGeckoProvider) GetSmallMarketValueRanking(ctx context.Context, req SmallMarketValueRequest) (*SmallMarketValueRankingData, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("small market value CoinGecko provider is not configured")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 3
	}

	cacheKey := p.cacheKey(req)
	p.mu.RLock()
	entry, ok := p.cache[cacheKey]
	p.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.data, nil
	}

	binanceSymbols, err := p.getBinanceSymbols(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch Binance tradable symbols: %w", err)
	}

	filterReq := req
	filterReq.Limit = limit
	filterReq.MinDepthUSD = 0 // CoinGecko does not provide order book depth.

	var allCoins []SmallMarketValueCoin
	var oiSuccessCount int
	var oiAttemptCount int
	for page := 1; page <= maxCoinGeckoPages; page++ {
		data, err := p.client.CoinsMarkets(ctx, coingecko.CoinsMarketsRequest{
			VSCCurrency: "usd",
			Order:       "market_cap_asc",
			PerPage:     defaultCoinGeckoFetchSize,
			Page:        page,
		})
		if err != nil {
			return nil, fmt.Errorf("fetch CoinGecko markets page %d: %w", page, err)
		}
		if len(data) == 0 {
			break
		}

		raw := make([]*SmallMarketValueCoin, 0, len(data))
		for _, d := range data {
			symbol := normalizeSymbol(d.Symbol, "")
			if symbol == "" {
				continue
			}
			if _, tradable := binanceSymbols[symbol]; !tradable {
				continue
			}
			raw = append(raw, &SmallMarketValueCoin{
				Symbol:            symbol,
				Price:             d.CurrentPrice,
				CirculatingSupply: d.CirculatingSupply,
				TotalSupply:       d.TotalSupply,
				MarketCap:         d.MarketCap,
				FDV:               d.FullyDilutedValuation,
				Volume24hUSD:      d.TotalVolume,
			})
		}

		attempts, successes := p.enrichOpenInterest(ctx, raw)
		oiAttemptCount += attempts
		oiSuccessCount += successes
		if attempts > 0 && successes == 0 && oiSuccessCount == 0 {
			filterReq.MinOpenInterestUSD = 0
		}

		for _, c := range raw {
			allCoins = append(allCoins, *c)
		}

		if len(filterAndScore(allCoins, filterReq)) >= limit || len(data) < defaultCoinGeckoFetchSize {
			break
		}
	}

	// If OI enrichment failed entirely, mark it unavailable and do not filter by OI.
	// This prevents silently filtering out all candidates when Binance OI is down.
	if oiAttemptCount > 0 && oiSuccessCount == 0 {
		filterReq.MinOpenInterestUSD = 0
	}

	filtered := filterAndScore(allCoins, filterReq)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	result := &SmallMarketValueRankingData{
		Coins:          filtered,
		FilterStats:    computeFilterStats(allCoins, filtered),
		DepthAvailable: false,
		OIAvailable:    oiAttemptCount == 0 || oiSuccessCount > 0,
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

func (p *CoinGeckoProvider) getBinanceSymbols(ctx context.Context) (map[string]struct{}, error) {
	p.symbolsMu.RLock()
	if p.binanceSymbols != nil && time.Since(p.symbolsFetchedAt) < binanceSymbolsCacheTTL {
		symbols := make(map[string]struct{}, len(p.binanceSymbols))
		for s := range p.binanceSymbols {
			symbols[s] = struct{}{}
		}
		p.symbolsMu.RUnlock()
		return symbols, nil
	}
	p.symbolsMu.RUnlock()

	httpClient := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, binanceExchangeInfoURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from Binance exchangeInfo", resp.StatusCode)
	}

	var payload struct {
		Symbols []struct {
			Symbol       string `json:"symbol"`
			Status       string `json:"status"`
			ContractType string `json:"contractType"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode exchangeInfo: %w", err)
	}

	symbols := make(map[string]struct{})
	for _, s := range payload.Symbols {
		if s.Status != "TRADING" {
			continue
		}
		if s.ContractType != "" && s.ContractType != "PERPETUAL" {
			continue
		}
		symbols[s.Symbol] = struct{}{}
	}

	p.symbolsMu.Lock()
	p.binanceSymbols = symbols
	p.symbolsFetchedAt = time.Now()
	p.symbolsMu.Unlock()

	return symbols, nil
}

func (p *CoinGeckoProvider) enrichOpenInterest(ctx context.Context, coins []*SmallMarketValueCoin) (attempts, successes int) {
	if len(coins) == 0 {
		return 0, 0
	}

	var wg sync.WaitGroup
	workCh := make(chan *SmallMarketValueCoin)
	var successMu sync.Mutex

	for i := 0; i < coinGeckoEnrichWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			httpClient := &http.Client{Timeout: 10 * time.Second}
			for coin := range workCh {
				base := baseFromSymbol(coin.Symbol)
				if base == "" {
					continue
				}
				oi, err := fetchBinanceOpenInterest(ctx, httpClient, base+"USDT")
				if err != nil {
					continue
				}
				coin.OpenInterestUSD = oi * coin.Price
				successMu.Lock()
				successes++
				successMu.Unlock()
			}
		}()
	}

	for _, coin := range coins {
		workCh <- coin
		attempts++
	}
	close(workCh)
	wg.Wait()
	return attempts, successes
}

func fetchBinanceOpenInterest(ctx context.Context, httpClient *http.Client, symbol string) (float64, error) {
	url := fmt.Sprintf("%s?symbol=%s", binanceOIAPIURL, symbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		OpenInterest string `json:"openInterest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}

	var oi float64
	if _, err := fmt.Sscanf(payload.OpenInterest, "%f", &oi); err != nil {
		return 0, err
	}
	return oi, nil
}

func (p *CoinGeckoProvider) cacheKey(req SmallMarketValueRequest) string {
	return fmt.Sprintf("coingecko|%s|%s|%d|%.0f|%.0f",
		strings.ToLower(strings.TrimSpace(req.Exchange)),
		req.SortBy,
		req.Limit,
		req.Min24hQuoteVolumeUSD,
		req.MinOpenInterestUSD,
	)
}
