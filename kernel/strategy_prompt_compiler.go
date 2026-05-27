package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"nofx/mcp"
	"strings"
)

type LLMStrategyCompiler struct {
	client mcp.AIClient
}

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

	type response struct {
		text string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		text, err := c.client.CallWithMessages(systemPrompt, userPrompt)
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
		if len(result.Errors) > 0 {
			return result, fmt.Errorf("strategy compile returned errors: %s", strings.Join(result.Errors, "; "))
		}
		if err := validateCompiledStrategy(result); err != nil {
			return result, err
		}
		return result, nil
	}
}

func buildStrategyCompilerSystemPrompt() string {
	return strings.TrimSpace(`
You compile user trading strategy prompts into deterministic executable rules.

Output only JSON inside <compiled_strategy> tags:
<compiled_strategy>
{
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
        "position_size_usd": 100,
        "stop_loss_pct": 2,
        "take_profit_pct": 6,
        "confidence": 70
      },
      "enabled": true
    }
  ],
  "warnings": [],
  "errors": []
}
</compiled_strategy>

Rules:
- Do not calculate indicators.
- Do not invent unavailable data sources.
- Convert user intent into indicator/external_factor/structure/literal operands.
- Supported actions: open_long, open_short, close_long, close_short, wait.
- Open actions must include leverage, position_size_usd, stop_loss_pct, take_profit_pct, confidence.
- Close and wait actions must include confidence.
- If the prompt lacks required execution parameters, put a clear message in errors instead of guessing.
- If Fibonacci or support/resistance is requested, use a structure operand and state any unresolved anchor requirement in errors.
`)
}

func buildStrategyCompilerUserPrompt(req StrategyCompileRequest) (string, error) {
	payload := struct {
		StrategyID      string `json:"strategy_id,omitempty"`
		StrategyVersion string `json:"strategy_version,omitempty"`
		Prompt          string `json:"prompt"`
	}{
		StrategyID:      req.StrategyID,
		StrategyVersion: req.StrategyVersion,
		Prompt:          req.Prompt,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal strategy compile payload: %w", err)
	}
	return string(data), nil
}

func parseStrategyCompileResponse(text string) (*StrategyCompileResult, error) {
	body := strings.TrimSpace(text)
	if start := strings.Index(body, "<compiled_strategy>"); start >= 0 {
		body = body[start+len("<compiled_strategy>"):]
	}
	if end := strings.Index(body, "</compiled_strategy>"); end >= 0 {
		body = body[:end]
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```json")
		body = strings.TrimPrefix(body, "```")
		body = strings.TrimSuffix(body, "```")
		body = strings.TrimSpace(body)
	}

	var result StrategyCompileResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("parse strategy compile response: %w", err)
	}
	return &result, nil
}

func validateCompiledStrategy(result *StrategyCompileResult) error {
	if result == nil {
		return fmt.Errorf("compiled strategy result is nil")
	}
	if len(result.Rules) == 0 {
		return fmt.Errorf("compiled strategy contains no executable rules")
	}
	for i := range result.Rules {
		if err := validateCompiledRule(result.Rules[i]); err != nil {
			return err
		}
	}
	return nil
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
	case "literal":
		return nil
	case "indicator", "external_factor", "structure":
		if strings.TrimSpace(operand.Name) == "" {
			return fmt.Errorf("rule %s condition %d %s operand missing name", ruleID, conditionIndex, side)
		}
	default:
		return fmt.Errorf("rule %s condition %d %s operand has unsupported kind %q", ruleID, conditionIndex, side, operand.Kind)
	}
	return nil
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
