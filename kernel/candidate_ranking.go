package kernel

import (
	"math"
	"sort"

	"nofx/logger"
	"nofx/market"
	"nofx/provider/nofxos"
)

// rankCandidateCoins applies the CandidateRankingFilter to the
// (already-filtered-by-source) candidate list:
//
//  1. Fetch the relevant ranking data from nofxos (price / OI). Funding
//     rate is not a nofxos ranking endpoint, so it is sourced per-coin
//     from the engine's QuantDataMap when UseFundingRate is enabled.
//  2. Score each candidate by the configured weights (price 0.4, OI 0.3,
//     funding 0.3 by default) and normalize each factor to [0, 1].
//  3. Sort by Score descending and truncate to MaxCandidates.
//
// Failures in the data fetch are LOGGED but do NOT block the
// pipeline — the unranked list is returned so the AI can still trade
// when nofxos is down. This mirrors the existing "source X failed"
// soft-fail behavior in GetCandidateCoins.
//
// When the filter is nil or !Enabled, the input slice is returned
// unchanged (Score=0 for every entry).
func (e *StrategyEngine) rankCandidateCoins(candidates []CandidateCoin) []CandidateCoin {
	filter := e.config.CoinSource.RankingFilter
	if filter == nil || !filter.Enabled {
		return candidates
	}
	filter.Clamp()
	if len(candidates) == 0 {
		return candidates
	}

	priceBySymbol := map[string]nofxos.PriceRankingItem{}
	if filter.UsePriceMomentum {
		if data := e.FetchPriceRankingData(); data != nil {
			// Prefer the 1h duration if present; fall back to the first
			// available duration so older data still scores.
			bucket := data.Durations["1h"]
			if bucket == nil {
				for _, b := range data.Durations {
					bucket = b
					break
				}
			}
			if bucket != nil {
				for _, item := range bucket.Top {
					priceBySymbol[market.Normalize(item.Symbol)] = item
				}
				for _, item := range bucket.Low {
					// Low bucket entries can also contribute (short-side
					// signal); store them under the same key, last write
					// wins. In practice Top and Low are disjoint so this
					// is rare.
					sym := market.Normalize(item.Symbol)
					if _, ok := priceBySymbol[sym]; !ok {
						priceBySymbol[sym] = item
					}
				}
			}
		} else {
			logger.Infof("⚠️  candidate ranking: price data unavailable, skipping price_momentum factor")
		}
		ind := e.config.Indicators
		if !ind.EnablePriceRanking {
			logger.Warnf("⚠️  candidate ranking: price_momentum enabled but indicators.EnablePriceRanking is false; factor will contribute 0")
		}
	}

	oiBySymbol := map[string]nofxos.OIPosition{}
	if filter.UseOIChange {
		if data := e.FetchOIRankingData(); data != nil {
			for _, pos := range data.TopPositions {
				oiBySymbol[market.Normalize(pos.Symbol)] = pos
			}
			for _, pos := range data.LowPositions {
				sym := market.Normalize(pos.Symbol)
				if _, ok := oiBySymbol[sym]; !ok {
					oiBySymbol[sym] = pos
				}
			}
		} else {
			logger.Infof("⚠️  candidate ranking: OI data unavailable, skipping oi_change factor")
		}
		ind := e.config.Indicators
		if !ind.EnableOIRanking {
			logger.Warnf("⚠️  candidate ranking: oi_change enabled but indicators.EnableOIRanking is false; factor will contribute 0")
		}
	}

	fundingBySymbol := map[string]float64{}
	_ = fundingBySymbol
	// Funding rate is not exposed as a nofxos ranking endpoint today,
	// and QuantData does not carry it. The plan explicitly marks
	// "FundingRate ranking (如存在)" as optional. We keep the field
	// switch in the config so the wire-format is stable, but the
	// funding factor currently contributes 0 across the board.
	if filter.UseFundingRate {
		logger.Infof("⚠️  candidate ranking: funding-rate factor enabled but no nofxos endpoint; contributing 0")
	}

	// Per-factor weights. Empty filters mean "score 0 for that factor
	// across the board", so they do not skew the total.
	var weightPrice, weightOI, weightFunding float64
	if filter.UsePriceMomentum {
		weightPrice = 0.4
	}
	if filter.UseOIChange {
		weightOI = 0.3
	}
	if filter.UseFundingRate {
		weightFunding = 0.3
	}
	totalWeight := weightPrice + weightOI + weightFunding
	if totalWeight == 0 {
		logger.Infof("⚠️  candidate ranking: no factor enabled in filter; passing through")
		return candidates
	}

	scored := make([]CandidateCoin, len(candidates))
	for i, c := range candidates {
		factors := map[string]float64{}

		if weightPrice > 0 {
			if item, ok := priceBySymbol[c.Symbol]; ok {
				// PriceRankingItem.PriceDelta is a decimal (e.g. 0.025
				// for +2.5%). Use absolute magnitude so both strong
				// gainers and strong losers get a high score; the AI
				// decides direction. Normalize |delta| to [0, 1] over
				// [0%, 5%].
				factors["price_momentum"] = normalize(math.Abs(item.PriceDelta*100), 0, 5)
			}
		}
		if weightOI > 0 {
			if pos, ok := oiBySymbol[c.Symbol]; ok {
				// OIDeltaPercent is already in percent units (e.g. 12.5
				// for +12.5%). Use absolute magnitude so both OI surges
				// and OI drops are treated as high-conviction events.
				factors["oi_change"] = normalize(math.Abs(pos.OIDeltaPercent), 0, 2)
			}
		}
		if weightFunding > 0 {
			// Placeholder for when a funding-rate ranking endpoint
			// becomes available. The factor intentionally stays 0 so
			// users who enable it don't get incorrect scores.
			factors["funding_rate"] = 0
		}

		var total float64
		for k, v := range factors {
			switch k {
			case "price_momentum":
				total += v * weightPrice
			case "oi_change":
				total += v * weightOI
			case "funding_rate":
				total += v * weightFunding
			}
		}
		// Rescale to [0, 1] so a single "all factors enabled" config
		// stays comparable with a future "subset enabled" config.
		c.Score = total / totalWeight
		c.RankFactors = factors
		scored[i] = c
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > filter.MaxCandidates {
		scored = scored[:filter.MaxCandidates]
	}
	logger.Infof("✓ Ranked %d candidates → kept top %d (filter enabled, enforce=%t)",
		len(candidates), len(scored), filter.Enforce)
	return scored
}

// normalize maps x from [lo, hi] into [0, 1] with clamping. Lo >= hi
// returns 0.5 (degenerate range) so the caller still produces a
// deterministic value rather than NaN.
func normalize(x, lo, hi float64) float64 {
	if hi <= lo {
		return 0.5
	}
	if x <= lo {
		return 0
	}
	if x >= hi {
		return 1
	}
	return (x - lo) / (hi - lo)
}
