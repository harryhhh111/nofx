// Package smallcap provides the Small Market Value candidate coin provider.
//
// Small Market Value is a coin selection strategy that filters a tradeable
// universe by liquidity, then ranks surviving coins by ascending market cap
// (or FDV), returning the smallest liquid names. It is not a technical
// indicator and does not rely on per-symbol K-line data.
package smallcap

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// SortBy controls the ranking field for small market value selection.
type SortBy string

const (
	SortByMarketCap SortBy = "market_cap"
	SortByFDV       SortBy = "fdv"
)

// SmallMarketValueRequest carries selection parameters.
type SmallMarketValueRequest struct {
	Exchange             string
	SortBy               SortBy
	Limit                int
	Min24hQuoteVolumeUSD float64
	MinOpenInterestUSD   float64
	MinDepthUSD          float64
}

// SmallMarketValueCoin represents a single coin after filtering and scoring.
type SmallMarketValueCoin struct {
	Symbol            string   `json:"symbol"`
	Price             float64  `json:"price"`
	CirculatingSupply float64  `json:"circulating_supply"`
	TotalSupply       float64  `json:"total_supply"`
	MarketCap         float64  `json:"market_cap"`
	FDV               float64  `json:"fdv"`
	Volume24hUSD      float64  `json:"volume_24h_usd"`
	OpenInterestUSD   float64  `json:"open_interest_usd"`
	DepthUSD          float64  `json:"depth_usd"`
	MarketCapRank     int      `json:"market_cap_rank"`
	LiquidityRank     int      `json:"liquidity_rank"`
	SmallCapScore     float64  `json:"small_cap_score"`
	FilterReasons     []string `json:"filter_reasons,omitempty"`
}

// FilterStats summarizes why coins were excluded from the candidate list.
type FilterStats struct {
	Total              int `json:"total"`
	Passed             int `json:"passed"`
	LowVolumeCount     int `json:"low_volume_count"`
	LowOICount         int `json:"low_oi_count"`
	LowDepthCount      int `json:"low_depth_count"`
	MissingSupplyCount int `json:"missing_supply_count"`
	NotTradableCount   int `json:"not_tradable_count"`
}

// SmallMarketValueRankingData is the provider result.
type SmallMarketValueRankingData struct {
	Coins          []SmallMarketValueCoin `json:"coins"`
	FilterStats    FilterStats            `json:"filter_stats"`
	DepthAvailable bool                   `json:"depth_available"`
	FetchedAt      time.Time              `json:"fetched_at"`
}

// SmallMarketValueProvider abstracts the source of small market value ranking data.
type SmallMarketValueProvider interface {
	GetSmallMarketValueRanking(ctx context.Context, req SmallMarketValueRequest) (*SmallMarketValueRankingData, error)
}

// NotImplementedProvider returns a clear error until a real data source is wired.
type NotImplementedProvider struct{}

func (p *NotImplementedProvider) GetSmallMarketValueRanking(_ context.Context, _ SmallMarketValueRequest) (*SmallMarketValueRankingData, error) {
	return nil, fmt.Errorf("small market value data source is not implemented yet")
}

// MockProvider is a test/provider stub that returns a configurable coin list.
type MockProvider struct {
	Coins []SmallMarketValueCoin
	Err   error
}

func (p *MockProvider) GetSmallMarketValueRanking(_ context.Context, req SmallMarketValueRequest) (*SmallMarketValueRankingData, error) {
	if p.Err != nil {
		return nil, p.Err
	}

	filtered := filterAndScore(p.Coins, req)
	limit := req.Limit
	if limit <= 0 {
		limit = 3
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return &SmallMarketValueRankingData{
		Coins:          filtered,
		FilterStats:    computeFilterStats(p.Coins, filtered),
		DepthAvailable: true,
		FetchedAt:      time.Now().UTC(),
	}, nil
}

func filterAndScore(coins []SmallMarketValueCoin, req SmallMarketValueRequest) []SmallMarketValueCoin {
	var passed []SmallMarketValueCoin
	for i := range coins {
		c := coins[i]
		reasons := []string{}
		if !c.IsTradable() {
			reasons = append(reasons, "not_tradable")
		}
		if c.MarketCap <= 0 && c.FDV <= 0 {
			reasons = append(reasons, "missing_supply_or_price")
		}
		if req.Min24hQuoteVolumeUSD > 0 && c.Volume24hUSD < req.Min24hQuoteVolumeUSD {
			reasons = append(reasons, "low_volume")
		}
		if req.MinOpenInterestUSD > 0 && c.OpenInterestUSD < req.MinOpenInterestUSD {
			reasons = append(reasons, "low_oi")
		}
		if req.MinDepthUSD > 0 && c.DepthUSD < req.MinDepthUSD {
			reasons = append(reasons, "low_depth")
		}
		if len(reasons) > 0 {
			coins[i].FilterReasons = reasons
			continue
		}
		passed = append(passed, c)
	}

	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = SortByMarketCap
	}
	sort.SliceStable(passed, func(i, j int) bool {
		a, b := valueForSort(passed[i], sortBy), valueForSort(passed[j], sortBy)
		if a == 0 && b != 0 {
			return false
		}
		if a != 0 && b == 0 {
			return true
		}
		return a < b
	})

	// Assign ranks and scores.
	total := len(passed)
	for i := range passed {
		passed[i].MarketCapRank = i + 1
		passed[i].LiquidityRank = i + 1 // Simplified: same order as market cap rank in mock.
		passed[i].SmallCapScore = computeSmallCapScore(passed[i], i+1, total, req)
		passed[i].FilterReasons = nil
	}

	return passed
}

func valueForSort(c SmallMarketValueCoin, sortBy SortBy) float64 {
	if sortBy == SortByFDV && c.FDV > 0 {
		return c.FDV
	}
	if c.MarketCap > 0 {
		return c.MarketCap
	}
	return c.FDV
}

func computeSmallCapScore(c SmallMarketValueCoin, rank, total int, req SmallMarketValueRequest) float64 {
	denominator := float64(total - 1)
	if denominator <= 0 {
		denominator = 1
	}
	rankScore := 1 - float64(rank-1)/denominator

	liquidityScore := math.MaxFloat64
	if req.Min24hQuoteVolumeUSD > 0 {
		liquidityScore = math.Min(liquidityScore, c.Volume24hUSD/req.Min24hQuoteVolumeUSD)
	}
	if req.MinOpenInterestUSD > 0 {
		liquidityScore = math.Min(liquidityScore, c.OpenInterestUSD/req.MinOpenInterestUSD)
	}
	if req.MinDepthUSD > 0 {
		liquidityScore = math.Min(liquidityScore, c.DepthUSD/req.MinDepthUSD)
	}
	if liquidityScore == math.MaxFloat64 {
		liquidityScore = 1
	}

	multiplier := 0.5
	if liquidityScore >= 2.0 {
		multiplier = 1.0
	} else if liquidityScore >= 1.0 {
		multiplier = 0.75
	}

	return math.Round(100 * rankScore * multiplier)
}

func computeFilterStats(all, passed []SmallMarketValueCoin) FilterStats {
	stats := FilterStats{Total: len(all), Passed: len(passed)}
	passedSet := make(map[string]struct{}, len(passed))
	for _, c := range passed {
		passedSet[c.Symbol] = struct{}{}
	}
	for _, c := range all {
		if _, ok := passedSet[c.Symbol]; ok {
			continue
		}
		for _, r := range c.FilterReasons {
			switch r {
			case "low_volume":
				stats.LowVolumeCount++
			case "low_oi":
				stats.LowOICount++
			case "low_depth":
				stats.LowDepthCount++
			case "missing_supply_or_price":
				stats.MissingSupplyCount++
			case "not_tradable":
				stats.NotTradableCount++
			}
		}
	}
	return stats
}

// IsTradable reports whether the coin can be traded on the target exchange.
// A zero price is treated as untradable for safety.
func (c SmallMarketValueCoin) IsTradable() bool {
	return c.Price > 0
}
