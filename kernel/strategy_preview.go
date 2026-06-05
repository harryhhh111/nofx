package kernel

import (
	"context"
	"nofx/market"
	"nofx/store"
	"time"
)

type StrategySignalPreview struct {
	Signals            []CandidateSignal        `json:"signals"`
	RuleEvaluations    []RuleEvaluationTrace    `json:"rule_evaluations"`
	ScoringEvaluations []ScoringEvaluationTrace `json:"scoring_evaluations"`
	SetupEvaluations   []SetupEvaluationTrace   `json:"setup_evaluations"`
}

func PreviewStrategySignals(config *store.StrategyConfig, candidates []CandidateCoin, factorSnapshots map[string]*market.FactorSnapshot, now time.Time) (*StrategySignalPreview, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	req := SignalRequest{
		Candidates:     candidates,
		Rules:          rulesFromStrategyConfig(config),
		Scoring:        scoringFromStrategyConfig(config),
		FactorSnapshot: factorSnapshots,
		Now:            now,
	}
	signals, err := signalEngineFromStrategyConfig(config).Generate(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return &StrategySignalPreview{
		Signals:            signals,
		RuleEvaluations:    TraceRuleEvaluations(req),
		ScoringEvaluations: TraceScoringEvaluations(req),
		SetupEvaluations:   TraceSetupEvaluations(req),
	}, nil
}
