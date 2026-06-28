package api

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"nofx/logger"
	"nofx/provider/smallcap"
	"nofx/store"
)

// handleSmallMarketValueCoins returns candidate coins for the small market value
// coin source. Query params override store defaults.
//
// Query params:
//   - limit (int, optional, default store.DefaultSmallMarketValueLimit, max MaxCandidateCoins)
//   - sort_by (string, optional, "market_cap" | "fdv")
//   - min_24h_quote_volume_usd (float64, optional)
//   - min_open_interest_usd (float64, optional)
//   - min_depth_usd (float64, optional)
func (s *Server) handleSmallMarketValueCoins(c *gin.Context) {
	req := parseSmallMarketValueRequest(c)

	if s.smallcapProvider == nil {
		s.smallcapProvider = smallcap.NewCoinAnkProvider(os.Getenv("COINANK_API_KEY"))
	}

	data, err := s.smallcapProvider.GetSmallMarketValueRanking(c.Request.Context(), req)
	if err != nil {
		logger.Errorf("[SmallMarketValue] provider error: %v", err)
		SafeErrorWithDetails(
			c,
			http.StatusBadGateway,
			err.Error(),
			"small_market_value.provider_error",
			nil,
			err,
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"coins":           data.Coins,
		"filter_stats":    data.FilterStats,
		"depth_available": data.DepthAvailable,
		"oi_available":    data.OIAvailable,
		"fetched_at":      data.FetchedAt,
	})
}

func parseSmallMarketValueRequest(c *gin.Context) smallcap.SmallMarketValueRequest {
	req := smallcap.SmallMarketValueRequest{
		Limit:                store.DefaultSmallMarketValueLimit,
		SortBy:               smallcap.SortByMarketCap,
		Min24hQuoteVolumeUSD: store.DefaultSmallMarketValueMinVolume,
		MinOpenInterestUSD:   store.DefaultSmallMarketValueMinOI,
		MinDepthUSD:          store.DefaultSmallMarketValueMinDepth,
	}

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed > store.MaxCandidateCoins {
				parsed = store.MaxCandidateCoins
			}
			req.Limit = parsed
		}
	}

	if sortBy := c.Query("sort_by"); sortBy == "fdv" {
		req.SortBy = smallcap.SortByFDV
	} else if sortBy == "market_cap" {
		req.SortBy = smallcap.SortByMarketCap
	}

	if v := c.Query("min_24h_quote_volume_usd"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed >= 0 {
			req.Min24hQuoteVolumeUSD = parsed
		}
	}
	if v := c.Query("min_open_interest_usd"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed >= 0 {
			req.MinOpenInterestUSD = parsed
		}
	}
	if v := c.Query("min_depth_usd"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed >= 0 {
			req.MinDepthUSD = parsed
		}
	}

	return req
}
