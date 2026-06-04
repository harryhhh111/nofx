package kernel

import (
	"context"
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/store"
	"regexp"
	"strings"
	"time"
)

// ============================================================================
// Pre-compiled regular expressions (performance optimization)
// ============================================================================

var (
	reJSONFence = regexp.MustCompile(`(?is)` + "```json\\s*(\\[\\s*\\{.*?\\}\\s*\\])\\s*```")
	reJSONArray = regexp.MustCompile(`(?is)\[\s*\{.*?\}\s*\]`)

	// XML tag extraction (supports any characters in reasoning chain)
	reReasoningTag = regexp.MustCompile(`(?s)<reasoning>(.*?)</reasoning>`)
)

// ============================================================================
// Entry Functions - Main API
// ============================================================================

// GetFullDecision gets AI's complete trading decision (batch analysis of all coins and positions)
// Uses default strategy configuration - for production use GetFullDecisionWithStrategy with explicit config
func GetFullDecision(ctx *Context, mcpClient mcp.AIClient) (*FullDecision, error) {
	defaultConfig := store.GetDefaultStrategyConfig("en")
	engine := NewStrategyEngine(&defaultConfig)
	return GetFullDecisionWithStrategy(ctx, mcpClient, engine, "")
}

// GetFullDecisionWithStrategy uses StrategyEngine to get AI decision (unified prompt generation)
func GetFullDecisionWithStrategy(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, variant string) (*FullDecision, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if engine == nil {
		defaultConfig := store.GetDefaultStrategyConfig("en")
		engine = NewStrategyEngine(&defaultConfig)
	}

	// Clamp strategy limits to prevent token overflow
	engineConfig := engine.GetConfig()
	engineConfig.ClampLimits()

	// Token estimation check: block if exceeding the specific model's context limit
	estimate := engineConfig.EstimateTokens()

	// Determine context limit for the specific model being used
	contextLimit := 131072 // safe default (strictest common limit)
	var providerName string
	if embedder, ok := mcpClient.(mcp.ClientEmbedder); ok {
		base := embedder.BaseClient()
		providerName = base.Provider
		contextLimit = store.GetContextLimitForClient(base.Provider, base.Model)
	}

	if estimate.Total > contextLimit {
		logger.Errorf("Token estimate %d exceeds %s context limit %d; blocking analysis",
			estimate.Total, providerName, contextLimit)
		return nil, fmt.Errorf("estimated %d tokens exceeds model context limit of %d; reduce coins, timeframes, or K-line count",
			estimate.Total, contextLimit)
	}
	if estimate.Total*100/contextLimit >= 80 {
		logger.Infof("Token estimate %d approaching %s context limit %d",
			estimate.Total, providerName, contextLimit)
	}

	// 1. Fetch market data using strategy config
	if len(ctx.MarketDataMap) == 0 {
		if err := fetchMarketDataWithStrategy(ctx, engine); err != nil {
			return nil, fmt.Errorf("failed to fetch market data: %w", err)
		}
	}

	// Ensure OITopDataMap is initialized
	if ctx.OITopDataMap == nil {
		ctx.OITopDataMap = make(map[string]*OITopData)
		oiPositions, err := engine.nofxosClient.GetOITopPositions()
		if err == nil {
			for _, pos := range oiPositions {
				ctx.OITopDataMap[pos.Symbol] = &OITopData{
					Rank:              pos.Rank,
					OIDeltaPercent:    pos.OIDeltaPercent,
					OIDeltaValue:      pos.OIDeltaValue,
					PriceDeltaPercent: pos.PriceDeltaPercent,
				}
			}
		}
	}

	riskConfig := engine.GetRiskControlConfig()
	factorSnapshots, err := buildFactorSnapshots(ctx, engineConfig)
	if err != nil {
		return nil, err
	}
	rules := rulesFromStrategyConfig(engine.GetConfig())
	scoring := scoringFromStrategyConfig(engine.GetConfig())
	if (engineConfig.StrategyMode == "scoring" || engineConfig.StrategyMode == "hybrid") && scoring == nil {
		return nil, fmt.Errorf("strategy_mode %s requires enabled scoring_config", engineConfig.StrategyMode)
	}
	marketPrices, minSLDistances := buildMarketValidationMaps(ctx, riskConfig)

	tradingEngine := NewTradingEngine(
		signalEngineFromStrategyConfig(engineConfig),
		NewLLMTradingEngine(mcpClient),
		NewDefaultRiskGate(
			riskConfig.BTCETHMaxLeverage,
			riskConfig.AltcoinMaxLeverage,
			riskConfig.BTCETHMaxPositionValueRatio,
			riskConfig.AltcoinMaxPositionValueRatio,
			riskConfig.MinRiskRewardRatio,
			marketPrices,
			minSLDistances,
		),
		ctx.TradeMemory,
	)

	aiCallStart := time.Now()
	result, err := tradingEngine.Evaluate(context.Background(), TradingEngineRequest{
		SignalRequest: SignalRequest{
			Account:        ctx.Account,
			Positions:      ctx.Positions,
			Candidates:     ctx.CandidateCoins,
			Rules:          rules,
			Scoring:        scoring,
			FactorSnapshot: factorSnapshots,
			Now:            time.Now().UTC(),
		},
	})
	aiCallDuration := time.Since(aiCallStart)
	if err != nil {
		return nil, err
	}

	decision := &FullDecision{
		Timestamp:           time.Now(),
		AIRequestDurationMs: aiCallDuration.Milliseconds(),
		Decisions:           decisionsFromTradingResult(result),
		CoTSummary:          tradingResultSummary(result, len(rules)),
		SystemPrompt:        buildLLMReviewSystemPrompt(),
		MarketContext:       result.MarketContext,
	}
	if userPrompt, promptErr := buildLLMReviewUserPrompt(AIReviewRequest{
		Signals:          result.Signals,
		FactorSnapshot:   factorSnapshots,
		MarketContext:    result.MarketContext,
		RelevantMemory:   result.Memory,
		CurrentPositions: ctx.Positions,
	}); promptErr == nil {
		decision.UserPrompt = userPrompt
	}
	return decision, nil
}

// ============================================================================
// Market Data Fetching
// ============================================================================

// fetchMarketDataWithStrategy fetches market data using strategy config (multiple timeframes)
func fetchMarketDataWithStrategy(ctx *Context, engine *StrategyEngine) error {
	config := engine.GetConfig()
	ctx.MarketDataMap = make(map[string]*market.Data)

	timeframes := config.Indicators.Klines.SelectedTimeframes
	primaryTimeframe := config.Indicators.Klines.PrimaryTimeframe
	displayCount := config.Indicators.Klines.PromptDisplayCount
	if displayCount <= 0 {
		displayCount = config.Indicators.Klines.PrimaryCount
	}
	computeLookback := config.Indicators.Klines.ComputeLookback

	// Compatible with old configuration
	if len(timeframes) == 0 {
		if primaryTimeframe != "" {
			timeframes = append(timeframes, primaryTimeframe)
		} else {
			timeframes = append(timeframes, "3m")
		}
		if config.Indicators.Klines.LongerTimeframe != "" {
			timeframes = append(timeframes, config.Indicators.Klines.LongerTimeframe)
		}
	}
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}
	if displayCount <= 0 {
		displayCount = 30
	}
	if computeLookback < displayCount {
		computeLookback = displayCount
	}

	logger.Infof("Strategy timeframes: %v, Primary: %s, display: %d, compute: %d", timeframes, primaryTimeframe, displayCount, computeLookback)

	// 1. First fetch data for position coins (must fetch)
	for _, pos := range ctx.Positions {
		data, err := market.GetWithTimeframesWindow(pos.Symbol, timeframes, primaryTimeframe, displayCount, computeLookback)
		if err != nil {
			logger.Infof("Failed to fetch market data for position %s: %v", pos.Symbol, err)
			continue
		}
		ctx.MarketDataMap[pos.Symbol] = data
	}

	// 2. Fetch data for all candidate coins
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		positionSymbols[pos.Symbol] = true
	}

	const minOIThresholdMillions = 15.0 // 15M USD minimum open interest value

	for _, coin := range ctx.CandidateCoins {
		if _, exists := ctx.MarketDataMap[coin.Symbol]; exists {
			continue
		}

		data, err := market.GetWithTimeframesWindow(coin.Symbol, timeframes, primaryTimeframe, displayCount, computeLookback)
		if err != nil {
			logger.Infof("Failed to fetch market data for %s: %v", coin.Symbol, err)
			ctx.DataFetchErrors = append(ctx.DataFetchErrors, fmt.Sprintf("%s: market data fetch failed", coin.Symbol))
			continue
		}

		// Liquidity filter (skip for xyz dex assets - they don't have OI data from Binance)
		isExistingPosition := positionSymbols[coin.Symbol]
		isXyzAsset := market.IsXyzDexAsset(coin.Symbol)
		if !isExistingPosition && !isXyzAsset && data.OpenInterest != nil && data.OpenInterest.Latest > 0 && data.CurrentPrice > 0 {
			oiValue := data.OpenInterest.Latest * data.CurrentPrice
			oiValueInMillions := oiValue / 1_000_000
			if oiValueInMillions < minOIThresholdMillions {
				logger.Infof("%s OI value too low (%.2fM USD < %.1fM), skipping coin",
					coin.Symbol, oiValueInMillions, minOIThresholdMillions)
				continue
			}
		}

		ctx.MarketDataMap[coin.Symbol] = data
	}

	logger.Infof("Successfully fetched multi-timeframe market data for %d coins", len(ctx.MarketDataMap))
	return nil
}

func buildFactorSnapshots(ctx *Context, config *store.StrategyConfig) (map[string]*market.FactorSnapshot, error) {
	snapshots := make(map[string]*market.FactorSnapshot, len(ctx.MarketDataMap))
	asOf := time.Now().UTC()
	req := IndicatorRequestFromStrategyConfig(config)
	structureReq := StructureRequestFromStrategyConfig(config)
	for symbol, data := range ctx.MarketDataMap {
		snapshot, err := market.BuildFactorSnapshotFromDataWithRequests(data, asOf, req, structureReq)
		if err != nil {
			return nil, fmt.Errorf("build factor snapshot for %s: %w", symbol, err)
		}
		snapshots[symbol] = snapshot
	}
	EnrichExternalFactors(ctx, snapshots, asOf)
	return snapshots, nil
}

func signalEngineFromStrategyConfig(config *store.StrategyConfig) SignalEngine {
	if config == nil {
		return NewRuleSignalEngine()
	}
	switch config.StrategyMode {
	case "scoring":
		return NewScoreSignalEngine()
	case "hybrid":
		return NewCompositeSignalEngine(NewRuleSignalEngine(), NewScoreSignalEngine())
	default:
		return NewRuleSignalEngine()
	}
}

func IndicatorRequestFromStrategyConfig(config *store.StrategyConfig) market.IndicatorRequest {
	if config == nil {
		return market.DefaultIndicatorRequest()
	}
	req := market.IndicatorRequest{}
	indicators := config.Indicators
	if indicators.EnableEMA {
		req.EMAPeriods = indicators.EMAPeriods
	}
	if indicators.EnableSMA {
		req.SMAPeriods = indicators.SMAPeriods
	}
	if indicators.EnableRSI {
		req.RSIPeriods = indicators.RSIPeriods
	}
	if indicators.EnableATR {
		req.ATRPeriods = indicators.ATRPeriods
	}
	if indicators.EnableADX {
		req.ADX = &market.ADXSpec{Period: indicators.ADXPeriod}
	}
	if indicators.EnableSAR {
		req.SAR = &market.SARSpec{Enabled: true}
	}
	if indicators.EnableBOLL {
		for _, period := range indicators.BOLLPeriods {
			req.BOLLPeriods = append(req.BOLLPeriods, market.BOLLSpec{Period: period, Multiplier: 2})
		}
	}
	if indicators.EnableMACD {
		req.MACD = &market.MACDSpec{Fast: indicators.MACDFastPeriod, Slow: indicators.MACDSlowPeriod, Signal: indicators.MACDSignalPeriod}
	}
	if indicators.EnableVolume {
		req.VolumePeriods = indicators.VolumePeriods
	}
	req.VWAPPeriods = indicators.VWAPPeriods
	req.DonchianPeriods = indicators.DonchianPeriods
	req.RealizedVolPeriods = indicators.RealizedVolPeriods
	req.PriceChangeWindows = indicators.PriceChangeWindows
	if config.ScoringConfig != nil && config.ScoringConfig.Enabled {
		for _, factor := range config.ScoringConfig.SelectedFactors {
			switch factor {
			case "trend":
				req.EMAPeriods = appendIntUnique(req.EMAPeriods, 20, 50)
				if req.MACD == nil {
					req.MACD = &market.MACDSpec{Fast: indicators.MACDFastPeriod, Slow: indicators.MACDSlowPeriod, Signal: indicators.MACDSignalPeriod}
				}
			case "momentum":
				req.RSIPeriods = appendIntUnique(req.RSIPeriods, 14)
			}
		}
	}
	if indicators.EnableSession {
		if len(indicators.Sessions) > 0 {
			for _, s := range indicators.Sessions {
				req.Sessions = append(req.Sessions, market.SessionSpec{
					Timezone: s.Timezone,
					Offset:   s.Offset,
					Duration: s.Duration,
				})
			}
		} else {
			req.Sessions = []market.SessionSpec{{Timezone: "UTC", Offset: "00:00", Duration: 1440}}
		}
	}
	if indicators.EnableOpeningRange {
		minutes := indicators.OpeningRangeMinutes
		if minutes <= 0 {
			minutes = 30
		}
		req.OpeningRange = &market.OpeningRangeSpec{RangeMinutes: minutes}
		// Ensure session definition is available for opening range calculation
		if len(req.Sessions) == 0 {
			req.Sessions = []market.SessionSpec{{Timezone: "UTC", Offset: "00:00", Duration: 1440}}
		}
	}
	req.EnableRBreaker = indicators.EnableRBreaker
	if indicators.EnableRBreaker && len(req.Sessions) == 0 {
		// R-Breaker also needs session definition
		req.Sessions = []market.SessionSpec{{Timezone: "UTC", Offset: "00:00", Duration: 1440}}
	}
	return req
}

func appendIntUnique(values []int, add ...int) []int {
	seen := map[int]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range add {
		if value <= 0 || seen[value] {
			continue
		}
		values = append(values, value)
		seen[value] = true
	}
	return values
}

func StructureRequestFromStrategyConfig(config *store.StrategyConfig) market.StructureRequest {
	if config == nil {
		return market.StructureRequest{}
	}
	config.ClampLimits()
	req := market.StructureRequest{}
	if config.ResolvedParameters.Structure.Fibonacci != nil {
		fib := config.ResolvedParameters.Structure.Fibonacci
		if fib.Timeframe != "" {
			req.Fibonacci = &market.FibonacciRequest{
				Timeframe:             fib.Timeframe,
				Lookback:              fib.Lookback,
				SwingWindow:           fib.SwingWindow,
				MinLegBars:            fib.MinLegBars,
				MinLegATRMultiple:     fib.MinLegATRMultiple,
				ZigZagThresholdPct:    fib.ZigZagThresholdPct,
				Levels:                append([]float64(nil), fib.Levels...),
				InvalidateOnBreakBase: fib.InvalidateOnBreakBase,
			}
		}
	}
	if config.ResolvedParameters.Structure.SupportResistance != nil {
		support := config.ResolvedParameters.Structure.SupportResistance
		if support.Timeframe != "" {
			req.Support = &market.SupportRequest{
				Timeframe:       support.Timeframe,
				Lookback:        support.Lookback,
				SwingWindow:     support.SwingWindow,
				ZoneWidthATR:    support.ZoneWidthATR,
				MinTouches:      support.MinTouches,
				MinDistanceBars: support.MinDistanceBars,
			}
		}
	}
	return req
}

func buildMarketValidationMaps(ctx *Context, riskConfig store.RiskControlConfig) (map[string]float64, map[string]float64) {
	marketPrices := make(map[string]float64)
	minSLDistances := make(map[string]float64)
	for symbol, data := range ctx.MarketDataMap {
		if data == nil {
			continue
		}
		if data.CurrentPrice > 0 {
			marketPrices[symbol] = data.CurrentPrice
		}
		var atr float64
		for _, tfData := range data.TimeframeData {
			if tfData != nil && tfData.ATR14 > 0 {
				atr = tfData.ATR14
				break
			}
		}
		if atr > 0 {
			atrBuffer := riskConfig.StopLossATRBuffer
			if atrBuffer <= 0 {
				atrBuffer = 2.0
			}
			minSLDistances[symbol] = atr * atrBuffer
		}
	}
	return marketPrices, minSLDistances
}

func decisionsFromTradingResult(result *TradingEngineResult) []Decision {
	if result == nil || result.Risk == nil {
		return nil
	}
	reviewBySignal := map[string]AIReviewDecision{}
	for _, review := range result.Reviews {
		reviewBySignal[review.SignalID] = review
	}
	decisions := make([]Decision, 0, len(result.Risk.Approved))
	for _, signal := range result.Risk.Approved {
		decisions = append(decisions, signal.ToDecision(reviewBySignal[signal.ID]))
	}
	return decisions
}

func tradingResultSummary(result *TradingEngineResult, ruleCount int) string {
	if result == nil {
		return fmt.Sprintf("structured trading flow: rules=%d signals=0 reviews=0 approved=0 rejected=0", ruleCount)
	}
	approved := 0
	rejected := 0
	if result.Risk != nil {
		approved = len(result.Risk.Approved)
		rejected = len(result.Risk.Rejected)
	}
	return fmt.Sprintf(
		"structured trading flow: rules=%d signals=%d reviews=%d approved=%d rejected=%d",
		ruleCount,
		len(result.Signals),
		len(result.Reviews),
		approved,
		rejected,
	)
}

// ============================================================================
// AI Response Parsing
// ============================================================================

func extractCoTTrace(response string) string {
	if match := reReasoningTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		logger.Infof("Extracted reasoning chain using <reasoning> tag")
		return strings.TrimSpace(match[1])
	}

	if decisionIdx := strings.Index(response, "<decision>"); decisionIdx > 0 {
		logger.Infof("Extracted content before <decision> tag as reasoning chain")
		return strings.TrimSpace(response[:decisionIdx])
	}

	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		logger.Infof("Extracted reasoning chain using old format ([ character separator)")
		return strings.TrimSpace(response[:jsonStart])
	}

	return strings.TrimSpace(response)
}
