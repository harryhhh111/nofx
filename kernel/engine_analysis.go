package kernel

import (
	"encoding/json"
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
	// Safe regex: precisely match ```json code blocks
	reJSONFence      = regexp.MustCompile(`(?is)` + "```json\\s*(\\[\\s*\\{.*?\\}\\s*\\])\\s*```")
	reJSONArray      = regexp.MustCompile(`(?is)\[\s*\{.*?\}\s*\]`)
	reArrayHead      = regexp.MustCompile(`^\[\s*\{`)
	reArrayOpenSpace = regexp.MustCompile(`^\[\s+\{`)
	reInvisibleRunes = regexp.MustCompile("[\u200B\u200C\u200D\uFEFF]")

	// XML tag extraction (supports any characters in reasoning chain)
	reReasoningTag        = regexp.MustCompile(`(?s)<reasoning>(.*?)</reasoning>`)
	reReasoningSummaryTag = regexp.MustCompile(`(?s)<reasoning_summary>(.*?)</reasoning_summary>`)
	reDecisionTag         = regexp.MustCompile(`(?s)<decision>(.*?)</decision>`)
	// Phase 2: AI's self-check JSON. Optional — missing section must not
	// break decision parsing.
	reGuardAssessmentTag = regexp.MustCompile(`(?s)<guard_assessment>(.*?)</guard_assessment>`)
	// Strip data source names from summary
	reDataSourceInSummary = regexp.MustCompile(`(?i)\b(ai500|oi[_\s]?top)\b`)
)

// ============================================================================
// Entry Functions - Main API
// ============================================================================

// GetFullDecision gets AI's complete trading decision (batch analysis of all coins and positions)
// Uses default strategy configuration - for production use GetFullDecisionWithStrategy with explicit config
func GetFullDecision(ctx *Context, mcpClient mcp.AIClient) (*FullDecision, error) {
	defaultConfig := store.GetDefaultStrategyConfig("en")
	engine := NewStrategyEngine(&defaultConfig)
	return GetFullDecisionWithStrategy(ctx, mcpClient, engine, "", nil)
}

// GetFullDecisionWithStrategy uses StrategyEngine to get AI decision (unified prompt generation)
//
// gc is optional. When non-nil, every guard hit during validation is
// recorded under gc.Events and returned on FullDecision.GuardEvents so
// the caller can bulk-insert them into guard_events after persisting the
// decision record.
func GetFullDecisionWithStrategy(ctx *Context, mcpClient mcp.AIClient, engine *StrategyEngine, variant string, gc *GuardContext) (*FullDecision, error) {
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

	// Token estimation check — block if exceeding the specific model's context limit
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
		logger.Errorf("🚫 Token estimate %d exceeds %s context limit %d — blocking analysis",
			estimate.Total, providerName, contextLimit)
		return nil, fmt.Errorf("estimated %d tokens exceeds model context limit of %d; reduce coins, timeframes, or K-line count",
			estimate.Total, contextLimit)
	}
	if estimate.Total*100/contextLimit >= 80 {
		logger.Infof("⚠️  Token estimate %d — approaching %s context limit %d",
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

	// 2. Build System Prompt using strategy engine
	riskConfig := engine.GetRiskControlConfig()
	systemPrompt := engine.BuildSystemPrompt(ctx.Account.TotalEquity, variant)

	// 2b. Capture the active guard config once so every GuardEvent can
	// reference the exact parameters that produced it (avoids re-marshaling
	// at each emit site and keeps the on-disk snapshot stable per cycle).
	if gc != nil && gc.ConfigSnapshot == nil {
		gc.ConfigSnapshot = configSnapshotJSON(riskConfig.EntryRiskGuard)
	}

	// 2a. Inject brake notice if trader layer has activated the brake system
	if ctx.BrakeNotice != "" {
		systemPrompt += "\n\n" + ctx.BrakeNotice + "\n"
	}

	// 3. Build User Prompt using strategy engine
	userPrompt := engine.BuildUserPrompt(ctx)

	// 4. Call AI API
	aiCallStart := time.Now()
	aiResponse, err := mcpClient.CallWithMessages(systemPrompt, userPrompt)
	aiCallDuration := time.Since(aiCallStart)
	if err != nil {
		return nil, fmt.Errorf("AI API call failed: %w", err)
	}

	// 4b. Pre-extract the AI's <guard_assessment> self-check JSON so
	// every guard event emitted by validateDecisions (which runs INSIDE
	// parseFullDecisionResponse below) can carry the ai_self_check
	// subset. This is the only way to attach AI assessment to events
	// that fire during validation; otherwise events would be missing
	// the comparison signal.
	if gc != nil && gc.AIAssessment == nil {
		if ga := extractGuardAssessment(aiResponse); ga != "" {
			gc.AIAssessment = aiSelfCheckSubset(ga)
		}
	}

	// 5. Parse AI response
	// Build market price map and min SL distance map for validation
	marketPrices := make(map[string]float64)
	minSLDistances := make(map[string]float64) // symbol -> min SL distance in price
	atrBuffer := riskConfig.StopLossATRBuffer
	if atrBuffer <= 0 {
		atrBuffer = 1.0 // balanced default
	}
	for symbol, md := range ctx.MarketDataMap {
		if md.CurrentPrice > 0 {
			marketPrices[symbol] = md.CurrentPrice
		}
		// Find best ATR14: prefer 15m, fallback 1h, then any available timeframe
		var bestATR float64
		if md.TimeframeData != nil {
			for _, tf := range []string{"15m", "1h", "30m", "4h"} {
				if tfData, ok := md.TimeframeData[tf]; ok && tfData.ATR14 > 0 {
					bestATR = tfData.ATR14
					break
				}
			}
		}
		if bestATR > 0 {
			minSLDistances[symbol] = bestATR * atrBuffer
		}
	}
	decision, err := parseFullDecisionResponse(
		aiResponse,
		ctx.Account.TotalEquity,
		riskConfig.BTCETHMaxLeverage,
		riskConfig.AltcoinMaxLeverage,
		riskConfig.BTCETHMaxPositionValueRatio,
		riskConfig.AltcoinMaxPositionValueRatio,
		riskConfig.MinRiskRewardRatio,
		riskConfig.EntryRiskGuard,
		ctx.MarketDataMap,
		marketPrices,
		minSLDistances,
		gc,
	)

	if decision != nil {
		decision.Timestamp = time.Now()
		decision.SystemPrompt = systemPrompt
		decision.UserPrompt = userPrompt
		decision.AIRequestDurationMs = aiCallDuration.Milliseconds()
		decision.RawResponse = aiResponse
		if gc != nil {
			decision.GuardEvents = gc.Events
		}
	}

	if err != nil {
		return decision, fmt.Errorf("failed to parse AI response: %w", err)
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
	klineCount := config.Indicators.Klines.PrimaryCount

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
	if klineCount <= 0 {
		klineCount = 30
	}

	logger.Infof("📊 Strategy timeframes: %v, Primary: %s, Kline count: %d", timeframes, primaryTimeframe, klineCount)

	// 1. First fetch data for position coins (must fetch)
	for _, pos := range ctx.Positions {
		data, err := market.GetWithTimeframes(pos.Symbol, timeframes, primaryTimeframe, klineCount, config.Indicators.SMAPeriods...)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch market data for position %s: %v", pos.Symbol, err)
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

		data, err := market.GetWithTimeframes(coin.Symbol, timeframes, primaryTimeframe, klineCount, config.Indicators.SMAPeriods...)
		if err != nil {
			logger.Infof("⚠️  Failed to fetch market data for %s: %v", coin.Symbol, err)
			ctx.DataFetchErrors = append(ctx.DataFetchErrors, fmt.Sprintf("%s: market data fetch failed", coin.Symbol))
			continue
		}

		// Liquidity filter (skip for xyz dex assets - they don't have OI data from Binance)
		// Also skip when OI data is unavailable (Latest <= 0 means fetch failure)
		isExistingPosition := positionSymbols[coin.Symbol]
		isXyzAsset := market.IsXyzDexAsset(coin.Symbol)
		if !isExistingPosition && !isXyzAsset && data.OpenInterest != nil && data.OpenInterest.Latest > 0 && data.CurrentPrice > 0 {
			oiValue := data.OpenInterest.Latest * data.CurrentPrice
			oiValueInMillions := oiValue / 1_000_000
			if oiValueInMillions < minOIThresholdMillions {
				logger.Infof("⚠️  %s OI value too low (%.2fM USD < %.1fM), skipping coin",
					coin.Symbol, oiValueInMillions, minOIThresholdMillions)
				continue
			}
		}

		ctx.MarketDataMap[coin.Symbol] = data
	}

	logger.Infof("📊 Successfully fetched multi-timeframe market data for %d coins", len(ctx.MarketDataMap))
	return nil
}

// ============================================================================
// AI Response Parsing
// ============================================================================

func parseFullDecisionResponse(aiResponse string, accountEquity float64, btcEthLeverage, altcoinLeverage int, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64, entryRiskGuard *store.EntryRiskGuardConfig, marketDataMap map[string]*market.Data, marketPrices map[string]float64, minSLDistances map[string]float64, gc *GuardContext) (*FullDecision, error) {
	// Detect truncated response: if AI started outputting (<reasoning> present)
	// but never closed the response (</decision> missing)
	if strings.Contains(aiResponse, "<reasoning>") && !strings.Contains(aiResponse, "</decision>") {
		return nil, fmt.Errorf("AI response appears truncated: missing </decision> tag")
	}

	cotTrace := extractCoTTrace(aiResponse)
	cotSummary := extractCoTSummary(aiResponse, cotTrace)
	guardAssessment := extractGuardAssessment(aiResponse)

	decisions, err := extractDecisions(aiResponse)
	if err != nil {
		return &FullDecision{
			CoTTrace:        cotTrace,
			CoTSummary:      cotSummary,
			GuardAssessment: guardAssessment,
			Decisions:       []Decision{},
		}, fmt.Errorf("failed to extract decisions: %w", err)
	}

	rejectedCount := validateDecisions(decisions, accountEquity, btcEthLeverage, altcoinLeverage, btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio, entryRiskGuard, marketDataMap, marketPrices, minSLDistances, gc)
	if rejectedCount > 0 {
		logger.Infof("⚠️ %d/%d decisions rejected during validation (converted to wait)", rejectedCount, len(decisions))
	}

	return &FullDecision{
		CoTTrace:        cotTrace,
		CoTSummary:      cotSummary,
		GuardAssessment: guardAssessment,
		Decisions:       decisions,
	}, nil
}

const maxSummaryLen = 500

func sanitizeCotSummary(s string) string {
	s = reDataSourceInSummary.ReplaceAllString(s, "")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

// extractCoTSummary extracts refined summary: prioritize <reasoning_summary> tag, fallback to cotTrace[:500]
func extractCoTSummary(response string, cotTrace string) string {
	if match := reReasoningSummaryTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		s := strings.TrimSpace(match[1])
		if s != "" {
			logger.Infof("✓ Extracted reasoning summary using <reasoning_summary> tag")
			return sanitizeCotSummary(s)
		}
	}
	if cotTrace == "" {
		return ""
	}
	if len(cotTrace) <= maxSummaryLen {
		return sanitizeCotSummary(cotTrace)
	}
	return sanitizeCotSummary(cotTrace[:maxSummaryLen] + "...")
}

func extractCoTTrace(response string) string {
	if match := reReasoningTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		logger.Infof("✓ Extracted reasoning chain using <reasoning> tag")
		return strings.TrimSpace(match[1])
	}

	if decisionIdx := strings.Index(response, "<decision>"); decisionIdx > 0 {
		logger.Infof("✓ Extracted content before <decision> tag as reasoning chain")
		return strings.TrimSpace(response[:decisionIdx])
	}

	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		logger.Infof("⚠️  Extracted reasoning chain using old format ([ character separator)")
		return strings.TrimSpace(response[:jsonStart])
	}

	return strings.TrimSpace(response)
}

// extractGuardAssessment pulls the AI's self-check JSON out of the
// response. Returns "" if the section is missing or empty — callers must
// treat this as a soft warning, not a parse error. The content is NOT
// validated: the prompt instructs the model to emit valid JSON, but
// downstream code only consumes ai_self_check / risk_signals for diff
// analysis and never uses it for risk control. Invalid content is
// preserved verbatim so post-mortem tooling can diagnose drift.
func extractGuardAssessment(response string) string {
	match := reGuardAssessmentTag.FindStringSubmatch(response)
	if match == nil || len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func extractDecisions(response string) ([]Decision, error) {
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)

	var jsonPart string
	if match := reDecisionTag.FindStringSubmatch(s); match != nil && len(match) > 1 {
		jsonPart = strings.TrimSpace(match[1])
		logger.Infof("✓ Extracted JSON using <decision> tag")
	} else {
		jsonPart = s
		logger.Infof("⚠️  <decision> tag not found, searching JSON in full text")
	}

	jsonPart = fixMissingQuotes(jsonPart)

	if m := reJSONFence.FindStringSubmatch(jsonPart); m != nil && len(m) > 1 {
		jsonContent := strings.TrimSpace(m[1])
		jsonContent = compactArrayOpen(jsonContent)
		jsonContent = fixMissingQuotes(jsonContent)
		if err := validateJSONFormat(jsonContent); err != nil {
			return nil, fmt.Errorf("JSON format validation failed: %w\nJSON content: %s\nFull response:\n%s", err, jsonContent, response)
		}
		var decisions []Decision
		if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
			return nil, fmt.Errorf("JSON parsing failed: %w\nJSON content: %s", err, jsonContent)
		}
		return decisions, nil
	}

	jsonContent := strings.TrimSpace(reJSONArray.FindString(jsonPart))
	if jsonContent == "" {
		logger.Infof("⚠️  [SafeFallback] AI didn't output JSON decision, entering safe wait mode")

		cotSummary := jsonPart
		if len(cotSummary) > 240 {
			cotSummary = cotSummary[:240] + "..."
		}

		fallbackDecision := Decision{
			Symbol:    "ALL",
			Action:    "wait",
			Reasoning: fmt.Sprintf("Model didn't output structured JSON decision, entering safe wait; summary: %s", cotSummary),
		}

		return []Decision{fallbackDecision}, nil
	}

	jsonContent = compactArrayOpen(jsonContent)
	jsonContent = fixMissingQuotes(jsonContent)

	if err := validateJSONFormat(jsonContent); err != nil {
		return nil, fmt.Errorf("JSON format validation failed: %w\nJSON content: %s\nFull response:\n%s", err, jsonContent, response)
	}

	var decisions []Decision
	if err := json.Unmarshal([]byte(jsonContent), &decisions); err != nil {
		return nil, fmt.Errorf("JSON parsing failed: %w\nJSON content: %s", err, jsonContent)
	}

	return decisions, nil
}

func fixMissingQuotes(jsonStr string) string {
	jsonStr = strings.ReplaceAll(jsonStr, "\u201c", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "\u201d", "\"")
	jsonStr = strings.ReplaceAll(jsonStr, "\u2018", "'")
	jsonStr = strings.ReplaceAll(jsonStr, "\u2019", "'")

	jsonStr = strings.ReplaceAll(jsonStr, "［", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "］", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "｛", "{")
	jsonStr = strings.ReplaceAll(jsonStr, "｝", "}")
	jsonStr = strings.ReplaceAll(jsonStr, "：", ":")
	jsonStr = strings.ReplaceAll(jsonStr, "，", ",")

	jsonStr = strings.ReplaceAll(jsonStr, "【", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "】", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "〔", "[")
	jsonStr = strings.ReplaceAll(jsonStr, "〕", "]")
	jsonStr = strings.ReplaceAll(jsonStr, "、", ",")

	jsonStr = strings.ReplaceAll(jsonStr, "　", " ")

	return jsonStr
}

func validateJSONFormat(jsonStr string) error {
	trimmed := strings.TrimSpace(jsonStr)

	if !reArrayHead.MatchString(trimmed) {
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed[:min(20, len(trimmed))], "{") {
			return fmt.Errorf("not a valid decision array (must contain objects {}), actual content: %s", trimmed[:min(50, len(trimmed))])
		}
		return fmt.Errorf("JSON must start with [{ (whitespace allowed), actual: %s", trimmed[:min(20, len(trimmed))])
	}

	// Check only JSON content outside quoted strings. Reasoning text may contain
	// commas in prices like "$76,400", which are valid string content.
	inQuote := false
	for i := 0; i < len(jsonStr); i++ {
		if jsonStr[i] == '"' && (i == 0 || jsonStr[i-1] != '\\') {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		if jsonStr[i] == '~' {
			return fmt.Errorf("JSON cannot contain range symbol ~ outside strings, all numbers must be precise single values")
		}
		if i <= len(jsonStr)-5 &&
			jsonStr[i] >= '0' && jsonStr[i] <= '9' &&
			jsonStr[i+1] == ',' &&
			jsonStr[i+2] >= '0' && jsonStr[i+2] <= '9' &&
			jsonStr[i+3] >= '0' && jsonStr[i+3] <= '9' &&
			jsonStr[i+4] >= '0' && jsonStr[i+4] <= '9' {
			return fmt.Errorf("JSON numbers cannot contain thousand separator comma, found: %s", jsonStr[i:min(i+10, len(jsonStr))])
		}
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func removeInvisibleRunes(s string) string {
	return reInvisibleRunes.ReplaceAllString(s, "")
}

func compactArrayOpen(s string) string {
	return reArrayOpenSpace.ReplaceAllString(strings.TrimSpace(s), "[{")
}
