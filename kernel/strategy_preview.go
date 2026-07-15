package kernel

import (
	"context"
	"nofx/market"
	"nofx/store"
	"time"
)

type StrategySignalPreview struct {
	Signals             []CandidateSignal        `json:"signals"`
	RuleEvaluations     []RuleEvaluationTrace    `json:"rule_evaluations"`
	EvidenceEvaluations []ScoringEvaluationTrace `json:"evidence_evaluations"`
	SetupEvaluations    []SetupEvaluationTrace   `json:"setup_evaluations"`
}

func PreviewStrategySignals(config *store.StrategyConfig, candidates []CandidateCoin, factorSnapshots map[string]*market.FactorSnapshot, now time.Time) (*StrategySignalPreview, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if config != nil {
		config.NormalizeForExecution()
		if err := config.ValidateExecutableSignalSource(); err != nil {
			return nil, err
		}
	}
	req := SignalRequest{
		Candidates:           candidates,
		Rules:                rulesFromStrategyConfig(config),
		Scoring:              scoringFromStrategyConfig(config),
		MinRiskRewardRatio:   config.RiskControl.MinRiskRewardRatio,
		ProtectiveATRBuffer:  config.RiskControl.StopLossATRBuffer,
		ProtectiveTimeframes: protectiveTimeframesFromRiskControl(config.RiskControl),
		FactorSnapshot:       factorSnapshots,
		Now:                  now,
	}
	signals, err := signalEngineFromStrategyConfig(config).Generate(context.Background(), req)
	if err != nil {
		return nil, err
	}
	evidenceEvaluations := TraceEvidenceEvaluations(req)
	return &StrategySignalPreview{
		Signals:             signals,
		RuleEvaluations:     TraceRuleEvaluations(req),
		EvidenceEvaluations: evidenceEvaluations,
		SetupEvaluations:    TraceSetupEvaluations(req),
	}, nil
}
