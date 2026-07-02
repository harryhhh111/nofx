package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"nofx/mcp"
	"strings"
	"time"
)

type LLMStrategyCompiler struct {
	client mcp.AIClient
}

const strategyCompileMaxTokens = 12000

func NewLLMStrategyCompiler(client mcp.AIClient) *LLMStrategyCompiler {
	return &LLMStrategyCompiler{client: client}
}

func (c *LLMStrategyCompiler) Compile(ctx context.Context, req StrategyCompileRequest) (*StrategyCompileResult, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("strategy compiler requires an AI client")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("strategy prompt is required")
	}

	systemPrompt := buildStrategyCompilerSystemPrompt()
	userPrompt, err := buildStrategyCompilerUserPrompt(req)
	if err != nil {
		return nil, err
	}

	reqBody, err := buildStrategyCompileLLMRequest(ctx, c.client, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}
	type response struct {
		text string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		text, err := c.client.CallWithRequest(reqBody)
		done <- response{text: text, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-done:
		if resp.err != nil {
			return nil, fmt.Errorf("strategy compile call failed: %w", resp.err)
		}
		result, err := parseStrategyCompileResponse(resp.text)
		if err != nil {
			return nil, err
		}
		normalizeCompiledScoringConfig(result)
		if len(result.Errors) > 0 {
			return result, fmt.Errorf("strategy compile returned errors: %s", strings.Join(result.Errors, "; "))
		}
		if err := validateCompiledStrategy(result); err != nil {
			return result, err
		}
		return result, nil
	}
}

func buildStrategyCompileLLMRequest(ctx context.Context, client mcp.AIClient, systemPrompt, userPrompt string) (*mcp.Request, error) {
	if err := ensureStructuredOutputsSupported(ctx, client); err != nil {
		return nil, err
	}
	reqBody := &mcp.Request{
		Messages: []mcp.Message{
			mcp.NewSystemMessage(systemPrompt),
			mcp.NewUserMessage(userPrompt),
		},
	}
	if strategyCompileUsesResponseFormat(client) {
		reqBody.ResponseFormat = strategyCompileResponseFormat()
	}
	maxTokens := strategyCompileMaxTokens
	temperature := 0.0
	reqBody.MaxTokens = &maxTokens
	reqBody.Temperature = &temperature
	if base, ok := client.(mcp.ClientEmbedder); ok && isOpenRouterBaseURL(base.BaseClient().BaseURL) {
		reqBody.Provider = map[string]any{"require_parameters": true}
	}
	return reqBody, nil
}

func strategyCompileUsesResponseFormat(client mcp.AIClient) bool {
	base := strategyCompileBaseClient(client)
	if base == nil {
		return true
	}
	if base.Provider == mcp.ProviderDeepSeek && !isOpenRouterBaseURL(base.BaseURL) {
		return false
	}
	return true
}

func strategyCompileBaseClient(client mcp.AIClient) *mcp.Client {
	embedder, ok := client.(mcp.ClientEmbedder)
	if !ok {
		return nil
	}
	return embedder.BaseClient()
}

func ensureStructuredOutputsSupported(ctx context.Context, client mcp.AIClient) error {
	base := strategyCompileBaseClient(client)
	if base == nil || !isOpenRouterBaseURL(base.BaseURL) {
		return nil
	}
	supported, err := openRouterModelSupportsStructuredOutputs(ctx, base)
	if err != nil {
		return fmt.Errorf("check OpenRouter structured output support for model %s: %w", base.Model, err)
	}
	if !supported {
		return fmt.Errorf("AI model %s on OpenRouter does not advertise structured_outputs support; choose a model that supports JSON Schema structured outputs before compiling a trading strategy", base.Model)
	}
	return nil
}

func isOpenRouterBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.Contains(strings.ToLower(raw), "openrouter.ai")
	}
	return strings.Contains(strings.ToLower(u.Host), "openrouter.ai")
}

func openRouterModelSupportsStructuredOutputs(ctx context.Context, base *mcp.Client) (bool, error) {
	endpoint := strings.TrimRight(base.BaseURL, "/") + "/models"
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "GET", endpoint, nil)
	if err != nil {
		return false, err
	}
	if base.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", base.APIKey))
	}
	httpClient := base.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("OpenRouter models API returned status %d: %s", resp.StatusCode, previewText(string(body), 240))
	}
	var payload struct {
		Data []struct {
			ID                  string   `json:"id"`
			CanonicalSlug       string   `json:"canonical_slug"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, err
	}
	for _, model := range payload.Data {
		if model.ID != base.Model && model.CanonicalSlug != base.Model {
			continue
		}
		for _, param := range model.SupportedParameters {
			if param == "structured_outputs" {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("model %s not found in OpenRouter models API", base.Model)
}

func buildStrategyCompilerSystemPrompt() string {
	return strings.TrimSpace(fmt.Sprintf(`
	You compile user trading strategy prompts into deterministic executable rules.

	Output only JSON matching the provided JSON Schema. Do not wrap it in markdown, XML tags, or prose.
	{
	  "strategy_mode": "rule",
  "rules": [
    {
      "id": "stable_short_id",
      "version": "string",
      "description": "brief rule description",
      "symbols": ["BTCUSDT"],
      "timeframe": "5m",
      "conditions": [
        {
          "left": {"kind":"indicator","name":"ema","timeframe":"5m","period":20},
          "operator": ">",
          "right": {"kind":"indicator","name":"ema","timeframe":"5m","period":50}
        }
      ],
      "action": "open_long",
      "execution": {
        "leverage": 3,
        "position_size_usd": 12,
        "stop_loss_pct": 2,
        "take_profit_pct": 6,
        "confidence": 70
      },
      "enabled": true
    }
  ],
  "scoring_config": null,
	  "warnings": [],
	  "errors": []
	}

	Rules:
- Do not calculate indicators.
- Do not invent unavailable data sources.
- Convert user intent into indicator/external_factor/structure/literal operands.
- If the user only selects indicators/factors but gives no exact trigger conditions, output strategy_mode="scoring" as the deterministic structure-setup mode and provide scoring_config only as evidence-filter settings.
- If both exact rules and factor scoring are useful, output strategy_mode="hybrid".
- Supported actions: open_long, open_short, close_long, close_short, wait.
- Open actions must include leverage, position_size_usd, stop_loss_pct, take_profit_pct, confidence.
- position_size_usd is a required minimum-order placeholder for schema compatibility. The program recalculates final notional size from account equity, configured risk per trade, and stop distance; do not invent position size.
- Close and wait actions must include confidence.
- For scoring_config, include enabled, selected_factors, factor_weights, long_threshold, short_threshold, min_available_weight_ratio, min_confidence, timeframe, execution. long_threshold and short_threshold are signed evidence safeguards required by the schema; do not describe them as the primary trade trigger.
- factor_weights must include all supported evidence factors: trend, momentum, structure, derivatives. Selected factors should have proportions from 0 to 1 and should sum to about 1 across selected_factors. Set unselected factor weights to 0. If the user gives percentages, convert them to proportions.
- Evidence scores are signed from -100 to 100. long_threshold must be positive, short_threshold must be negative. Example: long_threshold=60, short_threshold=-60. Never output short_threshold as a positive magnitude.
- min_available_weight_ratio should normally be 0.5 or higher so scoring does not trade from one missing-heavy factor snapshot.
- Supported evidence factors: trend, momentum, structure, derivatives.
- Supported indicator operands: %s.
- If the prompt lacks required execution parameters, put a clear message in errors instead of guessing.
- Fibonacci, support, and resistance are supported by the structure engine. Use structure operands or structure scoring; do not ask for manual anchors unless the user explicitly requires custom anchors.
	`, strings.Join(SupportedIndicatorOperands(), ", ")))
}

func strategyCompileResponseFormat() map[string]any {
	nullableString := map[string]any{"type": []string{"string", "null"}}
	nullableInteger := map[string]any{"type": []string{"integer", "null"}}
	nullableNumber := map[string]any{"type": []string{"number", "null"}}
	stringArray := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
	factorWeightsSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"trend":       map[string]any{"type": "number"},
			"momentum":    map[string]any{"type": "number"},
			"structure":   map[string]any{"type": "number"},
			"derivatives": map[string]any{"type": "number"},
		},
		"required":             []string{"trend", "momentum", "structure", "derivatives"},
		"additionalProperties": false,
	}
	executionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"leverage":          map[string]any{"type": "integer"},
			"position_size_usd": map[string]any{"type": "number"},
			"stop_loss_pct":     map[string]any{"type": "number"},
			"take_profit_pct":   map[string]any{"type": "number"},
			"confidence":        map[string]any{"type": "integer"},
		},
		"required":             []string{"leverage", "position_size_usd", "stop_loss_pct", "take_profit_pct", "confidence"},
		"additionalProperties": false,
	}
	operandSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind":      map[string]any{"type": "string", "enum": []string{"indicator", "external_factor", "structure", "literal", "value"}},
			"name":      nullableString,
			"timeframe": nullableString,
			"period":    nullableInteger,
			"field":     nullableString,
			"value":     nullableNumber,
		},
		"required":             []string{"kind", "name", "timeframe", "period", "field", "value"},
		"additionalProperties": false,
	}
	conditionSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"left":     operandSchema,
			"operator": map[string]any{"type": "string"},
			"right":    operandSchema,
		},
		"required":             []string{"left", "operator", "right"},
		"additionalProperties": false,
	}
	ruleSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"version":     map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"symbols":     stringArray,
			"timeframe":   map[string]any{"type": "string"},
			"conditions":  map[string]any{"type": "array", "items": conditionSchema},
			"action":      map[string]any{"type": "string", "enum": []string{"open_long", "open_short", "close_long", "close_short", "wait"}},
			"execution":   executionSchema,
			"enabled":     map[string]any{"type": "boolean"},
		},
		"required":             []string{"id", "version", "description", "symbols", "timeframe", "conditions", "action", "execution", "enabled"},
		"additionalProperties": false,
	}
	scoringSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled":                    map[string]any{"type": "boolean"},
			"version":                    map[string]any{"type": "string"},
			"selected_factors":           map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"trend", "momentum", "structure", "derivatives"}}},
			"factor_weights":             factorWeightsSchema,
			"long_threshold":             map[string]any{"type": "number"},
			"short_threshold":            map[string]any{"type": "number"},
			"min_available_weight_ratio": map[string]any{"type": "number"},
			"min_confidence":             map[string]any{"type": "integer"},
			"timeframe":                  map[string]any{"type": "string"},
			"symbols":                    stringArray,
			"execution":                  executionSchema,
		},
		"required":             []string{"enabled", "version", "selected_factors", "factor_weights", "long_threshold", "short_threshold", "min_available_weight_ratio", "min_confidence", "timeframe", "symbols", "execution"},
		"additionalProperties": false,
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"strategy_mode":  map[string]any{"type": "string", "enum": []string{"rule", "scoring", "hybrid"}},
			"rules":          map[string]any{"type": "array", "items": ruleSchema},
			"scoring_config": map[string]any{"anyOf": []any{scoringSchema, map[string]any{"type": "null"}}},
			"warnings":       stringArray,
			"errors":         stringArray,
		},
		"required":             []string{"strategy_mode", "rules", "scoring_config", "warnings", "errors"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "strategy_compile_result",
			"strict": true,
			"schema": schema,
		},
	}
}

func buildStrategyCompilerUserPrompt(req StrategyCompileRequest) (string, error) {
	payload := struct {
		StrategyID      string `json:"strategy_id,omitempty"`
		StrategyVersion string `json:"strategy_version,omitempty"`
		Prompt          string `json:"prompt"`
		Context         string `json:"context,omitempty"`
	}{
		StrategyID:      req.StrategyID,
		StrategyVersion: req.StrategyVersion,
		Prompt:          req.Prompt,
		Context:         req.Context,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal strategy compile payload: %w", err)
	}
	return string(data), nil
}

func parseStrategyCompileResponse(text string) (*StrategyCompileResult, error) {
	body, err := extractStrategyCompileJSON(text)
	if err != nil {
		return nil, err
	}

	var result StrategyCompileResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("parse strategy compile response: %w; response preview: %q", err, previewText(text, 240))
	}
	return &result, nil
}

func extractStrategyCompileJSON(text string) (string, error) {
	body := strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if body == "" {
		return "", fmt.Errorf("parse strategy compile response: empty response")
	}
	if json.Valid([]byte(body)) {
		return body, nil
	}
	if unwrapped, ok := unwrapSingleJSONFence(body); ok {
		if json.Valid([]byte(unwrapped)) {
			return unwrapped, nil
		}
		return "", fmt.Errorf("parse strategy compile response: JSON fenced response was incomplete or invalid; response preview: %q", previewText(text, 240))
	}
	return "", fmt.Errorf("parse strategy compile response: structured output was not valid JSON; response preview: %q", previewText(text, 240))
}

func unwrapSingleJSONFence(body string) (string, bool) {
	if !strings.HasPrefix(body, "```") {
		return "", false
	}
	firstLineEnd := strings.IndexByte(body, '\n')
	if firstLineEnd < 0 {
		return "", true
	}
	info := strings.TrimSpace(strings.TrimPrefix(body[:firstLineEnd], "```"))
	if info != "" && info != "json" && info != "JSON" {
		return "", false
	}
	rest := strings.TrimSpace(body[firstLineEnd+1:])
	if !strings.HasSuffix(rest, "```") {
		return "", true
	}
	return strings.TrimSpace(strings.TrimSuffix(rest, "```")), true
}

func previewText(text string, limit int) string {
	body := strings.TrimSpace(text)
	if len(body) <= limit {
		return body
	}
	return body[:limit] + "..."
}

func validateCompiledStrategy(result *StrategyCompileResult) error {
	if result == nil {
		return fmt.Errorf("compiled strategy result is nil")
	}
	if result.StrategyMode == "" {
		result.StrategyMode = "rule"
	}
	if result.StrategyMode != "rule" && result.StrategyMode != "scoring" && result.StrategyMode != "hybrid" {
		return fmt.Errorf("compiled strategy has unsupported strategy_mode %q", result.StrategyMode)
	}
	normalizeCompiledScoringConfig(result)
	if (result.StrategyMode == "rule" || result.StrategyMode == "hybrid") && len(result.Rules) == 0 {
		return fmt.Errorf("compiled strategy contains no executable rules")
	}
	if result.StrategyMode == "scoring" || result.StrategyMode == "hybrid" {
		if result.ScoringConfig == nil || !result.ScoringConfig.Enabled {
			return fmt.Errorf("compiled strategy missing enabled scoring_config")
		}
		if err := validateScoringStrategy(result.ScoringConfig); err != nil {
			return err
		}
	}
	for i := range result.Rules {
		if err := validateCompiledRule(result.Rules[i]); err != nil {
			return err
		}
	}
	return nil
}

func normalizeCompiledScoringConfig(result *StrategyCompileResult) {
	if result == nil || result.ScoringConfig == nil {
		return
	}
	scoring := result.ScoringConfig
	if scoring.ShortThreshold > 0 && scoring.ShortThreshold <= 100 {
		scoring.ShortThreshold = -scoring.ShortThreshold
		result.Warnings = append(result.Warnings, "Normalized evidence short_threshold from positive magnitude to negative signed score.")
	}
	if scoring.LongThreshold < 0 && scoring.LongThreshold >= -100 {
		scoring.LongThreshold = -scoring.LongThreshold
		result.Warnings = append(result.Warnings, "Normalized evidence long_threshold from negative value to positive signed score.")
	}
	normalizeCompiledFactorWeights(scoring)
}

func normalizeCompiledFactorWeights(scoring *ScoringStrategy) {
	if scoring == nil || len(scoring.SelectedFactors) == 0 || len(scoring.FactorWeights) == 0 {
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

func validateCompiledRule(rule StrategyRule) error {
	if strings.TrimSpace(rule.ID) == "" {
		return fmt.Errorf("compiled rule missing id")
	}
	if strings.TrimSpace(rule.Version) == "" {
		return fmt.Errorf("rule %s missing version", rule.ID)
	}
	if !isSupportedRuleAction(rule.Action) {
		return fmt.Errorf("rule %s has unsupported action %q", rule.ID, rule.Action)
	}
	if rule.Action != "wait" && len(rule.Conditions) == 0 {
		return fmt.Errorf("rule %s requires at least one condition", rule.ID)
	}
	for i, condition := range rule.Conditions {
		if err := validateRuleCondition(rule.ID, i, condition); err != nil {
			return err
		}
	}
	if rule.Execution.Confidence <= 0 {
		return fmt.Errorf("rule %s missing execution.confidence", rule.ID)
	}
	switch rule.Action {
	case "open_long", "open_short":
		if rule.Execution.Leverage <= 0 {
			return fmt.Errorf("rule %s missing execution.leverage", rule.ID)
		}
		if rule.Execution.PositionSizeUSD <= 0 {
			return fmt.Errorf("rule %s missing execution.position_size_usd", rule.ID)
		}
		if rule.Execution.StopLossPct <= 0 {
			return fmt.Errorf("rule %s missing execution.stop_loss_pct", rule.ID)
		}
		if rule.Execution.TakeProfitPct <= 0 {
			return fmt.Errorf("rule %s missing execution.take_profit_pct", rule.ID)
		}
	}
	return nil
}

func validateRuleCondition(ruleID string, index int, condition RuleCondition) error {
	if !isSupportedRuleOperator(condition.Operator) {
		return fmt.Errorf("rule %s condition %d has unsupported operator %q", ruleID, index, condition.Operator)
	}
	if err := validateRuleOperand(ruleID, index, "left", condition.Left); err != nil {
		return err
	}
	return validateRuleOperand(ruleID, index, "right", condition.Right)
}

func validateRuleOperand(ruleID string, conditionIndex int, side string, operand RuleOperand) error {
	switch operand.Kind {
	case "literal", "value":
		return nil
	case "indicator", "external_factor", "structure":
		if strings.TrimSpace(operand.Name) == "" {
			return fmt.Errorf("rule %s condition %d %s operand missing name", ruleID, conditionIndex, side)
		}
		if !isSupportedOperandName(operand) {
			return fmt.Errorf("rule %s condition %d %s operand has unsupported %s name %q", ruleID, conditionIndex, side, operand.Kind, operand.Name)
		}
	default:
		return fmt.Errorf("rule %s condition %d %s operand has unsupported kind %q", ruleID, conditionIndex, side, operand.Kind)
	}
	return nil
}

func isSupportedOperandName(operand RuleOperand) bool {
	name := strings.TrimSpace(operand.Name)
	switch operand.Kind {
	case "indicator":
		if IsSupportedIndicatorOperand(name) {
			return true
		}
		return false
	case "structure":
		if name != "fibonacci" && name != "support_resistance" {
			return false
		}
		switch operand.Field {
		case "", "valid", "invalid_price", "support", "resistance",
			"fib_0_236", "fib_0_382", "fib_0_5", "fib_0_618", "fib_0_786":
			return true
		default:
			return false
		}
	case "external_factor":
		if operand.Field != "" && operand.Field != "score" && operand.Field != "value" {
			return false
		}
		if name == "open_interest" || name == "funding_rate" || name == "oi_top_candidate" {
			return true
		}
		for _, prefix := range []string{
			"quant_price_change_", "quant_oi_", "quant_oi_delta_", "quant_netflow_",
			"oi_ranking_", "netflow_ranking_", "price_ranking_",
		} {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

func isSupportedRuleAction(action string) bool {
	switch action {
	case "open_long", "open_short", "close_long", "close_short", "wait":
		return true
	default:
		return false
	}
}

func isSupportedRuleOperator(operator string) bool {
	switch operator {
	case ">", "gt", ">=", "gte", "<", "lt", "<=", "lte", "==", "eq", "!=", "ne":
		return true
	default:
		return false
	}
}
