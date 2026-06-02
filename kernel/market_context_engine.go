package kernel

import (
	"context"
	"fmt"
	"nofx/market"
	"strings"
	"time"
)

type DefaultMarketContextEngine struct{}

func NewDefaultMarketContextEngine() *DefaultMarketContextEngine {
	return &DefaultMarketContextEngine{}
}

func (e *DefaultMarketContextEngine) Build(ctx context.Context, req MarketContextRequest) (*MarketContext, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if len(req.FactorSnapshot) == 0 {
		return nil, fmt.Errorf("market context requires factor snapshots")
	}

	btcTrend := assetTrend(req.FactorSnapshot["BTCUSDT"])
	ethTrend := assetTrend(req.FactorSnapshot["ETHUSDT"])
	fundingState, fundingHotRatio := aggregateFundingState(req.FactorSnapshot)
	breadthState, bullishRatio := aggregateBreadth(req.FactorSnapshot)
	volatilityState, avgRealizedVol := aggregateVolatility(req.FactorSnapshot)
	externalState, externalScore := aggregateExternalSignals(req.FactorSnapshot)

	riskFlags := []string{}
	if fundingState == "overheated" {
		riskFlags = append(riskFlags, "funding_overheated")
	}
	if volatilityState == "high_volatility" {
		riskFlags = append(riskFlags, "high_volatility")
	}
	if btcTrend == "bearish" && ethTrend == "bearish" {
		riskFlags = append(riskFlags, "major_assets_bearish")
	}
	if externalState == "external_bearish" {
		riskFlags = append(riskFlags, "external_factors_bearish")
	}

	regime := classifyMarketRegime(btcTrend, ethTrend, fundingState, volatilityState, bullishRatio)
	if regime == "chop" && externalState == "external_bullish" && bullishRatio >= 0.5 {
		regime = "risk_on"
	}
	return &MarketContext{
		GeneratedAt:    req.Now,
		MarketRegime:   regime,
		RiskFlags:      riskFlags,
		ContextSummary: buildContextSummary(regime, btcTrend, ethTrend, fundingState, breadthState, externalState, riskFlags),
		BTCTrend:       btcTrend,
		ETHTrend:       ethTrend,
		FundingState:   fundingState,
		BreadthState:   breadthState,
		Metrics: map[string]interface{}{
			"funding_hot_ratio":       fundingHotRatio,
			"bullish_breadth_ratio":   bullishRatio,
			"average_realized_vol_20": avgRealizedVol,
			"external_signal_score":   externalScore,
		},
	}, nil
}

func assetTrend(snapshot *market.FactorSnapshot) string {
	if snapshot == nil {
		return "unavailable"
	}
	timeframe := dominantTimeframe(snapshot)
	price, ok := snapshot.IndicatorValue("price", "", 0)
	if !ok || price <= 0 {
		price, ok = snapshot.IndicatorValue("price", timeframe, 0)
	}
	if !ok || price <= 0 {
		return "unavailable"
	}

	score := 0
	if ema20, ok := snapshot.IndicatorValue("ema", timeframe, 20); ok && ema20 > 0 {
		if price > ema20 {
			score++
		} else {
			score--
		}
	}
	if ema50, ok := snapshot.IndicatorValue("ema", timeframe, 50); ok && ema50 > 0 {
		if price > ema50 {
			score++
		} else {
			score--
		}
	}
	if hist, ok := snapshot.IndicatorValue("macd_histogram", timeframe, 0); ok {
		if hist > 0 {
			score++
		} else if hist < 0 {
			score--
		}
	}

	switch {
	case score >= 2:
		return "bullish"
	case score <= -2:
		return "bearish"
	case score != 0:
		return "mixed"
	default:
		return "neutral"
	}
}

func dominantTimeframe(snapshot *market.FactorSnapshot) string {
	if snapshot == nil || snapshot.Technical == nil {
		return ""
	}
	for _, points := range snapshot.Technical {
		for _, point := range points {
			if point.Timeframe != "" {
				return point.Timeframe
			}
		}
	}
	return ""
}

func aggregateFundingState(snapshots map[string]*market.FactorSnapshot) (string, float64) {
	total := 0
	hot := 0
	for _, snapshot := range snapshots {
		if snapshot == nil || snapshot.External == nil {
			continue
		}
		funding, ok := snapshot.External["funding_rate"]
		if !ok || !funding.Available {
			continue
		}
		total++
		if strings.HasPrefix(funding.State, "overheated") {
			hot++
		}
	}
	if total == 0 {
		return "unavailable", 0
	}
	ratio := float64(hot) / float64(total)
	if ratio >= 0.3 {
		return "overheated", ratio
	}
	return "normal", ratio
}

func aggregateBreadth(snapshots map[string]*market.FactorSnapshot) (string, float64) {
	total := 0
	bullish := 0
	for _, snapshot := range snapshots {
		trend := assetTrend(snapshot)
		if trend == "unavailable" {
			continue
		}
		total++
		if trend == "bullish" {
			bullish++
		}
	}
	if total == 0 {
		return "unavailable", 0
	}
	ratio := float64(bullish) / float64(total)
	switch {
	case ratio >= 0.65:
		return "broad_bullish", ratio
	case ratio <= 0.35:
		return "broad_bearish", ratio
	default:
		return "mixed", ratio
	}
}

func aggregateVolatility(snapshots map[string]*market.FactorSnapshot) (string, float64) {
	total := 0
	sum := 0.0
	for _, snapshot := range snapshots {
		if snapshot == nil {
			continue
		}
		timeframe := dominantTimeframe(snapshot)
		vol, ok := snapshot.IndicatorValue("realized_vol", timeframe, 20)
		if !ok {
			continue
		}
		total++
		sum += vol
	}
	if total == 0 {
		return "unavailable", 0
	}
	avg := sum / float64(total)
	if avg >= 0.035 {
		return "high_volatility", avg
	}
	return "normal", avg
}

func aggregateExternalSignals(snapshots map[string]*market.FactorSnapshot) (string, float64) {
	total := 0
	score := 0.0
	for _, snapshot := range snapshots {
		if snapshot == nil || snapshot.External == nil {
			continue
		}
		for name, factor := range snapshot.External {
			if !factor.Available {
				continue
			}
			switch {
			case strings.HasPrefix(name, "netflow_institution_future_top"):
				total++
				score += 1
			case strings.HasPrefix(name, "netflow_institution_future_low"):
				total++
				score -= 1
			case strings.HasPrefix(name, "oi_ranking_top"):
				total++
				if factor.Score > 0 {
					score += 0.5
				}
			case strings.HasPrefix(name, "price_ranking_top"):
				total++
				score += 0.5
			case strings.HasPrefix(name, "price_ranking_low"):
				total++
				score -= 0.5
			}
		}
	}
	if total == 0 {
		return "unavailable", 0
	}
	normalized := score / float64(total)
	switch {
	case normalized >= 0.25:
		return "external_bullish", normalized
	case normalized <= -0.25:
		return "external_bearish", normalized
	default:
		return "external_mixed", normalized
	}
}

func classifyMarketRegime(btcTrend, ethTrend, fundingState, volatilityState string, bullishRatio float64) string {
	if volatilityState == "high_volatility" {
		return "high_volatility"
	}
	if fundingState == "overheated" {
		return "overheated"
	}
	if btcTrend == "bullish" && ethTrend == "bullish" && bullishRatio >= 0.55 {
		return "risk_on"
	}
	if btcTrend == "bearish" && ethTrend == "bearish" && bullishRatio <= 0.45 {
		return "risk_off"
	}
	return "chop"
}

func buildContextSummary(regime, btcTrend, ethTrend, fundingState, breadthState, externalState string, riskFlags []string) string {
	parts := []string{
		"regime=" + regime,
		"btc=" + btcTrend,
		"eth=" + ethTrend,
		"funding=" + fundingState,
		"breadth=" + breadthState,
		"external=" + externalState,
	}
	if len(riskFlags) > 0 {
		parts = append(parts, "risk_flags="+strings.Join(riskFlags, ","))
	}
	return strings.Join(parts, "; ")
}
