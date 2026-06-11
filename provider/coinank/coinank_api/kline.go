package coinank_api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"nofx/provider/coinank"
	"nofx/provider/coinank/coinank_enum"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const MainApiUrl = "https://api.coinank.com"

// klineRateLimiter controls the free API call rate to avoid silent rate-limiting
// where CoinAnk returns {"success":true, "data":[]} instead of an error.
// 2 req/s (500ms spacing) is conservative for the free tier.
var klineRateLimiter = rate.NewLimiter(rate.Every(500*time.Millisecond), 1)

// Kline open free kline from coinank
func Kline(ctx context.Context, symbol string, exchange coinank_enum.Exchange, ts int64, side coinank_enum.Side, size int,
	interval coinank_enum.Interval) ([]coinank.KlineResult, error) {
	// Respect rate limit to avoid silent throttling (empty data disguised as success)
	if err := klineRateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}
	paramsMap := make(map[string]string, 6)
	paramsMap["symbol"] = symbol
	paramsMap["exchange"] = string(exchange)
	paramsMap["side"] = string(side)
	paramsMap["size"] = strconv.Itoa(size)
	paramsMap["ts"] = strconv.FormatInt(ts, 10)
	paramsMap["interval"] = string(interval)
	resp, err := get(ctx, "/api/kline/list/open", paramsMap)
	if err != nil {
		return nil, err
	}
	var result coinank.CoinankResponse[[][]float64]
	err = json.Unmarshal([]byte(resp), &result)
	if err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, coinank.HttpError
	}
	klines := make([]coinank.KlineResult, len(result.Data))
	for i, k := range result.Data {
		klines[i].StartTime = int64(k[0] + 0.001)
		klines[i].EndTime = int64(k[1] + 0.001)
		klines[i].Open = k[2]
		klines[i].Close = k[3]
		klines[i].High = k[4]
		klines[i].Low = k[5]
		klines[i].Volume = k[6]
		klines[i].Quantity = k[7]
		klines[i].Count = k[8]
	}
	return klines, nil
}

func get(ctx context.Context, path string, paramsMap map[string]string) (string, error) {
	data := url.Values{}
	for key, value := range paramsMap {
		data.Add(key, value)
	}
	fullURL := fmt.Sprintf("%s%s?%s", MainApiUrl, path, data.Encode())
	request, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

var client = &http.Client{
	Timeout: 30 * time.Second,
}
