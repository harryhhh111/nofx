package kernel

import (
	"context"
	"fmt"
	"math"
	"nofx/market"
	"strings"
	"time"
)

type DefaultMarketContextEngine struct{}

const (
	marketContextShortVolPeriod    = 20
	marketContextBaselineVolPeriod = 60
	marketContextATRPeriod         = 14

	highNormalizedRealizedVol      = 0.45
	elevatedNormalizedRealizedVol  = 0.25
	highNormalizedATRPercent       = 0.90
	elevatedNormalizedATRPercent   = 0.50
	highVolatilityExpansionRatio   = 1.80
	raisedVolatilityExpansionRatio = 1.50
)

type volatilityAggregate struct {
	State                          string
	AverageRealizedVol20           float64
	AverageRealizedVol60           float64
	AverageNormalizedRealizedVol20 float64
	AverageATRPercent14            float64
	AverageNormalizedATRPercent14  float64
	AverageExpansionRatio          float64
	SampleCount                    int
	BaselineSampleCount            int
	ATRSampleCount                 int
}

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
	volatility := aggregateVolatility(req.FactorSnapshot)
	volatilityState := volatility.State
	externalState, externalScore := aggregateExternalSignals(req.FactorSnapshot)
	directionBias := classifyDirectionBias(btcTrend, ethTrend, bullishRatio)

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
		GeneratedAt:      req.Now,
		MarketRegime:     regime,
		DirectionBias:    directionBias,
		VolatilityRegime: volatilityState,
		RiskFlags:        riskFlags,
		ContextSummary:   buildContextSummary(regime, directionBias, volatilityState, btcTrend, ethTrend, fundingState, breadthState, externalState, riskFlags),
		BTCTrend:         btcTrend,
		ETHTrend:         ethTrend,
		FundingState:     fundingState,
		BreadthState:     breadthState,
		Metrics: map[string]interface{}{
			"funding_hot_ratio":                      fundingHotRatio,
			"bullish_breadth_ratio":                  bullishRatio,
			"average_realized_vol_20":                volatility.AverageRealizedVol20,
			"average_realized_vol_60":                volatility.AverageRealizedVol60,
			"average_realized_vol_20_15m_equivalent": volatility.AverageNormalizedRealizedVol20,
			"average_atr_percent_14":                 volatility.AverageATRPercent14,
			"average_atr_percent_14_15m_equivalent":  volatility.AverageNormalizedATRPercent14,
			"average_realized_vol_expansion_ratio":   volatility.AverageExpansionRatio,
			"volatility_sample_count":                volatility.SampleCount,
			"volatility_baseline_sample_count":       volatility.BaselineSampleCount,
			"volatility_atr_sample_count":            volatility.ATRSampleCount,
			"external_signal_score":                  externalScore,
		},
	}, nil
}

func assetTrend(snapshot *market.FactorSnapshot) string {
	if snapshot == nil {
		return "unavailable"
	}
	timeframe := dominantTimeframe(snapshot)
	price, ok := snapshotPrice(timeframe, snapshot)
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
	for _, preferred := range []string{"15m", "1h", "4h", "5m", "3m", "1m"} {
		for _, points := range snapshot.Technical {
			for _, point := range points {
				if point.Timeframe == preferred {
					return preferred
				}
			}
		}
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

func aggregateVolatility(snapshots map[string]*market.FactorSnapshot) volatilityAggregate {
	total := 0
	shortSum := 0.0
	normalizedShortSum := 0.0
	baselineTotal := 0
	baselineSum := 0.0
	expansionTotal := 0
	expansionSum := 0.0
	atrTotal := 0
	atrPercentSum := 0.0
	normalizedATRPercentSum := 0.0
	for _, snapshot := range snapshots {
		if snapshot == nil {
			continue
		}
		timeframe := dominantTimeframe(snapshot)
		vol, ok := snapshot.IndicatorValue("realized_vol", timeframe, marketContextShortVolPeriod)
		if !ok {
			continue
		}
		total++
		shortSum += vol
		normalizedShortSum += normalizeVolatilityTo15m(timeframe, vol)
		if baseline, ok := snapshot.IndicatorValue("realized_vol", timeframe, marketContextBaselineVolPeriod); ok && baseline > 0 {
			baselineTotal++
			baselineSum += baseline
			expansionTotal++
			expansionSum += vol / baseline
		}
		if price, ok := snapshotPrice(timeframe, snapshot); ok && price > 0 {
			if atr, ok := snapshot.IndicatorValue("atr", timeframe, marketContextATRPeriod); ok && atr > 0 {
				atrTotal++
				atrPercent := atr / price * 100
				atrPercentSum += atrPercent
				normalizedATRPercentSum += normalizeVolatilityTo15m(timeframe, atrPercent)
			}
		}
	}
	if total == 0 {
		return volatilityAggregate{State: "unavailable"}
	}
	result := volatilityAggregate{
		State:                          "normal",
		AverageRealizedVol20:           shortSum / float64(total),
		AverageNormalizedRealizedVol20: normalizedShortSum / float64(total),
		SampleCount:                    total,
		BaselineSampleCount:            baselineTotal,
		ATRSampleCount:                 atrTotal,
	}
	if baselineTotal > 0 {
		result.AverageRealizedVol60 = baselineSum / float64(baselineTotal)
	}
	if expansionTotal > 0 {
		result.AverageExpansionRatio = expansionSum / float64(expansionTotal)
	}
	if atrTotal > 0 {
		result.AverageATRPercent14 = atrPercentSum / float64(atrTotal)
		result.AverageNormalizedATRPercent14 = normalizedATRPercentSum / float64(atrTotal)
	}
	if isHighVolatility(result) {
		result.State = "high_volatility"
	}
	return result
}

func isHighVolatility(vol volatilityAggregate) bool {
	if vol.AverageNormalizedRealizedVol20 >= highNormalizedRealizedVol {
		return true
	}
	if vol.ATRSampleCount > 0 && vol.AverageNormalizedATRPercent14 >= highNormalizedATRPercent {
		return true
	}
	if vol.AverageExpansionRatio >= highVolatilityExpansionRatio && vol.AverageNormalizedRealizedVol20 >= elevatedNormalizedRealizedVol {
		return true
	}
	if vol.AverageExpansionRatio >= raisedVolatilityExpansionRatio &&
		vol.ATRSampleCount > 0 &&
		vol.AverageNormalizedATRPercent14 >= elevatedNormalizedATRPercent {
		return true
	}
	return false
}

func normalizeVolatilityTo15m(timeframe string, value float64) float64 {
	if value <= 0 {
		return value
	}
	duration, err := market.TFDuration(timeframe)
	if err != nil || duration <= 0 {
		return value
	}
	minutes := duration.Minutes()
	if minutes <= 0 {
		return value
	}
	scale := math.Sqrt(minutes / 15)
	if scale <= 0 {
		return value
	}
	return value / scale
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

func classifyDirectionBias(btcTrend, ethTrend string, bullishRatio float64) string {
	if btcTrend == "bullish" && ethTrend != "bearish" && bullishRatio >= 0.55 {
		return "bullish"
	}
	if btcTrend == "bearish" && ethTrend != "bullish" && bullishRatio <= 0.45 {
		return "bearish"
	}
	if btcTrend == "bullish" && ethTrend == "bullish" {
		return "bullish"
	}
	if btcTrend == "bearish" && ethTrend == "bearish" {
		return "bearish"
	}
	if bullishRatio >= 0.65 {
		return "bullish"
	}
	if bullishRatio <= 0.35 {
		return "bearish"
	}
	return "neutral"
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

func buildContextSummary(regime, directionBias, volatilityState, btcTrend, ethTrend, fundingState, breadthState, externalState string, riskFlags []string) string {
	parts := []string{
		"regime=" + regime,
		"direction=" + directionBias,
		"volatility=" + volatilityState,
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
