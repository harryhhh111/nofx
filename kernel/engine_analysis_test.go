package kernel

import (
	"strings"
	"testing"

	"nofx/market"
	"nofx/store"
)

func TestStructureRequestIncludesScoringRoleTimeframes(t *testing.T) {
	config := store.GetDefaultStrategyConfig("zh")
	config.Indicators.Klines.PrimaryTimeframe = "15m"
	config.Indicators.Klines.EntryTimeframe = "5m"
	config.Indicators.Klines.ConfirmationTimeframes = []string{"1h"}
	config.Indicators.Klines.SelectedTimeframes = []string{"5m", "15m", "1h"}
	config.ScoringConfig = &store.ScoringStrategyConfig{}
	config.ScoringConfig.Enabled = true
	config.ScoringConfig.SelectedFactors = []string{"trend", "momentum", "structure", "derivatives"}
	config.ClampLimits()

	req := StructureRequestFromStrategyConfig(&config)

	fibTimeframes := map[string]bool{}
	if req.Fibonacci != nil {
		fibTimeframes[req.Fibonacci.Timeframe] = true
	}
	for _, fib := range req.Fibonaccis {
		fibTimeframes[fib.Timeframe] = true
	}
	supportTimeframes := map[string]bool{}
	if req.Support != nil {
		supportTimeframes[req.Support.Timeframe] = true
	}
	for _, support := range req.Supports {
		supportTimeframes[support.Timeframe] = true
	}

	for _, timeframe := range []string{"15m", "5m", "1h"} {
		if !fibTimeframes[timeframe] {
			t.Fatalf("expected fibonacci structure request for %s, got %+v", timeframe, req)
		}
		if !supportTimeframes[timeframe] {
			t.Fatalf("expected support/resistance structure request for %s, got %+v", timeframe, req)
		}
	}
}

func TestUserDecisionSummaryUsesFriendlyNoTradeReason(t *testing.T) {
	result := &TradingEngineResult{
		SetupEvaluations: []SetupEvaluationTrace{
			{
				Symbol:   "BTCUSDT",
				Setup:    "no_trade_threshold_not_met",
				Eligible: false,
				Reason:   "no setup: primary score 56.50, entry score 10.00; long not ready",
				Primary: ScoringEvaluationTrace{
					Score:    56.5,
					Eligible: true,
				},
				Entry: ScoringEvaluationTrace{
					Score:    10,
					Eligible: true,
				},
			},
		},
	}

	summary := buildUserDecisionSummary(result)
	if summary == nil || len(summary.Symbols) != 1 {
		t.Fatalf("expected one symbol summary, got %+v", summary)
	}

	symbol := summary.Symbols[0]
	if symbol.Reason == result.SetupEvaluations[0].Reason {
		t.Fatalf("expected friendly reason, got raw technical reason %q", symbol.Reason)
	}
	if strings.Contains(symbol.Reason, "primary score") || strings.Contains(symbol.Reason, "long not ready") {
		t.Fatalf("friendly reason still exposes technical trace: %q", symbol.Reason)
	}
	if !strings.Contains(symbol.Reason, "入场触发") {
		t.Fatalf("expected entry trigger oriented reason, got %q", symbol.Reason)
	}

	hasTechnicalDetail := false
	for _, detail := range symbol.Details {
		if strings.Contains(detail, result.SetupEvaluations[0].Reason) {
			hasTechnicalDetail = true
			break
		}
	}
	if !hasTechnicalDetail {
		t.Fatalf("expected raw technical reason to remain in details, got %+v", symbol.Details)
	}
}

func TestUserFriendlyNoTradeReasonHandlesEvidenceGaps(t *testing.T) {
	reason := userFriendlyNoTradeReason(SetupEvaluationTrace{
		Primary: ScoringEvaluationTrace{Eligible: true, Score: 20},
		Entry:   ScoringEvaluationTrace{Eligible: false, Reason: "missing factors"},
	})
	if !strings.Contains(reason, "入场周期") || strings.Contains(reason, "missing factors") {
		t.Fatalf("expected friendly entry evidence gap reason, got %q", reason)
	}
}

func TestPreferredATR14FollowsStopLossTimeframeMode(t *testing.T) {
	config := store.GetDefaultStrategyConfig("zh")
	config.Indicators.Klines.PrimaryTimeframe = "15m"
	config.Indicators.Klines.EntryTimeframe = "5m"
	config.Indicators.Klines.SelectedTimeframes = []string{"5m", "15m", "1h"}
	config.ClampLimits()
	data := &market.Data{
		TimeframeData: map[string]*market.TimeframeSeriesData{
			"5m":  {Timeframe: "5m", ATR14: 1},
			"15m": {Timeframe: "15m", ATR14: 10},
		},
	}

	autoATR := preferredATR14(data, &config, config.RiskControl)
	if autoATR != 10 {
		t.Fatalf("expected auto stop-loss ATR to use primary timeframe, got %.2f", autoATR)
	}

	config.RiskControl.StopLossTimeframeMode = store.StopLossTimeframeModeEntry
	entryATR := preferredATR14(data, &config, config.RiskControl)
	if entryATR != 1 {
		t.Fatalf("expected entry stop-loss ATR to use entry timeframe, got %.2f", entryATR)
	}
}
