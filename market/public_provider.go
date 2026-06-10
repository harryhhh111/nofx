package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"nofx/provider/hyperliquid"
	"strconv"
	"strings"
	"time"
)

var publicMarketSourceOrder = []string{"binance", "bybit", "okx", "aster", "hyperliquid"}

func PublicMarketSources(preferred string, allowFallback bool) []string {
	preferred = normalizePublicMarketSource(preferred)
	if preferred == "" || preferred == "auto" {
		return append([]string(nil), publicMarketSourceOrder...)
	}
	if !allowFallback {
		return []string{preferred}
	}

	out := []string{preferred}
	seen := map[string]bool{preferred: true}
	for _, source := range publicMarketSourceOrder {
		if !seen[source] {
			out = append(out, source)
			seen[source] = true
		}
	}
	return out
}

func GetPublicTickerPrice(ctx context.Context, source, symbol string, allowFallback bool) (float64, string, error) {
	var lastErr error
	for _, candidate := range PublicMarketSources(source, allowFallback) {
		price, err := getPublicTickerPricePinned(ctx, candidate, symbol)
		if err == nil && price > 0 {
			return price, candidate, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("%s returned non-positive price %.8f", candidate, price)
		}
	}
	return 0, "", lastErr
}

func GetPublicKlines(ctx context.Context, source, symbol, interval string, limit int, allowFallback bool) ([]Kline, string, error) {
	var lastErr error
	for _, candidate := range PublicMarketSources(source, allowFallback) {
		klines, err := getKlinesFromOfficialFuturesContext(ctx, symbol, interval, limit, candidate)
		if err == nil && len(klines) > 0 {
			return klines, candidate, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("%s returned empty kline data", candidate)
		}
	}
	return nil, "", lastErr
}

func normalizePublicMarketSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "", "paper":
		return "binance"
	default:
		return source
	}
}

func getPublicTickerPricePinned(ctx context.Context, source, symbol string) (float64, error) {
	symbol = Normalize(symbol)
	switch normalizePublicMarketSource(source) {
	case "binance":
		return NewAPIClient().GetCurrentPrice(symbol)
	case "bybit":
		return getBybitTickerPrice(ctx, symbol)
	case "okx":
		return getOKXTickerPrice(ctx, symbol)
	case "aster":
		return getAsterTickerPrice(ctx, symbol)
	case "hyperliquid":
		return getHyperliquidTickerPrice(ctx, symbol)
	default:
		return 0, fmt.Errorf("public ticker source %q is not implemented", source)
	}
}

func getBybitTickerPrice(ctx context.Context, symbol string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.bybit.com/v5/market/tickers", nil)
	if err != nil {
		return 0, err
	}
	q := req.URL.Query()
	q.Set("category", "linear")
	q.Set("symbol", symbol)
	req.URL.RawQuery = q.Encode()

	body, err := doMarketRequestBody(&http.Client{Timeout: 10 * time.Second}, req)
	if err != nil {
		return 0, err
	}
	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				LastPrice string `json:"lastPrice"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if resp.RetCode != 0 {
		return 0, fmt.Errorf("bybit returned %d: %s", resp.RetCode, resp.RetMsg)
	}
	if len(resp.Result.List) == 0 {
		return 0, fmt.Errorf("bybit returned no ticker for %s", symbol)
	}
	return strconv.ParseFloat(resp.Result.List[0].LastPrice, 64)
}

func getOKXTickerPrice(ctx context.Context, symbol string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.okx.com/api/v5/market/ticker", nil)
	if err != nil {
		return 0, err
	}
	q := req.URL.Query()
	q.Set("instId", okxSwapInstrument(symbol))
	req.URL.RawQuery = q.Encode()

	body, err := doMarketRequestBody(&http.Client{Timeout: 10 * time.Second}, req)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Last string `json:"last"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if resp.Code != "0" {
		return 0, fmt.Errorf("okx returned %s: %s", resp.Code, resp.Msg)
	}
	if len(resp.Data) == 0 {
		return 0, fmt.Errorf("okx returned no ticker for %s", symbol)
	}
	return strconv.ParseFloat(resp.Data[0].Last, 64)
}

func getAsterTickerPrice(ctx context.Context, symbol string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://fapi.asterdex.com/fapi/v3/ticker/price", nil)
	if err != nil {
		return 0, err
	}
	q := req.URL.Query()
	q.Set("symbol", symbol)
	req.URL.RawQuery = q.Encode()

	body, err := doMarketRequestBody(&http.Client{Timeout: 10 * time.Second}, req)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if resp.Price == "" {
		return 0, fmt.Errorf("aster returned no ticker for %s", symbol)
	}
	return strconv.ParseFloat(resp.Price, 64)
}

func getHyperliquidTickerPrice(ctx context.Context, symbol string) (float64, error) {
	coin := hyperliquidCoin(symbol)
	client := hyperliquid.NewClient()
	var mids map[string]string
	var err error
	if strings.HasPrefix(coin, "xyz:") {
		mids, err = client.GetAllMidsXYZ(ctx)
		coin = strings.TrimPrefix(coin, "xyz:")
	} else {
		mids, err = client.GetAllMids(ctx)
	}
	if err != nil {
		return 0, err
	}
	priceStr, ok := mids[coin]
	if !ok {
		return 0, fmt.Errorf("hyperliquid returned no ticker for %s", symbol)
	}
	return strconv.ParseFloat(priceStr, 64)
}

func hyperliquidCoin(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if strings.HasPrefix(symbol, "xyz:") {
		return symbol
	}
	symbol = strings.TrimSuffix(Normalize(symbol), "USDT")
	symbol = strings.TrimSuffix(symbol, "PERP")
	return symbol
}
