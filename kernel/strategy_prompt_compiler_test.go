package kernel

import "testing"

func TestValidateCompiledStrategyNormalizesPositiveShortThreshold(t *testing.T) {
	result := &StrategyCompileResult{
		StrategyMode: "scoring",
		Rules:        []StrategyRule{},
		ScoringConfig: &ScoringStrategy{
			Enabled: true,
			SelectedFactors: []string{
				"trend",
				"momentum",
			},
			FactorWeights: map[string]float64{
				"trend":    0.5,
				"momentum": 0.5,
			},
			LongThreshold:           70,
			ShortThreshold:          30,
			MinAvailableWeightRatio: 0.5,
			MinConfidence:           70,
			Timeframe:               "15m",
			Execution: RuleExecution{
				Leverage:        2,
				PositionSizeUSD: 12,
				StopLossPct:     2,
				TakeProfitPct:   5,
			},
		},
	}

	if err := validateCompiledStrategy(result); err != nil {
		t.Fatalf("validateCompiledStrategy returned error: %v", err)
	}
	if result.ScoringConfig.ShortThreshold != -30 {
		t.Fatalf("expected short threshold to normalize to -30, got %v", result.ScoringConfig.ShortThreshold)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected normalization warning")
	}
}
