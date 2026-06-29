package api

import (
	"nofx/store"
	"testing"
)

func TestStrategyEvolutionGateAllowsOnlyEvidenceBackedStates(t *testing.T) {
	allowed := []string{"paper_ready", "needs_review"}
	for _, gate := range allowed {
		if !strategyEvolutionGateAllows(&store.SignalCalibrationReport{QualityGate: gate}) {
			t.Fatalf("expected gate %s to be allowed", gate)
		}
	}
	blocked := []string{"no_data", "collecting", "blocked", "paper_collecting", "outcome_collecting", ""}
	for _, gate := range blocked {
		if strategyEvolutionGateAllows(&store.SignalCalibrationReport{QualityGate: gate}) {
			t.Fatalf("expected gate %s to be blocked", gate)
		}
	}
}
