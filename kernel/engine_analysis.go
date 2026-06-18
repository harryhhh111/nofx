package kernel

import (
	"context"
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/provider/nofxos"
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

	// Ensure OITopDataMap is initialized only when the strategy needs OI ranking.
	if ctx.OITopDataMap == nil {
		ctx.OITopDataMap = make(map[string]*OITopData)
		if engineConfig.Indicators.EnableOIRanking {
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
	}

	// Use the clamped config (engineConfig) so risk-control defaults — notably
	// leverage — are applied. The raw engine.GetRiskControlConfig() can carry a
	// zero leverage limit that would make the risk gate reject every signal.
	riskConfig := engineConfig.RiskControl
	factorSnapshots, err := buildFactorSnapshots(ctx, engineConfig)
	if err != nil {
		return nil, err
	}
	rules := rulesFromStrategyConfig(engine.GetConfig())
	scoring := scoringFromStrategyConfig(engine.GetConfig())
	if (engineConfig.StrategyMode == "scoring" || engineConfig.StrategyMode == "hybrid") && scoring == nil {
		return nil, fmt.Errorf("strategy_mode %s requires enabled scoring_config", engineConfig.StrategyMode)
	}
	marketPrices, minSLDistances := buildMarketValidationMaps(ctx, engineConfig, riskConfig)

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
	signalRequest := SignalRequest{
		Account:             ctx.Account,
		Positions:           ctx.Positions,
		Candidates:          ctx.CandidateCoins,
		Rules:               rules,
		Scoring:             scoring,
		PositionSizing:      positionSizingFromRiskControl(riskConfig),
		ProtectiveATRBuffer: riskConfig.StopLossATRBuffer,
		FactorSnapshot:      factorSnapshots,
		Now:                 time.Now().UTC(),
	}
	result, err := tradingEngine.Evaluate(context.Background(), TradingEngineRequest{
		SignalRequest: signalRequest,
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
		MarketContext:       result.MarketContext,
		Signals:             result.Signals,
		SetupEvaluations:    result.SetupEvaluations,
		ScoringEvaluations:  TraceScoringEvaluations(signalRequest),
		RuleEvaluations:     result.RuleEvaluations,
		Reviews:             result.Reviews,
		Risk:                result.Risk,
		InputAudit:          buildTradingInputAudit(ctx, engineConfig),
		UserDecisionSummary: buildUserDecisionSummary(result),
		CalibrationSamples:  BuildSignalCalibrationSamples(signalRequest, result),
	}
	if len(result.Signals) > 0 {
		decision.SystemPrompt = buildLLMReviewSystemPrompt()
		if userPrompt, promptErr := buildLLMReviewUserPrompt(AIReviewRequest{
			Signals:          result.Signals,
			FactorSnapshot:   factorSnapshots,
			MarketContext:    result.MarketContext,
			RelevantMemory:   result.Memory,
			CurrentPositions: ctx.Positions,
		}); promptErr == nil {
			decision.UserPrompt = userPrompt
		}
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
		data, err := market.GetWithTimeframesWindowContextWithExchange(context.Background(), pos.Symbol, timeframes, primaryTimeframe, displayCount, computeLookback, config.Indicators.Klines.IncludeOpenBar, config.Indicators.Klines.MarketDataSource)
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

		data, err := market.GetWithTimeframesWindowContextWithExchange(context.Background(), coin.Symbol, timeframes, primaryTimeframe, displayCount, computeLookback, config.Indicators.Klines.IncludeOpenBar, config.Indicators.Klines.MarketDataSource)
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

func buildTradingInputAudit(ctx *Context, config *store.StrategyConfig) *TradingInputAudit {
	if ctx == nil || config == nil {
		return nil
	}
	klines := config.Indicators.Klines
	timeframes := append([]string(nil), klines.SelectedTimeframes...)
	if len(timeframes) == 0 && klines.PrimaryTimeframe != "" {
		timeframes = append(timeframes, klines.PrimaryTimeframe)
	}
	displayCount := klines.PromptDisplayCount
	if displayCount <= 0 {
		displayCount = klines.PrimaryCount
	}
	if displayCount <= 0 {
		displayCount = 30
	}
	computeLookback := klines.ComputeLookback
	if computeLookback < displayCount {
		computeLookback = displayCount
	}
	requiredLookback := requiredCalculationLookback(config)
	warmupTarget := requiredLookback * 2
	candidates := make([]string, 0, len(ctx.CandidateCoins))
	sourceBySymbol := map[string][]string{}
	for _, coin := range ctx.CandidateCoins {
		candidates = append(candidates, coin.Symbol)
		sourceBySymbol[coin.Symbol] = append([]string(nil), coin.Sources...)
	}
	symbols := map[string]SymbolInputAudit{}
	for symbol, data := range ctx.MarketDataMap {
		if data == nil {
			continue
		}
		item := SymbolInputAudit{
			Source:     sourceBySymbol[symbol],
			Timeframes: map[string]TimeframeInputAudit{},
			Price:      data.CurrentPrice,
		}
		for tf, tfData := range data.TimeframeData {
			if tfData == nil {
				continue
			}
			tfAudit := TimeframeInputAudit{
				DisplayBars:      len(tfData.Klines),
				ComputeBars:      len(tfData.ComputeBars),
				RequiredLookback: requiredLookback,
				WarmupBars:       len(tfData.ComputeBars) - requiredLookback,
			}
			switch {
			case tfAudit.ComputeBars < requiredLookback:
				tfAudit.CalculationHealthy = false
				tfAudit.HealthReason = "compute bars below required indicator/structure lookback"
			case tfAudit.ComputeBars < warmupTarget:
				tfAudit.CalculationHealthy = true
				tfAudit.HealthReason = "minimum satisfied; limited warm-up margin"
			default:
				tfAudit.CalculationHealthy = true
				tfAudit.HealthReason = "enough warm-up bars for stable indicator calculation"
			}
			if len(tfData.Klines) > 0 {
				latest := time.UnixMilli(tfData.Klines[len(tfData.Klines)-1].Time).UTC()
				tfAudit.LatestTime = &latest
				tfAudit.LatestClose = tfData.Klines[len(tfData.Klines)-1].Close
			}
			item.Timeframes[tf] = tfAudit
		}
		if len(item.Timeframes) == 0 {
			item.Warnings = append(item.Warnings, "no timeframe data")
		}
		symbols[symbol] = item
	}
	return &TradingInputAudit{
		GeneratedAt:    time.Now().UTC(),
		CandidateCoins: candidates,
		Klines: KlineInputAudit{
			MarketDataSource: klines.MarketDataSource,
			Timeframes:       timeframes,
			PrimaryTimeframe: klines.PrimaryTimeframe,
			EntryTimeframe:   klines.EntryTimeframe,
			Confirmations:    append([]string(nil), klines.ConfirmationTimeframes...),
			UnusedTimeframes: unusedScoringTimeframes(timeframes, klines.PrimaryTimeframe, klines.EntryTimeframe, klines.ConfirmationTimeframes),
			DisplayCount:     displayCount,
			ComputeLookback:  computeLookback,
			RequiredLookback: requiredLookback,
			WarmupTarget:     warmupTarget,
			IncludeOpenBar:   klines.IncludeOpenBar,
		},
		Indicators: map[string]interface{}{
			"ema_periods":            config.Indicators.EMAPeriods,
			"sma_periods":            config.Indicators.SMAPeriods,
			"rsi_periods":            config.Indicators.RSIPeriods,
			"atr_periods":            config.Indicators.ATRPeriods,
			"adx_period":             config.Indicators.ADXPeriod,
			"boll_periods":           config.Indicators.BOLLPeriods,
			"volume_periods":         config.Indicators.VolumePeriods,
			"enable_ema":             config.Indicators.EnableEMA,
			"enable_sma":             config.Indicators.EnableSMA,
			"enable_macd":            config.Indicators.EnableMACD,
			"enable_rsi":             config.Indicators.EnableRSI,
			"enable_atr":             config.Indicators.EnableATR,
			"enable_adx":             config.Indicators.EnableADX,
			"enable_boll":            config.Indicators.EnableBOLL,
			"enable_volume":          config.Indicators.EnableVolume,
			"enable_oi":              config.Indicators.EnableOI,
			"enable_funding_rate":    config.Indicators.EnableFundingRate,
			"enable_quant_data":      config.Indicators.EnableQuantData,
			"enable_oi_ranking":      config.Indicators.EnableOIRanking,
			"enable_netflow_ranking": config.Indicators.EnableNetFlowRanking,
			"enable_price_ranking":   config.Indicators.EnablePriceRanking,
		},
		ExternalData: ExternalDataAudit{
			QuantEnabled:          config.Indicators.EnableQuantData,
			QuantSymbols:          len(ctx.QuantDataMap),
			OIRankingEnabled:      config.Indicators.EnableOIRanking,
			OIRankingAvailable:    hasOIRankingData(ctx.OIRankingData),
			NetFlowEnabled:        config.Indicators.EnableNetFlowRanking,
			NetFlowAvailable:      hasNetFlowRankingData(ctx.NetFlowRankingData),
			PriceRankingEnabled:   config.Indicators.EnablePriceRanking,
			PriceRankingAvailable: hasPriceRankingData(ctx.PriceRankingData),
			Statuses: map[string]string{
				"quant":         externalDataStatus(config.Indicators.EnableQuantData, len(ctx.QuantDataMap) > 0),
				"oi_ranking":    externalDataStatus(config.Indicators.EnableOIRanking, hasOIRankingData(ctx.OIRankingData)),
				"netflow":       externalDataStatus(config.Indicators.EnableNetFlowRanking, hasNetFlowRankingData(ctx.NetFlowRankingData)),
				"price_ranking": externalDataStatus(config.Indicators.EnablePriceRanking, hasPriceRankingData(ctx.PriceRankingData)),
			},
			DataFetchErrors: append([]string(nil), ctx.DataFetchErrors...),
		},
		Symbols: symbols,
	}
}

func BuildTradingInputAudit(ctx *Context, config *store.StrategyConfig) *TradingInputAudit {
	return buildTradingInputAudit(ctx, config)
}

func unusedScoringTimeframes(timeframes []string, primary string, entry string, confirmations []string) []string {
	used := map[string]bool{}
	if primary != "" {
		used[primary] = true
	}
	if entry != "" {
		used[entry] = true
	}
	for _, tf := range confirmations {
		if tf != "" {
			used[tf] = true
		}
	}
	out := []string{}
	for _, tf := range timeframes {
		if tf != "" && !used[tf] {
			out = append(out, tf)
		}
	}
	return out
}

func requiredCalculationLookback(config *store.StrategyConfig) int {
	if config == nil {
		return 0
	}
	indicators := config.Indicators
	required := 1
	if indicators.EnableEMA {
		required = maxInt(required, maxIntSlice(indicators.EMAPeriods))
	}
	if indicators.EnableSMA {
		required = maxInt(required, maxIntSlice(indicators.SMAPeriods))
	}
	if indicators.EnableRSI {
		required = maxInt(required, maxIntSlice(indicators.RSIPeriods)+1)
	}
	if indicators.EnableATR {
		required = maxInt(required, maxIntSlice(indicators.ATRPeriods)+1)
	}
	if indicators.EnableADX {
		required = maxInt(required, indicators.ADXPeriod+1)
	}
	if indicators.EnableBOLL {
		required = maxInt(required, maxIntSlice(indicators.BOLLPeriods))
	}
	if indicators.EnableVolume || indicators.EnableVolumeSpike {
		required = maxInt(required, maxIntSlice(indicators.VolumePeriods))
	}
	if indicators.EnableMACD {
		required = maxInt(required, indicators.MACDSlowPeriod+indicators.MACDSignalPeriod)
	}
	if indicators.EnableVWAP {
		required = maxInt(required, maxIntSlice(indicators.VWAPPeriods))
	}
	if indicators.EnableDonchian {
		required = maxInt(required, maxIntSlice(indicators.DonchianPeriods))
	}
	if indicators.EnableRollingPercentile {
		required = maxInt(required, maxIntSlice(indicators.RollingPercentilePeriods))
	}
	required = maxInt(required, maxIntSlice(indicators.RealizedVolPeriods)+1)
	required = maxInt(required, maxIntSlice(indicators.PriceChangeWindows)+1)
	required = maxInt(required, maxNamedWindowBars(indicators.PriceChangeNamedWindows)+1)
	if config.Structure.EnableFibonacci {
		required = maxInt(required, config.Structure.Fibonacci.Lookback)
	}
	if config.Structure.EnableSupportResistance {
		required = maxInt(required, config.Structure.SupportResistance.Lookback)
	}
	return required
}

func maxIntSlice(values []int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

// maxNamedWindowBars returns the largest bar count required by the given named
// price-change windows, assuming the smallest supported timeframe (1m). The
// result is capped to store.MaxComputeLookback; modules skip windows that still
// exceed the available bars for a given timeframe.
func maxNamedWindowBars(windows []string) int {
	maxBars := 0
	for _, w := range windows {
		switch w {
		case "1h":
			maxBars = maxInt(maxBars, 60)
		case "4h":
			maxBars = maxInt(maxBars, 240)
		case "24h":
			maxBars = maxInt(maxBars, 1440)
		case "3d":
			maxBars = maxInt(maxBars, 4320)
		}
	}
	if maxBars > store.MaxComputeLookback {
		return store.MaxComputeLookback
	}
	return maxBars
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func externalDataStatus(enabled bool, available bool) string {
	if !enabled {
		return "disabled"
	}
	if available {
		return "available"
	}
	return "enabled_but_unavailable_this_cycle"
}

func hasOIRankingData(data *nofxos.OIRankingData) bool {
	return data != nil && (len(data.TopPositions) > 0 || len(data.LowPositions) > 0)
}

func hasNetFlowRankingData(data *nofxos.NetFlowRankingData) bool {
	return data != nil &&
		(len(data.InstitutionFutureTop) > 0 ||
			len(data.InstitutionFutureLow) > 0 ||
			len(data.PersonalFutureTop) > 0 ||
			len(data.PersonalFutureLow) > 0)
}

func hasPriceRankingData(data *nofxos.PriceRankingData) bool {
	return data != nil && len(data.Durations) > 0
}

func signalEngineFromStrategyConfig(config *store.StrategyConfig) SignalEngine {
	if config == nil {
		return NewRuleSignalEngine()
	}
	switch config.StrategyMode {
	case "scoring":
		return NewSetupSignalEngine()
	case "hybrid":
		return NewCompositeSignalEngine(NewRuleSignalEngine(), NewSetupSignalEngine())
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
		multiplier := indicators.BOLLMultiplier
		if multiplier <= 0 {
			multiplier = 2.0
		}
		for _, period := range indicators.BOLLPeriods {
			req.BOLLPeriods = append(req.BOLLPeriods, market.BOLLSpec{Period: period, Multiplier: multiplier})
		}
	}
	if indicators.EnableMACD {
		req.MACD = &market.MACDSpec{Fast: indicators.MACDFastPeriod, Slow: indicators.MACDSlowPeriod, Signal: indicators.MACDSignalPeriod}
	}
	if indicators.EnableVolume || indicators.EnableVolumeSpike {
		req.VolumePeriods = indicators.VolumePeriods
	}
	req.EnableVolume = indicators.EnableVolume
	if indicators.EnableVolumeSpike {
		req.EnableVolumeSpike = true
		req.VolumeSpikeMultiplier = indicators.VolumeSpikeMultiplier
	}
	if indicators.EnableVWAP {
		req.VWAPPeriods = indicators.VWAPPeriods
	}
	if indicators.EnableDonchian {
		req.DonchianPeriods = indicators.DonchianPeriods
	}
	if indicators.EnableRollingPercentile {
		req.RollingPercentilePeriods = indicators.RollingPercentilePeriods
	}
	req.RealizedVolPeriods = indicators.RealizedVolPeriods
	req.PriceChangeWindows = indicators.PriceChangeWindows
	req.PriceChangeNamedWindows = indicators.PriceChangeNamedWindows
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
	req.EnableMTSI = indicators.EnableMTSI
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
	timeframes := structureRequestTimeframes(config)
	if config.ResolvedParameters.Structure.Fibonacci != nil {
		fib := config.ResolvedParameters.Structure.Fibonacci
		fibRequests := make([]market.FibonacciRequest, 0, len(timeframes))
		for _, timeframe := range timeframes {
			if timeframe == "" {
				continue
			}
			fibRequests = append(fibRequests, market.FibonacciRequest{
				Timeframe:             timeframe,
				Lookback:              fib.Lookback,
				SwingWindow:           fib.SwingWindow,
				MinLegBars:            fib.MinLegBars,
				MinLegATRMultiple:     fib.MinLegATRMultiple,
				ZigZagThresholdPct:    fib.ZigZagThresholdPct,
				Levels:                append([]float64(nil), fib.Levels...),
				InvalidateOnBreakBase: fib.InvalidateOnBreakBase,
			})
		}
		if len(fibRequests) > 0 {
			req.Fibonacci = &fibRequests[0]
			req.Fibonaccis = fibRequests[1:]
		}
	}
	if config.ResolvedParameters.Structure.SupportResistance != nil {
		support := config.ResolvedParameters.Structure.SupportResistance
		supportRequests := make([]market.SupportRequest, 0, len(timeframes))
		for _, timeframe := range timeframes {
			if timeframe == "" {
				continue
			}
			supportRequests = append(supportRequests, market.SupportRequest{
				Timeframe:       timeframe,
				Lookback:        support.Lookback,
				SwingWindow:     support.SwingWindow,
				ZoneWidthATR:    support.ZoneWidthATR,
				MinTouches:      support.MinTouches,
				MinDistanceBars: support.MinDistanceBars,
			})
		}
		if len(supportRequests) > 0 {
			req.Support = &supportRequests[0]
			req.Supports = supportRequests[1:]
		}
	}
	return req
}

func structureRequestTimeframes(config *store.StrategyConfig) []string {
	if config == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	add(config.Indicators.Klines.PrimaryTimeframe)
	add(config.Indicators.Klines.EntryTimeframe)
	for _, timeframe := range config.Indicators.Klines.ConfirmationTimeframes {
		add(timeframe)
	}
	if config.ResolvedParameters.Structure.Fibonacci != nil {
		add(config.ResolvedParameters.Structure.Fibonacci.Timeframe)
	}
	if config.ResolvedParameters.Structure.SupportResistance != nil {
		add(config.ResolvedParameters.Structure.SupportResistance.Timeframe)
	}
	return out
}

func buildMarketValidationMaps(ctx *Context, config *store.StrategyConfig, riskConfig store.RiskControlConfig) (map[string]float64, map[string]float64) {
	marketPrices := make(map[string]float64)
	minSLDistances := make(map[string]float64)
	for symbol, data := range ctx.MarketDataMap {
		if data == nil {
			continue
		}
		if data.CurrentPrice > 0 {
			marketPrices[symbol] = data.CurrentPrice
		}
		atr := preferredATR14(data, config)
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

func positionSizingFromRiskControl(riskConfig store.RiskControlConfig) *PositionSizingConfig {
	return &PositionSizingConfig{
		RiskPerTradePct:              riskConfig.RiskPerTradePct,
		MinPositionSizeUSD:           riskConfig.MinPositionSize,
		MaxMarginUsage:               riskConfig.MaxMarginUsage,
		BTCETHMaxPositionValueRatio:  riskConfig.BTCETHMaxPositionValueRatio,
		AltcoinMaxPositionValueRatio: riskConfig.AltcoinMaxPositionValueRatio,
	}
}

func preferredATR14(data *market.Data, config *store.StrategyConfig) float64 {
	if data == nil || len(data.TimeframeData) == 0 {
		return 0
	}
	preferred := []string{}
	add := func(tf string) {
		tf = strings.TrimSpace(tf)
		if tf != "" {
			preferred = append(preferred, tf)
		}
	}
	if config != nil {
		add(config.Indicators.Klines.PrimaryTimeframe)
		add(config.Indicators.Klines.EntryTimeframe)
		for _, tf := range config.Indicators.Klines.ConfirmationTimeframes {
			add(tf)
		}
		for _, tf := range config.Indicators.Klines.SelectedTimeframes {
			add(tf)
		}
	}
	for _, tf := range []string{"5m", "15m", "1h", "4h", "3m", "1m"} {
		add(tf)
	}
	seen := map[string]bool{}
	for _, tf := range preferred {
		if seen[tf] {
			continue
		}
		seen[tf] = true
		if tfData := data.TimeframeData[tf]; tfData != nil && tfData.ATR14 > 0 {
			return tfData.ATR14
		}
	}
	return 0
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
		"structured trading flow: rules=%d setups=%d signals=%d reviews=%d approved=%d rejected=%d",
		ruleCount,
		len(result.SetupEvaluations),
		len(result.Signals),
		len(result.Reviews),
		approved,
		rejected,
	)
}

func buildUserDecisionSummary(result *TradingEngineResult) *UserDecisionSummary {
	if result == nil {
		return &UserDecisionSummary{
			Status:   "error",
			Headline: "本轮没有可用的交易评估结果。",
		}
	}

	approved := 0
	rejected := 0
	if result.Risk != nil {
		approved = len(result.Risk.Approved)
		rejected = len(result.Risk.Rejected)
	}

	status := "no_trade"
	headline := "本轮没有产生可执行交易。"
	switch {
	case approved > 0:
		status = "trade"
		headline = fmt.Sprintf("本轮通过风控，生成 %d 个可执行交易动作。", approved)
	case len(result.Signals) > 0 && rejected > 0:
		status = "rejected"
		headline = fmt.Sprintf("本轮识别到 %d 个候选信号，但未通过最终复核或风控。", len(result.Signals))
	case len(result.Signals) > 0:
		status = "reviewed"
		headline = fmt.Sprintf("本轮识别到 %d 个候选信号，但没有形成可执行动作。", len(result.Signals))
	case len(result.SetupEvaluations) > 0:
		headline = "本轮完成市场评估，但没有满足开仓条件的信号。"
	}

	steps := []UserDecisionSummaryStep{
		{
			Title:   "数据与候选",
			Status:  "ok",
			Summary: fmt.Sprintf("完成 %d 个交易对象的代码评估。", maxInt(len(result.SetupEvaluations), len(result.Signals))),
		},
	}
	if len(result.Signals) == 0 {
		steps = append(steps, UserDecisionSummaryStep{
			Title:   "机会识别",
			Status:  "skip",
			Summary: "代码没有生成可执行开仓或平仓候选信号，因此没有进入 AI 复核和下单流程。",
		})
	} else {
		steps = append(steps, UserDecisionSummaryStep{
			Title:   "机会识别",
			Status:  "ok",
			Summary: fmt.Sprintf("代码生成 %d 个候选信号，后续进入 AI 复核和风控校验。", len(result.Signals)),
		})
	}

	if len(result.Reviews) > 0 {
		pass, warn, reject := reviewStatusCounts(result.Reviews)
		steps = append(steps, UserDecisionSummaryStep{
			Title:   "AI 复核",
			Status:  reviewStepStatus(pass, warn, reject),
			Summary: fmt.Sprintf("AI 复核结果：通过 %d，警告 %d，拒绝 %d。", pass, warn, reject),
		})
	}

	if result.Risk != nil {
		riskStatus := "ok"
		if approved == 0 && rejected > 0 {
			riskStatus = "reject"
		} else if approved == 0 {
			riskStatus = "skip"
		}
		steps = append(steps, UserDecisionSummaryStep{
			Title:   "风控结果",
			Status:  riskStatus,
			Summary: fmt.Sprintf("风控通过 %d 个信号，拒绝 %d 个信号。", approved, rejected),
		})
	}

	return &UserDecisionSummary{
		Status:   status,
		Headline: headline,
		Steps:    steps,
		Symbols:  buildUserDecisionSymbolSummaries(result),
	}
}

func reviewStatusCounts(reviews []AIReviewDecision) (pass, warn, reject int) {
	for _, review := range reviews {
		switch strings.ToLower(strings.TrimSpace(review.Status)) {
		case "pass":
			pass++
		case "reject":
			reject++
		default:
			warn++
		}
	}
	return pass, warn, reject
}

func reviewStepStatus(pass, warn, reject int) string {
	switch {
	case reject > 0:
		return "reject"
	case warn > 0:
		return "warn"
	case pass > 0:
		return "ok"
	default:
		return "skip"
	}
}

func buildUserDecisionSymbolSummaries(result *TradingEngineResult) []UserDecisionSymbolSummary {
	if result == nil {
		return nil
	}
	signalByID := map[string]CandidateSignal{}
	for _, signal := range result.Signals {
		signalByID[signal.ID] = signal
	}
	approved := map[string]bool{}
	rejected := map[string]string{}
	if result.Risk != nil {
		for _, signal := range result.Risk.Approved {
			approved[signal.ID] = true
		}
		for _, item := range result.Risk.Rejected {
			rejected[item.SignalID] = item.Reason
		}
	}

	out := make([]UserDecisionSymbolSummary, 0, len(result.Signals)+len(result.SetupEvaluations))
	seen := map[string]bool{}
	for _, signal := range result.Signals {
		seen[signal.Symbol] = true
		decision := "review"
		reason := signal.TriggerReason
		if approved[signal.ID] {
			decision = signal.Action
			reason = "通过 AI 复核和风控校验"
		} else if rejectReason := rejected[signal.ID]; rejectReason != "" {
			decision = "skip"
			reason = rejectReason
		}
		out = append(out, UserDecisionSymbolSummary{
			Symbol:   signal.Symbol,
			Decision: decision,
			Reason:   reason,
			Details:  signalUserDetails(signal),
		})
	}
	for _, trace := range result.SetupEvaluations {
		if trace.Symbol == "" || seen[trace.Symbol] {
			continue
		}
		seen[trace.Symbol] = true
		out = append(out, UserDecisionSymbolSummary{
			Symbol:   trace.Symbol,
			Decision: "skip",
			Reason:   trace.Reason,
			Details: []string{
				fmt.Sprintf("主周期评分 %.1f，入场周期评分 %.1f", trace.Primary.Score, trace.Entry.Score),
				fmt.Sprintf("场景：%s", emptyAs(trace.Setup, "未满足交易场景")),
			},
		})
	}
	return out
}

func signalUserDetails(signal CandidateSignal) []string {
	details := []string{}
	if signal.Setup != "" {
		details = append(details, "交易场景："+signal.Setup)
	}
	if signal.EntryPrice > 0 {
		details = append(details, fmt.Sprintf("入场参考 %.4f", signal.EntryPrice))
	}
	if signal.StopLoss > 0 {
		details = append(details, fmt.Sprintf("止损 %.4f", signal.StopLoss))
	}
	if signal.TakeProfit > 0 {
		details = append(details, fmt.Sprintf("止盈 %.4f", signal.TakeProfit))
	}
	if levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace); ok {
		details = append(details, fmt.Sprintf("止损来源：%s，止盈来源：%s，实际盈亏比 %.2f", levels.StopSource, levels.TargetSource, levels.RiskReward))
	}
	return details
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
