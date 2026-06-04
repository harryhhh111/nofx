package nofxos

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// OIPosition represents open interest data for a single coin
type OIPosition struct {
	Symbol            string  `json:"symbol"`
	Rank              int     `json:"rank"`
	Price             float64 `json:"price"`
	CurrentOI         float64 `json:"current_oi"`
	OIDelta           float64 `json:"oi_delta"`
	OIDeltaPercent    float64 `json:"oi_delta_percent"`
	OIDeltaValue      float64 `json:"oi_delta_value"`
	PriceDeltaPercent float64 `json:"price_delta_percent"`
	NetLong           float64 `json:"net_long"`
	NetShort          float64 `json:"net_short"`
}

// OIRankingResponse is the API response structure for OI ranking
type OIRankingResponse struct {
	Success bool `json:"success"`
	Code    int  `json:"code"`
	Data    struct {
		Positions      []OIPosition `json:"positions"`
		Count          int          `json:"count"`
		Exchange       string       `json:"exchange"`
		TimeRange      string       `json:"time_range"`
		TimeRangeParam string       `json:"time_range_param"`
		RankType       string       `json:"rank_type"`
		Limit          int          `json:"limit"`
	} `json:"data"`
}

// OIRankingData contains both top and low OI rankings
type OIRankingData struct {
	TimeRange    string       `json:"time_range"`
	Duration     string       `json:"duration"`
	TopPositions []OIPosition `json:"top_positions"`
	LowPositions []OIPosition `json:"low_positions"`
	FetchedAt    time.Time    `json:"fetched_at"`
}

// Per-parameter OI caches
var (
	oiCaches   = make(map[string]*simpleCache[*OIRankingData])
	oiCachesMu sync.Mutex
)

// GetOIRanking retrieves OI ranking data with global caching.
func (c *Client) GetOIRanking(duration string, limit int) (*OIRankingData, error) {
	if duration == "" {
		duration = "1h"
	}
	if limit <= 0 {
		limit = 20
	}
	key := fmt.Sprintf("%s:%d", duration, limit)

	oiCachesMu.Lock()
	cache, ok := oiCaches[key]
	if !ok {
		cache = &simpleCache[*OIRankingData]{ttl: defaultCacheTTL}
		oiCaches[key] = cache
	}
	oiCachesMu.Unlock()

	if data, ok := cache.get(); ok {
		return data, nil
	}

	data, err := fetchOIRankingData(GetGlobalClient(), duration, limit)
	if err != nil {
		return nil, err
	}
	cache.set(data)
	return data, nil
}

func fetchOIRankingData(client *Client, duration string, limit int) (*OIRankingData, error) {
	result := &OIRankingData{
		Duration:  duration,
		FetchedAt: time.Now(),
	}

	topPositions, timeRange, err := client.fetchOIRanking("top", duration, limit)
	if err != nil {
		log.Printf("⚠️  Failed to fetch OI top ranking: %v", err)
	} else {
		result.TopPositions = topPositions
		result.TimeRange = timeRange
	}

	lowPositions, _, err := client.fetchOIRanking("low", duration, limit)
	if err != nil {
		log.Printf("⚠️  Failed to fetch OI low ranking: %v", err)
	} else {
		result.LowPositions = lowPositions
	}

	log.Printf("✓ Fetched OI ranking data: %d top, %d low (duration: %s)",
		len(result.TopPositions), len(result.LowPositions), duration)

	return result, nil
}

func (c *Client) fetchOIRanking(rankType, duration string, limit int) ([]OIPosition, string, error) {
	endpoint := fmt.Sprintf("/api/oi/%s-ranking?limit=%d&duration=%s", rankType, limit, duration)

	body, err := c.doRequest(endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}

	var response OIRankingResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", fmt.Errorf("JSON parsing failed: %w", err)
	}

	if !response.Success && response.Code != 0 {
		return nil, "", fmt.Errorf("API returned error code: %d", response.Code)
	}

	return response.Data.Positions, response.Data.TimeRange, nil
}

// GetOITopPositions retrieves top OI increase positions
func (c *Client) GetOITopPositions() ([]OIPosition, error) {
	data, err := c.GetOIRanking("1h", 20)
	if err != nil {
		return nil, err
	}
	return data.TopPositions, nil
}

// GetOITopSymbols retrieves OI top coin symbol list
func (c *Client) GetOITopSymbols() ([]string, error) {
	data, err := c.GetOIRanking("1h", 20)
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, pos := range data.TopPositions {
		symbols = append(symbols, NormalizeSymbol(pos.Symbol))
	}

	return symbols, nil
}

// GetOILowPositions retrieves OI decrease positions
func (c *Client) GetOILowPositions() ([]OIPosition, error) {
	data, err := c.GetOIRanking("1h", 20)
	if err != nil {
		return nil, err
	}
	return data.LowPositions, nil
}

// GetOILowSymbols retrieves OI low coin symbol list
func (c *Client) GetOILowSymbols() ([]string, error) {
	data, err := c.GetOIRanking("1h", 20)
	if err != nil {
		return nil, err
	}

	var symbols []string
	for _, pos := range data.LowPositions {
		symbols = append(symbols, NormalizeSymbol(pos.Symbol))
	}

	return symbols, nil
}

// ── Formatting ──────────────────────────────────────────────────────────────

func FormatOIRankingForAI(data *OIRankingData, lang Language, tradingSymbols map[string]bool) string {
	if data == nil {
		return ""
	}
	if lang == LangChinese {
		return formatOIRankingZH(data, tradingSymbols)
	}
	return formatOIRankingEN(data, tradingSymbols)
}

func formatOIRankingZH(data *OIRankingData, tradingSymbols map[string]bool) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 持仓量变化排行 (%s)\n\n", data.Duration))
	if len(data.TopPositions) > 0 {
		sb.WriteString("### 持仓增加榜\n")
		sb.WriteString("资金流入，趋势延续或新仓建立信号:\n\n")
		sb.WriteString("| 排名 | 币种 | 持仓变化(USDT) | OI变化% | 价格变化% |\n")
		sb.WriteString("|------|------|----------------|---------|----------|\n")
		for _, pos := range data.TopPositions {
			if tradingSymbols != nil && !tradingSymbols[pos.Symbol] {
				continue
			}
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %+.2f%% | %+.2f%% |\n",
				pos.Rank, pos.Symbol, formatValue(pos.OIDeltaValue), pos.OIDeltaPercent, pos.PriceDeltaPercent))
		}
		sb.WriteString("\n")
	}
	if len(data.LowPositions) > 0 {
		sb.WriteString("### 持仓减少榜\n")
		sb.WriteString("资金流出，趋势反转或仓位平仓信号:\n\n")
		sb.WriteString("| 排名 | 币种 | 持仓变化(USDT) | OI变化% | 价格变化% |\n")
		sb.WriteString("|------|------|----------------|---------|----------|\n")
		for _, pos := range data.LowPositions {
			if tradingSymbols != nil && !tradingSymbols[pos.Symbol] {
				continue
			}
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %+.2f%% | %+.2f%% |\n",
				pos.Rank, pos.Symbol, formatValue(pos.OIDeltaValue), pos.OIDeltaPercent, pos.PriceDeltaPercent))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("**解读**: OI增+价涨=多头主导 | OI增+价跌=空头主导 | OI减+价涨=空头平仓 | OI减+价跌=多头平仓\n\n")
	return sb.String()
}

func formatOIRankingEN(data *OIRankingData, tradingSymbols map[string]bool) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Open Interest Changes (%s)\n\n", data.Duration))
	if len(data.TopPositions) > 0 {
		sb.WriteString("### OI Increase Ranking\n")
		sb.WriteString("Capital inflow signals - trend continuation or new positions:\n\n")
		sb.WriteString("| Rank | Symbol | OI Change (USDT) | OI Change % | Price Change % |\n")
		sb.WriteString("|------|--------|------------------|-------------|----------------|\n")
		for _, pos := range data.TopPositions {
			if tradingSymbols != nil && !tradingSymbols[pos.Symbol] {
				continue
			}
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %+.2f%% | %+.2f%% |\n",
				pos.Rank, pos.Symbol, formatValue(pos.OIDeltaValue), pos.OIDeltaPercent, pos.PriceDeltaPercent))
		}
		sb.WriteString("\n")
	}
	if len(data.LowPositions) > 0 {
		sb.WriteString("### OI Decrease Ranking\n")
		sb.WriteString("Capital outflow signals - trend reversal or position closing:\n\n")
		sb.WriteString("| Rank | Symbol | OI Change (USDT) | OI Change % | Price Change % |\n")
		sb.WriteString("|------|--------|------------------|-------------|----------------|\n")
		for _, pos := range data.LowPositions {
			if tradingSymbols != nil && !tradingSymbols[pos.Symbol] {
				continue
			}
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %+.2f%% | %+.2f%% |\n",
				pos.Rank, pos.Symbol, formatValue(pos.OIDeltaValue), pos.OIDeltaPercent, pos.PriceDeltaPercent))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("**Key**: OI up + Price up = Bulls dominant | OI up + Price down = Bears dominant | OI down + Price up = Short covering | OI down + Price down = Long liquidation\n\n")
	return sb.String()
}
