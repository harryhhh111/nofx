package kernel

import (
	"testing"

	"nofx/store"
)

func TestApplyStrategyEvolutionPatchNormalizesSafeFields(t *testing.T) {
	config := store.GetDefaultStrategyConfig("zh")
	config.StrategyMode = "scoring"
	config.ScoringConfig = &store.ScoringStrategyConfig{
		Enabled:                 true,
		SelectedFactors:         []string{"trend", "momentum", "structure", "derivatives"},
		FactorWeights:           map[string]float64{"trend": 0.25, "momentum": 0.25, "structure": 0.25, "derivatives": 0.25},
		LongThreshold:           70,
		ShortThreshold:          -70,
		MinAvailableWeightRatio: 0.5,
		MinConfidence:           70,
		Execution: store.CompiledRuleExecution{
			Leverage:   5,
			Confidence: 70,
		},
	}

	shortThreshold := 55.0
	longThreshold := 62.0
	minConfidence := 58
	risk := 0.6
	lookback := 800
	proposed, warnings, err := ApplyStrategyEvolutionPatch(&config, StrategyEvolutionConfigPatch{
		RiskProfile: "aggressive",
		ScoringConfig: &ScoringEvolutionPatch{
			FactorWeights:  map[string]float64{"trend": 3, "momentum": 1, "structure": 1, "derivatives": 0},
			LongThreshold:  &longThreshold,
			ShortThreshold: &shortThreshold,
			MinConfidence:  &minConfidence,
		},
		RiskControl: &RiskControlEvolutionPatch{
			RiskPerTradePct: &risk,
			MinConfidence:   &minConfidence,
		},
		Klines: &KlineEvolutionPatch{
			PrimaryTimeframe: "5m",
			EntryTimeframe:   "15m",
			ComputeLookback:  &lookback,
		},
	})
	if err != nil {
		t.Fatalf("ApplyStrategyEvolutionPatch returned error: %v", err)
	}
	if proposed.RiskProfile != "aggressive" {
		t.Fatalf("expected risk profile to update, got %q", proposed.RiskProfile)
	}
	if proposed.ScoringConfig.ShortThreshold != -55 {
		t.Fatalf("expected positive short threshold to normalize to -55, got %.2f", proposed.ScoringConfig.ShortThreshold)
	}
	if len(warnings) == 0 {
		t.Fatal("expected normalization warning")
	}
	sum := 0.0
	for _, factor := range proposed.ScoringConfig.SelectedFactors {
		sum += proposed.ScoringConfig.FactorWeights[factor]
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("expected normalized weights to sum to 1, got %.4f", sum)
	}
	if proposed.RiskControl.RiskPerTradePct != risk {
		t.Fatalf("expected risk_per_trade_pct to update, got %.2f", proposed.RiskControl.RiskPerTradePct)
	}
	if proposed.Indicators.Klines.PrimaryTimeframe != "5m" || proposed.Indicators.Klines.EntryTimeframe != "15m" {
		t.Fatalf("expected timeframe roles to update, got %+v", proposed.Indicators.Klines)
	}
	if proposed.Indicators.Klines.ComputeLookback != lookback {
		t.Fatalf("expected compute lookback to update, got %d", proposed.Indicators.Klines.ComputeLookback)
	}
	if proposed.ScoringConfig.Timeframe != proposed.Indicators.Klines.PrimaryTimeframe {
		t.Fatalf("expected scoring timeframe to follow primary timeframe, got %s", proposed.ScoringConfig.Timeframe)
	}
}

func TestParseStrategyEvolutionResponseRequiresSummary(t *testing.T) {
	_, err := parseStrategyEvolutionResponse(`{"evidence_quality":"limited","data_used":{"samples":1,"closed_trades":0,"setups":1},"diagnosis":[],"recommended_changes":[],"config_patch":{},"requires_paper_validation":true}`)
	if err == nil {
		t.Fatal("expected missing summary to fail")
	}
}

func TestParseStrategyEvolutionResponseNormalizesStringItems(t *testing.T) {
	proposal, err := parseStrategyEvolutionResponse(`{
		"summary": "证据有限，建议先小幅优化。",
		"evidence_quality": "limited",
		"data_used": {"samples": 12, "closed_trades": 1, "setups": 3},
		"diagnosis": ["样本不足，不能大幅调参"],
		"recommended_changes": ["保持风险参数，仅调整评分阈值"],
		"config_patch": {},
		"requires_paper_validation": true
	}`)
	if err != nil {
		t.Fatalf("parseStrategyEvolutionResponse returned error: %v", err)
	}
	if len(proposal.Diagnosis) != 1 || proposal.Diagnosis[0].Area != "general" || proposal.Diagnosis[0].Finding == "" {
		t.Fatalf("expected string diagnosis to normalize, got %+v", proposal.Diagnosis)
	}
	if len(proposal.RecommendedChanges) != 1 || proposal.RecommendedChanges[0].Field != "strategy" || proposal.RecommendedChanges[0].Rationale == "" {
		t.Fatalf("expected string recommended change to normalize, got %+v", proposal.RecommendedChanges)
	}
}
