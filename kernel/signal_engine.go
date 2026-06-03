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

type CompositeSignalEngine struct {
	engines []SignalEngine
}

func NewCompositeSignalEngine(engines ...SignalEngine) *CompositeSignalEngine {
	return &CompositeSignalEngine{engines: engines}
}

func (e *CompositeSignalEngine) Generate(ctx context.Context, req SignalRequest) ([]CandidateSignal, error) {
	if e == nil || len(e.engines) == 0 {
		return nil, nil
	}
	out := []CandidateSignal{}
	for _, engine := range e.engines {
		if engine == nil {
			continue
		}
		signals, err := engine.Generate(ctx, req)
		if err != nil {
			return nil, err
		}
		out = append(out, signals...)
	}
	return out, nil
}

type ScoreSignalEngine struct{}

func NewScoreSignalEngine() *ScoreSignalEngine {
	return &ScoreSignalEngine{}
}

func (e *ScoreSignalEngine) Generate(ctx context.Context, req SignalRequest) ([]CandidateSignal, error) {
	if req.Scoring == nil || !req.Scoring.Enabled {
		return nil, nil
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if err := validateScoringStrategy(req.Scoring); err != nil {
		return nil, err
	}

	symbols := candidateSymbolSet(req.Candidates, req.Positions)
	out := []CandidateSignal{}
	for symbol := range symbolsForScoring(req.Scoring, symbols) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		snapshot := req.FactorSnapshot[symbol]
		if snapshot == nil {
			continue
		}
		score, evidence := scoreSnapshot(req.Scoring, snapshot)
		action := ""
		switch {
		case score >= req.Scoring.LongThreshold:
			action = "open_long"
		case score <= req.Scoring.ShortThreshold:
			action = "open_short"
		default:
			continue
		}
		entry, ok := snapshot.IndicatorValue("price", "", 0)
		if !ok || entry <= 0 {
			return nil, fmt.Errorf("scoring signal for %s cannot open position without a positive entry price", symbol)
		}
		confidence := scoringConfidence(req.Scoring.MinConfidence, score)
		rule := StrategyRule{
			ID:        "scoring",
			Version:   req.Scoring.Version,
			Timeframe: req.Scoring.Timeframe,
			Action:    action,
			Execution: RuleExecution{
				Leverage:        req.Scoring.Execution.Leverage,
				PositionSizeUSD: req.Scoring.Execution.PositionSizeUSD,
				StopLossPct:     req.Scoring.Execution.StopLossPct,
				TakeProfitPct:   req.Scoring.Execution.TakeProfitPct,
				Confidence:      confidence,
			},
			Enabled: true,
		}
		signal, err := buildCandidateSignal(rule, symbol, entry, fmt.Sprintf("score %.2f reached %s threshold", score, action), req.Now)
		if err != nil {
			return nil, err
		}
		signal.Evidence["score"] = score
		signal.Evidence["components"] = evidence
		out = append(out, signal)
	}
	return out, nil
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

func validateScoringStrategy(scoring *ScoringStrategy) error {
	if scoring == nil || !scoring.Enabled {
		return nil
	}
	if scoring.Timeframe == "" {
		return fmt.Errorf("scoring_config.timeframe is required")
	}
	if scoring.LongThreshold <= 0 || scoring.LongThreshold > 100 {
		return fmt.Errorf("scoring_config.long_threshold must be within (0,100]")
	}
	if scoring.ShortThreshold >= 0 || scoring.ShortThreshold < -100 {
		return fmt.Errorf("scoring_config.short_threshold must be within [-100,0)")
	}
	if len(scoring.SelectedFactors) == 0 {
		return fmt.Errorf("scoring_config.selected_factors is required")
	}
	if len(scoring.FactorWeights) == 0 {
		return fmt.Errorf("scoring_config.factor_weights is required")
	}
	for _, factor := range scoring.SelectedFactors {
		factor = strings.TrimSpace(factor)
		if !isSupportedScoringFactor(factor) {
			return fmt.Errorf("scoring_config selected unsupported factor %q", factor)
		}
		if scoring.FactorWeights[factor] <= 0 {
			return fmt.Errorf("scoring_config factor %q requires a positive weight", factor)
		}
	}
	if scoring.MinConfidence <= 0 {
		return fmt.Errorf("scoring_config.min_confidence is required")
	}
	synthetic := StrategyRule{ID: "scoring", Action: "open_long", Execution: scoring.Execution}
	return validateOpenExecution(synthetic, 1)
}

func isSupportedScoringFactor(factor string) bool {
	switch factor {
	case "trend", "momentum", "structure", "derivatives":
		return true
	default:
		return false
	}
}

func symbolsForScoring(scoring *ScoringStrategy, fallback map[string]bool) map[string]bool {
	if scoring == nil || len(scoring.Symbols) == 0 {
		return fallback
	}
	out := map[string]bool{}
	for _, symbol := range scoring.Symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol != "" {
			out[symbol] = true
		}
	}
	return out
}

func scoreSnapshot(scoring *ScoringStrategy, snapshot *market.FactorSnapshot) (float64, map[string]interface{}) {
	totalWeight := 0.0
	totalScore := 0.0
	components := map[string]interface{}{}
	for _, factor := range scoring.SelectedFactors {
		factor = strings.TrimSpace(factor)
		weight := scoring.FactorWeights[factor]
		if weight <= 0 {
			continue
		}
		component, ok := scoreComponent(factor, scoring.Timeframe, snapshot)
		if !ok {
			components[factor] = "unavailable"
			continue
		}
		totalWeight += weight
		totalScore += component * weight
		components[factor] = component
	}
	if totalWeight == 0 {
		return 0, components
	}
	return totalScore / totalWeight, components
}

func scoreComponent(factor, timeframe string, snapshot *market.FactorSnapshot) (float64, bool) {
	switch factor {
	case "trend":
		return trendScore(timeframe, snapshot)
	case "momentum":
		return momentumScore(timeframe, snapshot)
	case "structure":
		return structureScore(timeframe, snapshot)
	case "derivatives":
		return derivativesScore(snapshot)
	default:
		return 0, false
	}
}

func trendScore(timeframe string, snapshot *market.FactorSnapshot) (float64, bool) {
	price, ok := snapshot.IndicatorValue("price", "", 0)
	if !ok || price <= 0 {
		return 0, false
	}
	score := 0.0
	used := 0
	if ema20, ok := snapshot.IndicatorValue("ema", timeframe, 20); ok && ema20 > 0 {
		used++
		if price > ema20 {
			score += 35
		} else {
			score -= 35
		}
	}
	if ema50, ok := snapshot.IndicatorValue("ema", timeframe, 50); ok && ema50 > 0 {
		used++
		if price > ema50 {
			score += 35
		} else {
			score -= 35
		}
	}
	if hist, ok := snapshot.IndicatorValue("macd_histogram", timeframe, 0); ok {
		used++
		if hist > 0 {
			score += 30
		} else if hist < 0 {
			score -= 30
		}
	}
	if adx, ok := snapshot.IndicatorValue("adx", timeframe, 14); ok {
		plusDI, hasPlus := snapshot.IndicatorValue("plus_di", timeframe, 14)
		minusDI, hasMinus := snapshot.IndicatorValue("minus_di", timeframe, 14)
		if adx >= 20 && hasPlus && hasMinus {
			used++
			if plusDI > minusDI {
				score += 25
			} else if minusDI > plusDI {
				score -= 25
			}
		}
	}
	if sarUptrend, ok := snapshot.IndicatorValue("sar_uptrend", timeframe, 0); ok {
		used++
		if sarUptrend >= 0.5 {
			score += 15
		} else {
			score -= 15
		}
	}
	if breakAbove, ok := snapshot.IndicatorValue("break_above_donchian", timeframe, 20); ok && breakAbove >= 0.5 {
		used++
		score += 20
	}
	if breakBelow, ok := snapshot.IndicatorValue("break_below_donchian", timeframe, 20); ok && breakBelow >= 0.5 {
		used++
		score -= 20
	}
	if used == 0 {
		return 0, false
	}
	return clampScore(score), true
}

func momentumScore(timeframe string, snapshot *market.FactorSnapshot) (float64, bool) {
	rsi, ok := snapshot.IndicatorValue("rsi", timeframe, 14)
	if !ok {
		return 0, false
	}
	switch {
	case rsi >= 70:
		return -50, true
	case rsi >= 55:
		return 60, true
	case rsi <= 30:
		return 50, true
	case rsi <= 45:
		return -60, true
	default:
		return 0, true
	}
}

func structureScore(timeframe string, snapshot *market.FactorSnapshot) (float64, bool) {
	price, ok := snapshot.IndicatorValue("price", "", 0)
	if !ok || price <= 0 || snapshot == nil || snapshot.Structures == nil {
		return 0, false
	}
	best := 0.0
	used := false
	for _, structure := range snapshot.Structures["support_resistance"] {
		if timeframe != "" && structure.Timeframe != timeframe {
			continue
		}
		if !structure.Valid || structure.KeyLevels == nil {
			continue
		}
		used = true
		if support := structure.KeyLevels["support"]; support > 0 {
			distance := (price - support) / price * 100
			if distance >= 0 && distance <= 1.5 {
				best += 45
			}
		}
		if resistance := structure.KeyLevels["resistance"]; resistance > 0 {
			distance := (resistance - price) / price * 100
			if distance >= 0 && distance <= 1.5 {
				best -= 45
			}
		}
	}
	for _, structure := range snapshot.Structures["fibonacci"] {
		if timeframe != "" && structure.Timeframe != timeframe {
			continue
		}
		if !structure.Valid || structure.KeyLevels == nil {
			continue
		}
		used = true
		if fib := structure.KeyLevels["fib_0_618"]; fib > 0 {
			if price > fib {
				best += 20
			} else {
				best -= 20
			}
		}
	}
	return clampScore(best), used
}

func derivativesScore(snapshot *market.FactorSnapshot) (float64, bool) {
	if snapshot == nil || snapshot.External == nil {
		return 0, false
	}
	funding, ok := snapshot.External["funding_rate"]
	if !ok || !funding.Available {
		return 0, false
	}
	switch funding.State {
	case "overheated_positive":
		return -40, true
	case "overheated_negative":
		return 40, true
	case "positive":
		return 10, true
	case "negative":
		return -10, true
	default:
		return 0, true
	}
}

func scoringConfidence(minConfidence int, score float64) int {
	if score < 0 {
		score = -score
	}
	confidence := minConfidence + int((score-50)/2)
	if confidence < minConfidence {
		confidence = minConfidence
	}
	if confidence > 95 {
		confidence = 95
	}
	return confidence
}

func clampScore(score float64) float64 {
	if score > 100 {
		return 100
	}
	if score < -100 {
		return -100
	}
	return score
}
