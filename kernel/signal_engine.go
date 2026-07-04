package kernel

import (
	"context"
	"errors"
	"fmt"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

const defaultMinScoringAvailableWeightRatio = 0.5
const defaultProtectiveATRBuffer = 2.0
const defaultProtectiveRiskReward = store.DefaultMinRiskRewardRatio
const maxOppositePrimaryScoreForReversalSetup = 25.0

var errSignalRejected = errors.New("signal rejected")

// RuleSignalEngine evaluates compiled strategy rules against FactorSnapshot.
// It is deterministic and does not call LLM.
type RuleSignalEngine struct{}

func NewRuleSignalEngine() *RuleSignalEngine {
	return &RuleSignalEngine{}
}

type RuleEvaluationTrace struct {
	RuleID     string                     `json:"rule_id"`
	Symbol     string                     `json:"symbol"`
	Action     string                     `json:"action"`
	Timeframe  string                     `json:"timeframe,omitempty"`
	Matched    bool                       `json:"matched"`
	Missing    bool                       `json:"missing"`
	Reason     string                     `json:"reason,omitempty"`
	Conditions []ConditionEvaluationTrace `json:"conditions"`
}

type ConditionEvaluationTrace struct {
	Left           string   `json:"left"`
	Operator       string   `json:"operator"`
	Right          string   `json:"right"`
	LeftAvailable  bool     `json:"left_available"`
	RightAvailable bool     `json:"right_available"`
	LeftValue      *float64 `json:"left_value,omitempty"`
	RightValue     *float64 `json:"right_value,omitempty"`
	Passed         bool     `json:"passed"`
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

type SetupSignalEngine struct{}

func NewSetupSignalEngine() *SetupSignalEngine {
	return &SetupSignalEngine{}
}

func GeneratePositionLifecycleSignals(req SignalRequest, existing []CandidateSignal) []CandidateSignal {
	if req.Scoring == nil || !req.Scoring.Enabled || len(req.Positions) == 0 {
		return nil
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	existingClose := existingCloseSignalKeys(existing)
	out := []CandidateSignal{}
	for _, pos := range req.Positions {
		side := normalizedPositionSide(pos.Side)
		if side == "" {
			continue
		}
		symbol := market.Normalize(pos.Symbol)
		if symbol == "" {
			continue
		}
		closeAction := closeActionForPositionSide(side)
		if existingClose[symbol+":"+closeAction] {
			continue
		}
		snapshot := factorSnapshotForSymbol(req.FactorSnapshot, symbol)
		if snapshot == nil {
			continue
		}
		setupTrace := evaluateSetupSnapshot(req.Scoring, symbol, snapshot)
		lifecycle, ok := evaluatePositionLifecycle(req.Scoring, pos, setupTrace)
		if !ok {
			continue
		}
		referencePrice := pos.MarkPrice
		if referencePrice <= 0 {
			referencePrice = pos.EntryPrice
		}
		if referencePrice <= 0 {
			referencePrice, _ = snapshotPrice(setupTrace.Timeframes.Primary, snapshot)
		}
		confidence := lifecycleCloseConfidence(req.Scoring.MinConfidence, setupTrace)
		signal := CandidateSignal{
			ID:              fmt.Sprintf("position_lifecycle_%s:%s:%d", closeAction, symbol, now.UnixMilli()),
			RuleID:          "position_lifecycle",
			Setup:           lifecycle.State,
			StrategyVersion: req.Scoring.Version,
			Symbol:          symbol,
			Action:          closeAction,
			Timeframe:       setupTrace.Timeframes.Primary,
			EntryPrice:      referencePrice,
			Confidence:      confidence,
			TriggerReason:   lifecycle.Reason,
			Evidence: map[string]interface{}{
				"position_lifecycle":       lifecycle,
				"setup":                    setupTrace,
				"primary_evaluation":       setupTrace.Primary,
				"entry_evaluation":         setupTrace.Entry,
				"confirmation_evaluations": setupTrace.Confirmations,
			},
			GeneratedAt: now,
		}
		out = append(out, signal)
	}
	return out
}

func mergePositionLifecycleSignals(signals, lifecycleSignals []CandidateSignal) []CandidateSignal {
	closeBySymbol := map[string]bool{}
	for _, signal := range lifecycleSignals {
		if signal.Action == "close_long" || signal.Action == "close_short" {
			closeBySymbol[market.Normalize(signal.Symbol)] = true
		}
	}
	for _, signal := range signals {
		if signal.Action == "close_long" || signal.Action == "close_short" {
			closeBySymbol[market.Normalize(signal.Symbol)] = true
		}
	}
	if len(closeBySymbol) == 0 && len(lifecycleSignals) == 0 {
		return signals
	}
	merged := make([]CandidateSignal, 0, len(signals)+len(lifecycleSignals))
	merged = append(merged, lifecycleSignals...)
	for _, signal := range signals {
		if closeBySymbol[market.Normalize(signal.Symbol)] && (signal.Action == "open_long" || signal.Action == "open_short") {
			continue
		}
		merged = append(merged, signal)
	}
	return merged
}

type ScoringEvaluationTrace struct {
	Symbol                  string                 `json:"symbol"`
	Timeframe               string                 `json:"timeframe,omitempty"`
	Score                   float64                `json:"score"`
	Action                  string                 `json:"action,omitempty"`
	Threshold               float64                `json:"threshold,omitempty"`
	AvailableFactors        []string               `json:"available_factors"`
	MissingFactors          []string               `json:"missing_factors"`
	SelectedFactorCount     int                    `json:"selected_factor_count"`
	RequiredFactorCount     int                    `json:"required_factor_count"`
	AvailableFactorCount    int                    `json:"available_factor_count"`
	TotalWeight             float64                `json:"total_weight"`
	AvailableWeight         float64                `json:"available_weight"`
	AvailableWeightRatio    float64                `json:"available_weight_ratio"`
	MinAvailableWeightRatio float64                `json:"min_available_weight_ratio"`
	Eligible                bool                   `json:"eligible"`
	Reason                  string                 `json:"reason,omitempty"`
	FactorWeights           map[string]float64     `json:"factor_weights,omitempty"`
	Components              map[string]interface{} `json:"components"`
}

type TimeframeRoleTrace struct {
	Entry         string   `json:"entry,omitempty"`
	Primary       string   `json:"primary,omitempty"`
	Confirmations []string `json:"confirmations,omitempty"`
}

type ProtectiveLevelTrace struct {
	Action           string  `json:"action"`
	Entry            float64 `json:"entry"`
	StopLoss         float64 `json:"stop_loss"`
	TakeProfit       float64 `json:"take_profit"`
	StopSource       string  `json:"stop_source"`
	StopTimeframe    string  `json:"stop_timeframe,omitempty"`
	StopAnchor       float64 `json:"stop_anchor,omitempty"`
	TargetSource     string  `json:"target_source"`
	TargetTimeframe  string  `json:"target_timeframe,omitempty"`
	TargetAnchor     float64 `json:"target_anchor,omitempty"`
	ATR              float64 `json:"atr,omitempty"`
	ATRTimeframe     string  `json:"atr_timeframe,omitempty"`
	ATRBuffer        float64 `json:"atr_buffer"`
	StopMode         string  `json:"stop_mode,omitempty"`
	RequestedStopTF  string  `json:"requested_stop_timeframe,omitempty"`
	TargetRiskReward float64 `json:"target_risk_reward"`
	RiskReward       float64 `json:"risk_reward"`
}

type PositionLifecycleTrace struct {
	Symbol             string                 `json:"symbol"`
	Side               string                 `json:"side"`
	State              string                 `json:"state"`
	Action             string                 `json:"action"`
	Reason             string                 `json:"reason"`
	Timeframes         TimeframeRoleTrace     `json:"timeframes"`
	PrimaryScore       float64                `json:"primary_score"`
	EntryScore         float64                `json:"entry_score"`
	ConfirmationScores map[string]float64     `json:"confirmation_scores,omitempty"`
	OppositeSetup      string                 `json:"opposite_setup,omitempty"`
	EntryPrice         float64                `json:"entry_price,omitempty"`
	MarkPrice          float64                `json:"mark_price,omitempty"`
	UnrealizedPnLPct   float64                `json:"unrealized_pnl_pct,omitempty"`
	PrimaryEvaluation  ScoringEvaluationTrace `json:"primary_evaluation"`
	EntryEvaluation    ScoringEvaluationTrace `json:"entry_evaluation"`
}

type SetupEvaluationTrace struct {
	Symbol        string                   `json:"symbol"`
	Setup         string                   `json:"setup,omitempty"`
	Action        string                   `json:"action,omitempty"`
	Signals       []string                 `json:"signals,omitempty"`
	Timeframes    TimeframeRoleTrace       `json:"timeframes"`
	Eligible      bool                     `json:"eligible"`
	Reason        string                   `json:"reason,omitempty"`
	Primary       ScoringEvaluationTrace   `json:"primary"`
	Entry         ScoringEvaluationTrace   `json:"entry"`
	Confirmations []ScoringEvaluationTrace `json:"confirmations,omitempty"`
}

func (e *SetupSignalEngine) Generate(ctx context.Context, req SignalRequest) ([]CandidateSignal, error) {
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
		trace := evaluateSetupSnapshot(req.Scoring, symbol, snapshot)
		if !trace.Eligible || trace.Action == "" {
			continue
		}
		entry, ok := snapshotPrice(scoringTimeframeRoles(req.Scoring).Entry, snapshot)
		if !ok || entry <= 0 {
			return nil, fmt.Errorf("setup signal for %s cannot open position without a positive entry price", symbol)
		}
		confidence := setupConfidence(req.Scoring.MinConfidence, trace)
		rule := StrategyRule{
			ID:        trace.Setup,
			Version:   req.Scoring.Version,
			Timeframe: trace.Timeframes.Primary,
			Action:    trace.Action,
			Execution: RuleExecution{
				Leverage:        req.Scoring.Execution.Leverage,
				PositionSizeUSD: req.Scoring.Execution.PositionSizeUSD,
				Confidence:      confidence,
			},
			Enabled: true,
		}
		signal, err := buildCandidateSignal(rule, symbol, entry, trace.Reason, req.Now, snapshot, trace.Timeframes, req.ProtectiveATRBuffer, req.ProtectiveTimeframes, req.MinRiskRewardRatio)
		if err != nil {
			if errors.Is(err, errSignalRejected) {
				continue
			}
			return nil, err
		}
		signal.Setup = trace.Setup
		signal.Evidence["setup"] = trace
		signal.Evidence["primary_evaluation"] = trace.Primary
		signal.Evidence["entry_evaluation"] = trace.Entry
		signal.Evidence["confirmation_evaluations"] = trace.Confirmations
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
			entry, _ := snapshotPrice(rule.Timeframe, snapshot)
			roles := TimeframeRoleTrace{Entry: rule.Timeframe, Primary: rule.Timeframe}
			signal, err := buildCandidateSignal(rule, symbol, entry, strings.Join(reasons, "; "), req.Now, snapshot, roles, req.ProtectiveATRBuffer, req.ProtectiveTimeframes, req.MinRiskRewardRatio)
			if err != nil {
				if errors.Is(err, errSignalRejected) {
					continue
				}
				return nil, err
			}
			out = append(out, signal)
		}
	}
	return out, nil
}

func TraceRuleEvaluations(req SignalRequest) []RuleEvaluationTrace {
	if len(req.Rules) == 0 {
		return nil
	}
	symbols := candidateSymbolSet(req.Candidates, req.Positions)
	traces := []RuleEvaluationTrace{}
	for _, rule := range req.Rules {
		if !rule.Enabled {
			continue
		}
		for symbol := range symbolsForRule(rule, symbols) {
			snapshot := req.FactorSnapshot[symbol]
			trace := RuleEvaluationTrace{
				RuleID:     rule.ID,
				Symbol:     symbol,
				Action:     rule.Action,
				Timeframe:  rule.Timeframe,
				Conditions: make([]ConditionEvaluationTrace, 0, len(rule.Conditions)),
			}
			if snapshot == nil {
				trace.Missing = true
				trace.Reason = "factor snapshot missing"
				traces = append(traces, trace)
				continue
			}
			matched := true
			reasons := make([]string, 0, len(rule.Conditions))
			for _, condition := range rule.Conditions {
				left, leftOK := resolveOperand(condition.Left, snapshot)
				right, rightOK := resolveOperand(condition.Right, snapshot)
				passed := leftOK && rightOK && compareValues(left, right, condition.Operator)
				conditionTrace := ConditionEvaluationTrace{
					Left:           operandLabel(condition.Left),
					Operator:       condition.Operator,
					Right:          operandLabel(condition.Right),
					LeftAvailable:  leftOK,
					RightAvailable: rightOK,
					Passed:         passed,
				}
				if leftOK {
					leftValue := left
					conditionTrace.LeftValue = &leftValue
				}
				if rightOK {
					rightValue := right
					conditionTrace.RightValue = &rightValue
				}
				trace.Conditions = append(trace.Conditions, conditionTrace)
				if !leftOK || !rightOK {
					trace.Missing = true
					matched = false
					continue
				}
				if !passed {
					matched = false
					continue
				}
				reasons = append(reasons, fmt.Sprintf("%s %.4f %s %.4f", operandLabel(condition.Left), left, condition.Operator, right))
			}
			trace.Matched = matched
			if len(reasons) > 0 {
				trace.Reason = strings.Join(reasons, "; ")
			}
			traces = append(traces, trace)
		}
	}
	return traces
}

func TraceEvidenceEvaluations(req SignalRequest) []ScoringEvaluationTrace {
	if req.Scoring == nil || !req.Scoring.Enabled {
		return nil
	}
	symbols := candidateSymbolSet(req.Candidates, req.Positions)
	traces := []ScoringEvaluationTrace{}
	for symbol := range symbolsForScoring(req.Scoring, symbols) {
		snapshot := req.FactorSnapshot[symbol]
		if snapshot == nil {
			traces = append(traces, ScoringEvaluationTrace{
				Symbol:                  symbol,
				Timeframe:               req.Scoring.Timeframe,
				AvailableFactors:        []string{},
				MissingFactors:          selectedScoringFactors(req.Scoring),
				RequiredFactorCount:     requiredScoringFactorCount(req.Scoring),
				MinAvailableWeightRatio: minAvailableWeightRatio(req.Scoring),
				Eligible:                false,
				Reason:                  "factor snapshot missing",
				Components:              map[string]interface{}{},
			})
			continue
		}
		traces = append(traces, evaluateScoringSnapshot(req.Scoring, symbol, snapshot))
	}
	return traces
}

func TraceSetupEvaluations(req SignalRequest) []SetupEvaluationTrace {
	if req.Scoring == nil || !req.Scoring.Enabled {
		return nil
	}
	symbols := candidateSymbolSet(req.Candidates, req.Positions)
	traces := []SetupEvaluationTrace{}
	for symbol := range symbolsForScoring(req.Scoring, symbols) {
		snapshot := req.FactorSnapshot[symbol]
		if snapshot == nil {
			roles := scoringTimeframeRoles(req.Scoring)
			traces = append(traces, SetupEvaluationTrace{
				Symbol:     symbol,
				Setup:      "no_trade_insufficient_evidence",
				Timeframes: roles,
				Eligible:   false,
				Reason:     "factor snapshot missing",
			})
			continue
		}
		trace := evaluateSetupSnapshot(req.Scoring, symbol, snapshot)
		trace = applyProtectiveEligibilityToSetupTrace(req.Scoring, symbol, snapshot, trace, req.ProtectiveATRBuffer, req.ProtectiveTimeframes, req.MinRiskRewardRatio)
		traces = append(traces, trace)
	}
	return traces
}

func evaluatePositionLifecycle(scoring *ScoringStrategy, pos PositionInfo, trace SetupEvaluationTrace) (PositionLifecycleTrace, bool) {
	side := normalizedPositionSide(pos.Side)
	if scoring == nil || side == "" {
		return PositionLifecycleTrace{}, false
	}
	lifecycle := PositionLifecycleTrace{
		Symbol:             market.Normalize(pos.Symbol),
		Side:               side,
		Timeframes:         trace.Timeframes,
		PrimaryScore:       trace.Primary.Score,
		EntryScore:         trace.Entry.Score,
		ConfirmationScores: confirmationScoreMap(trace.Confirmations),
		EntryPrice:         pos.EntryPrice,
		MarkPrice:          pos.MarkPrice,
		UnrealizedPnLPct:   pos.UnrealizedPnLPct,
		PrimaryEvaluation:  trace.Primary,
		EntryEvaluation:    trace.Entry,
	}
	if !trace.Primary.Eligible || !trace.Entry.Eligible {
		return PositionLifecycleTrace{}, false
	}

	closeAction := closeActionForPositionSide(side)
	oppositeOpenAction := oppositeOpenActionForPositionSide(side)
	if trace.Eligible && trace.Action == oppositeOpenAction {
		lifecycle.State = "opposite_setup"
		lifecycle.Action = closeAction
		lifecycle.OppositeSetup = trace.Setup
		lifecycle.Reason = fmt.Sprintf("position lifecycle: existing %s thesis is invalidated by opposite setup %s; close first, do not reverse in the same cycle", side, trace.Setup)
		return lifecycle, true
	}

	longConfirmOK, shortConfirmOK, confirmReason := confirmationDirection(trace.Confirmations)
	switch side {
	case "long":
		if trace.Primary.Score <= scoring.ShortThreshold && trace.Entry.Score <= 0 && shortConfirmOK {
			lifecycle.State = "thesis_invalidated"
			lifecycle.Action = closeAction
			lifecycle.Reason = fmt.Sprintf("position lifecycle: existing long thesis invalidated; primary score %.2f crossed short threshold %.2f, entry score %.2f no longer supports long, %s", trace.Primary.Score, scoring.ShortThreshold, trace.Entry.Score, confirmReason)
			return lifecycle, true
		}
	case "short":
		if trace.Primary.Score >= scoring.LongThreshold && trace.Entry.Score >= 0 && longConfirmOK {
			lifecycle.State = "thesis_invalidated"
			lifecycle.Action = closeAction
			lifecycle.Reason = fmt.Sprintf("position lifecycle: existing short thesis invalidated; primary score %.2f crossed long threshold %.2f, entry score %.2f no longer supports short, %s", trace.Primary.Score, scoring.LongThreshold, trace.Entry.Score, confirmReason)
			return lifecycle, true
		}
	}
	return PositionLifecycleTrace{}, false
}

func existingCloseSignalKeys(signals []CandidateSignal) map[string]bool {
	out := map[string]bool{}
	for _, signal := range signals {
		if signal.Action != "close_long" && signal.Action != "close_short" {
			continue
		}
		symbol := market.Normalize(signal.Symbol)
		if symbol == "" {
			continue
		}
		out[symbol+":"+signal.Action] = true
	}
	return out
}

func factorSnapshotForSymbol(snapshots map[string]*market.FactorSnapshot, symbol string) *market.FactorSnapshot {
	if len(snapshots) == 0 {
		return nil
	}
	if snapshot := snapshots[symbol]; snapshot != nil {
		return snapshot
	}
	normalized := market.Normalize(symbol)
	for key, snapshot := range snapshots {
		if market.Normalize(key) == normalized {
			return snapshot
		}
	}
	return nil
}

func normalizedPositionSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long":
		return "long"
	case "short":
		return "short"
	default:
		return ""
	}
}

func closeActionForPositionSide(side string) string {
	if side == "short" {
		return "close_short"
	}
	return "close_long"
}

func oppositeOpenActionForPositionSide(side string) string {
	if side == "short" {
		return "open_long"
	}
	return "open_short"
}

func confirmationScoreMap(confirmations []ScoringEvaluationTrace) map[string]float64 {
	if len(confirmations) == 0 {
		return nil
	}
	out := map[string]float64{}
	for _, trace := range confirmations {
		if trace.Timeframe == "" || !trace.Eligible {
			continue
		}
		out[trace.Timeframe] = trace.Score
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func lifecycleCloseConfidence(minConfidence int, trace SetupEvaluationTrace) int {
	strength := absScore(trace.Primary.Score)
	entryStrength := absScore(trace.Entry.Score)
	if entryStrength > 0 {
		strength = (strength + entryStrength) / 2
	}
	return scoringConfidence(minConfidence, strength)
}

func absScore(score float64) float64 {
	if score < 0 {
		return -score
	}
	return score
}

func evaluateSetupSnapshot(scoring *ScoringStrategy, symbol string, snapshot *market.FactorSnapshot) SetupEvaluationTrace {
	roles := scoringTimeframeRoles(scoring)
	primaryScoring := scoringForTimeframe(scoring, roles.Primary)
	entryScoring := scoringForTimeframe(scoring, roles.Entry)
	primary := evaluateScoringSnapshot(primaryScoring, symbol, snapshot)
	entry := evaluateScoringSnapshot(entryScoring, symbol, snapshot)
	confirmations := make([]ScoringEvaluationTrace, 0, len(roles.Confirmations))
	for _, tf := range roles.Confirmations {
		confirmations = append(confirmations, evaluateScoringSnapshot(scoringForTimeframe(scoring, tf), symbol, snapshot))
	}

	trace := SetupEvaluationTrace{
		Symbol:        symbol,
		Timeframes:    roles,
		Eligible:      false,
		Primary:       primary,
		Entry:         entry,
		Confirmations: confirmations,
	}
	if !primary.Eligible {
		trace.Setup = "no_trade_insufficient_evidence"
		trace.Reason = "primary timeframe evidence is incomplete: " + primary.Reason
		return trace
	}
	if !entry.Eligible {
		trace.Setup = "no_trade_insufficient_evidence"
		trace.Reason = "entry timeframe evidence is incomplete: " + entry.Reason
		return trace
	}

	longConfirmOK, shortConfirmOK, confirmReason := confirmationDirection(confirmations)
	if setup, ok := preferredStructureSetup(snapshot, roles); ok {
		trace = applyStructureSetup(scoring, trace, setup, longConfirmOK, shortConfirmOK, confirmReason)
		if trace.Setup != "" {
			return trace
		}
	}
	trace.Setup = "no_trade_no_structure_setup"
	trace.Reason = fmt.Sprintf("no deterministic structure setup available; score evidence only: primary %.2f, entry %.2f, %s", primary.Score, entry.Score, confirmReason)
	return trace
}

func preferredStructureSetup(snapshot *market.FactorSnapshot, roles TimeframeRoleTrace) (market.StructureSnapshot, bool) {
	if snapshot == nil || snapshot.Structures == nil {
		return market.StructureSnapshot{}, false
	}
	primaryEntrySetups := structureSetupsForTimeframes(snapshot, uniqueTimeframes(roles.Primary, roles.Entry))
	for _, setup := range primaryEntrySetups {
		if structureSetupHardBlocks(setup) {
			return setup, true
		}
	}
	for _, setup := range primaryEntrySetups {
		if actionableStructureSetup(setup) {
			return setup, true
		}
	}
	for _, setup := range primaryEntrySetups {
		if strings.TrimSpace(setup.Setup) != "" {
			return setup, true
		}
	}
	confirmationSetups := structureSetupsForTimeframes(snapshot, roles.Confirmations)
	for _, setup := range confirmationSetups {
		if actionableStructureSetup(setup) {
			return setup, true
		}
	}
	for _, setup := range append(primaryEntrySetups, confirmationSetups...) {
		if strings.TrimSpace(setup.Setup) != "" {
			return setup, true
		}
	}
	for _, setup := range snapshot.Structures["setup"] {
		if actionableStructureSetup(setup) || strings.TrimSpace(setup.Setup) != "" {
			return setup, true
		}
	}
	return market.StructureSnapshot{}, false
}

func structureSetupsForTimeframes(snapshot *market.FactorSnapshot, timeframes []string) []market.StructureSnapshot {
	out := []market.StructureSnapshot{}
	for _, timeframe := range timeframes {
		if setup, ok := structureSetupForTimeframe(snapshot, timeframe); ok {
			out = append(out, setup)
		}
	}
	return out
}

func structureSetupForTimeframe(snapshot *market.FactorSnapshot, timeframe string) (market.StructureSnapshot, bool) {
	for _, setup := range snapshot.Structures["setup"] {
		if timeframe != "" && setup.Timeframe != timeframe {
			continue
		}
		if strings.TrimSpace(setup.Setup) != "" {
			return setup, true
		}
	}
	return market.StructureSnapshot{}, false
}

func actionableStructureSetup(setup market.StructureSnapshot) bool {
	name := strings.TrimSpace(setup.Setup)
	return setup.Valid && name != "" && !strings.HasPrefix(name, "no_trade") && actionForStructureSetup(setup) != ""
}

func structureSetupHardBlocks(setup market.StructureSnapshot) bool {
	name := strings.TrimSpace(setup.Setup)
	return setup.Phase == "invalidated" || name == "no_trade_structure_invalidated"
}

func applyStructureSetup(scoring *ScoringStrategy, trace SetupEvaluationTrace, setup market.StructureSnapshot, longConfirmOK, shortConfirmOK bool, confirmReason string) SetupEvaluationTrace {
	name := strings.TrimSpace(setup.Setup)
	if name == "" {
		return trace
	}
	if !setup.Valid || strings.HasPrefix(name, "no_trade") {
		trace.Setup = name
		trace.Signals = append([]string(nil), setup.Signals...)
		trace.Reason = fmt.Sprintf("%s: %s", name, nonEmptyReason(setup.Reason, "structure detector did not find a tradable setup"))
		return trace
	}
	action := actionForStructureSetup(setup)
	if action == "" {
		trace.Setup = "no_trade_insufficient_evidence"
		trace.Reason = fmt.Sprintf("structure setup %s has no actionable direction", name)
		return trace
	}
	if !scoresSupportStructureSetup(action, name, trace.Primary, trace.Entry) {
		trace.Setup = "no_trade_threshold_not_met"
		trace.Signals = append([]string(nil), setup.Signals...)
		trace.Reason = fmt.Sprintf("structure setup %s exists, but score evidence does not support %s: primary %.2f, entry %.2f", name, action, trace.Primary.Score, trace.Entry.Score)
		return trace
	}
	if action == "open_long" && !longConfirmOK {
		trace.Setup = "no_trade_threshold_not_met"
		trace.Signals = append([]string(nil), setup.Signals...)
		trace.Reason = fmt.Sprintf("structure setup %s blocked by confirmation timeframe: %s", name, confirmReason)
		return trace
	}
	if action == "open_short" && !shortConfirmOK {
		trace.Setup = "no_trade_threshold_not_met"
		trace.Signals = append([]string(nil), setup.Signals...)
		trace.Reason = fmt.Sprintf("structure setup %s blocked by confirmation timeframe: %s", name, confirmReason)
		return trace
	}
	trace.Eligible = true
	trace.Action = action
	trace.Setup = name
	trace.Signals = append([]string(nil), setup.Signals...)
	trace.Reason = fmt.Sprintf("%s: structural setup confirmed; primary score %.2f, entry score %.2f, %s", name, trace.Primary.Score, trace.Entry.Score, confirmReason)
	if len(trace.Signals) == 0 {
		trace.Signals = append(trace.Signals, "deterministic market structure setup")
	}
	if scoring != nil && scoring.MinConfidence > 0 {
		trace.Primary.Threshold = scoring.LongThreshold
		if action == "open_short" {
			trace.Primary.Threshold = scoring.ShortThreshold
		}
	}
	return trace
}

func actionForStructureSetup(setup market.StructureSnapshot) string {
	direction := strings.ToLower(strings.TrimSpace(setup.Direction))
	name := strings.ToLower(strings.TrimSpace(setup.Setup))
	if direction == "long" || strings.HasSuffix(name, "_long") {
		return "open_long"
	}
	if direction == "short" || strings.HasSuffix(name, "_short") {
		return "open_short"
	}
	return ""
}

func scoresSupportStructureSetup(action, setup string, primary, entry ScoringEvaluationTrace) bool {
	reversal := strings.Contains(setup, "reversal") || strings.Contains(setup, "bounce") || strings.Contains(setup, "exhaustion") || strings.Contains(setup, "failed_breakout")
	switch action {
	case "open_long":
		if reversal {
			return primary.Score > -maxOppositePrimaryScoreForReversalSetup && entry.Score >= 0
		}
		return primary.Score > 0 && entry.Score >= -5
	case "open_short":
		if reversal {
			return primary.Score < maxOppositePrimaryScoreForReversalSetup && entry.Score <= 0
		}
		return primary.Score < 0 && entry.Score <= 5
	default:
		return false
	}
}

func nonEmptyReason(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func applyProtectiveEligibilityToSetupTrace(scoring *ScoringStrategy, symbol string, snapshot *market.FactorSnapshot, trace SetupEvaluationTrace, atrBuffer float64, timeframeConfig ProtectiveTimeframeConfig, minRiskRewardRatio float64) SetupEvaluationTrace {
	if scoring == nil || !trace.Eligible || trace.Action == "" {
		return trace
	}
	entry, ok := snapshotPrice(trace.Timeframes.Entry, snapshot)
	if !ok || entry <= 0 {
		trace.Eligible = false
		trace.Reason = appendTraceReason(trace.Reason, fmt.Sprintf("protective filter: %s has no positive entry price", symbol))
		return trace
	}
	_, err := calculateProtectiveLevels(trace.Setup, trace.Action, entry, snapshot, trace.Timeframes, atrBuffer, timeframeConfig, minRiskRewardRatio)
	if err == nil {
		return trace
	}
	if reason, ok := signalRejectionReason(err); ok {
		trace.Eligible = false
		trace.Reason = appendTraceReason(trace.Reason, "protective filter: "+reason)
		return trace
	}
	trace.Eligible = false
	trace.Reason = appendTraceReason(trace.Reason, "protective levels unavailable: "+err.Error())
	return trace
}

func appendTraceReason(base, detail string) string {
	if strings.TrimSpace(base) == "" {
		return detail
	}
	return base + "; " + detail
}

func scoringTimeframeRoles(scoring *ScoringStrategy) TimeframeRoleTrace {
	primary := strings.TrimSpace(scoring.Timeframe)
	entry := strings.TrimSpace(scoring.EntryTimeframe)
	if primary == "" {
		primary = entry
	}
	if entry == "" {
		entry = primary
	}
	confirmations := []string{}
	seen := map[string]bool{}
	for _, tf := range scoring.ConfirmationTimeframes {
		tf = strings.TrimSpace(tf)
		if tf == "" || tf == primary || tf == entry || seen[tf] {
			continue
		}
		confirmations = append(confirmations, tf)
		seen[tf] = true
	}
	return TimeframeRoleTrace{Entry: entry, Primary: primary, Confirmations: confirmations}
}

func scoringForTimeframe(scoring *ScoringStrategy, timeframe string) *ScoringStrategy {
	next := *scoring
	next.Timeframe = timeframe
	return &next
}

func confirmationDirection(confirmations []ScoringEvaluationTrace) (bool, bool, string) {
	if len(confirmations) == 0 {
		return true, true, "no confirmation timeframe configured"
	}
	longOK := true
	shortOK := true
	available := 0
	for _, trace := range confirmations {
		if !trace.Eligible {
			continue
		}
		available++
		if trace.Score <= -35 {
			longOK = false
		}
		if trace.Score >= 35 {
			shortOK = false
		}
	}
	if available == 0 {
		return false, false, "confirmation timeframe evidence is unavailable"
	}
	return longOK, shortOK, fmt.Sprintf("%d confirmation timeframe(s) available", available)
}

func setupConfidence(minConfidence int, trace SetupEvaluationTrace) int {
	score := (absFloat(trace.Primary.Score) + absFloat(trace.Entry.Score)) / 2
	return scoringConfidence(minConfidence, score)
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func buildCandidateSignal(rule StrategyRule, symbol string, entry float64, reason string, now time.Time, snapshot *market.FactorSnapshot, roles TimeframeRoleTrace, atrBuffer float64, timeframeConfig ProtectiveTimeframeConfig, minRiskRewardRatio float64) (CandidateSignal, error) {
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
			"execution": reviewExecutionEvidence(rule.Execution),
		},
		GeneratedAt: now,
	}

	switch rule.Action {
	case "open_long":
		if err := validateOpenExecution(rule, entry); err != nil {
			return CandidateSignal{}, err
		}
		levels, err := calculateProtectiveLevels(rule.ID, rule.Action, entry, snapshot, roles, atrBuffer, timeframeConfig, minRiskRewardRatio)
		if err != nil {
			return CandidateSignal{}, fmt.Errorf("rule %s protective levels: %w", rule.ID, err)
		}
		signal.StopLoss = levels.StopLoss
		signal.TakeProfit = levels.TakeProfit
		signal.Evidence["protective_levels"] = levels
	case "open_short":
		if err := validateOpenExecution(rule, entry); err != nil {
			return CandidateSignal{}, err
		}
		levels, err := calculateProtectiveLevels(rule.ID, rule.Action, entry, snapshot, roles, atrBuffer, timeframeConfig, minRiskRewardRatio)
		if err != nil {
			return CandidateSignal{}, fmt.Errorf("rule %s protective levels: %w", rule.ID, err)
		}
		signal.StopLoss = levels.StopLoss
		signal.TakeProfit = levels.TakeProfit
		signal.Evidence["protective_levels"] = levels
	case "close_long", "close_short", "wait":
	default:
		return CandidateSignal{}, fmt.Errorf("rule %s has unsupported action %q", rule.ID, rule.Action)
	}

	return signal, nil
}

func reviewExecutionEvidence(execution RuleExecution) map[string]interface{} {
	return map[string]interface{}{
		"leverage":          execution.Leverage,
		"position_size_usd": execution.PositionSizeUSD,
		"confidence":        execution.Confidence,
	}
}

func calculateProtectiveLevels(setup, action string, entry float64, snapshot *market.FactorSnapshot, roles TimeframeRoleTrace, atrBuffer float64, timeframeConfig ProtectiveTimeframeConfig, minRiskRewardRatio float64) (ProtectiveLevelTrace, error) {
	if atrBuffer <= 0 {
		atrBuffer = defaultProtectiveATRBuffer
	}
	timeframes := resolveProtectiveTimeframes(roles, timeframeConfig)
	trace := ProtectiveLevelTrace{
		Action:           action,
		Entry:            entry,
		ATRBuffer:        atrBuffer,
		StopMode:         timeframes.StopMode,
		RequestedStopTF:  timeframes.RequestedStopTF,
		TargetRiskReward: protectiveTargetRiskReward(minRiskRewardRatio),
	}
	if snapshot == nil {
		return trace, fmt.Errorf("factor snapshot is required for market-based stop loss and take profit")
	}
	atr, atrTF, ok := preferredATR(snapshot, timeframes.ATR)
	if !ok || atr <= 0 {
		return trace, rejectSignal("ATR14 is required for market-based stop loss and take profit")
	}
	trace.ATR = atr
	trace.ATRTimeframe = atrTF

	switch action {
	case "open_long":
		stopAnchor, stopTF, stopSource, hasStopAnchor := protectiveStopAnchor("long", entry, snapshot, timeframes.Stop)
		if !hasStopAnchor {
			return trace, rejectSignal("long setup %q has no structural stop-loss anchor", setup)
		}
		trace.StopLoss = stopAnchor - atr*trace.ATRBuffer
		trace.StopAnchor = stopAnchor
		trace.StopTimeframe = stopTF
		trace.StopSource = stopSource
		if trace.StopLoss <= 0 || trace.StopLoss >= entry {
			return trace, fmt.Errorf("long stop loss %.8f is not below entry %.8f", trace.StopLoss, entry)
		}
		targetAnchor, targetTF, targetSource, hasTargetAnchor := protectiveTargetAnchor("long", entry, snapshot, timeframes.Target)
		if hasTargetAnchor && targetAnchor > entry {
			trace.TakeProfit = targetAnchor
			trace.TargetAnchor = targetAnchor
			trace.TargetTimeframe = targetTF
			trace.TargetSource = targetSource
		} else {
			return trace, rejectSignal("long setup %q has no structural take-profit target", setup)
		}
	case "open_short":
		stopAnchor, stopTF, stopSource, hasStopAnchor := protectiveStopAnchor("short", entry, snapshot, timeframes.Stop)
		if !hasStopAnchor {
			return trace, rejectSignal("short setup %q has no structural stop-loss anchor", setup)
		}
		trace.StopLoss = stopAnchor + atr*trace.ATRBuffer
		trace.StopAnchor = stopAnchor
		trace.StopTimeframe = stopTF
		trace.StopSource = stopSource
		if trace.StopLoss <= entry {
			return trace, fmt.Errorf("short stop loss %.8f is not above entry %.8f", trace.StopLoss, entry)
		}
		targetAnchor, targetTF, targetSource, hasTargetAnchor := protectiveTargetAnchor("short", entry, snapshot, timeframes.Target)
		if hasTargetAnchor && targetAnchor > 0 && targetAnchor < entry {
			trace.TakeProfit = targetAnchor
			trace.TargetAnchor = targetAnchor
			trace.TargetTimeframe = targetTF
			trace.TargetSource = targetSource
		} else {
			return trace, rejectSignal("short setup %q has no structural take-profit target", setup)
		}
		if trace.TakeProfit <= 0 {
			return trace, fmt.Errorf("short take profit %.8f is not positive", trace.TakeProfit)
		}
	default:
		return trace, fmt.Errorf("unsupported open action %q", action)
	}
	trace.RiskReward = protectiveRiskReward(action, entry, trace.StopAnchor, trace.TakeProfit)
	return trace, nil
}

func rejectSignal(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", errSignalRejected, fmt.Sprintf(format, args...))
}

func signalRejectionReason(err error) (string, bool) {
	if !errors.Is(err, errSignalRejected) {
		return "", false
	}
	msg := err.Error()
	marker := errSignalRejected.Error() + ": "
	if idx := strings.LastIndex(msg, marker); idx >= 0 {
		return msg[idx+len(marker):], true
	}
	return msg, true
}

func protectiveTargetRiskReward(configured float64) float64 {
	if configured > 0 {
		return configured
	}
	return defaultProtectiveRiskReward
}

func protectiveRiskReward(action string, entry, stopLoss, takeProfit float64) float64 {
	switch action {
	case "open_long":
		if entry <= stopLoss {
			return 0
		}
		return (takeProfit - entry) / (entry - stopLoss)
	case "open_short":
		if stopLoss <= entry {
			return 0
		}
		return (entry - takeProfit) / (stopLoss - entry)
	default:
		return 0
	}
}

func protectiveStopTimeframes(roles TimeframeRoleTrace) []string {
	return uniqueTimeframes(roles.Primary, roles.Entry)
}

func protectiveTargetTimeframes(roles TimeframeRoleTrace) []string {
	values := []string{roles.Primary, roles.Entry}
	values = append(values, roles.Confirmations...)
	return uniqueTimeframes(values...)
}

func protectiveATRTimeframes(roles TimeframeRoleTrace) []string {
	values := []string{roles.Primary, roles.Entry}
	values = append(values, roles.Confirmations...)
	return uniqueTimeframes(values...)
}

type resolvedProtectiveTimeframes struct {
	Stop            []string
	Target          []string
	ATR             []string
	StopMode        string
	RequestedStopTF string
}

func resolveProtectiveTimeframes(roles TimeframeRoleTrace, config ProtectiveTimeframeConfig) resolvedProtectiveTimeframes {
	mode := normalizeProtectiveStopMode(config.StopLossMode)
	requested := strings.TrimSpace(config.StopLossTimeframe)
	stop := []string{}
	switch mode {
	case store.StopLossTimeframeModeEntry:
		stop = uniqueTimeframes(roles.Entry, roles.Primary)
	case store.StopLossTimeframeModeCustom:
		if requested == "" {
			mode = store.StopLossTimeframeModeAuto
			stop = protectiveStopTimeframes(roles)
		} else {
			stop = uniqueTimeframes(requested, roles.Primary, roles.Entry)
		}
	default:
		stop = protectiveStopTimeframes(roles)
	}
	target := protectiveTargetTimeframes(roles)
	atr := append([]string{}, stop...)
	atr = append(atr, roles.Confirmations...)
	return resolvedProtectiveTimeframes{
		Stop:            stop,
		Target:          target,
		ATR:             uniqueTimeframes(atr...),
		StopMode:        mode,
		RequestedStopTF: requested,
	}
}

func normalizeProtectiveStopMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case store.StopLossTimeframeModePrimary:
		return store.StopLossTimeframeModePrimary
	case store.StopLossTimeframeModeEntry:
		return store.StopLossTimeframeModeEntry
	case store.StopLossTimeframeModeCustom:
		return store.StopLossTimeframeModeCustom
	default:
		return store.StopLossTimeframeModeAuto
	}
}

func uniqueTimeframes(values ...string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func preferredATR(snapshot *market.FactorSnapshot, timeframes []string) (float64, string, bool) {
	for _, timeframe := range timeframes {
		if value, ok := snapshot.IndicatorValue("atr", timeframe, 14); ok && value > 0 {
			return value, timeframe, true
		}
	}
	for _, point := range snapshot.Technical["atr"] {
		if point.Period == 14 && point.Value > 0 {
			return point.Value, point.Timeframe, true
		}
	}
	return 0, "", false
}

func protectiveStopAnchor(side string, entry float64, snapshot *market.FactorSnapshot, timeframes []string) (float64, string, string, bool) {
	if side == "long" {
		if level, timeframe, ok := nearestStructureLevel(snapshot, "support_resistance", "support", timeframes, entry, false); ok {
			return level, timeframe, "support_resistance.support", true
		}
		if level, timeframe, ok := nearestStructureLevel(snapshot, "market_structure", "support", timeframes, entry, false); ok {
			return level, timeframe, "market_structure.support", true
		}
		if level, timeframe, ok := nearestFibonacciStop(snapshot, "long", timeframes, entry); ok {
			return level, timeframe, "fibonacci.stop_anchor", true
		}
		return 0, "", "", false
	}
	if level, timeframe, ok := nearestStructureLevel(snapshot, "support_resistance", "resistance", timeframes, entry, true); ok {
		return level, timeframe, "support_resistance.resistance", true
	}
	if level, timeframe, ok := nearestStructureLevel(snapshot, "market_structure", "resistance", timeframes, entry, true); ok {
		return level, timeframe, "market_structure.resistance", true
	}
	if level, timeframe, ok := nearestFibonacciStop(snapshot, "short", timeframes, entry); ok {
		return level, timeframe, "fibonacci.stop_anchor", true
	}
	return 0, "", "", false
}

func protectiveTargetAnchor(side string, entry float64, snapshot *market.FactorSnapshot, timeframes []string) (float64, string, string, bool) {
	if side == "long" {
		if level, timeframe, ok := nearestStructureLevel(snapshot, "support_resistance", "resistance", timeframes, entry, true); ok {
			return level, timeframe, "support_resistance.resistance", true
		}
		if level, timeframe, ok := nearestStructureLevel(snapshot, "market_structure", "resistance", timeframes, entry, true); ok {
			return level, timeframe, "market_structure.resistance", true
		}
		if level, timeframe, ok := nearestFibonacciTarget(snapshot, "long", timeframes, entry); ok {
			return level, timeframe, "fibonacci.target_level", true
		}
		return 0, "", "", false
	}
	if level, timeframe, ok := nearestStructureLevel(snapshot, "support_resistance", "support", timeframes, entry, false); ok {
		return level, timeframe, "support_resistance.support", true
	}
	if level, timeframe, ok := nearestStructureLevel(snapshot, "market_structure", "support", timeframes, entry, false); ok {
		return level, timeframe, "market_structure.support", true
	}
	if level, timeframe, ok := nearestFibonacciTarget(snapshot, "short", timeframes, entry); ok {
		return level, timeframe, "fibonacci.target_level", true
	}
	return 0, "", "", false
}

func nearestStructureLevel(snapshot *market.FactorSnapshot, name, field string, timeframes []string, entry float64, above bool) (float64, string, bool) {
	for _, timeframe := range timeframes {
		best := 0.0
		for _, structure := range snapshot.Structures[name] {
			if timeframe != "" && structure.Timeframe != timeframe {
				continue
			}
			if !structure.Valid || structure.KeyLevels == nil {
				continue
			}
			level := structure.KeyLevels[field]
			if !isCandidateLevel(level, entry, above) {
				continue
			}
			if best == 0 || closerLevel(level, best, above) {
				best = level
			}
		}
		if best > 0 {
			return best, timeframe, true
		}
	}
	return 0, "", false
}

func nearestFibonacciStop(snapshot *market.FactorSnapshot, side string, timeframes []string, entry float64) (float64, string, bool) {
	above := side == "short"
	for _, timeframe := range timeframes {
		best := 0.0
		for _, structure := range snapshot.Structures["fibonacci"] {
			if timeframe != "" && structure.Timeframe != timeframe {
				continue
			}
			if !structure.Valid {
				continue
			}
			if isCandidateLevel(structure.InvalidPrice, entry, above) && (best == 0 || closerLevel(structure.InvalidPrice, best, above)) {
				best = structure.InvalidPrice
			}
			for name, level := range structure.KeyLevels {
				if !strings.HasPrefix(name, "fib_") || !isCandidateLevel(level, entry, above) {
					continue
				}
				if best == 0 || closerLevel(level, best, above) {
					best = level
				}
			}
		}
		if best > 0 {
			return best, timeframe, true
		}
	}
	return 0, "", false
}

func nearestFibonacciTarget(snapshot *market.FactorSnapshot, side string, timeframes []string, entry float64) (float64, string, bool) {
	above := side == "long"
	for _, timeframe := range timeframes {
		best := 0.0
		for _, structure := range snapshot.Structures["fibonacci"] {
			if timeframe != "" && structure.Timeframe != timeframe {
				continue
			}
			if !structure.Valid || structure.KeyLevels == nil {
				continue
			}
			for name, level := range structure.KeyLevels {
				if !strings.HasPrefix(name, "fib_") || !isCandidateLevel(level, entry, above) {
					continue
				}
				if best == 0 || closerLevel(level, best, above) {
					best = level
				}
			}
		}
		if best > 0 {
			return best, timeframe, true
		}
	}
	return 0, "", false
}

func isCandidateLevel(level, entry float64, above bool) bool {
	if level <= 0 || entry <= 0 {
		return false
	}
	if above {
		return level > entry
	}
	return level < entry
}

func closerLevel(candidate, current float64, above bool) bool {
	if above {
		return candidate < current
	}
	return candidate > current
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
	case "literal", "value":
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
	if scoring.MinAvailableWeightRatio < 0 || scoring.MinAvailableWeightRatio > 1 {
		return fmt.Errorf("scoring_config.min_available_weight_ratio must be within [0,1]")
	}
	if scoring.MinAvailableWeightRatio > 0 && scoring.MinAvailableWeightRatio < defaultMinScoringAvailableWeightRatio {
		return fmt.Errorf("scoring_config.min_available_weight_ratio must be 0 or at least %.2f", defaultMinScoringAvailableWeightRatio)
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

func evaluateScoringSnapshot(scoring *ScoringStrategy, symbol string, snapshot *market.FactorSnapshot) ScoringEvaluationTrace {
	score, components, availableFactors, missingFactors, totalWeight, availableWeight := scoreSnapshot(scoring, snapshot)
	ratio := 0.0
	if totalWeight > 0 {
		ratio = availableWeight / totalWeight
	}
	minRatio := minAvailableWeightRatio(scoring)
	requiredCount := requiredScoringFactorCount(scoring)
	trace := ScoringEvaluationTrace{
		Symbol:                  symbol,
		Timeframe:               scoring.Timeframe,
		Score:                   score,
		AvailableFactors:        availableFactors,
		MissingFactors:          missingFactors,
		SelectedFactorCount:     len(selectedScoringFactors(scoring)),
		RequiredFactorCount:     requiredCount,
		AvailableFactorCount:    len(availableFactors),
		TotalWeight:             totalWeight,
		AvailableWeight:         availableWeight,
		AvailableWeightRatio:    ratio,
		MinAvailableWeightRatio: minRatio,
		Eligible:                true,
		FactorWeights:           scoringFactorWeights(scoring),
		Components:              components,
	}
	switch {
	case totalWeight <= 0:
		trace.Eligible = false
		trace.Reason = "scoring_config has no positive factor weights"
	case len(availableFactors) < requiredCount:
		trace.Eligible = false
		trace.Reason = fmt.Sprintf("available scoring factors %d below required %d", len(availableFactors), requiredCount)
	case ratio < minRatio:
		trace.Eligible = false
		trace.Reason = fmt.Sprintf("available scoring weight ratio %.2f below required %.2f", ratio, minRatio)
	}
	return trace
}

func scoringFactorWeights(scoring *ScoringStrategy) map[string]float64 {
	if scoring == nil {
		return nil
	}
	out := map[string]float64{}
	for _, factor := range scoring.SelectedFactors {
		factor = strings.TrimSpace(factor)
		weight := scoring.FactorWeights[factor]
		if factor != "" && weight > 0 {
			out[factor] = weight
		}
	}
	return out
}

func scoreSnapshot(scoring *ScoringStrategy, snapshot *market.FactorSnapshot) (float64, map[string]interface{}, []string, []string, float64, float64) {
	totalWeight := 0.0
	totalScore := 0.0
	components := map[string]interface{}{}
	availableFactors := []string{}
	missingFactors := []string{}
	for _, factor := range scoring.SelectedFactors {
		factor = strings.TrimSpace(factor)
		weight := scoring.FactorWeights[factor]
		if weight <= 0 {
			continue
		}
		totalWeight += weight
		component, ok := scoreComponent(factor, scoring.Timeframe, snapshot)
		if !ok {
			components[factor] = "unavailable"
			missingFactors = append(missingFactors, factor)
			continue
		}
		availableFactors = append(availableFactors, factor)
		totalScore += component * weight
		components[factor] = component
	}
	availableWeight := 0.0
	for _, factor := range availableFactors {
		availableWeight += scoring.FactorWeights[factor]
	}
	if availableWeight == 0 {
		return 0, components, availableFactors, missingFactors, totalWeight, availableWeight
	}
	return totalScore / availableWeight, components, availableFactors, missingFactors, totalWeight, availableWeight
}

func selectedScoringFactors(scoring *ScoringStrategy) []string {
	if scoring == nil {
		return nil
	}
	out := []string{}
	for _, factor := range scoring.SelectedFactors {
		factor = strings.TrimSpace(factor)
		if factor != "" && scoring.FactorWeights[factor] > 0 {
			out = append(out, factor)
		}
	}
	return out
}

func requiredScoringFactorCount(scoring *ScoringStrategy) int {
	count := len(selectedScoringFactors(scoring))
	if count > 1 {
		return 2
	}
	return count
}

func minAvailableWeightRatio(scoring *ScoringStrategy) float64 {
	if scoring == nil || scoring.MinAvailableWeightRatio <= 0 {
		return defaultMinScoringAvailableWeightRatio
	}
	if scoring.MinAvailableWeightRatio > 1 {
		return 1
	}
	return scoring.MinAvailableWeightRatio
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

func snapshotPrice(timeframe string, snapshot *market.FactorSnapshot) (float64, bool) {
	if snapshot == nil {
		return 0, false
	}
	timeframe = strings.TrimSpace(timeframe)
	if timeframe != "" {
		if price, ok := snapshot.IndicatorValue("price", timeframe, 0); ok && price > 0 {
			return price, true
		}
	}
	price, ok := snapshot.IndicatorValue("price", "", 0)
	return price, ok && price > 0
}

func trendScore(timeframe string, snapshot *market.FactorSnapshot) (float64, bool) {
	price, ok := snapshotPrice(timeframe, snapshot)
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
	price, ok := snapshotPrice(timeframe, snapshot)
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
	score := 0.0
	used := 0
	if funding, ok := snapshot.External["funding_rate"]; ok && funding.Available {
		switch funding.State {
		case "overheated_positive":
			used++
			score -= 40
		case "overheated_negative":
			used++
			score += 40
		}
	}
	for name, factor := range snapshot.External {
		if !factor.Available || name == "funding_rate" {
			continue
		}
		component, ok := derivativeExternalComponent(name, factor.Score)
		if ok {
			used++
			score += component
		}
	}
	if used == 0 {
		return 0, false
	}
	return clampScore(score / float64(used)), true
}

func derivativeExternalComponent(name string, score float64) (float64, bool) {
	switch {
	case strings.HasPrefix(name, "oi_ranking_top"), strings.HasPrefix(name, "oi_top_candidate"):
		return clampScore(score), true
	case strings.HasPrefix(name, "oi_ranking_low"):
		return -absFloat(clampScore(score)), true
	case strings.HasPrefix(name, "netflow_institution_future_top"):
		return absFloat(clampScore(score)), true
	case strings.HasPrefix(name, "netflow_institution_future_low"):
		return -absFloat(clampScore(score)), true
	case strings.HasPrefix(name, "price_ranking_top"):
		return absFloat(clampScore(score)), true
	case strings.HasPrefix(name, "price_ranking_low"):
		return -absFloat(clampScore(score)), true
	case strings.HasPrefix(name, "quant_oi_delta_"), strings.HasPrefix(name, "quant_netflow_"):
		return clampScore(score), true
	default:
		return 0, false
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
