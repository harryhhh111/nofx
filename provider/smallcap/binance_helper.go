package smallcap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// binanceHelper encapsulates Binance futures symbol discovery and open-interest
// enrichment. It is embedded by small-market-value providers that validate
// candidates against Binance perpetual contracts.
type binanceHelper struct {
	symbolsMu        sync.RWMutex
	binanceSymbols   map[string]struct{}
	symbolsFetchedAt time.Time
}

func (b *binanceHelper) getBinanceSymbols(ctx context.Context) (map[string]struct{}, error) {
	b.symbolsMu.RLock()
	if b.binanceSymbols != nil && time.Since(b.symbolsFetchedAt) < binanceSymbolsCacheTTL {
		symbols := make(map[string]struct{}, len(b.binanceSymbols))
		for s := range b.binanceSymbols {
			symbols[s] = struct{}{}
		}
		b.symbolsMu.RUnlock()
		return symbols, nil
	}
	b.symbolsMu.RUnlock()

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

	b.symbolsMu.Lock()
	b.binanceSymbols = symbols
	b.symbolsFetchedAt = time.Now()
	b.symbolsMu.Unlock()

	return symbols, nil
}

func (b *binanceHelper) enrichOpenInterest(ctx context.Context, coins []*SmallMarketValueCoin) (attempts, successes int) {
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
		return 0, fmt.Errorf("decode openInterest: %w", err)
	}

	var oi float64
	if _, err := fmt.Sscanf(payload.OpenInterest, "%f", &oi); err != nil {
		return 0, err
	}
	return oi, nil
}

func baseFromSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	for _, suffix := range []string{"USDT", "USDC", "BUSD", "USD"} {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}
