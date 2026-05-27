package kernel

import (
	"context"
	"fmt"
	"nofx/market"
	"strings"
	"time"
)

// RuleSignalEngine evaluates compiled strategy rules against FactorSnapshot.
// It is deterministic and does not call LLM.
type RuleSignalEngine struct{}

func NewRuleSignalEngine() *RuleSignalEngine {
	return &RuleSignalEngine{}
}

func (e *RuleSignalEngine) Generate(ctx context.Context, req SignalRequest) ([]CandidateSignal, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if len(req.Rules) == 0 {
		return nil, nil
	}

	symbols := candidateSymbolSet(req.Candidates, req.Positions)
	out := []CandidateSignal{}
	for _, rule := range req.Rules {
		if !rule.Enabled {
			continue
		}
		for symbol := range symbolsForRule(rule, symbols) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			snapshot := req.FactorSnapshot[symbol]
			if snapshot == nil {
				continue
			}
			ok, reasons := evaluateRule(rule, snapshot)
			if !ok {
				continue
			}
			entry, _ := snapshot.IndicatorValue("price", "", 0)
			signal, err := buildCandidateSignal(rule, symbol, entry, strings.Join(reasons, "; "), req.Now)
			if err != nil {
				return nil, err
			}
			out = append(out, signal)
		}
	}
	return out, nil
}

func buildCandidateSignal(rule StrategyRule, symbol string, entry float64, reason string, now time.Time) (CandidateSignal, error) {
	confidence := rule.Execution.Confidence
	if confidence <= 0 {
		return CandidateSignal{}, fmt.Errorf("rule %s missing execution.confidence", rule.ID)
	}

	signal := CandidateSignal{
		ID:              fmt.Sprintf("%s:%s:%d", rule.ID, symbol, now.UnixMilli()),
		RuleID:          rule.ID,
		StrategyVersion: rule.Version,
		Symbol:          symbol,
		Action:          rule.Action,
		Timeframe:       rule.Timeframe,
		EntryPrice:      entry,
		Leverage:        rule.Execution.Leverage,
		PositionSizeUSD: rule.Execution.PositionSizeUSD,
		Confidence:      confidence,
		TriggerReason:   reason,
		Evidence: map[string]interface{}{
			"execution": rule.Execution,
		},
		GeneratedAt: now,
	}

	switch rule.Action {
	case "open_long":
		if err := validateOpenExecution(rule, entry); err != nil {
			return CandidateSignal{}, err
		}
		signal.StopLoss = entry * (1 - rule.Execution.StopLossPct/100)
		signal.TakeProfit = entry * (1 + rule.Execution.TakeProfitPct/100)
	case "open_short":
		if err := validateOpenExecution(rule, entry); err != nil {
			return CandidateSignal{}, err
		}
		signal.StopLoss = entry * (1 + rule.Execution.StopLossPct/100)
		signal.TakeProfit = entry * (1 - rule.Execution.TakeProfitPct/100)
	case "close_long", "close_short", "wait":
	default:
		return CandidateSignal{}, fmt.Errorf("rule %s has unsupported action %q", rule.ID, rule.Action)
	}

	return signal, nil
}

func validateOpenExecution(rule StrategyRule, entry float64) error {
	if entry <= 0 {
		return fmt.Errorf("rule %s cannot open position without a positive entry price", rule.ID)
	}
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
	return nil
}

func candidateSymbolSet(candidates []CandidateCoin, positions []PositionInfo) map[string]bool {
	out := map[string]bool{}
	for _, c := range candidates {
		if c.Symbol != "" {
			out[c.Symbol] = true
		}
	}
	for _, p := range positions {
		if p.Symbol != "" {
			out[p.Symbol] = true
		}
	}
	return out
}

func symbolsForRule(rule StrategyRule, fallback map[string]bool) map[string]bool {
	if len(rule.Symbols) == 0 {
		return fallback
	}
	out := map[string]bool{}
	for _, symbol := range rule.Symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol != "" {
			out[symbol] = true
		}
	}
	return out
}

func evaluateRule(rule StrategyRule, snapshot *market.FactorSnapshot) (bool, []string) {
	reasons := make([]string, 0, len(rule.Conditions))
	for _, condition := range rule.Conditions {
		left, ok := resolveOperand(condition.Left, snapshot)
		if !ok {
			return false, nil
		}
		right, ok := resolveOperand(condition.Right, snapshot)
		if !ok {
			return false, nil
		}
		if !compareValues(left, right, condition.Operator) {
			return false, nil
		}
		reasons = append(reasons, fmt.Sprintf("%s %.4f %s %.4f", operandLabel(condition.Left), left, condition.Operator, right))
	}
	return true, reasons
}

func resolveOperand(op RuleOperand, snapshot *market.FactorSnapshot) (float64, bool) {
	switch op.Kind {
	case "literal":
		return op.Value, true
	case "indicator":
		return snapshot.IndicatorValue(op.Name, op.Timeframe, op.Period)
	case "external_factor":
		if snapshot == nil || snapshot.External == nil {
			return 0, false
		}
		factor, ok := snapshot.External[op.Name]
		if !ok || !factor.Available {
			return 0, false
		}
		switch op.Field {
		case "score":
			return factor.Score, true
		default:
			return factor.Value, true
		}
	case "structure":
		if snapshot == nil || snapshot.Structures == nil {
			return 0, false
		}
		for _, structure := range snapshot.Structures[op.Name] {
			if op.Timeframe != "" && structure.Timeframe != op.Timeframe {
				continue
			}
			switch op.Field {
			case "valid":
				if structure.Valid {
					return 1, true
				}
				return 0, true
			case "invalid_price":
				return structure.InvalidPrice, structure.InvalidPrice > 0
			default:
				if structure.KeyLevels != nil {
					value, ok := structure.KeyLevels[op.Field]
					if ok {
						return value, true
					}
				}
			}
		}
		return 0, false
	default:
		return 0, false
	}
}

func compareValues(left, right float64, operator string) bool {
	switch operator {
	case ">", "gt":
		return left > right
	case ">=", "gte":
		return left >= right
	case "<", "lt":
		return left < right
	case "<=", "lte":
		return left <= right
	case "==", "eq":
		return left == right
	case "!=", "ne":
		return left != right
	default:
		return false
	}
}

func operandLabel(op RuleOperand) string {
	if op.Kind == "literal" {
		return "literal"
	}
	if op.Period > 0 {
		return fmt.Sprintf("%s.%s%d[%s]", op.Kind, op.Name, op.Period, op.Timeframe)
	}
	return fmt.Sprintf("%s.%s[%s]", op.Kind, op.Name, op.Timeframe)
}
