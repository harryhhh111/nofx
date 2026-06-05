package kernel

import (
	"strings"
	"testing"
)

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

func TestParseStrategyCompileResponseRequiresStrictJSON(t *testing.T) {
	text := `
{
  "strategy_mode": "scoring",
  "rules": [],
  "scoring_config": {
    "enabled": true,
    "selected_factors": ["trend"],
    "factor_weights": {"trend": 1},
    "long_threshold": 70,
    "short_threshold": -70,
    "min_available_weight_ratio": 0.5,
    "min_confidence": 70,
    "timeframe": "15m",
    "execution": {
      "leverage": 2,
      "position_size_usd": 12,
      "stop_loss_pct": 2,
      "take_profit_pct": 5
    }
  },
  "warnings": [],
  "errors": []
}
`

	result, err := parseStrategyCompileResponse(text)
	if err != nil {
		t.Fatalf("parseStrategyCompileResponse returned error: %v", err)
	}
	if result.StrategyMode != "scoring" {
		t.Fatalf("expected scoring mode, got %q", result.StrategyMode)
	}
}

func TestParseStrategyCompileResponseAcceptsCompleteJSONFence(t *testing.T) {
	text := "```json\n" + `
{
  "strategy_mode": "scoring",
  "rules": [],
  "scoring_config": {
    "enabled": true,
    "selected_factors": ["trend"],
    "factor_weights": {"trend": 1},
    "long_threshold": 70,
    "short_threshold": -70,
    "min_available_weight_ratio": 0.5,
    "min_confidence": 70,
    "timeframe": "15m",
    "execution": {
      "leverage": 2,
      "position_size_usd": 12,
      "stop_loss_pct": 2,
      "take_profit_pct": 5
    }
  },
  "warnings": [],
  "errors": []
}
` + "```"

	result, err := parseStrategyCompileResponse(text)
	if err != nil {
		t.Fatalf("parseStrategyCompileResponse returned error: %v", err)
	}
	if result.StrategyMode != "scoring" {
		t.Fatalf("expected scoring mode, got %q", result.StrategyMode)
	}
}

func TestParseStrategyCompileResponseRejectsIncompleteJSONFence(t *testing.T) {
	_, err := parseStrategyCompileResponse("```json\n{\"strategy_mode\":\"scoring\"")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "incomplete or invalid") {
		t.Fatalf("expected incomplete fenced JSON error, got %v", err)
	}
}

func TestParseStrategyCompileResponseReportsNonJSONPreview(t *testing.T) {
	_, err := parseStrategyCompileResponse("ânot jsonâ")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "structured output was not valid JSON") {
		t.Fatalf("expected structured output JSON error, got %v", err)
	}
}
