package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	_ "nofx/mcp/payment"
	_ "nofx/mcp/provider"
	"nofx/provider/nofxos"
	"nofx/store"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// validateStrategyConfig validates strategy configuration and returns warnings
func validateStrategyConfig(config *store.StrategyConfig) []string {
	var warnings []string

	if config.RiskControl.MinCloseConfidence > 0 &&
		config.RiskControl.MinConfidence > 0 &&
		config.RiskControl.MinCloseConfidence < config.RiskControl.MinConfidence {
		warnings = append(warnings, "Early-close confidence is lower than entry confidence. AI may exit positions too aggressively.")
	}

	return warnings
}

// handleEstimateTokens estimates token usage for a strategy config (no auth required, pure computation)
func (s *Server) handleEstimateTokens(c *gin.Context) {
	var req struct {
		Config store.StrategyConfig `json:"config" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	req.Config.ClampLimits()
	estimate := req.Config.EstimateTokens()
	c.JSON(http.StatusOK, estimate)
}

// handlePublicStrategies Get public strategies for strategy market (no auth required)
func (s *Server) handlePublicStrategies(c *gin.Context) {
	strategies, err := s.store.Strategy().ListPublic()
	if err != nil {
		SafeInternalError(c, "Failed to get public strategies", err)
		return
	}

	// Convert to frontend format with visibility control
	result := make([]gin.H, 0, len(strategies))
	for _, st := range strategies {
		item := gin.H{
			"id":             st.ID,
			"name":           st.Name,
			"description":    st.Description,
			"author_email":   "", // Will be filled if we have user info
			"is_public":      st.IsPublic,
			"config_visible": st.ConfigVisible,
			"created_at":     st.CreatedAt,
			"updated_at":     st.UpdatedAt,
		}

		// Only include config if config_visible is true
		if st.ConfigVisible {
			config, parseErr := st.ParseConfig()
			if parseErr == nil {
				item["config"] = config
			}
		}

		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"strategies": result,
	})
}

// handleGetStrategies Get strategy list
func (s *Server) handleGetStrategies(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategies, err := s.store.Strategy().List(userID)
	if err != nil {
		SafeInternalError(c, "Failed to get strategy list", err)
		return
	}

	// Convert to frontend format
	result := make([]gin.H, 0, len(strategies))
	for _, st := range strategies {
		config, parseErr := st.ParseConfig()
		if parseErr != nil {
			SafeInternalError(c, "Failed to parse strategy config", parseErr)
			return
		}

		result = append(result, gin.H{
			"id":             st.ID,
			"name":           st.Name,
			"description":    st.Description,
			"is_active":      st.IsActive,
			"is_default":     st.IsDefault,
			"is_public":      st.IsPublic,
			"config_visible": st.ConfigVisible,
			"config":         config,
			"created_at":     st.CreatedAt,
			"updated_at":     st.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"strategies": result,
	})
}

// handleGetStrategy Get single strategy
func (s *Server) handleGetStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategy, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}

	config, parseErr := strategy.ParseConfig()
	if parseErr != nil {
		SafeInternalError(c, "Failed to parse strategy config", parseErr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          strategy.ID,
		"name":        strategy.Name,
		"description": strategy.Description,
		"is_active":   strategy.IsActive,
		"is_default":  strategy.IsDefault,
		"config":      config,
		"created_at":  strategy.CreatedAt,
		"updated_at":  strategy.UpdatedAt,
	})
}

// handleCreateStrategy Create strategy.
// If "config" is omitted from the request body, the system default config is used automatically.
func (s *Server) handleCreateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Name        string          `json:"name" binding:"required"`
		Description string          `json:"description"`
		Lang        string          `json:"lang"`   // "zh" or "en", used when config is omitted
		Config      json.RawMessage `json:"config"` // optional; partial config is merged with defaults
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	lang := req.Lang
	if lang == "" {
		lang = "zh"
	}
	config, err := store.ParseStrategyConfigWithDefaults(req.Config, lang)
	if err != nil {
		SafeBadRequest(c, "Invalid config JSON")
		return
	}

	// Serialize configuration
	configJSON, err := json.Marshal(config)
	if err != nil {
		SafeInternalError(c, "Serialize configuration", err)
		return
	}

	strategy := &store.Strategy{
		ID:          uuid.New().String(),
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    false,
		IsDefault:   false,
		Config:      string(configJSON),
	}

	if err := s.store.Strategy().Create(strategy); err != nil {
		SafeInternalError(c, "Failed to create strategy", err)
		return
	}

	// Validate configuration and collect warnings
	warnings := validateStrategyConfig(config)

	response := gin.H{
		"id":      strategy.ID,
		"message": "Strategy created successfully",
	}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}

	c.JSON(http.StatusOK, response)
}

// handleUpdateStrategy Update strategy.
// The incoming config is merged with the existing one: top-level sections present in the
// request overwrite the corresponding existing sections; absent sections are preserved.
// This prevents partial updates from zeroing out unmentioned fields.
func (s *Server) handleUpdateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Check if it's a system default strategy
	existing, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	if existing.IsDefault {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot modify system default strategy"})
		return
	}

	var req struct {
		Name          string          `json:"name"`
		Description   string          `json:"description"`
		Config        json.RawMessage `json:"config"` // raw JSON so we can merge
		IsPublic      bool            `json:"is_public"`
		ConfigVisible bool            `json:"config_visible"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	// Start with the existing config as base, including defaults for older or
	// externally-created partial configs.
	mergedConfig, err := existing.ParseConfig()
	if err != nil {
		mergedConfig = &store.StrategyConfig{}
	}

	// Apply incoming config on top: top-level sections present in the request overwrite
	// their corresponding existing section; absent sections remain unchanged.
	if len(req.Config) > 0 && string(req.Config) != "null" {
		if err := json.Unmarshal(req.Config, mergedConfig); err != nil {
			SafeBadRequest(c, "Invalid config JSON")
			return
		}
	}
	mergedJSON, err := json.Marshal(mergedConfig)
	if err != nil {
		SafeInternalError(c, "Serialize configuration", err)
		return
	}
	defaultedConfig, err := store.ParseStrategyConfigWithDefaults(mergedJSON, mergedConfig.Language)
	if err != nil {
		SafeBadRequest(c, "Invalid config JSON")
		return
	}

	// Preserve existing name/description when not supplied
	name := req.Name
	if name == "" {
		name = existing.Name
	}
	description := req.Description
	if description == "" {
		description = existing.Description
	}

	configJSON, err := json.Marshal(defaultedConfig)
	if err != nil {
		SafeInternalError(c, "Serialize configuration", err)
		return
	}

	strategy := &store.Strategy{
		ID:            strategyID,
		UserID:        userID,
		Name:          name,
		Description:   description,
		Config:        string(configJSON),
		IsPublic:      req.IsPublic,
		ConfigVisible: req.ConfigVisible,
	}

	if err := s.store.Strategy().Update(strategy); err != nil {
		SafeInternalError(c, "Failed to update strategy", err)
		return
	}

	// Token overflow check: block save if all models exceed context limits
	if defaultedConfig.StrategyType == "" || defaultedConfig.StrategyType == "ai_trading" {
		estimate := defaultedConfig.EstimateTokens()
		allExceed := true
		for _, ml := range estimate.ModelLimits {
			if ml.UsagePct <= 100 {
				allExceed = false
				break
			}
		}
		if allExceed && len(estimate.ModelLimits) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":          fmt.Sprintf("Estimated %d tokens exceeds all known model context limits. Reduce coins, timeframes, or K-line count.", estimate.Total),
				"token_estimate": estimate,
			})
			return
		}
	}

	// Validate merged configuration and collect warnings
	warnings := validateStrategyConfig(defaultedConfig)

	response := gin.H{"message": "Strategy updated successfully"}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}

	c.JSON(http.StatusOK, response)
}

// handleDeleteStrategy Delete strategy
func (s *Server) handleDeleteStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := s.store.Strategy().Delete(userID, strategyID); err != nil {
		errText := strings.ToLower(err.Error())
		switch {
		case strings.Contains(errText, "in use"):
			SafeErrorWithDetails(c, http.StatusBadRequest, "Strategy is being used by traders", "strategy.delete.in_use", nil, err)
		case strings.Contains(errText, "system default"):
			SafeErrorWithDetails(c, http.StatusBadRequest, "System default strategy cannot be deleted", "strategy.delete.default", nil, err)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": SanitizeError(err, "Failed to delete strategy")})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Strategy deleted successfully"})
}

// handleActivateStrategy Activate strategy
func (s *Server) handleActivateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	strategyID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := s.store.Strategy().SetActive(userID, strategyID); err != nil {
		SafeInternalError(c, "Failed to activate strategy", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Strategy activated successfully"})
}

// handleDuplicateStrategy Duplicate strategy
func (s *Server) handleDuplicateStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	sourceID := c.Param("id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	newID := uuid.New().String()
	if err := s.store.Strategy().Duplicate(userID, sourceID, newID, req.Name); err != nil {
		SafeInternalError(c, "Failed to duplicate strategy", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      newID,
		"message": "Strategy duplicated successfully",
	})
}

// handleGetActiveStrategy Get currently active strategy
func (s *Server) handleGetActiveStrategy(c *gin.Context) {
	userID := c.GetString("user_id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategy, err := s.store.Strategy().GetActive(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No active strategy"})
		return
	}

	config, parseErr := strategy.ParseConfig()
	if parseErr != nil {
		SafeInternalError(c, "Failed to parse strategy config", parseErr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          strategy.ID,
		"name":        strategy.Name,
		"description": strategy.Description,
		"is_active":   strategy.IsActive,
		"is_default":  strategy.IsDefault,
		"config":      config,
		"created_at":  strategy.CreatedAt,
		"updated_at":  strategy.UpdatedAt,
	})
}

// handleGetDefaultStrategyConfig Get default strategy configuration template
func (s *Server) handleGetDefaultStrategyConfig(c *gin.Context) {
	// Get language from query parameter, default to "en"
	lang := c.Query("lang")
	if lang != "zh" {
		lang = "en"
	}

	// Return default configuration with i18n support
	defaultConfig := store.GetDefaultStrategyConfig(lang)
	c.JSON(http.StatusOK, defaultConfig)
}

// handlePreviewPrompt previews the structured strategy flow instead of the old prompt.
func (s *Server) handlePreviewPrompt(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	config, err := parsePreviewStrategyConfig(c)
	if err != nil {
		SafeBadRequest(c, err.Error())
		return
	}
	config.ClampLimits()
	endpoint := c.FullPath()
	c.JSON(http.StatusOK, gin.H{
		"endpoint":             endpoint,
		"legacy_compatible":    endpoint == "/api/strategies/preview-prompt",
		"replacement_endpoint": "/api/strategies/preview-flow",
		"flow":                 "market_data -> factor_snapshot -> signal_engine -> llm_review -> risk_gate",
		"strategy_mode":        config.StrategyMode,
		"compiled_rules":       config.CompiledRules,
		"compiled_rule_count":  len(config.CompiledRules),
		"dependency_check":     strategyDependencyStatus(config),
		"scoring_config":       config.ScoringConfig,
		"resolved_parameters":  config.ResolvedParameters,
		"kline": gin.H{
			"primary_timeframe":    config.Indicators.Klines.PrimaryTimeframe,
			"selected_timeframes":  config.Indicators.Klines.SelectedTimeframes,
			"compute_lookback":     config.Indicators.Klines.ComputeLookback,
			"prompt_display_count": config.Indicators.Klines.PromptDisplayCount,
		},
		"risk_control": config.RiskControl,
		"note":         "Old long prompt preview has been removed on this branch. Natural-language prompts must be compiled into compiled_rules before trading.",
	})
}

func parsePreviewStrategyConfig(c *gin.Context) (*store.StrategyConfig, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body")
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	var envelope struct {
		Config json.RawMessage `json:"config"`
		Lang   string          `json:"lang"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid request JSON")
	}

	rawConfig := body
	if len(envelope.Config) > 0 && string(envelope.Config) != "null" {
		rawConfig = envelope.Config
	}
	lang := envelope.Lang
	if lang == "" {
		lang = "zh"
	}

	config, err := store.ParseStrategyConfigWithDefaults(rawConfig, lang)
	if err != nil {
		return nil, fmt.Errorf("invalid strategy config")
	}
	return config, nil
}

// handleCompileStrategyPrompt compiles a natural-language strategy prompt into deterministic rules.
func (s *Server) handleCompileStrategyPrompt(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		StrategyID      string `json:"strategy_id"`
		StrategyVersion string `json:"strategy_version"`
		Prompt          string `json:"prompt" binding:"required"`
		AIModelID       string `json:"ai_model_id" binding:"required"`
		Persist         bool   `json:"persist"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}
	if req.Persist && req.StrategyID == "" {
		SafeBadRequest(c, "strategy_id is required when persist=true")
		return
	}
	if req.StrategyVersion == "" {
		req.StrategyVersion = time.Now().UTC().Format("20060102150405")
	}

	aiClient, err := s.createAIClientForModel(userID, req.AIModelID)
	if err != nil {
		SafeBadRequest(c, err.Error())
		return
	}

	compiler := kernel.NewLLMStrategyCompiler(aiClient)
	result, err := compiler.Compile(c.Request.Context(), kernel.StrategyCompileRequest{
		StrategyID:      req.StrategyID,
		StrategyVersion: req.StrategyVersion,
		Prompt:          req.Prompt,
	})
	if err != nil {
		response := gin.H{"error": err.Error()}
		if result != nil {
			response["strategy_mode"] = result.StrategyMode
			response["compiled_rules"] = strategyRulesToStore(result.Rules)
			response["scoring_config"] = scoringStrategyToStore(result.ScoringConfig)
			response["warnings"] = result.Warnings
			response["errors"] = result.Errors
		}
		c.JSON(http.StatusBadRequest, response)
		return
	}

	compiledRules := strategyRulesToStore(result.Rules)
	scoringConfig := scoringStrategyToStore(result.ScoringConfig)
	if req.Persist {
		strategy, err := s.store.Strategy().Get(userID, req.StrategyID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
			return
		}
		if strategy.IsDefault {
			c.JSON(http.StatusForbidden, gin.H{"error": "Cannot modify system default strategy"})
			return
		}
		config, err := strategy.ParseConfig()
		if err != nil {
			SafeInternalError(c, "Failed to parse strategy config", err)
			return
		}
		config.StrategyPrompt = req.Prompt
		config.StrategyMode = result.StrategyMode
		config.CompiledRules = compiledRules
		config.ScoringConfig = scoringConfig
		config.ResolvedParameters.Scoring = scoringConfig
		if err := strategy.SetConfig(config); err != nil {
			SafeInternalError(c, "Serialize configuration", err)
			return
		}
		if err := s.store.Strategy().Update(strategy); err != nil {
			SafeInternalError(c, "Failed to update strategy", err)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"strategy_prompt":     req.Prompt,
		"strategy_mode":       result.StrategyMode,
		"compiled_rules":      compiledRules,
		"scoring_config":      scoringConfig,
		"resolved_parameters": store.ResolvedStrategyParameters{Scoring: scoringConfig},
		"warnings":            result.Warnings,
		"errors":              result.Errors,
		"persisted":           req.Persist,
	})
}

// handleEvolveStrategy creates a versioned improvement proposal. It never
// persists or activates the proposal; applying it must be a separate action.
func (s *Server) handleEvolveStrategy(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	strategyID := c.Param("id")
	strategy, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	config, err := strategy.ParseConfig()
	if err != nil {
		SafeInternalError(c, "Failed to parse strategy config", err)
		return
	}

	var req struct {
		AIModelID     string                 `json:"ai_model_id" binding:"required"`
		Trigger       string                 `json:"trigger" binding:"required"`
		BaseVersion   string                 `json:"base_version"`
		Notes         string                 `json:"notes"`
		Performance   map[string]interface{} `json:"performance"`
		MarketContext *kernel.MarketContext  `json:"market_context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}
	if req.Trigger != "manual" {
		SafeBadRequest(c, "Strategy evolution must be triggered manually by the user")
		return
	}

	aiClient, err := s.createAIClientForModel(userID, req.AIModelID)
	if err != nil {
		SafeBadRequest(c, err.Error())
		return
	}

	evolver := kernel.NewLLMStrategyEvolver(aiClient)
	performance := mergeEvolutionPerformance(req.Performance, s.buildStrategyEvolutionPerformance(userID, strategyID))
	proposal, err := evolver.Propose(c.Request.Context(), kernel.StrategyEvolutionRequest{
		StrategyID:    strategyID,
		BaseVersion:   req.BaseVersion,
		Trigger:       req.Trigger,
		CurrentConfig: config,
		MarketContext: req.MarketContext,
		Performance:   performance,
		Notes:         req.Notes,
		RequestedAt:   time.Now().UTC(),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"proposal": proposal,
		"applied":  false,
		"note":     "Proposal only. It has not been saved, activated, or applied to live trading.",
	})
}

// handleStrategyTestRun runs the new structured strategy flow for preview/testing.
func (s *Server) handleStrategyTestRun(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Config        store.StrategyConfig `json:"config" binding:"required"`
		PromptVariant string               `json:"prompt_variant"`
		AIModelID     string               `json:"ai_model_id"`
		RunRealAI     bool                 `json:"run_real_ai"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		SafeBadRequest(c, "Invalid request parameters")
		return
	}

	if req.PromptVariant == "" {
		req.PromptVariant = "balanced"
	}
	req.Config.ClampLimits()

	engine := kernel.NewStrategyEngine(&req.Config, s.getClaw402WalletKey(userID))
	candidates, err := engine.GetCandidateCoins()
	if err != nil {
		logger.Errorf("[API Error] Failed to get candidate coins: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": kernel.DescribeExternalDataError(err)})
		return
	}

	externalContext := &kernel.Context{}
	externalDataWarnings := []string{}
	externalDataTimeout := strategyTestExternalDataTimeout()
	externalDataCtx, cancelExternalData := context.WithTimeout(c.Request.Context(), externalDataTimeout)
	defer cancelExternalData()
	symbols := make([]string, 0, len(candidates))
	for _, coin := range candidates {
		symbols = append(symbols, coin.Symbol)
	}
	var externalWarningsMu sync.Mutex
	var externalWG sync.WaitGroup
	appendExternalWarning := func(message string) {
		externalWarningsMu.Lock()
		externalDataWarnings = append(externalDataWarnings, message)
		externalWarningsMu.Unlock()
	}
	if req.Config.Indicators.EnableQuantData {
		externalWG.Add(1)
		go func() {
			defer externalWG.Done()
			data := engine.FetchQuantDataBatchContext(externalDataCtx, symbols)
			externalContext.QuantDataMap = data
			if len(symbols) > 0 && len(data) == 0 {
				appendExternalWarning("NofxOS coin-level quant data is enabled but unavailable. Check the configured Claw402 wallet balance/payment channel; derivatives factors may be missing.")
			}
		}()
	}
	if req.Config.Indicators.EnableOIRanking {
		externalWG.Add(1)
		go func() {
			defer externalWG.Done()
			data := engine.FetchOIRankingDataContext(externalDataCtx)
			externalContext.OIRankingData = data
			if !hasOIRankingData(data) {
				appendExternalWarning("NofxOS OI ranking is enabled but unavailable. Check the configured Claw402 wallet balance/payment channel; derivatives scoring may have less evidence.")
			}
		}()
	}
	if req.Config.Indicators.EnableNetFlowRanking {
		externalWG.Add(1)
		go func() {
			defer externalWG.Done()
			data := engine.FetchNetFlowRankingDataContext(externalDataCtx)
			externalContext.NetFlowRankingData = data
			if !hasNetFlowRankingData(data) {
				appendExternalWarning("NofxOS NetFlow ranking is enabled but unavailable. Check the configured Claw402 wallet balance/payment channel; derivatives scoring may have less evidence.")
			}
		}()
	}
	if req.Config.Indicators.EnablePriceRanking {
		externalWG.Add(1)
		go func() {
			defer externalWG.Done()
			data := engine.FetchPriceRankingDataContext(externalDataCtx)
			externalContext.PriceRankingData = data
			if !hasPriceRankingData(data) {
				appendExternalWarning("NofxOS price ranking is enabled but unavailable. Check the configured Claw402 wallet balance/payment channel; price ranking factors may be missing.")
			}
		}()
	}
	externalWG.Wait()
	if externalDataCtx.Err() != nil {
		externalDataWarnings = append(externalDataWarnings, fmt.Sprintf("External data fetch stopped after %s: %s", externalDataTimeout, kernel.DescribeExternalDataError(externalDataCtx.Err())))
	}

	timeframes := req.Config.Indicators.Klines.SelectedTimeframes
	primaryTimeframe := req.Config.Indicators.Klines.PrimaryTimeframe
	if len(timeframes) == 0 {
		if primaryTimeframe != "" {
			timeframes = append(timeframes, primaryTimeframe)
		} else {
			timeframes = append(timeframes, "3m")
		}
		if req.Config.Indicators.Klines.LongerTimeframe != "" {
			timeframes = append(timeframes, req.Config.Indicators.Klines.LongerTimeframe)
		}
	}
	if primaryTimeframe == "" {
		primaryTimeframe = timeframes[0]
	}
	displayCount := req.Config.Indicators.Klines.PromptDisplayCount
	if displayCount <= 0 {
		displayCount = req.Config.Indicators.Klines.PrimaryCount
	}
	computeLookback := req.Config.Indicators.Klines.ComputeLookback
	if computeLookback < displayCount {
		computeLookback = displayCount
	}

	marketDataMap := make(map[string]*market.Data)
	factorSnapshots := make(map[string]*market.FactorSnapshot)
	marketDataWarnings := []string{}
	marketDataTimeout := strategyTestMarketDataTimeout()
	asOf := time.Now().UTC()
	indicatorRequest := kernel.IndicatorRequestFromStrategyConfig(&req.Config)
	structureRequest := kernel.StructureRequestFromStrategyConfig(&req.Config)
	var marketMu sync.Mutex
	var marketWG sync.WaitGroup
	for _, coin := range candidates {
		symbol := coin.Symbol
		marketWG.Add(1)
		go func() {
			defer marketWG.Done()
			marketDataCtx, cancelMarketData := context.WithTimeout(c.Request.Context(), marketDataTimeout)
			defer cancelMarketData()
			data, err := market.GetWithTimeframesWindowContextWithExchange(marketDataCtx, symbol, timeframes, primaryTimeframe, displayCount, computeLookback, req.Config.Indicators.Klines.IncludeOpenBar, "paper")
			if err != nil {
				logger.Infof("Failed to get market data for %s: %v", symbol, err)
				marketMu.Lock()
				marketDataWarnings = append(marketDataWarnings, fmt.Sprintf("%s market data unavailable: %v", symbol, err))
				marketMu.Unlock()
				return
			}
			snapshot, err := market.BuildFactorSnapshotFromDataWithRequests(
				data,
				asOf,
				indicatorRequest,
				structureRequest,
			)
			if err != nil {
				logger.Infof("Failed to build factor snapshot for %s: %v", symbol, err)
				return
			}
			marketMu.Lock()
			marketDataMap[symbol] = data
			factorSnapshots[symbol] = snapshot
			marketMu.Unlock()
		}()
	}
	marketWG.Wait()
	kernel.EnrichExternalFactors(externalContext, factorSnapshots, asOf)
	signalPreview, previewErr := kernel.PreviewStrategySignals(&req.Config, candidates, factorSnapshots, asOf)
	if previewErr != nil {
		logger.Infof("Failed to preview strategy signals: %v", previewErr)
		signalPreview = &kernel.StrategySignalPreview{}
	}

	testContext := &kernel.Context{
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		RuntimeMinutes: 0,
		CallCount:      1,
		Exchange:       "paper",
		Account: kernel.AccountInfo{
			TotalEquity:      1000.0,
			AvailableBalance: 1000.0,
		},
		Positions:          []kernel.PositionInfo{},
		CandidateCoins:     candidates,
		PromptVariant:      req.PromptVariant,
		MarketDataMap:      marketDataMap,
		QuantDataMap:       externalContext.QuantDataMap,
		OIRankingData:      externalContext.OIRankingData,
		NetFlowRankingData: externalContext.NetFlowRankingData,
		PriceRankingData:   externalContext.PriceRankingData,
	}

	if !req.RunRealAI || req.AIModelID == "" {
		c.JSON(http.StatusOK, gin.H{
			"flow":                   "market_data -> factor_snapshot -> signal_engine -> llm_review -> risk_gate",
			"strategy_mode":          req.Config.StrategyMode,
			"candidate_count":        len(candidates),
			"candidates":             candidates,
			"compiled_rules":         req.Config.CompiledRules,
			"compiled_rule_count":    len(req.Config.CompiledRules),
			"dependency_check":       strategyDependencyStatus(&req.Config),
			"scoring_config":         req.Config.ScoringConfig,
			"resolved_parameters":    req.Config.ResolvedParameters,
			"market_context":         buildPreviewMarketContext(factorSnapshots),
			"factor_snapshot_count":  len(factorSnapshots),
			"factor_snapshots":       factorSnapshots,
			"signal_count":           len(signalPreview.Signals),
			"signals":                signalPreview.Signals,
			"rule_evaluations":       signalPreview.RuleEvaluations,
			"scoring_evaluations":    signalPreview.ScoringEvaluations,
			"setup_evaluations":      signalPreview.SetupEvaluations,
			"external_data_warnings": externalDataWarnings,
			"market_data_warnings":   marketDataWarnings,
			"signal_preview_error":   errorString(previewErr),
			"note":                   "Real AI review was not run. Provide ai_model_id and run_real_ai=true to execute the full structured flow.",
		})
		return
	}

	aiClient, err := s.createAIClientForModel(userID, req.AIModelID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"flow":                   "market_data -> factor_snapshot -> signal_engine -> llm_review -> risk_gate",
			"strategy_mode":          req.Config.StrategyMode,
			"candidate_count":        len(candidates),
			"candidates":             candidates,
			"compiled_rules":         req.Config.CompiledRules,
			"dependency_check":       strategyDependencyStatus(&req.Config),
			"scoring_config":         req.Config.ScoringConfig,
			"factor_snapshot_count":  len(factorSnapshots),
			"signal_count":           len(signalPreview.Signals),
			"signals":                signalPreview.Signals,
			"rule_evaluations":       signalPreview.RuleEvaluations,
			"scoring_evaluations":    signalPreview.ScoringEvaluations,
			"setup_evaluations":      signalPreview.SetupEvaluations,
			"external_data_warnings": externalDataWarnings,
			"market_data_warnings":   marketDataWarnings,
			"signal_preview_error":   errorString(previewErr),
			"ai_error":               err.Error(),
			"note":                   "AI client setup failed",
		})
		return
	}

	decision, err := kernel.GetFullDecisionWithStrategy(testContext, aiClient, engine, req.PromptVariant)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"flow":                   "market_data -> factor_snapshot -> signal_engine -> llm_review -> risk_gate",
			"strategy_mode":          req.Config.StrategyMode,
			"candidate_count":        len(candidates),
			"candidates":             candidates,
			"compiled_rules":         req.Config.CompiledRules,
			"dependency_check":       strategyDependencyStatus(&req.Config),
			"scoring_config":         req.Config.ScoringConfig,
			"factor_snapshot_count":  len(factorSnapshots),
			"signal_count":           len(signalPreview.Signals),
			"signals":                signalPreview.Signals,
			"rule_evaluations":       signalPreview.RuleEvaluations,
			"scoring_evaluations":    signalPreview.ScoringEvaluations,
			"external_data_warnings": externalDataWarnings,
			"market_data_warnings":   marketDataWarnings,
			"signal_preview_error":   errorString(previewErr),
			"ai_error":               err.Error(),
			"note":                   "Structured strategy run failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"flow":                   "market_data -> factor_snapshot -> signal_engine -> llm_review -> risk_gate",
		"strategy_mode":          req.Config.StrategyMode,
		"candidate_count":        len(candidates),
		"candidates":             candidates,
		"compiled_rules":         req.Config.CompiledRules,
		"dependency_check":       strategyDependencyStatus(&req.Config),
		"scoring_config":         req.Config.ScoringConfig,
		"resolved_parameters":    req.Config.ResolvedParameters,
		"factor_snapshot_count":  len(factorSnapshots),
		"signal_count":           len(signalPreview.Signals),
		"signals":                signalPreview.Signals,
		"rule_evaluations":       signalPreview.RuleEvaluations,
		"scoring_evaluations":    signalPreview.ScoringEvaluations,
		"setup_evaluations":      signalPreview.SetupEvaluations,
		"external_data_warnings": externalDataWarnings,
		"market_data_warnings":   marketDataWarnings,
		"signal_preview_error":   errorString(previewErr),
		"market_context":         decision.MarketContext,
		"decision":               decision,
		"note":                   "Structured strategy run completed",
	})
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func strategyTestExternalDataTimeout() time.Duration {
	const defaultTimeout = 12 * time.Second
	raw := strings.TrimSpace(os.Getenv("STRATEGY_TEST_EXTERNAL_DATA_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

func strategyTestMarketDataTimeout() time.Duration {
	const defaultTimeout = 15 * time.Second
	raw := strings.TrimSpace(os.Getenv("STRATEGY_TEST_MARKET_DATA_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(seconds) * time.Second
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

func buildPreviewMarketContext(factorSnapshots map[string]*market.FactorSnapshot) *kernel.MarketContext {
	marketContext, err := kernel.NewDefaultMarketContextEngine().Build(context.Background(), kernel.MarketContextRequest{
		FactorSnapshot: factorSnapshots,
		Now:            time.Now().UTC(),
	})
	if err != nil {
		return nil
	}
	return marketContext
}

func strategyRulesToStore(rules []kernel.StrategyRule) []store.CompiledStrategyRule {
	out := make([]store.CompiledStrategyRule, 0, len(rules))
	for _, rule := range rules {
		conditions := make([]store.CompiledRuleCondition, 0, len(rule.Conditions))
		for _, condition := range rule.Conditions {
			conditions = append(conditions, store.CompiledRuleCondition{
				Left:     ruleOperandToStore(condition.Left),
				Operator: condition.Operator,
				Right:    ruleOperandToStore(condition.Right),
			})
		}
		out = append(out, store.CompiledStrategyRule{
			ID:          rule.ID,
			Version:     rule.Version,
			Description: rule.Description,
			Symbols:     rule.Symbols,
			Timeframe:   rule.Timeframe,
			Conditions:  conditions,
			Action:      rule.Action,
			Execution: store.CompiledRuleExecution{
				Leverage:        rule.Execution.Leverage,
				PositionSizeUSD: rule.Execution.PositionSizeUSD,
				StopLossPct:     rule.Execution.StopLossPct,
				TakeProfitPct:   rule.Execution.TakeProfitPct,
				Confidence:      rule.Execution.Confidence,
			},
			Enabled: rule.Enabled,
		})
	}
	return out
}

func ruleOperandToStore(operand kernel.RuleOperand) store.CompiledRuleOperand {
	return store.CompiledRuleOperand{
		Kind:      operand.Kind,
		Name:      operand.Name,
		Timeframe: operand.Timeframe,
		Period:    operand.Period,
		Field:     operand.Field,
		Value:     operand.Value,
	}
}

func scoringStrategyToStore(scoring *kernel.ScoringStrategy) *store.ScoringStrategyConfig {
	if scoring == nil {
		return nil
	}
	return &store.ScoringStrategyConfig{
		Enabled:                 scoring.Enabled,
		SelectedFactors:         append([]string(nil), scoring.SelectedFactors...),
		FactorWeights:           copyAPIFloatMap(scoring.FactorWeights),
		LongThreshold:           scoring.LongThreshold,
		ShortThreshold:          scoring.ShortThreshold,
		MinAvailableWeightRatio: scoring.MinAvailableWeightRatio,
		MinConfidence:           scoring.MinConfidence,
		Timeframe:               scoring.Timeframe,
		Symbols:                 append([]string(nil), scoring.Symbols...),
		Execution: store.CompiledRuleExecution{
			Leverage:        scoring.Execution.Leverage,
			PositionSizeUSD: scoring.Execution.PositionSizeUSD,
			StopLossPct:     scoring.Execution.StopLossPct,
			TakeProfitPct:   scoring.Execution.TakeProfitPct,
			Confidence:      scoring.Execution.Confidence,
		},
	}
}

func copyAPIFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Server) createAIClientForModel(userID, modelID string) (mcp.AIClient, error) {
	// Get AI model configuration
	model, err := s.store.AIModel().Get(userID, modelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI model: %w", err)
	}

	if !model.Enabled {
		return nil, fmt.Errorf("AI model %s is not enabled", model.Name)
	}

	apiKey := strings.TrimSpace(string(model.APIKey))
	if apiKey == "" {
		return nil, fmt.Errorf("AI model %s is missing API Key", model.Name)
	}
	if s.isEncryptedStorageValue(apiKey) {
		return nil, fmt.Errorf("AI model %s API Key cannot be decrypted with the current DATA_ENCRYPTION_KEY; please re-save this model API key in Settings > Model Config", model.Name)
	}

	// Create AI client via registry
	provider := model.Provider
	if provider == "claw402" {
		return nil, fmt.Errorf("claw402 is a payment/data channel and cannot be used as the LLM reasoning model")
	}

	aiClient := mcp.NewAIClientByProvider(provider)
	if aiClient == nil {
		aiClient = mcp.NewClient()
	}

	aiClient.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)

	return aiClient, nil
}
