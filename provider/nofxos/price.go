package nofxos

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// PriceRankingItem represents single coin price ranking data
type PriceRankingItem struct {
	Pair         string  `json:"pair"`
	Symbol       string  `json:"symbol"`
	PriceDelta   float64 `json:"price_delta"`
	Price        float64 `json:"price"`
	FutureFlow   float64 `json:"future_flow"`
	SpotFlow     float64 `json:"spot_flow"`
	OI           float64 `json:"oi"`
	OIDelta      float64 `json:"oi_delta"`
	OIDeltaValue float64 `json:"oi_delta_value"`
}

// PriceRankingDuration contains top gainers and losers for a single duration
type PriceRankingDuration struct {
	Top []PriceRankingItem `json:"top"`
	Low []PriceRankingItem `json:"low"`
}

// PriceRankingResponse is the API response structure
type PriceRankingResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Durations []string                        `json:"durations"`
		Limit     int                             `json:"limit"`
		Data      map[string]PriceRankingDuration `json:"data"`
	} `json:"data"`
}

// PriceRankingData contains price ranking data for multiple durations
type PriceRankingData struct {
	Durations map[string]*PriceRankingDuration `json:"durations"`
	FetchedAt time.Time                        `json:"fetched_at"`
}

// Per-parameter Price caches
var (
	priceCaches   = make(map[string]*simpleCache[*PriceRankingData])
	priceCachesMu sync.Mutex
)

// GetPriceRanking retrieves price ranking data with global caching.
func (c *Client) GetPriceRanking(durations string, limit int) (*PriceRankingData, error) {
	return c.GetPriceRankingContext(context.Background(), durations, limit)
}

// GetPriceRankingContext retrieves price ranking data with global caching.
func (c *Client) GetPriceRankingContext(ctx context.Context, durations string, limit int) (*PriceRankingData, error) {
	if durations == "" {
		durations = "1h"
	}
	if limit <= 0 {
		limit = 10
	}
	key := fmt.Sprintf("%s:%d", durations, limit)

	priceCachesMu.Lock()
	cache, ok := priceCaches[key]
	if !ok {
		cache = &simpleCache[*PriceRankingData]{ttl: defaultCacheTTL}
		priceCaches[key] = cache
	}
	priceCachesMu.Unlock()

	if data, ok := cache.get(); ok {
		return data, nil
	}

	data, err := fetchPriceRankingData(ctx, c, durations, limit)
	if err != nil {
		return nil, err
	}
	cache.set(data)
	return data, nil
}

func fetchPriceRankingData(ctx context.Context, client *Client, durations string, limit int) (*PriceRankingData, error) {
	result := &PriceRankingData{
		Durations: make(map[string]*PriceRankingDuration),
		FetchedAt: time.Now(),
	}
	errs := []string{}

	for _, duration := range splitPriceRankingDurations(durations) {
		data, err := fetchPriceRankingDuration(ctx, client, duration, limit)
		if err != nil {
			log.Printf("⚠️  Failed to fetch Price ranking for %s: %v", duration, err)
			errs = append(errs, fmt.Sprintf("%s: %v", duration, err))
			continue
		}
		for key, ranking := range data {
			d := ranking
			trimPriceRankingDuration(&d, limit)
			result.Durations[key] = &d
		}
	}

	if len(result.Durations) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all Price ranking requests failed: %s", strings.Join(errs, "; "))
	}

	log.Printf("✓ Fetched Price ranking data for %d durations", len(result.Durations))

	return result, nil
}

func splitPriceRankingDurations(durations string) []string {
	parts := strings.Split(durations, ",")
	result := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		duration := strings.TrimSpace(part)
		if duration == "" || seen[duration] {
			continue
		}
		seen[duration] = true
		result = append(result, duration)
	}
	if len(result) == 0 {
		return []string{"1h"}
	}
	return result
}

func fetchPriceRankingDuration(ctx context.Context, client *Client, duration string, limit int) (map[string]PriceRankingDuration, error) {
	endpoint := fmt.Sprintf("/api/price/ranking?duration=%s", duration)

	body, err := client.doRequestContext(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	var response PriceRankingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned failure status")
	}

	if len(response.Data.Data) == 0 {
		return nil, fmt.Errorf("API returned empty ranking data")
	}
	return response.Data.Data, nil
}

func trimPriceRankingDuration(data *PriceRankingDuration, limit int) {
	if data == nil || limit <= 0 {
		return
	}
	if len(data.Top) > limit {
		data.Top = data.Top[:limit]
	}
	if len(data.Low) > limit {
		data.Low = data.Low[:limit]
	}
}

// ── Formatting ──────────────────────────────────────────────────────────────

func FormatPriceRankingForAI(data *PriceRankingData, lang Language) string {
	if data == nil || len(data.Durations) == 0 {
		return ""
	}
	if lang == LangChinese {
		return formatPriceRankingZH(data)
	}
	return formatPriceRankingEN(data)
}

func formatPriceRankingZH(data *PriceRankingData) string {
	var sb strings.Builder
	sb.WriteString("## 涨跌幅排行\n\n")
	durationOrder := []string{"1h", "4h", "24h"}
	for _, duration := range durationOrder {
		durationData, exists := data.Durations[duration]
		if !exists || durationData == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s 涨跌幅\n\n", duration))
		if len(durationData.Top) > 0 {
			sb.WriteString("**涨幅榜**\n")
			sb.WriteString("| 币种 | 涨幅 | 价格 | 资金流 | OI变化 |\n")
			sb.WriteString("|------|------|------|--------|--------|\n")
			for _, item := range durationData.Top {
				sb.WriteString(fmt.Sprintf("| %s | %+.2f%% | $%.4f | %s | %s |\n",
					item.Symbol, item.PriceDelta*100, item.Price,
					formatValue(item.FutureFlow), formatValue(item.OIDeltaValue)))
			}
			sb.WriteString("\n")
		}
		if len(durationData.Low) > 0 {
			sb.WriteString("**跌幅榜**\n")
			sb.WriteString("| 币种 | 跌幅 | 价格 | 资金流 | OI变化 |\n")
			sb.WriteString("|------|------|------|--------|--------|\n")
			for _, item := range durationData.Low {
				sb.WriteString(fmt.Sprintf("| %s | %.2f%% | $%.4f | %s | %s |\n",
					item.Symbol, item.PriceDelta*100, item.Price,
					formatValue(item.FutureFlow), formatValue(item.OIDeltaValue)))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("**解读**: 涨幅大+资金流入+OI增加=强势上涨 | 跌幅大+资金流出+OI减少=弱势下跌\n\n")
	return sb.String()
}

func formatPriceRankingEN(data *PriceRankingData) string {
	var sb strings.Builder
	sb.WriteString("## Price Gainers/Losers\n\n")
	durationOrder := []string{"1h", "4h", "24h"}
	for _, duration := range durationOrder {
		durationData, exists := data.Durations[duration]
		if !exists || durationData == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s Price Change\n\n", duration))
		if len(durationData.Top) > 0 {
			sb.WriteString("**Top Gainers**\n")
			sb.WriteString("| Symbol | Change | Price | Fund Flow | OI Change |\n")
			sb.WriteString("|--------|--------|-------|-----------|----------|\n")
			for _, item := range durationData.Top {
				sb.WriteString(fmt.Sprintf("| %s | %+.2f%% | $%.4f | %s | %s |\n",
					item.Symbol, item.PriceDelta*100, item.Price,
					formatValue(item.FutureFlow), formatValue(item.OIDeltaValue)))
			}
			sb.WriteString("\n")
		}
		if len(durationData.Low) > 0 {
			sb.WriteString("**Top Losers**\n")
			sb.WriteString("| Symbol | Change | Price | Fund Flow | OI Change |\n")
			sb.WriteString("|--------|--------|-------|-----------|----------|\n")
			for _, item := range durationData.Low {
				sb.WriteString(fmt.Sprintf("| %s | %.2f%% | $%.4f | %s | %s |\n",
					item.Symbol, item.PriceDelta*100, item.Price,
					formatValue(item.FutureFlow), formatValue(item.OIDeltaValue)))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("**Key**: Big gain + Fund inflow + OI increase = Strong bullish | Big loss + Fund outflow + OI decrease = Strong bearish\n\n")
	return sb.String()
}
