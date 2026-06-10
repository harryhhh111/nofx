package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Hard limits to prevent token explosion in AI requests
const (
	MaxCandidateCoins  = 10
	MaxPositions       = 3
	MaxTimeframes      = 5
	MinKlineCount      = 10
	MaxKlineCount      = 100
	MaxComputeLookback = 1000

	DefaultMinConfidence      = 50
	MinMinConfidence          = 50
	MaxMinConfidence          = 90
	DefaultMinCloseConfidence = 85
	MinMinCloseConfidence     = 70
	MaxMinCloseConfidence     = 95
	DefaultMinRiskRewardRatio = 2.5
	DefaultRiskPerTradePct    = 1.0
	DefaultMinPositionSize    = 12.0
)

// ClampLimits enforces product-level limits on strategy config to prevent token overflow.
func (c *StrategyConfig) ClampLimits() {
	if c.StrategyMode == "" {
		c.StrategyMode = "rule"
	}
	if c.StrategyMode != "rule" && c.StrategyMode != "scoring" && c.StrategyMode != "hybrid" {
		c.StrategyMode = "rule"
	}

	c.normalizeCoinSourceFlags()
	c.Indicators.EnableRawKlines = true
	// Claw402's current official nofx catalog exposes market-wide ranking
	// endpoints, but not the legacy coin-level quant endpoint. Keep the legacy
	// fields for saved JSON compatibility while preventing unsupported 404
	// requests from entering the trading loop.
	c.Indicators.EnableQuantData = false
	c.Indicators.EnableQuantOI = false
	c.Indicators.EnableQuantNetflow = false

	// Clamp coin source limits
	if c.CoinSource.AI500Limit > MaxCandidateCoins {
		c.CoinSource.AI500Limit = MaxCandidateCoins
	}
	if c.CoinSource.OITopLimit > MaxCandidateCoins {
		c.CoinSource.OITopLimit = MaxCandidateCoins
	}
	if c.CoinSource.OILowLimit > MaxCandidateCoins {
		c.CoinSource.OILowLimit = MaxCandidateCoins
	}

	// Clamp static coins
	if len(c.CoinSource.StaticCoins) > MaxCandidateCoins {
		c.CoinSource.StaticCoins = c.CoinSource.StaticCoins[:MaxCandidateCoins]
	}

	// Clamp kline count
	if c.Indicators.Klines.PrimaryCount < MinKlineCount {
		c.Indicators.Klines.PrimaryCount = MinKlineCount
	}
	if c.Indicators.Klines.PrimaryCount > MaxKlineCount {
		c.Indicators.Klines.PrimaryCount = MaxKlineCount
	}
	if c.Indicators.Klines.LongerCount > MaxKlineCount {
		c.Indicators.Klines.LongerCount = MaxKlineCount
	}
	if c.Indicators.Klines.ComputeLookback <= 0 {
		c.Indicators.Klines.ComputeLookback = 300
	}
	if c.Indicators.Klines.ComputeLookback < c.Indicators.Klines.PrimaryCount {
		c.Indicators.Klines.ComputeLookback = c.Indicators.Klines.PrimaryCount
	}
	if c.Indicators.Klines.ComputeLookback > MaxComputeLookback {
		c.Indicators.Klines.ComputeLookback = MaxComputeLookback
	}
	if c.Indicators.Klines.PromptDisplayCount <= 0 {
		c.Indicators.Klines.PromptDisplayCount = c.Indicators.Klines.PrimaryCount
	}
	if c.Indicators.Klines.PromptDisplayCount < MinKlineCount {
		c.Indicators.Klines.PromptDisplayCount = MinKlineCount
	}
	if c.Indicators.Klines.PromptDisplayCount > MaxKlineCount {
		c.Indicators.Klines.PromptDisplayCount = MaxKlineCount
	}

	// Clamp timeframes
	if len(c.Indicators.Klines.SelectedTimeframes) > MaxTimeframes {
		c.Indicators.Klines.SelectedTimeframes = c.Indicators.Klines.SelectedTimeframes[:MaxTimeframes]
	}
	c.normalizeTimeframeRoles()
	c.clampIndicatorConfig()

	// Clamp max positions
	if c.RiskControl.MaxPositions <= 0 {
		c.RiskControl.MaxPositions = MaxPositions
	}
	if c.RiskControl.MaxPositions > MaxPositions {
		c.RiskControl.MaxPositions = MaxPositions
	}

	// Default leverage when not provided (zero means unset). A zero leverage
	// limit would make the risk gate reject every signal ("Nx > 0x"), silently
	// rendering the strategy untradeable. Mirror the trader column default (5x).
	if c.RiskControl.BTCETHMaxLeverage <= 0 {
		c.RiskControl.BTCETHMaxLeverage = 5
	}
	if c.RiskControl.AltcoinMaxLeverage <= 0 {
		c.RiskControl.AltcoinMaxLeverage = 5
	}

	// Default position value ratios when not provided (zero means unset)
	if c.RiskControl.BTCETHMaxPositionValueRatio <= 0 {
		c.RiskControl.BTCETHMaxPositionValueRatio = 5.0
	}
	if c.RiskControl.AltcoinMaxPositionValueRatio <= 0 {
		c.RiskControl.AltcoinMaxPositionValueRatio = 1.0
	}

	// Default margin usage, position sizing and min position size when not provided.
	if c.RiskControl.MaxMarginUsage <= 0 {
		c.RiskControl.MaxMarginUsage = 0.9
	}
	if c.RiskControl.RiskPerTradePct <= 0 {
		c.RiskControl.RiskPerTradePct = DefaultRiskPerTradePct
	}
	if c.RiskControl.RiskPerTradePct < 0.1 {
		c.RiskControl.RiskPerTradePct = 0.1
	}
	if c.RiskControl.RiskPerTradePct > 5 {
		c.RiskControl.RiskPerTradePct = 5
	}
	if c.RiskControl.MinPositionSize <= 0 {
		c.RiskControl.MinPositionSize = DefaultMinPositionSize
	}
	if c.RiskControl.MinRiskRewardRatio <= 0 {
		c.RiskControl.MinRiskRewardRatio = DefaultMinRiskRewardRatio
	}

	// Clamp AI confidence thresholds to safe product ranges.
	if c.RiskControl.MinConfidence <= 0 {
		c.RiskControl.MinConfidence = DefaultMinConfidence
	}
	if c.RiskControl.MinConfidence < MinMinConfidence {
		c.RiskControl.MinConfidence = MinMinConfidence
	}
	if c.RiskControl.MinConfidence > MaxMinConfidence {
		c.RiskControl.MinConfidence = MaxMinConfidence
	}
	if c.RiskControl.MinCloseConfidence <= 0 {
		c.RiskControl.MinCloseConfidence = DefaultMinCloseConfidence
	}
	if c.RiskControl.MinCloseConfidence < MinMinCloseConfidence {
		c.RiskControl.MinCloseConfidence = MinMinCloseConfidence
	}
	if c.RiskControl.MinCloseConfidence > MaxMinCloseConfidence {
		c.RiskControl.MinCloseConfidence = MaxMinCloseConfidence
	}

	// Drawdown-close defaults: treat zero values as "not yet configured" and apply defaults.
	// DrawdownCloseEnabled defaults to true (opt-out model).
	// We use a sentinel: if both min-profit and trigger are zero, assume first-time setup.
	if c.RiskControl.DrawdownCloseMinProfitPct == 0 && c.RiskControl.DrawdownCloseTriggerPct == 0 {
		c.RiskControl.DrawdownCloseEnabled = true
		c.RiskControl.DrawdownCloseMinProfitPct = 5.0
		c.RiskControl.DrawdownCloseTriggerPct = 40.0
	}
	// Clamp to sensible ranges
	if c.RiskControl.DrawdownCloseMinProfitPct < 1.0 {
		c.RiskControl.DrawdownCloseMinProfitPct = 1.0
	}
	if c.RiskControl.DrawdownCloseTriggerPct < 10.0 {
		c.RiskControl.DrawdownCloseTriggerPct = 10.0
	}
	if c.RiskControl.DrawdownCloseTriggerPct > 90.0 {
		c.RiskControl.DrawdownCloseTriggerPct = 90.0
	}

	c.clampStructureConfig()
	c.clampScoringConfig()
	c.ensureComputeLookbackForCalculations()
	c.resolveParameters()
}

func (c *StrategyConfig) normalizeCoinSourceFlags() {
	switch c.CoinSource.SourceType {
	case "mixed":
		return
	case "static":
		c.CoinSource.UseAI500 = false
		c.CoinSource.UseOITop = false
		c.CoinSource.UseOILow = false
		c.CoinSource.UseHyperAll = false
		c.CoinSource.UseHyperMain = false
	case "ai500":
		c.CoinSource.UseAI500 = true
		c.CoinSource.UseOITop = false
		c.CoinSource.UseOILow = false
		c.CoinSource.UseHyperAll = false
		c.CoinSource.UseHyperMain = false
	case "oi_top":
		c.CoinSource.UseAI500 = false
		c.CoinSource.UseOITop = true
		c.CoinSource.UseOILow = false
		c.CoinSource.UseHyperAll = false
		c.CoinSource.UseHyperMain = false
	case "oi_low":
		c.CoinSource.UseAI500 = false
		c.CoinSource.UseOITop = false
		c.CoinSource.UseOILow = true
		c.CoinSource.UseHyperAll = false
		c.CoinSource.UseHyperMain = false
	default:
		c.CoinSource.SourceType = "static"
		c.CoinSource.UseAI500 = false
		c.CoinSource.UseOITop = false
		c.CoinSource.UseOILow = false
		c.CoinSource.UseHyperAll = false
		c.CoinSource.UseHyperMain = false
	}
}

func (c *StrategyConfig) normalizeTimeframeRoles() {
	klines := &c.Indicators.Klines
	klines.MarketDataSource = normalizeMarketDataSource(klines.MarketDataSource)
	selected := sanitizeTimeframeList(klines.SelectedTimeframes)
	if len(selected) == 0 {
		if klines.PrimaryTimeframe != "" {
			selected = append(selected, klines.PrimaryTimeframe)
		} else {
			selected = append(selected, "5m")
		}
	}

	sort.SliceStable(selected, func(i, j int) bool {
		return timeframeMinutes(selected[i]) < timeframeMinutes(selected[j])
	})

	entryWasEmpty := klines.EntryTimeframe == ""
	if len(selected) >= 2 && klines.PrimaryTimeframe == selected[0] && (entryWasEmpty || klines.EntryTimeframe == selected[0]) {
		klines.PrimaryTimeframe = selected[1]
	}
	if !containsString(selected, klines.PrimaryTimeframe) {
		if len(selected) >= 2 {
			klines.PrimaryTimeframe = selected[1]
		} else {
			klines.PrimaryTimeframe = selected[0]
		}
	}
	if entryWasEmpty || !containsString(selected, klines.EntryTimeframe) {
		klines.EntryTimeframe = selected[0]
	}

	confirmations := sanitizeTimeframeList(klines.ConfirmationTimeframes)
	if len(confirmations) == 0 {
		for _, tf := range selected {
			if tf != klines.EntryTimeframe && tf != klines.PrimaryTimeframe {
				confirmations = append(confirmations, tf)
			}
		}
	}
	filtered := make([]string, 0, len(confirmations))
	for _, tf := range confirmations {
		if containsString(selected, tf) && tf != klines.EntryTimeframe && tf != klines.PrimaryTimeframe {
			filtered = append(filtered, tf)
		}
	}

	klines.SelectedTimeframes = selected
	klines.ConfirmationTimeframes = filtered
	klines.EnableMultiTimeframe = len(selected) > 1
}

func normalizeMarketDataSource(source string) string {
	normalized := strings.ToLower(strings.TrimSpace(source))
	switch normalized {
	case "", "paper":
		return "binance"
	case "auto", "binance", "bybit", "okx", "aster", "hyperliquid":
		return normalized
	default:
		return normalized
	}
}

func sanitizeTimeframeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		out = append(out, value)
		seen[value] = true
	}
	return out
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func timeframeMinutes(tf string) int {
	if tf == "" {
		return 0
	}
	unit := tf[len(tf)-1:]
	number := tf[:len(tf)-1]
	value := 0
	if _, err := fmt.Sscanf(number, "%d", &value); err != nil || value <= 0 {
		return 0
	}
	switch unit {
	case "m":
		return value
	case "h":
		return value * 60
	case "d":
		return value * 60 * 24
	case "w":
		return value * 60 * 24 * 7
	default:
		return value
	}
}

func (c *StrategyConfig) clampIndicatorConfig() {
	c.Indicators.EMAPeriods = sanitizeIndicatorPeriods(c.Indicators.EMAPeriods, []int{20, 50})
	c.Indicators.SMAPeriods = sanitizeIndicatorPeriods(c.Indicators.SMAPeriods, []int{5, 20, 50})
	c.Indicators.RSIPeriods = sanitizeIndicatorPeriods(c.Indicators.RSIPeriods, []int{7, 14})
	c.Indicators.ATRPeriods = sanitizeIndicatorPeriods(c.Indicators.ATRPeriods, []int{14})
	c.Indicators.BOLLPeriods = sanitizeIndicatorPeriods(c.Indicators.BOLLPeriods, []int{20})
	c.Indicators.VolumePeriods = sanitizeIndicatorPeriods(c.Indicators.VolumePeriods, []int{20})
	c.Indicators.VWAPPeriods = sanitizeIndicatorPeriods(c.Indicators.VWAPPeriods, []int{20})
	c.Indicators.DonchianPeriods = sanitizeIndicatorPeriods(c.Indicators.DonchianPeriods, []int{20})
	c.Indicators.RealizedVolPeriods = sanitizeIndicatorPeriods(c.Indicators.RealizedVolPeriods, []int{20})
	c.Indicators.PriceChangeWindows = sanitizeIndicatorPeriods(c.Indicators.PriceChangeWindows, []int{12, 48})

	if c.Indicators.MACDFastPeriod <= 0 {
		c.Indicators.MACDFastPeriod = 12
	}
	if c.Indicators.MACDSlowPeriod <= 0 {
		c.Indicators.MACDSlowPeriod = 26
	}
	if c.Indicators.MACDSignalPeriod <= 0 {
		c.Indicators.MACDSignalPeriod = 9
	}
	if c.Indicators.ADXPeriod <= 0 {
		c.Indicators.ADXPeriod = 14
	}
	if c.Indicators.ADXPeriod > MaxComputeLookback/2 {
		c.Indicators.ADXPeriod = MaxComputeLookback / 2
	}
	if c.Indicators.MACDFastPeriod >= c.Indicators.MACDSlowPeriod {
		c.Indicators.MACDFastPeriod = 12
		c.Indicators.MACDSlowPeriod = 26
	}
	if c.Indicators.MACDSlowPeriod > MaxComputeLookback/2 {
		c.Indicators.MACDSlowPeriod = MaxComputeLookback / 2
	}
	if c.Indicators.MACDSignalPeriod > MaxComputeLookback/2 {
		c.Indicators.MACDSignalPeriod = MaxComputeLookback / 2
	}
}

func sanitizeIndicatorPeriods(values []int, defaults []int) []int {
	out := make([]int, 0, len(values))
	seen := map[int]bool{}
	for _, value := range values {
		if value <= 0 || value > MaxComputeLookback || seen[value] {
			continue
		}
		out = append(out, value)
		seen[value] = true
	}
	if len(out) == 0 {
		return append([]int(nil), defaults...)
	}
	sort.Ints(out)
	return out
}

func (c *StrategyConfig) ensureComputeLookbackForCalculations() {
	required := c.Indicators.Klines.ComputeLookback
	required = maxInt(required, maxPeriod(c.Indicators.EMAPeriods))
	required = maxInt(required, maxPeriod(c.Indicators.SMAPeriods))
	required = maxInt(required, maxPeriod(c.Indicators.RSIPeriods)+1)
	required = maxInt(required, maxPeriod(c.Indicators.ATRPeriods)+1)
	required = maxInt(required, c.Indicators.ADXPeriod+1)
	required = maxInt(required, maxPeriod(c.Indicators.BOLLPeriods))
	required = maxInt(required, maxPeriod(c.Indicators.VolumePeriods))
	required = maxInt(required, maxPeriod(c.Indicators.VWAPPeriods))
	required = maxInt(required, maxPeriod(c.Indicators.DonchianPeriods))
	required = maxInt(required, maxPeriod(c.Indicators.RealizedVolPeriods)+1)
	required = maxInt(required, maxPeriod(c.Indicators.PriceChangeWindows)+1)
	required = maxInt(required, c.Indicators.MACDSlowPeriod+c.Indicators.MACDSignalPeriod)

	if c.Structure.EnableFibonacci {
		required = maxInt(required, c.Structure.Fibonacci.Lookback)
	}
	if c.Structure.EnableSupportResistance {
		required = maxInt(required, c.Structure.SupportResistance.Lookback)
	}
	if required > MaxComputeLookback {
		required = MaxComputeLookback
	}
	if required > c.Indicators.Klines.ComputeLookback {
		c.Indicators.Klines.ComputeLookback = required
	}
}

func maxPeriod(values []int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c *StrategyConfig) clampScoringConfig() {
	if c.ScoringConfig == nil {
		return
	}
	if c.ScoringConfig.Timeframe == "" {
		c.ScoringConfig.Timeframe = c.resolveStructureTimeframe("")
	}
	if c.Indicators.Klines.PrimaryTimeframe != "" && c.Indicators.Klines.EntryTimeframe != "" &&
		c.Indicators.Klines.PrimaryTimeframe != c.Indicators.Klines.EntryTimeframe &&
		c.ScoringConfig.Timeframe == c.Indicators.Klines.EntryTimeframe {
		c.ScoringConfig.Timeframe = c.Indicators.Klines.PrimaryTimeframe
	}
	if c.ScoringConfig.LongThreshold <= 0 {
		c.ScoringConfig.LongThreshold = 60
	}
	if c.ScoringConfig.LongThreshold > 100 {
		c.ScoringConfig.LongThreshold = 100
	}
	if c.ScoringConfig.ShortThreshold <= 0 {
		c.ScoringConfig.ShortThreshold = -60
	}
	if c.ScoringConfig.ShortThreshold < -100 {
		c.ScoringConfig.ShortThreshold = -100
	}
	if c.ScoringConfig.MinConfidence <= 0 {
		c.ScoringConfig.MinConfidence = c.RiskControl.MinConfidence
	}
	if c.ScoringConfig.MinConfidence < MinMinConfidence {
		c.ScoringConfig.MinConfidence = MinMinConfidence
	}
	if c.ScoringConfig.MinConfidence > MaxMinConfidence {
		c.ScoringConfig.MinConfidence = MaxMinConfidence
	}
	if len(c.ScoringConfig.SelectedFactors) == 0 {
		c.ScoringConfig.SelectedFactors = []string{"trend", "momentum", "structure", "derivatives"}
	}
	if len(c.ScoringConfig.FactorWeights) == 0 {
		c.ScoringConfig.FactorWeights = map[string]float64{
			"trend":       0.30,
			"momentum":    0.25,
			"structure":   0.25,
			"derivatives": 0.20,
		}
	}
	normalizeScoringFactorWeights(c.ScoringConfig)
	for _, factor := range c.ScoringConfig.SelectedFactors {
		if factor == "structure" {
			c.Structure.EnableFibonacci = true
			c.Structure.EnableSupportResistance = true
		}
	}
}

func normalizeScoringFactorWeights(scoring *ScoringStrategyConfig) {
	if scoring == nil || len(scoring.SelectedFactors) == 0 {
		return
	}
	total := 0.0
	for _, factor := range scoring.SelectedFactors {
		weight := scoring.FactorWeights[factor]
		if weight <= 0 {
			weight = 0.01
		}
		if weight > 1 && weight <= 100 {
			weight = weight / 100
		}
		if weight > 1 {
			weight = 1
		}
		scoring.FactorWeights[factor] = weight
		total += weight
	}
	if total <= 0 {
		equal := 1 / float64(len(scoring.SelectedFactors))
		for _, factor := range scoring.SelectedFactors {
			scoring.FactorWeights[factor] = equal
		}
		return
	}
	for _, factor := range scoring.SelectedFactors {
		scoring.FactorWeights[factor] = scoring.FactorWeights[factor] / total
	}
}

func (c *StrategyConfig) clampStructureConfig() {
	defaults := defaultStructureFactorConfig()
	if c.Structure.Fibonacci.Timeframe == "" {
		c.Structure.Fibonacci.Timeframe = defaults.Fibonacci.Timeframe
	}
	if c.Structure.Fibonacci.Lookback <= 0 {
		c.Structure.Fibonacci.Lookback = defaults.Fibonacci.Lookback
	}
	if c.Structure.Fibonacci.Lookback > MaxComputeLookback {
		c.Structure.Fibonacci.Lookback = MaxComputeLookback
	}
	if c.Structure.Fibonacci.Lookback < 20 {
		c.Structure.Fibonacci.Lookback = 20
	}
	if c.Structure.Fibonacci.SwingWindow <= 0 {
		c.Structure.Fibonacci.SwingWindow = defaults.Fibonacci.SwingWindow
	}
	if c.Structure.Fibonacci.SwingWindow > 20 {
		c.Structure.Fibonacci.SwingWindow = 20
	}
	if c.Structure.Fibonacci.MinLegBars <= 0 {
		c.Structure.Fibonacci.MinLegBars = defaults.Fibonacci.MinLegBars
	}
	if c.Structure.Fibonacci.MinLegBars > c.Structure.Fibonacci.Lookback {
		c.Structure.Fibonacci.MinLegBars = c.Structure.Fibonacci.Lookback
	}
	if c.Structure.Fibonacci.MinLegATRMultiple <= 0 {
		c.Structure.Fibonacci.MinLegATRMultiple = defaults.Fibonacci.MinLegATRMultiple
	}
	if c.Structure.Fibonacci.MinLegATRMultiple > 20 {
		c.Structure.Fibonacci.MinLegATRMultiple = 20
	}
	if c.Structure.Fibonacci.ZigZagThresholdPct <= 0 {
		c.Structure.Fibonacci.ZigZagThresholdPct = defaults.Fibonacci.ZigZagThresholdPct
	}
	if c.Structure.Fibonacci.ZigZagThresholdPct > 100 {
		c.Structure.Fibonacci.ZigZagThresholdPct = 100
	}
	c.Structure.Fibonacci.Levels = sanitizeFibLevels(c.Structure.Fibonacci.Levels)
	if len(c.Structure.Fibonacci.Levels) == 0 {
		c.Structure.Fibonacci.Levels = append([]float64(nil), defaults.Fibonacci.Levels...)
	}

	if c.Structure.SupportResistance.Timeframe == "" {
		c.Structure.SupportResistance.Timeframe = defaults.SupportResistance.Timeframe
	}
	if c.Structure.SupportResistance.Lookback <= 0 {
		c.Structure.SupportResistance.Lookback = defaults.SupportResistance.Lookback
	}
	if c.Structure.SupportResistance.Lookback > MaxComputeLookback {
		c.Structure.SupportResistance.Lookback = MaxComputeLookback
	}
	if c.Structure.SupportResistance.Lookback < 20 {
		c.Structure.SupportResistance.Lookback = 20
	}
	if c.Structure.SupportResistance.SwingWindow <= 0 {
		c.Structure.SupportResistance.SwingWindow = defaults.SupportResistance.SwingWindow
	}
	if c.Structure.SupportResistance.SwingWindow > 20 {
		c.Structure.SupportResistance.SwingWindow = 20
	}
	if c.Structure.SupportResistance.ZoneWidthATR <= 0 {
		c.Structure.SupportResistance.ZoneWidthATR = defaults.SupportResistance.ZoneWidthATR
	}
	if c.Structure.SupportResistance.ZoneWidthATR > 10 {
		c.Structure.SupportResistance.ZoneWidthATR = 10
	}
	if c.Structure.SupportResistance.MinTouches <= 0 {
		c.Structure.SupportResistance.MinTouches = defaults.SupportResistance.MinTouches
	}
	if c.Structure.SupportResistance.MinTouches > 20 {
		c.Structure.SupportResistance.MinTouches = 20
	}
	if c.Structure.SupportResistance.MinDistanceBars <= 0 {
		c.Structure.SupportResistance.MinDistanceBars = defaults.SupportResistance.MinDistanceBars
	}
	if c.Structure.SupportResistance.MinDistanceBars > c.Structure.SupportResistance.Lookback {
		c.Structure.SupportResistance.MinDistanceBars = c.Structure.SupportResistance.Lookback
	}
}

func (c *StrategyConfig) resolveParameters() {
	c.ResolvedParameters = ResolvedStrategyParameters{}
	if c.Structure.EnableFibonacci {
		fib := c.Structure.Fibonacci
		fib.Timeframe = c.resolveStructureTimeframe(fib.Timeframe)
		c.ResolvedParameters.Structure.Fibonacci = &fib
	}
	if c.Structure.EnableSupportResistance {
		support := c.Structure.SupportResistance
		support.Timeframe = c.resolveStructureTimeframe(support.Timeframe)
		c.ResolvedParameters.Structure.SupportResistance = &support
	}
	if c.ScoringConfig != nil && c.ScoringConfig.Enabled {
		scoring := *c.ScoringConfig
		if scoring.FactorWeights != nil {
			scoring.FactorWeights = copyFloatMap(scoring.FactorWeights)
		}
		scoring.SelectedFactors = append([]string(nil), scoring.SelectedFactors...)
		scoring.Symbols = append([]string(nil), scoring.Symbols...)
		c.ResolvedParameters.Scoring = &scoring
	}
}

func copyFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (c *StrategyConfig) resolveStructureTimeframe(timeframe string) string {
	if timeframe != "" {
		return timeframe
	}
	if c.Indicators.Klines.PrimaryTimeframe != "" {
		return c.Indicators.Klines.PrimaryTimeframe
	}
	if len(c.Indicators.Klines.SelectedTimeframes) > 0 {
		return c.Indicators.Klines.SelectedTimeframes[0]
	}
	return ""
}

func sanitizeFibLevels(levels []float64) []float64 {
	sanitized := make([]float64, 0, len(levels))
	seen := map[float64]bool{}
	for _, level := range levels {
		if level <= 0 || level >= 1 || seen[level] {
			continue
		}
		seen[level] = true
		sanitized = append(sanitized, level)
	}
	sort.Float64s(sanitized)
	return sanitized
}

// StrategyStore strategy storage
type StrategyStore struct {
	db *gorm.DB
}

// Strategy strategy configuration
type Strategy struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	UserID        string    `gorm:"column:user_id;not null;default:'';index" json:"user_id"`
	Name          string    `gorm:"not null" json:"name"`
	Description   string    `gorm:"default:''" json:"description"`
	IsActive      bool      `gorm:"column:is_active;default:false;index" json:"is_active"`
	IsDefault     bool      `gorm:"column:is_default;default:false" json:"is_default"`
	IsPublic      bool      `gorm:"column:is_public;default:false;index" json:"is_public"`    // whether visible in strategy market
	ConfigVisible bool      `gorm:"column:config_visible;default:true" json:"config_visible"` // whether config details are visible
	Config        string    `gorm:"not null;default:'{}'" json:"config"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Strategy) TableName() string { return "strategies" }

// StrategyConfig strategy configuration details (JSON structure)
type StrategyConfig struct {
	// Strategy type: "ai_trading" (default) or "grid_trading"
	StrategyType string `json:"strategy_type,omitempty"`
	// Trading decision mode: rule, scoring, or hybrid.
	StrategyMode string `json:"strategy_mode,omitempty"`

	// language setting: "zh" for Chinese, "en" for English
	Language string `json:"language,omitempty"`
	// coin source configuration
	CoinSource CoinSourceConfig `json:"coin_source"`
	// quantitative data configuration
	Indicators IndicatorConfig `json:"indicators"`
	// deterministic market-structure factor configuration
	Structure StructureFactorConfig `json:"structure,omitempty"`
	// whether AI should see historical closed trades and performance stats
	// default: true. current open positions are NOT affected by this switch.
	IncludeHistoricalContext *bool `json:"include_historical_context,omitempty"`
	// risk control configuration
	RiskControl RiskControlConfig `json:"risk_control"`
	// Natural-language strategy source. It must be compiled into CompiledRules
	// before it can affect live trading.
	StrategyPrompt string `json:"strategy_prompt,omitempty"`
	// Compiled strategy rules. User natural-language prompts should be
	// converted into these deterministic rules before live trading.
	CompiledRules []CompiledStrategyRule `json:"compiled_rules,omitempty"`
	// Deterministic scoring configuration. AI can suggest this during strategy
	// creation, but live trading reads it as fixed configuration.
	ScoringConfig *ScoringStrategyConfig `json:"scoring_config,omitempty"`
	// Final parameters actually used by the deterministic engines. This is
	// persisted for audit, replay, and UI display.
	ResolvedParameters ResolvedStrategyParameters `json:"resolved_parameters,omitempty"`

	// Grid trading configuration (only used when StrategyType == "grid_trading")
	GridConfig *GridStrategyConfig `json:"grid_config,omitempty"`
}

type ResolvedStrategyParameters struct {
	Structure ResolvedStructureParameters `json:"structure,omitempty"`
	Scoring   *ScoringStrategyConfig      `json:"scoring,omitempty"`
}

type ResolvedStructureParameters struct {
	Fibonacci         *StructureFibonacciConfig         `json:"fibonacci,omitempty"`
	SupportResistance *StructureSupportResistanceConfig `json:"support_resistance,omitempty"`
}

type CompiledStrategyRule struct {
	ID          string                  `json:"id"`
	Version     string                  `json:"version"`
	Description string                  `json:"description,omitempty"`
	Symbols     []string                `json:"symbols,omitempty"`
	Timeframe   string                  `json:"timeframe,omitempty"`
	Conditions  []CompiledRuleCondition `json:"conditions"`
	Action      string                  `json:"action"`
	Execution   CompiledRuleExecution   `json:"execution"`
	Enabled     bool                    `json:"enabled"`
}

type CompiledRuleExecution struct {
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLossPct     float64 `json:"stop_loss_pct,omitempty"`
	TakeProfitPct   float64 `json:"take_profit_pct,omitempty"`
	Confidence      int     `json:"confidence,omitempty"`
}

type CompiledRuleCondition struct {
	Left     CompiledRuleOperand `json:"left"`
	Operator string              `json:"operator"`
	Right    CompiledRuleOperand `json:"right"`
}

type CompiledRuleOperand struct {
	Kind      string  `json:"kind"` // indicator, external_factor, structure, literal
	Name      string  `json:"name,omitempty"`
	Timeframe string  `json:"timeframe,omitempty"`
	Period    int     `json:"period,omitempty"`
	Field     string  `json:"field,omitempty"`
	Value     float64 `json:"value,omitempty"`
}

type ScoringStrategyConfig struct {
	Enabled                 bool                  `json:"enabled"`
	SelectedFactors         []string              `json:"selected_factors,omitempty"`
	FactorWeights           map[string]float64    `json:"factor_weights,omitempty"`
	LongThreshold           float64               `json:"long_threshold,omitempty"`
	ShortThreshold          float64               `json:"short_threshold,omitempty"`
	MinAvailableWeightRatio float64               `json:"min_available_weight_ratio,omitempty"`
	MinConfidence           int                   `json:"min_confidence,omitempty"`
	Timeframe               string                `json:"timeframe,omitempty"`
	Symbols                 []string              `json:"symbols,omitempty"`
	Execution               CompiledRuleExecution `json:"execution"`
}

type StructureFactorConfig struct {
	EnableFibonacci         bool                             `json:"enable_fibonacci"`
	EnableSupportResistance bool                             `json:"enable_support_resistance"`
	Fibonacci               StructureFibonacciConfig         `json:"fibonacci,omitempty"`
	SupportResistance       StructureSupportResistanceConfig `json:"support_resistance,omitempty"`
}

type StructureFibonacciConfig struct {
	Timeframe             string    `json:"timeframe,omitempty"`
	Lookback              int       `json:"lookback,omitempty"`
	SwingWindow           int       `json:"swing_window,omitempty"`
	MinLegBars            int       `json:"min_leg_bars,omitempty"`
	MinLegATRMultiple     float64   `json:"min_leg_atr_multiple,omitempty"`
	ZigZagThresholdPct    float64   `json:"zigzag_threshold_pct,omitempty"`
	Levels                []float64 `json:"levels,omitempty"`
	InvalidateOnBreakBase bool      `json:"invalidate_on_break_base"`
}

type StructureSupportResistanceConfig struct {
	Timeframe       string  `json:"timeframe,omitempty"`
	Lookback        int     `json:"lookback,omitempty"`
	SwingWindow     int     `json:"swing_window,omitempty"`
	ZoneWidthATR    float64 `json:"zone_width_atr,omitempty"`
	MinTouches      int     `json:"min_touches,omitempty"`
	MinDistanceBars int     `json:"min_distance_bars,omitempty"`
}

// GridStrategyConfig grid trading specific configuration
type GridStrategyConfig struct {
	// Trading pair (e.g., "BTCUSDT")
	Symbol string `json:"symbol"`
	// Number of grid levels (5-50)
	GridCount int `json:"grid_count"`
	// Total investment in USDT
	TotalInvestment float64 `json:"total_investment"`
	// Leverage (1-20)
	Leverage int `json:"leverage"`
	// Upper price boundary (0 = auto-calculate from ATR)
	UpperPrice float64 `json:"upper_price"`
	// Lower price boundary (0 = auto-calculate from ATR)
	LowerPrice float64 `json:"lower_price"`
	// Use ATR to auto-calculate bounds
	UseATRBounds bool `json:"use_atr_bounds"`
	// ATR multiplier for bound calculation (default 2.0)
	ATRMultiplier float64 `json:"atr_multiplier"`
	// Position distribution: "uniform" | "gaussian" | "pyramid"
	Distribution string `json:"distribution"`
	// Maximum drawdown percentage before emergency exit
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	// Stop loss percentage per position
	StopLossPct float64 `json:"stop_loss_pct"`
	// Daily loss limit percentage
	DailyLossLimitPct float64 `json:"daily_loss_limit_pct"`
	// Use maker-only orders for lower fees
	UseMakerOnly bool `json:"use_maker_only"`
	// Enable automatic grid direction adjustment based on box breakouts
	EnableDirectionAdjust bool `json:"enable_direction_adjust"`
	// Direction bias ratio for long_bias/short_bias modes (default 0.7 = 70%/30%)
	DirectionBiasRatio float64 `json:"direction_bias_ratio"`
}

// CoinSourceConfig coin source configuration
type CoinSourceConfig struct {
	// source type: "static" | "ai500" | "oi_top" | "oi_low" | "mixed"
	SourceType string `json:"source_type"`
	// static coin list (used when source_type = "static")
	StaticCoins []string `json:"static_coins,omitempty"`
	// excluded coins list (filtered out from all sources)
	ExcludedCoins []string `json:"excluded_coins,omitempty"`
	// whether to use AI500 coin pool
	UseAI500 bool `json:"use_ai500"`
	// AI500 coin pool maximum count
	AI500Limit int `json:"ai500_limit,omitempty"`
	// whether to use OI Top (OI increase ranking, suitable for long positions)
	UseOITop bool `json:"use_oi_top"`
	// OI Top maximum count
	OITopLimit int `json:"oi_top_limit,omitempty"`
	// whether to use OI Low (OI decrease ranking, suitable for short positions)
	UseOILow bool `json:"use_oi_low"`
	// OI Low maximum count
	OILowLimit int `json:"oi_low_limit,omitempty"`
	// whether to use Hyperliquid All coins (all available perp pairs)
	UseHyperAll bool `json:"use_hyper_all"`
	// whether to use Hyperliquid Main coins (top N by 24h volume)
	UseHyperMain bool `json:"use_hyper_main"`
	// Hyperliquid Main maximum count (default 20)
	HyperMainLimit int `json:"hyper_main_limit,omitempty"`
	// Note: AI500/NofxOS data is billed through the configured Claw402 wallet.
}

// IndicatorConfig indicator configuration
type IndicatorConfig struct {
	// K-line configuration
	Klines KlineConfig `json:"klines"`
	// raw kline data (OHLCV) - always enabled, required for AI analysis
	EnableRawKlines bool `json:"enable_raw_klines"`
	// technical indicator switches
	EnableEMA          bool `json:"enable_ema"`
	EnableSMA          bool `json:"enable_sma"` // Simple Moving Average
	EnableMACD         bool `json:"enable_macd"`
	EnableRSI          bool `json:"enable_rsi"`
	EnableATR          bool `json:"enable_atr"`
	EnableADX          bool `json:"enable_adx"`           // ADX/DMI trend strength
	EnableSAR          bool `json:"enable_sar"`           // Parabolic SAR
	EnableBOLL         bool `json:"enable_boll"`          // Bollinger Bands
	EnableSession      bool `json:"enable_session"`       // Previous session OHLCV
	EnableOpeningRange bool `json:"enable_opening_range"` // Opening Range (first N minutes of session)
	EnableRBreaker     bool `json:"enable_rbreaker"`      // R-Breaker pivot levels
	EnableVolume       bool `json:"enable_volume"`
	EnableOI           bool `json:"enable_oi"`           // open interest
	EnableFundingRate  bool `json:"enable_funding_rate"` // funding rate
	// EMA period configuration
	EMAPeriods []int `json:"ema_periods,omitempty"` // default [20, 50]
	// SMA period configuration
	SMAPeriods []int `json:"sma_periods,omitempty"` // default [5, 20, 50]
	// RSI period configuration
	RSIPeriods []int `json:"rsi_periods,omitempty"` // default [7, 14]
	// ATR period configuration
	ATRPeriods []int `json:"atr_periods,omitempty"` // default [14]
	// ADX period configuration
	ADXPeriod int `json:"adx_period,omitempty"` // default 14
	// BOLL period configuration (period, standard deviation multiplier is fixed at 2)
	BOLLPeriods []int `json:"boll_periods,omitempty"` // default [20] - can select multiple timeframes
	// MACD period configuration
	MACDFastPeriod   int `json:"macd_fast_period,omitempty"`   // default 12
	MACDSlowPeriod   int `json:"macd_slow_period,omitempty"`   // default 26
	MACDSignalPeriod int `json:"macd_signal_period,omitempty"` // default 9
	// Additional K-line derived indicator period configuration.
	VolumePeriods      []int `json:"volume_periods,omitempty"`       // default [20]
	VWAPPeriods        []int `json:"vwap_periods,omitempty"`         // default [20]
	DonchianPeriods    []int `json:"donchian_periods,omitempty"`     // default [20]
	RealizedVolPeriods []int `json:"realized_vol_periods,omitempty"` // default [20]
	PriceChangeWindows []int `json:"price_change_windows,omitempty"` // default [12, 48], bar windows
	// Session configuration (Phase 1: UTC day only)
	Sessions []SessionSpec `json:"sessions,omitempty"`
	// Opening Range configuration (shares session definition with SessionModule)
	OpeningRangeMinutes int `json:"opening_range_minutes,omitempty"` // default 30
	// external data sources
	ExternalDataSources []ExternalDataSource `json:"external_data_sources,omitempty"`

	// ========== NofxOS Legacy Compatibility ==========
	// Legacy field retained for saved strategy JSON compatibility; runtime data requests use Claw402 wallet billing.
	NofxOSAPIKey string `json:"nofxos_api_key,omitempty"`

	// quantitative data sources (capital flow, position changes, price changes)
	EnableQuantData    bool `json:"enable_quant_data"`    // whether to enable quantitative data
	EnableQuantOI      bool `json:"enable_quant_oi"`      // whether to show OI data
	EnableQuantNetflow bool `json:"enable_quant_netflow"` // whether to show Netflow data

	// OI ranking data (market-wide open interest increase/decrease rankings)
	EnableOIRanking   bool   `json:"enable_oi_ranking"`             // whether to enable OI ranking data
	OIRankingDuration string `json:"oi_ranking_duration,omitempty"` // duration: 1h, 4h, 24h
	OIRankingLimit    int    `json:"oi_ranking_limit,omitempty"`    // number of entries (default 10)

	// NetFlow ranking data (market-wide fund flow rankings - institution/personal)
	EnableNetFlowRanking   bool   `json:"enable_netflow_ranking"`             // whether to enable NetFlow ranking data
	NetFlowRankingDuration string `json:"netflow_ranking_duration,omitempty"` // duration: 1h, 4h, 24h
	NetFlowRankingLimit    int    `json:"netflow_ranking_limit,omitempty"`    // number of entries (default 10)

	// Price ranking data (market-wide gainers/losers)
	EnablePriceRanking   bool   `json:"enable_price_ranking"`             // whether to enable price ranking data
	PriceRankingDuration string `json:"price_ranking_duration,omitempty"` // durations: "1h" or "1h,4h,24h"
	PriceRankingLimit    int    `json:"price_ranking_limit,omitempty"`    // number of entries per ranking (default 10)
}

// KlineConfig K-line configuration
type KlineConfig struct {
	// market data source used for OHLCV/K-line calculations. It is intentionally
	// separate from the execution exchange so unstable K-line providers can be
	// replaced without changing where orders are sent.
	MarketDataSource string `json:"market_data_source,omitempty"`
	// primary timeframe: "1m", "3m", "5m", "15m", "1h", "4h"
	PrimaryTimeframe string `json:"primary_timeframe"`
	// primary timeframe K-line count
	PrimaryCount int `json:"primary_count"`
	// K-line count reserved for deterministic calculations. This can be
	// higher than the prompt display count so structure detection has enough
	// history without expanding the AI prompt.
	ComputeLookback int `json:"compute_lookback,omitempty"`
	// Number of raw K-lines exposed to the AI prompt. Indicator and structure
	// calculations should use ComputeLookback instead.
	PromptDisplayCount int `json:"prompt_display_count,omitempty"`
	// Whether live calculations may include the currently forming candle.
	// Replay/backtest paths should use closed candles only.
	IncludeOpenBar bool `json:"include_open_bar,omitempty"`
	// longer timeframe
	LongerTimeframe string `json:"longer_timeframe,omitempty"`
	// longer timeframe K-line count
	LongerCount int `json:"longer_count,omitempty"`
	// entry timeframe: lower timeframe used for the final trigger.
	EntryTimeframe string `json:"entry_timeframe,omitempty"`
	// confirmation timeframes: higher/peer timeframes used as directional filters.
	ConfirmationTimeframes []string `json:"confirmation_timeframes,omitempty"`
	// whether to enable multi-timeframe analysis
	EnableMultiTimeframe bool `json:"enable_multi_timeframe"`
	// selected timeframe list (new: supports multi-timeframe selection)
	SelectedTimeframes []string `json:"selected_timeframes,omitempty"`
}

// ExternalDataSource external data source configuration
type ExternalDataSource struct {
	Name         string            `json:"name"`   // data source name
	Type         string            `json:"type"`   // type: "api" | "webhook"
	URL          string            `json:"url"`    // API URL
	Method       string            `json:"method"` // HTTP method
	Headers      map[string]string `json:"headers,omitempty"`
	DataPath     string            `json:"data_path,omitempty"`     // JSON data path
	RefreshSecs  int               `json:"refresh_secs,omitempty"`  // refresh interval (seconds)
	Description  string            `json:"description,omitempty"`   // AI interpretation hint
	ContextLabel string            `json:"context_label,omitempty"` // display title in prompt; defaults to Name
}

// SessionSpec defines a trading session boundary for Previous Session OHLCV.
type SessionSpec struct {
	Timezone string `json:"timezone"` // IANA timezone name, e.g. "UTC"
	Offset   string `json:"offset"`   // Session start time, e.g. "00:00"
	Duration int    `json:"duration"` // Session length in minutes, default 1440 (24h)
}

// RiskControlConfig risk control configuration
type RiskControlConfig struct {
	// Max number of coins held simultaneously (CODE ENFORCED)
	MaxPositions int `json:"max_positions"`

	// BTC/ETH exchange leverage for opening positions (AI guided)
	BTCETHMaxLeverage int `json:"btc_eth_max_leverage"`
	// Altcoin exchange leverage for opening positions (AI guided)
	AltcoinMaxLeverage int `json:"altcoin_max_leverage"`

	// BTC/ETH single position max value = equity 脳 this ratio (CODE ENFORCED, default: 5)
	BTCETHMaxPositionValueRatio float64 `json:"btc_eth_max_position_value_ratio"`
	// Altcoin single position max value = equity 脳 this ratio (CODE ENFORCED, default: 1)
	AltcoinMaxPositionValueRatio float64 `json:"altcoin_max_position_value_ratio"`

	// Max margin utilization (e.g. 0.9 = 90%) (CODE ENFORCED)
	MaxMarginUsage float64 `json:"max_margin_usage"`
	// Risk budget for one new position. Effective notional is derived by code:
	// equity * risk_per_trade_pct / stop-distance-ratio, then capped by max
	// position value and available margin. AI must not freely choose this size.
	RiskPerTradePct float64 `json:"risk_per_trade_pct"`
	// Min position size in USDT (CODE ENFORCED)
	MinPositionSize float64 `json:"min_position_size"`

	// Min take_profit / stop_loss ratio (AI guided)
	MinRiskRewardRatio float64 `json:"min_risk_reward_ratio"`
	// Min AI confidence to open position (AI guided)
	MinConfidence int `json:"min_confidence"`
	// Min AI confidence to proactively close before exchange SL/TP triggers (AI guided)
	MinCloseConfidence int `json:"min_close_confidence"`

	// Stop loss ATR buffer multiplier (AI guided)
	// Stop loss = support - (ATR14 脳 this value) for longs, resistance + (ATR14 脳 this value) for shorts
	// 0 means use mode default: Conservative=1.5, Balanced=1.0, Aggressive=0.5, Scalping=0.3
	StopLossATRBuffer float64 `json:"stop_loss_atr_buffer"`

	// 鈹€鈹€ Drawdown-based position close (risk monitor, runs every minute) 鈹€鈹€鈹€鈹€鈹€鈹€
	// Whether the drawdown-close mechanism is enabled. Default: true.
	DrawdownCloseEnabled bool `json:"drawdown_close_enabled"`
	// Min unrealised leveraged profit (%) before drawdown is measured. Default: 5.0.
	// Example: 5.0 means the mechanism only activates once the position is 鈮?% in profit.
	DrawdownCloseMinProfitPct float64 `json:"drawdown_close_min_profit_pct"`
	// Drawdown threshold (%) relative to peak profit that triggers the close. Default: 40.0.
	// Example: 40.0 means: if profit dropped from peak by 鈮?0%, close the position.
	DrawdownCloseTriggerPct float64 `json:"drawdown_close_trigger_pct"`
	// When true, instead of closing immediately the system injects a "drawdown alert"
	// into the next AI cycle so the AI decides whether to close. Default: false (close immediately).
	DrawdownCloseUseAI bool `json:"drawdown_close_use_ai"`
}

// NewStrategyStore creates a new StrategyStore
func NewStrategyStore(db *gorm.DB) *StrategyStore {
	return &StrategyStore{db: db}
}

func (s *StrategyStore) initTables() error {
	// AutoMigrate will add missing columns without dropping existing data
	return s.db.AutoMigrate(&Strategy{})
}

func (s *StrategyStore) initDefaultData() error {
	// No longer pre-populate strategies - create on demand when user configures
	return nil
}

// GetDefaultStrategyConfig returns the default strategy configuration for the given language
func GetDefaultStrategyConfig(lang string) StrategyConfig {
	// Normalize language to "zh" or "en"
	normalizedLang := "en"
	if lang == "zh" {
		normalizedLang = "zh"
	}

	config := StrategyConfig{
		StrategyType: "ai_trading",
		StrategyMode: "rule",
		Language:     normalizedLang,
		CoinSource: CoinSourceConfig{
			SourceType: "ai500",
			UseAI500:   true,
			AI500Limit: 3,
			UseOITop:   false,
			OITopLimit: 3,
			UseOILow:   false,
			OILowLimit: 3,
		},
		IncludeHistoricalContext: boolPtr(true),
		Indicators: IndicatorConfig{
			Klines: KlineConfig{
				MarketDataSource:   "binance",
				PrimaryTimeframe:   "15m",
				PrimaryCount:       20,
				ComputeLookback:    300,
				PromptDisplayCount: 20,
				IncludeOpenBar:     true,
				LongerTimeframe:    "4h",
				LongerCount:        10,
				EntryTimeframe:     "5m",
				ConfirmationTimeframes: []string{
					"1h",
				},
				EnableMultiTimeframe: true,
				SelectedTimeframes:   []string{"5m", "15m", "1h"},
			},
			EnableRawKlines:        true, // Required - raw OHLCV data for AI analysis
			EnableEMA:              true, // Core trend indicator
			EnableSMA:              false,
			EnableMACD:             false,
			EnableRSI:              false,
			EnableATR:              true, // Stop-loss sizing
			EnableADX:              true, // Trend strength confirmation
			EnableSAR:              false,
			EnableBOLL:             false,
			EnableSession:          false,
			EnableOpeningRange:     false,
			OpeningRangeMinutes:    30,
			EnableRBreaker:         false,
			EnableVolume:           true,
			EnableOI:               true,
			EnableFundingRate:      true,
			EMAPeriods:             []int{20, 50},
			SMAPeriods:             []int{5, 20, 50},
			RSIPeriods:             []int{7, 14},
			ATRPeriods:             []int{14},
			ADXPeriod:              14,
			BOLLPeriods:            []int{20},
			MACDFastPeriod:         12,
			MACDSlowPeriod:         26,
			MACDSignalPeriod:       9,
			VolumePeriods:          []int{20},
			VWAPPeriods:            []int{20},
			DonchianPeriods:        []int{20},
			RealizedVolPeriods:     []int{20},
			PriceChangeWindows:     []int{12, 48},
			NofxOSAPIKey:           "",
			EnableQuantData:        false,
			EnableQuantOI:          false,
			EnableQuantNetflow:     false,
			EnableOIRanking:        true,
			OIRankingDuration:      "1h",
			OIRankingLimit:         10,
			EnableNetFlowRanking:   true,
			NetFlowRankingDuration: "1h",
			NetFlowRankingLimit:    10,
			EnablePriceRanking:     true,
			PriceRankingDuration:   "1h,4h,24h",
			PriceRankingLimit:      10,
		},
		Structure: defaultStructureFactorConfig(),
		RiskControl: RiskControlConfig{
			MaxPositions:                 3,
			BTCETHMaxLeverage:            5,
			AltcoinMaxLeverage:           5,
			BTCETHMaxPositionValueRatio:  5.0,
			AltcoinMaxPositionValueRatio: 1.0,
			MaxMarginUsage:               0.9,
			RiskPerTradePct:              DefaultRiskPerTradePct,
			MinPositionSize:              DefaultMinPositionSize,
			MinRiskRewardRatio:           DefaultMinRiskRewardRatio, // Min 2.5:1 profit/loss ratio (AI guided) - adjusted for 5m/15m multi-TF
			MinConfidence:                DefaultMinConfidence,
			MinCloseConfidence:           75, // Lowered from 85 to allow more flexible exits
		},
	}
	config.ClampLimits()
	return config
}

func defaultStructureFactorConfig() StructureFactorConfig {
	return StructureFactorConfig{
		EnableFibonacci:         true,
		EnableSupportResistance: true,
		Fibonacci: StructureFibonacciConfig{
			Lookback:              120,
			SwingWindow:           3,
			MinLegBars:            8,
			MinLegATRMultiple:     3,
			ZigZagThresholdPct:    2,
			Levels:                []float64{0.236, 0.382, 0.5, 0.618, 0.786},
			InvalidateOnBreakBase: true,
		},
		SupportResistance: StructureSupportResistanceConfig{
			Lookback:        120,
			SwingWindow:     3,
			ZoneWidthATR:    0.5,
			MinTouches:      2,
			MinDistanceBars: 5,
		},
	}
}

// GetDefaultGridStrategyConfig returns backend-owned defaults for grid strategies.
func GetDefaultGridStrategyConfig() *GridStrategyConfig {
	return &GridStrategyConfig{
		Symbol:                "BTCUSDT",
		GridCount:             10,
		TotalInvestment:       1000,
		Leverage:              5,
		UpperPrice:            0,
		LowerPrice:            0,
		UseATRBounds:          true,
		ATRMultiplier:         2.0,
		Distribution:          "gaussian",
		MaxDrawdownPct:        15,
		StopLossPct:           5,
		DailyLossLimitPct:     10,
		UseMakerOnly:          true,
		EnableDirectionAdjust: false,
		DirectionBiasRatio:    0.7,
	}
}

func boolPtr(v bool) *bool {
	return &v
}

// ParseStrategyConfigWithDefaults overlays a partial JSON config onto backend
// defaults. Missing fields inherit defaults, while fields explicitly provided by
// callers, including false/0 values, are preserved.
func ParseStrategyConfigWithDefaults(raw []byte, fallbackLang string) (*StrategyConfig, error) {
	override := map[string]interface{}{}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal(raw, &override); err != nil {
			return nil, fmt.Errorf("failed to parse strategy configuration: %w", err)
		}
	}

	lang := fallbackLang
	if rawLang, ok := override["language"].(string); ok && rawLang != "" {
		lang = rawLang
	}
	if lang != "zh" {
		lang = "en"
	}

	strategyType := "ai_trading"
	if rawType, ok := override["strategy_type"].(string); ok && rawType != "" {
		strategyType = rawType
	}

	defaultConfig := GetDefaultStrategyConfig(lang)
	defaultConfig.StrategyType = strategyType
	if strategyType == "grid_trading" {
		defaultConfig.GridConfig = GetDefaultGridStrategyConfig()
	}

	defaultJSON, err := json.Marshal(defaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize default strategy configuration: %w", err)
	}

	defaultMap := map[string]interface{}{}
	if err := json.Unmarshal(defaultJSON, &defaultMap); err != nil {
		return nil, fmt.Errorf("failed to parse default strategy configuration: %w", err)
	}

	merged := mergeConfigMaps(defaultMap, override)
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize merged strategy configuration: %w", err)
	}

	var config StrategyConfig
	if err := json.Unmarshal(mergedJSON, &config); err != nil {
		return nil, fmt.Errorf("failed to parse merged strategy configuration: %w", err)
	}
	if config.StrategyType == "" {
		config.StrategyType = "ai_trading"
	}
	if config.StrategyType == "grid_trading" && config.GridConfig == nil {
		config.GridConfig = GetDefaultGridStrategyConfig()
	}
	config.ClampLimits()
	return &config, nil
}

func mergeConfigMaps(defaults, overrides map[string]interface{}) map[string]interface{} {
	for key, overrideValue := range overrides {
		overrideMap, overrideIsMap := overrideValue.(map[string]interface{})
		defaultMap, defaultIsMap := defaults[key].(map[string]interface{})
		if overrideIsMap && defaultIsMap {
			defaults[key] = mergeConfigMaps(defaultMap, overrideMap)
			continue
		}
		defaults[key] = overrideValue
	}
	return defaults
}

// ShouldIncludeHistoricalContext returns whether historical closed-trade context
// should be provided to AI. Default is true for backward compatibility.
func (c *StrategyConfig) ShouldIncludeHistoricalContext() bool {
	if c == nil || c.IncludeHistoricalContext == nil {
		return true
	}
	return *c.IncludeHistoricalContext
}

// Create create a strategy
func (s *StrategyStore) Create(strategy *Strategy) error {
	return s.db.Create(strategy).Error
}

// Update update a strategy
func (s *StrategyStore) Update(strategy *Strategy) error {
	return s.db.Model(&Strategy{}).
		Where("id = ? AND user_id = ?", strategy.ID, strategy.UserID).
		Updates(map[string]interface{}{
			"name":           strategy.Name,
			"description":    strategy.Description,
			"config":         strategy.Config,
			"is_public":      strategy.IsPublic,
			"config_visible": strategy.ConfigVisible,
			"updated_at":     time.Now().UTC(),
		}).Error
}

// Delete delete a strategy
func (s *StrategyStore) Delete(userID, id string) error {
	// do not allow deleting system default strategy
	var st Strategy
	if err := s.db.Where("id = ?", id).First(&st).Error; err == nil {
		if st.IsDefault {
			return fmt.Errorf("cannot delete system default strategy")
		}
	}

	// Check if any trader references this strategy
	var count int64
	if err := s.db.Model(&Trader{}).
		Where("user_id = ? AND strategy_id = ?", userID, id).
		Count(&count).Error; err == nil && count > 0 {
		return fmt.Errorf("cannot delete strategy in use by %d trader(s) - reassign those traders first", count)
	}

	return s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&Strategy{}).Error
}

// List get user's strategy list
func (s *StrategyStore) List(userID string) ([]*Strategy, error) {
	var strategies []*Strategy
	err := s.db.Where("user_id = ? OR is_default = ?", userID, true).
		Order("is_default DESC, created_at DESC").
		Find(&strategies).Error
	if err != nil {
		return nil, err
	}
	return strategies, nil
}

// ListPublic get all public strategies for the strategy market
func (s *StrategyStore) ListPublic() ([]*Strategy, error) {
	var strategies []*Strategy
	err := s.db.Where("is_public = ?", true).
		Order("created_at DESC").
		Find(&strategies).Error
	if err != nil {
		return nil, err
	}
	return strategies, nil
}

// Get get a single strategy
func (s *StrategyStore) Get(userID, id string) (*Strategy, error) {
	var st Strategy
	err := s.db.Where("id = ? AND (user_id = ? OR is_default = ?)", id, userID, true).
		First(&st).Error
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetActive get user's currently active strategy
func (s *StrategyStore) GetActive(userID string) (*Strategy, error) {
	var st Strategy
	err := s.db.Where("user_id = ? AND is_active = ?", userID, true).First(&st).Error
	if err == gorm.ErrRecordNotFound {
		// no active strategy, return system default strategy
		return s.GetDefault()
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// GetDefault get system default strategy
func (s *StrategyStore) GetDefault() (*Strategy, error) {
	var st Strategy
	err := s.db.Where("is_default = ?", true).First(&st).Error
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// SetActive set active strategy (will first deactivate other strategies)
func (s *StrategyStore) SetActive(userID, strategyID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// first deactivate all strategies for the user
		if err := tx.Model(&Strategy{}).Where("user_id = ?", userID).
			Update("is_active", false).Error; err != nil {
			return err
		}

		// activate specified strategy
		return tx.Model(&Strategy{}).
			Where("id = ? AND (user_id = ? OR is_default = ?)", strategyID, userID, true).
			Update("is_active", true).Error
	})
}

// Duplicate duplicate a strategy (used to create custom strategy based on default strategy)
func (s *StrategyStore) Duplicate(userID, sourceID, newID, newName string) error {
	// get source strategy
	source, err := s.Get(userID, sourceID)
	if err != nil {
		return fmt.Errorf("failed to get source strategy: %w", err)
	}

	// create new strategy
	newStrategy := &Strategy{
		ID:          newID,
		UserID:      userID,
		Name:        newName,
		Description: "Created based on [" + source.Name + "]",
		IsActive:    false,
		IsDefault:   false,
		Config:      source.Config,
	}

	return s.Create(newStrategy)
}

// ParseConfig parse strategy configuration JSON
func (s *Strategy) ParseConfig() (*StrategyConfig, error) {
	return ParseStrategyConfigWithDefaults([]byte(s.Config), "")
}

// SetConfig set strategy configuration
func (s *Strategy) SetConfig(config *StrategyConfig) error {
	if config != nil {
		config.ClampLimits()
	}
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize strategy configuration: %w", err)
	}
	s.Config = string(data)
	return nil
}

// ============================================================================
// Token Estimation
// ============================================================================

// TokenEstimate holds the result of token estimation
type TokenEstimate struct {
	Total       int            `json:"total"`
	Breakdown   TokenBreakdown `json:"breakdown"`
	ModelLimits []ModelLimit   `json:"model_limits"`
	Suggestions []string       `json:"suggestions"`
}

// TokenBreakdown shows estimated tokens per component
type TokenBreakdown struct {
	SystemPrompt  int `json:"system_prompt"`
	MarketData    int `json:"market_data"`
	RankingData   int `json:"ranking_data"`
	QuantData     int `json:"quant_data"`
	FixedOverhead int `json:"fixed_overhead"`
}

// ModelLimit shows token usage against a specific model's context limit
type ModelLimit struct {
	Name         string `json:"name"`
	ContextLimit int    `json:"context_limit"`
	UsagePct     int    `json:"usage_pct"`
	Level        string `json:"level"` // "ok" | "warning" | "danger"
}

// Context window sizes (tokens) for each model family
const (
	contextLimitDeepSeek = 131_072   // 128K
	contextLimitOpenAI   = 128_000   // 128K
	contextLimitClaude   = 200_000   // 200K
	contextLimitQwen     = 131_072   // 128K
	contextLimitGemini   = 1_000_000 // 1M
	contextLimitGrok     = 131_072   // 128K
	contextLimitKimi     = 131_072   // 128K
	contextLimitMinimax  = 1_000_000 // 1M
)

// ModelContextLimits maps provider names to their context window sizes (in tokens)
var ModelContextLimits = map[string]int{
	"deepseek": contextLimitDeepSeek,
	"openai":   contextLimitOpenAI,
	"claude":   contextLimitClaude,
	"qwen":     contextLimitQwen,
	"gemini":   contextLimitGemini,
	"grok":     contextLimitGrok,
	"kimi":     contextLimitKimi,
	"minimax":  contextLimitMinimax,
}

// GetContextLimit returns the context limit for a given provider
func GetContextLimit(provider string) int {
	if limit, ok := ModelContextLimits[provider]; ok {
		return limit
	}
	return contextLimitDeepSeek // safe default
}

// GetContextLimitForClient returns context limit for a provider+model pair.
// For claw402, the underlying model is inferred from the model name prefix.
func GetContextLimitForClient(provider, model string) int {
	if provider == "claw402" {
		switch {
		case strings.HasPrefix(model, "claude"):
			return ModelContextLimits["claude"]
		case strings.HasPrefix(model, "gpt"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"):
			return ModelContextLimits["openai"]
		case strings.HasPrefix(model, "gemini"):
			return ModelContextLimits["gemini"]
		case strings.HasPrefix(model, "grok"):
			return ModelContextLimits["grok"]
		case strings.HasPrefix(model, "kimi"):
			return ModelContextLimits["kimi"]
		case strings.HasPrefix(model, "qwen"):
			return ModelContextLimits["qwen"]
		case strings.HasPrefix(model, "minimax"):
			return ModelContextLimits["minimax"]
		case strings.HasPrefix(model, "deepseek"):
			return ModelContextLimits["deepseek"]
		default:
			return ModelContextLimits["deepseek"]
		}
	}
	return GetContextLimit(provider)
}

// EstimateTokens estimates the total token count for a strategy configuration.
// This is a pure computation based on config fields, no network calls.
func (c *StrategyConfig) EstimateTokens() TokenEstimate {
	breakdown := TokenBreakdown{}

	// --- LLM review instructions ---
	// The new flow sends a compact review prompt plus structured signals/factors.
	breakdown.SystemPrompt = 1000

	// --- Fixed Overhead ---
	// Time, BTC price, account info, section headers
	breakdown.FixedOverhead = 800 / 4 // ~200 tokens

	// --- Market Data ---
	numCoins := c.getEffectiveCoinCount()
	numTimeframes := c.getEffectiveTimeframeCount()
	klineCount := c.Indicators.Klines.PromptDisplayCount
	if klineCount <= 0 {
		klineCount = c.Indicators.Klines.PrimaryCount
	}
	if klineCount <= 0 {
		klineCount = 20
	}

	// Per coin per timeframe: kline OHLCV rows
	charsPerCoinTF := klineCount * 80 // each OHLCV line ~80 chars

	// Add enabled indicator overhead per timeframe
	indicatorCharsPerLine := 0
	if c.Indicators.EnableEMA {
		indicatorCharsPerLine += 20 // EMA values appended
	}
	if c.Indicators.EnableMACD {
		indicatorCharsPerLine += 30
	}
	if c.Indicators.EnableRSI {
		indicatorCharsPerLine += 15
	}
	if c.Indicators.EnableATR {
		indicatorCharsPerLine += 15
	}
	if c.Indicators.EnableADX {
		indicatorCharsPerLine += 30 // ADX + +DI + -DI + direction + trending + strength
	}
	if c.Indicators.EnableSAR {
		indicatorCharsPerLine += 25 // SAR + direction + flip signals
	}
	if c.Indicators.EnableBOLL {
		indicatorCharsPerLine += 25
	}
	if c.Indicators.EnableSession {
		indicatorCharsPerLine += 50 // session OHLCV + prev session + breakout signals
	}
	if c.Indicators.EnableVolume {
		indicatorCharsPerLine += 10
	}
	charsPerCoinTF += klineCount * indicatorCharsPerLine

	totalMarketChars := numCoins * numTimeframes * charsPerCoinTF

	// OI + Funding per coin
	if c.Indicators.EnableOI || c.Indicators.EnableFundingRate {
		totalMarketChars += numCoins * 100
	}

	breakdown.MarketData = totalMarketChars / 4 // numeric data: ~4 chars per token

	// --- Quant Data ---
	if c.Indicators.EnableQuantData {
		quantCharsPerCoin := 0
		if c.Indicators.EnableQuantOI {
			quantCharsPerCoin += 300
		}
		if c.Indicators.EnableQuantNetflow {
			quantCharsPerCoin += 300
		}
		breakdown.QuantData = (numCoins * quantCharsPerCoin) / 4
	}

	// --- Ranking Data ---
	rankingChars := 0
	if c.Indicators.EnableOIRanking {
		limit := c.Indicators.OIRankingLimit
		if limit <= 0 {
			limit = 10
		}
		rankingChars += limit * 60
	}
	if c.Indicators.EnableNetFlowRanking {
		limit := c.Indicators.NetFlowRankingLimit
		if limit <= 0 {
			limit = 10
		}
		rankingChars += limit * 80
	}
	if c.Indicators.EnablePriceRanking {
		limit := c.Indicators.PriceRankingLimit
		if limit <= 0 {
			limit = 10
		}
		// Count durations (comma-separated)
		numDurations := 1
		if c.Indicators.PriceRankingDuration != "" {
			numDurations = len(strings.Split(c.Indicators.PriceRankingDuration, ","))
		}
		rankingChars += limit * numDurations * 40
	}
	breakdown.RankingData = rankingChars / 4

	// --- Total with 15% safety margin ---
	subtotal := breakdown.SystemPrompt + breakdown.MarketData + breakdown.RankingData + breakdown.QuantData + breakdown.FixedOverhead
	total := subtotal * 115 / 100

	// --- Model limits ---
	modelLimits := make([]ModelLimit, 0, len(ModelContextLimits))
	for name, limit := range ModelContextLimits {
		pct := total * 100 / limit
		level := "ok"
		if pct >= 100 {
			level = "danger"
		} else if pct >= 80 {
			level = "warning"
		}
		modelLimits = append(modelLimits, ModelLimit{
			Name:         name,
			ContextLimit: limit,
			UsagePct:     pct,
			Level:        level,
		})
	}

	// Sort by usage_pct desc, then name asc for deterministic order
	sort.Slice(modelLimits, func(i, j int) bool {
		if modelLimits[i].UsagePct != modelLimits[j].UsagePct {
			return modelLimits[i].UsagePct > modelLimits[j].UsagePct
		}
		return modelLimits[i].Name < modelLimits[j].Name
	})

	// --- Suggestions ---
	var suggestions []string
	// Find the strictest model (smallest context)
	minLimit := 0
	for _, limit := range ModelContextLimits {
		if minLimit == 0 || limit < minLimit {
			minLimit = limit
		}
	}
	if minLimit > 0 && total > minLimit {
		if numTimeframes > 1 {
			savedPerTF := (numCoins * klineCount * (80 + indicatorCharsPerLine)) / 4 * 115 / 100
			suggestions = append(suggestions, fmt.Sprintf("Reduce 1 timeframe to save ~%d tokens", savedPerTF))
		}
		if numCoins > 1 {
			savedPerCoin := (numTimeframes * klineCount * (80 + indicatorCharsPerLine)) / 4 * 115 / 100
			suggestions = append(suggestions, fmt.Sprintf("Reduce 1 coin to save ~%d tokens", savedPerCoin))
		}
		if klineCount > 15 {
			suggestions = append(suggestions, "Reduce K-line count to 15 to save tokens")
		}
	}

	return TokenEstimate{
		Total:       total,
		Breakdown:   breakdown,
		ModelLimits: modelLimits,
		Suggestions: suggestions,
	}
}

// getEffectiveCoinCount returns the estimated number of coins that will be analyzed
func (c *StrategyConfig) getEffectiveCoinCount() int {
	count := 0
	switch c.CoinSource.SourceType {
	case "static":
		count = len(c.CoinSource.StaticCoins)
	case "ai500":
		count = c.CoinSource.AI500Limit
	case "oi_top":
		count = c.CoinSource.OITopLimit
	case "oi_low":
		count = c.CoinSource.OILowLimit
	case "mixed":
		if c.CoinSource.UseAI500 {
			count += c.CoinSource.AI500Limit
		}
		if c.CoinSource.UseOITop {
			count += c.CoinSource.OITopLimit
		}
		if c.CoinSource.UseOILow {
			count += c.CoinSource.OILowLimit
		}
	default:
		count = c.CoinSource.AI500Limit
	}
	if count <= 0 {
		count = 3
	}
	return count
}

// getEffectiveTimeframeCount returns the number of timeframes that will be used
func (c *StrategyConfig) getEffectiveTimeframeCount() int {
	if len(c.Indicators.Klines.SelectedTimeframes) > 0 {
		return len(c.Indicators.Klines.SelectedTimeframes)
	}
	count := 1
	if c.Indicators.Klines.LongerTimeframe != "" {
		count++
	}
	return count
}
