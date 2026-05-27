package kernel

import "nofx/store"

func rulesFromStrategyConfig(config *store.StrategyConfig) []StrategyRule {
	if config == nil || len(config.CompiledRules) == 0 {
		return nil
	}
	out := make([]StrategyRule, 0, len(config.CompiledRules))
	for _, rule := range config.CompiledRules {
		conditions := make([]RuleCondition, 0, len(rule.Conditions))
		for _, condition := range rule.Conditions {
			conditions = append(conditions, RuleCondition{
				Left:     operandFromStore(condition.Left),
				Operator: condition.Operator,
				Right:    operandFromStore(condition.Right),
			})
		}
		out = append(out, StrategyRule{
			ID:          rule.ID,
			Version:     rule.Version,
			Description: rule.Description,
			Symbols:     rule.Symbols,
			Timeframe:   rule.Timeframe,
			Conditions:  conditions,
			Action:      rule.Action,
			Execution:   executionFromStore(rule.Execution),
			Enabled:     rule.Enabled,
		})
	}
	return out
}

func executionFromStore(execution store.CompiledRuleExecution) RuleExecution {
	return RuleExecution{
		Leverage:        execution.Leverage,
		PositionSizeUSD: execution.PositionSizeUSD,
		StopLossPct:     execution.StopLossPct,
		TakeProfitPct:   execution.TakeProfitPct,
		Confidence:      execution.Confidence,
	}
}

func operandFromStore(op store.CompiledRuleOperand) RuleOperand {
	return RuleOperand{
		Kind:      op.Kind,
		Name:      op.Name,
		Timeframe: op.Timeframe,
		Period:    op.Period,
		Field:     op.Field,
		Value:     op.Value,
	}
}
