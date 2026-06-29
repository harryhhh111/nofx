package kernel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nofx/market"
	"nofx/store"
)

func TestEvidenceTraceBlocksSingleAvailableFactor(t *testing.T) {
	scoring := testScoringStrategy()
	snapshot := testScoringSnapshot(true, false)

	traces := TraceEvidenceEvaluations(SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
	})
	if len(traces) != 1 {
		t.Fatalf("expected one evidence trace, got %d", len(traces))
	}
	if traces[0].Eligible {
		t.Fatalf("expected evidence trace to be ineligible: %+v", traces[0])
	}
	if traces[0].AvailableFactorCount != 1 || traces[0].RequiredFactorCount != 2 {
		t.Fatalf("unexpected factor counts: %+v", traces[0])
	}
}

func TestEvidenceTraceRemainsEvidenceOnlyWhenEvidenceIsSufficient(t *testing.T) {
	scoring := testScoringStrategy()
	snapshot := testScoringSnapshot(true, true)

	traces := TraceEvidenceEvaluations(SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(1, 0).UTC(),
	})
	if len(traces) != 1 {
		t.Fatalf("expected one evidence trace, got %d", len(traces))
	}
	if traces[0].Action != "" || traces[0].Threshold != 0 {
		t.Fatalf("evidence trace should not label an executable action, got %+v", traces[0])
	}
	if !traces[0].Eligible || traces[0].AvailableWeightRatio < scoring.MinAvailableWeightRatio {
		t.Fatalf("unexpected scoring evidence: %+v", traces[0])
	}
}

func TestSetupSignalEngineRequiresEntryAndConfirmationRoles(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testMultiTimeframeSetupSnapshot()

	signals, err := NewSetupSignalEngine().Generate(context.Background(), SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected one setup signal, got %d", len(signals))
	}
	if signals[0].RuleID != "trend_continuation_long" || signals[0].Timeframe != "15m" {
		t.Fatalf("unexpected setup signal: %+v", signals[0])
	}
	setup, ok := signals[0].Evidence["setup"].(SetupEvaluationTrace)
	if !ok {
		t.Fatalf("expected setup evidence, got %#v", signals[0].Evidence["setup"])
	}
	if !setup.Eligible || setup.Timeframes.Entry != "5m" || setup.Timeframes.Primary != "15m" {
		t.Fatalf("unexpected setup evidence: %+v", setup)
	}

	snapshot.Technical["ema"] = append(snapshot.Technical["ema"],
		market.IndicatorPoint{Name: "ema", Timeframe: "1h", Period: 20, Value: 120},
		market.IndicatorPoint{Name: "ema", Timeframe: "1h", Period: 50, Value: 130},
	)
	snapshot.Technical["macd_histogram"] = append(snapshot.Technical["macd_histogram"],
		market.IndicatorPoint{Name: "macd_histogram", Timeframe: "1h", Value: -1},
	)
	snapshot.Technical["rsi"] = append(snapshot.Technical["rsi"],
		market.IndicatorPoint{Name: "rsi", Timeframe: "1h", Period: 14, Value: 40},
	)
	blocked, err := NewSetupSignalEngine().Generate(context.Background(), SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Generate returned error after bearish confirmation: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("expected bearish confirmation to block long setup, got %d", len(blocked))
	}
}

func TestDerivativesScoreRequiresMaterialExternalEvidence(t *testing.T) {
	normalFunding := &market.FactorSnapshot{
		External: map[string]market.ExternalFactor{
			"funding_rate": {
				Name:      "funding_rate",
				State:     "positive",
				Available: true,
			},
		},
	}
	if score, ok := derivativesScore(normalFunding); ok {
		t.Fatalf("normal funding alone should not make derivatives available, got score %.2f", score)
	}

	overheatedFunding := &market.FactorSnapshot{
		External: map[string]market.ExternalFactor{
			"funding_rate": {
				Name:      "funding_rate",
				State:     "overheated_negative",
				Available: true,
			},
		},
	}
	if score, ok := derivativesScore(overheatedFunding); !ok || score != 40 {
		t.Fatalf("expected overheated funding to produce material derivatives evidence, got score %.2f ok=%v", score, ok)
	}

	rankingEvidence := &market.FactorSnapshot{
		External: map[string]market.ExternalFactor{
			"funding_rate": {
				Name:      "funding_rate",
				State:     "positive",
				Available: true,
			},
			"oi_ranking_top_1h": {
				Name:      "oi_ranking_top_1h",
				Score:     80,
				Available: true,
			},
		},
	}
	if score, ok := derivativesScore(rankingEvidence); !ok || score != 80 {
		t.Fatalf("expected available ranking evidence to drive derivatives score, got score %.2f ok=%v", score, ok)
	}
}

func TestSetupSignalEngineGeneratesShortWhenTimeframesAlignBearish(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.SelectedFactors = []string{"trend", "momentum"}
	scoring.FactorWeights = map[string]float64{"trend": 0.5, "momentum": 0.5}
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testBearishMultiTimeframeSetupSnapshot()

	signals, err := NewSetupSignalEngine().Generate(context.Background(), SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(4, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected one bearish setup signal, got %d", len(signals))
	}
	if signals[0].Action != "open_short" {
		t.Fatalf("expected open_short signal, got %+v", signals[0])
	}
	if signals[0].RuleID != "trend_continuation_short" || signals[0].Timeframe != "15m" {
		t.Fatalf("unexpected short setup signal: %+v", signals[0])
	}
	setup, ok := signals[0].Evidence["setup"].(SetupEvaluationTrace)
	if !ok {
		t.Fatalf("expected setup evidence, got %#v", signals[0].Evidence["setup"])
	}
	if !setup.Eligible || setup.Action != "open_short" || setup.Primary.Score >= 0 || setup.Entry.Score >= 0 {
		t.Fatalf("unexpected short setup evidence: %+v", setup)
	}
}

func TestPositionLifecycleClosesLongBeforeOppositeSetup(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.SelectedFactors = []string{"trend", "momentum"}
	scoring.FactorWeights = map[string]float64{"trend": 0.5, "momentum": 0.5}
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testBearishMultiTimeframeSetupSnapshot()
	req := SignalRequest{
		Positions: []PositionInfo{{
			Symbol:     "BTCUSDT",
			Side:       "long",
			EntryPrice: 110,
			MarkPrice:  100,
		}},
		Scoring: scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(5, 0).UTC(),
	}

	openSignals, err := NewSetupSignalEngine().Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(openSignals) != 1 || openSignals[0].Action != "open_short" {
		t.Fatalf("expected opposite open_short setup before lifecycle merge, got %+v", openSignals)
	}

	lifecycleSignals := GeneratePositionLifecycleSignals(req, openSignals)
	if len(lifecycleSignals) != 1 {
		t.Fatalf("expected one lifecycle close signal, got %d", len(lifecycleSignals))
	}
	if lifecycleSignals[0].Action != "close_long" || lifecycleSignals[0].Setup != "opposite_setup" {
		t.Fatalf("expected close_long opposite_setup signal, got %+v", lifecycleSignals[0])
	}
	trace, ok := lifecycleSignals[0].Evidence["position_lifecycle"].(PositionLifecycleTrace)
	if !ok {
		t.Fatalf("expected position lifecycle evidence, got %#v", lifecycleSignals[0].Evidence["position_lifecycle"])
	}
	if trace.PrimaryScore >= 0 || trace.EntryScore >= 0 || trace.OppositeSetup == "" {
		t.Fatalf("unexpected lifecycle trace: %+v", trace)
	}

	merged := mergePositionLifecycleSignals(openSignals, lifecycleSignals)
	if len(merged) != 1 || merged[0].Action != "close_long" {
		t.Fatalf("expected lifecycle close to suppress same-cycle reverse open, got %+v", merged)
	}
}

func TestPositionLifecycleDoesNotCloseWhenLongThesisStillAligned(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.SelectedFactors = []string{"trend", "momentum"}
	scoring.FactorWeights = map[string]float64{"trend": 0.5, "momentum": 0.5}
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	req := SignalRequest{
		Positions: []PositionInfo{{
			Symbol:     "BTCUSDT",
			Side:       "long",
			EntryPrice: 90,
			MarkPrice:  100,
		}},
		Scoring: scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": testMultiTimeframeSetupSnapshot(),
		},
		Now: time.Unix(6, 0).UTC(),
	}

	lifecycleSignals := GeneratePositionLifecycleSignals(req, nil)
	if len(lifecycleSignals) != 0 {
		t.Fatalf("expected no lifecycle close while long thesis is aligned, got %+v", lifecycleSignals)
	}
}

func TestMergePositionLifecycleSignalsSuppressesOpenWhenAnyCloseExists(t *testing.T) {
	signals := []CandidateSignal{
		{ID: "close", Symbol: "BTCUSDT", Action: "close_long"},
		{ID: "open", Symbol: "BTCUSDT", Action: "open_short"},
		{ID: "other", Symbol: "ETHUSDT", Action: "open_short"},
	}

	merged := mergePositionLifecycleSignals(signals, nil)
	if len(merged) != 2 {
		t.Fatalf("expected close plus unrelated open, got %+v", merged)
	}
	if merged[0].ID != "close" || merged[1].ID != "other" {
		t.Fatalf("expected same-symbol open to be suppressed when close exists, got %+v", merged)
	}
}

func TestTraceSetupEvaluationsKeepsStructuralTargetForRiskGate(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testMultiTimeframeSetupSnapshot()
	snapshot.Structures = map[string][]market.StructureSnapshot{
		"setup": {{
			Name:      "setup",
			Timeframe: "15m",
			Valid:     true,
			Setup:     "support_resistance_bounce_long",
			Direction: "long",
			Signals:   []string{"support bounce", "mean reversion setup"},
		}},
		"support_resistance": {{
			Name:      "support_resistance",
			Timeframe: "5m",
			Valid:     true,
			KeyLevels: map[string]float64{"support": 99, "resistance": 105},
		}},
	}

	traces := TraceSetupEvaluations(SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
	})
	if len(traces) != 1 {
		t.Fatalf("expected one setup trace, got %d", len(traces))
	}
	if !traces[0].Eligible {
		t.Fatalf("expected setup trace to stay eligible with structural target; risk gate evaluates final RR later: %+v", traces[0])
	}
}

func TestCandidateSignalUsesStructureATRAndRiskRewardForLong(t *testing.T) {
	rule := StrategyRule{
		ID:        "long_setup",
		Version:   "v1",
		Timeframe: "5m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "5m", Period: 14, Value: 10}},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "5m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 95, "resistance": 180},
			}},
		},
	}

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if err != nil {
		t.Fatalf("buildCandidateSignal returned error: %v", err)
	}
	if signal.StopLoss != 75 {
		t.Fatalf("expected stop below support with ATR buffer, got %.2f", signal.StopLoss)
	}
	if signal.TakeProfit != 180 {
		t.Fatalf("expected structural resistance target, got %.2f", signal.TakeProfit)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if levels.StopSource != "support_resistance.support" || levels.TargetSource != "support_resistance.resistance" || levels.RiskReward < 2.5 {
		t.Fatalf("unexpected protective trace: %+v", levels)
	}
}

func TestCandidateSignalUsesStructureATRAndRiskRewardForShort(t *testing.T) {
	rule := StrategyRule{
		ID:        "short_setup",
		Version:   "v1",
		Timeframe: "5m",
		Action:    "open_short",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "5m", Period: 14, Value: 10}},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "5m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 30, "resistance": 105},
			}},
		},
	}

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if err != nil {
		t.Fatalf("buildCandidateSignal returned error: %v", err)
	}
	if signal.StopLoss != 125 {
		t.Fatalf("expected stop above resistance with ATR buffer, got %.2f", signal.StopLoss)
	}
	if signal.TakeProfit != 30 {
		t.Fatalf("expected structural support target, got %.2f", signal.TakeProfit)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if levels.StopSource != "support_resistance.resistance" || levels.TargetSource != "support_resistance.support" || levels.RiskReward < 2.5 {
		t.Fatalf("unexpected protective trace: %+v", levels)
	}
}

func TestCandidateSignalRequiresATRForMarketBasedProtection(t *testing.T) {
	execution := RuleExecution{
		Leverage:        2,
		PositionSizeUSD: 100,
		StopLossPct:     2,
		TakeProfitPct:   5,
		Confidence:      80,
	}
	_, err := calculateProtectiveLevels("trend_continuation_long", "open_long", 100, execution, &market.FactorSnapshot{}, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if err == nil {
		t.Fatalf("expected missing ATR to fail market-based protective level calculation")
	}
	if !errors.Is(err, errSignalRejected) {
		t.Fatalf("expected missing ATR to be a signal rejection, got %v", err)
	}
}

func TestRuleSignalEngineSkipsCandidateWhenATRUnavailable(t *testing.T) {
	engine := NewRuleSignalEngine()
	rule := StrategyRule{
		ID:        "support_resistance_bounce_long",
		Timeframe: "5m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
		Enabled: true,
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		Technical: map[string][]market.IndicatorPoint{
			"price": {{Name: "price", Timeframe: "5m", Value: 100}},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "5m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 95, "resistance": 115},
			}},
		},
	}

	signals, err := engine.Generate(context.Background(), SignalRequest{
		Rules: []StrategyRule{rule},
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Now:        time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("expected missing ATR to skip the candidate, got error %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected missing ATR candidate to be skipped, got %+v", signals)
	}
}

func TestCandidateSignalRejectsProjectionForNonTrendSetup(t *testing.T) {
	rule := StrategyRule{
		ID:        "range_reversal_long",
		Version:   "v1",
		Timeframe: "5m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "5m", Period: 14, Value: 10}},
		},
	}

	_, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if !errors.Is(err, errSignalRejected) {
		t.Fatalf("expected non-trend setup without structure target to be rejected, got %v", err)
	}
}

func TestCandidateSignalRequiresStructuralStopForTrendSetup(t *testing.T) {
	rule := StrategyRule{
		ID:        "trend_continuation_long",
		Version:   "v1",
		Timeframe: "5m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "5m", Period: 14, Value: 10}},
		},
	}

	_, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if !errors.Is(err, errSignalRejected) {
		t.Fatalf("expected missing structural stop to reject trend setup, got %v", err)
	}
	if reason, ok := signalRejectionReason(err); !ok || !strings.Contains(reason, "no structural stop-loss anchor") {
		t.Fatalf("expected structural stop rejection, got %q err=%v", reason, err)
	}
}

func TestCandidateSignalRequiresStructuralTargetForTrendSetup(t *testing.T) {
	rule := StrategyRule{
		ID:        "trend_continuation_long",
		Version:   "v1",
		Timeframe: "5m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "5m", Period: 14, Value: 10}},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "5m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 95},
			}},
		},
	}

	_, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if !errors.Is(err, errSignalRejected) {
		t.Fatalf("expected missing structural target to reject trend setup, got %v", err)
	}
	if reason, ok := signalRejectionReason(err); !ok || !strings.Contains(reason, "no structural take-profit target") {
		t.Fatalf("expected structural target rejection, got %q err=%v", reason, err)
	}
}

func TestCandidateSignalKeepsNearStructuralTargetForBreakoutSetup(t *testing.T) {
	rule := StrategyRule{
		ID:        "breakout_long",
		Version:   "v1",
		Timeframe: "5m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "5m", Period: 14, Value: 10}},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "5m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 95, "resistance": 102},
			}},
		},
	}

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0, ProtectiveTimeframeConfig{})
	if err != nil {
		t.Fatalf("breakout signal should keep near structural target for later risk review, got error: %v", err)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if levels.TargetSource != "support_resistance.resistance" || signal.TakeProfit != 102 {
		t.Fatalf("expected breakout target to remain the structural resistance, got signal=%+v levels=%+v", signal, levels)
	}
	if levels.RiskReward >= levels.TargetRiskReward {
		t.Fatalf("fixture should keep low RR for risk gate instead of moving target, got %+v", levels)
	}
}

func TestCandidateSignalUsesPrimaryTimeframeForProtectiveLevels(t *testing.T) {
	rule := StrategyRule{
		ID:        "primary_setup",
		Version:   "v1",
		Timeframe: "15m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {
				{Name: "atr", Timeframe: "5m", Period: 14, Value: 1},
				{Name: "atr", Timeframe: "15m", Period: 14, Value: 10},
			},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {
				{
					Name:      "support_resistance",
					Timeframe: "5m",
					Valid:     true,
					KeyLevels: map[string]float64{"support": 99, "resistance": 110},
				},
				{
					Name:      "support_resistance",
					Timeframe: "15m",
					Valid:     true,
					KeyLevels: map[string]float64{"support": 90, "resistance": 220},
				},
			},
		},
	}

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "15m"}, 3, ProtectiveTimeframeConfig{})
	if err != nil {
		t.Fatalf("buildCandidateSignal returned error: %v", err)
	}
	if signal.StopLoss != 60 {
		t.Fatalf("expected primary timeframe stop loss, got %.2f", signal.StopLoss)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if levels.ATRTimeframe != "15m" || levels.StopTimeframe != "15m" || levels.ATRBuffer != 3 {
		t.Fatalf("expected primary timeframe ATR/structure with configured buffer, got %+v", levels)
	}
}

func TestCandidateSignalCanUseEntryTimeframeForProtectiveLevels(t *testing.T) {
	rule := StrategyRule{
		ID:        "entry_protective_setup",
		Version:   "v1",
		Timeframe: "15m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {
				{Name: "atr", Timeframe: "5m", Period: 14, Value: 1},
				{Name: "atr", Timeframe: "15m", Period: 14, Value: 10},
			},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {
				{
					Name:      "support_resistance",
					Timeframe: "5m",
					Valid:     true,
					KeyLevels: map[string]float64{"support": 99, "resistance": 180},
				},
				{
					Name:      "support_resistance",
					Timeframe: "15m",
					Valid:     true,
					KeyLevels: map[string]float64{"support": 90, "resistance": 220},
				},
			},
		},
	}

	signal, err := buildCandidateSignal(
		rule,
		"BTCUSDT",
		100,
		"test",
		time.Unix(2, 0).UTC(),
		snapshot,
		TimeframeRoleTrace{Entry: "5m", Primary: "15m"},
		3,
		ProtectiveTimeframeConfig{StopLossMode: store.StopLossTimeframeModeEntry},
	)
	if err != nil {
		t.Fatalf("buildCandidateSignal returned error: %v", err)
	}
	if signal.StopLoss != 96 {
		t.Fatalf("expected entry timeframe stop loss, got %.2f", signal.StopLoss)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if levels.ATRTimeframe != "5m" || levels.StopTimeframe != "5m" || levels.StopMode != store.StopLossTimeframeModeEntry {
		t.Fatalf("expected entry timeframe ATR/structure, got %+v", levels)
	}
}

func TestCandidateSignalKeepsTechnicalTargetWhenATRBufferWidensStop(t *testing.T) {
	rule := StrategyRule{
		ID:        "technical_target",
		Version:   "v1",
		Timeframe: "15m",
		Action:    "open_long",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   5,
			Confidence:      80,
		},
	}
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"atr": {{Name: "atr", Timeframe: "15m", Period: 14, Value: 10}},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "15m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 95, "resistance": 120},
			}},
		},
	}

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "15m"}, 3, ProtectiveTimeframeConfig{})
	if err != nil {
		t.Fatalf("expected technical target to remain unchanged for downstream risk gate, got err=%v", err)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if signal.TakeProfit != 120 || levels.TargetSource != "support_resistance.resistance" {
		t.Fatalf("expected ATR buffer to widen stop without moving structural take profit, got signal=%+v levels=%+v", signal, levels)
	}
	if levels.RiskReward >= levels.TargetRiskReward {
		t.Fatalf("fixture should expose low RR to risk gate instead of moving target, got %+v", levels)
	}
}

func TestTraceSetupEvaluationsBlocksMissingStructureSetup(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testNeutralMultiTimeframeSnapshot()

	traces := TraceSetupEvaluations(SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
	})
	if len(traces) != 1 {
		t.Fatalf("expected one setup trace, got %d", len(traces))
	}
	if traces[0].Eligible {
		t.Fatalf("expected neutral trace to be ineligible: %+v", traces[0])
	}
	if traces[0].Setup != "no_trade_no_structure_setup" {
		t.Fatalf("expected missing structure setup to block trading, got %+v", traces[0])
	}
}

func TestSetupSignalEngineUsesStructureBreakoutSetup(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testMultiTimeframeSetupSnapshot()
	snapshot.Structures["setup"][0].Setup = "breakout_long"
	snapshot.Structures["setup"][0].Signals = []string{"confirmed range break", "price above resistance"}

	signals, err := NewSetupSignalEngine().Generate(context.Background(), SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(3, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected one breakout setup signal, got %d", len(signals))
	}
	if signals[0].RuleID != "breakout_long" {
		t.Fatalf("expected breakout_long setup, got %+v", signals[0])
	}
}

func TestSetupSignalEngineDoesNotOpenFromScoresWithoutStructureSetup(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testMultiTimeframeSetupSnapshot()
	snapshot.Structures = nil

	signals, err := NewSetupSignalEngine().Generate(context.Background(), SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(3, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("expected no signal without deterministic structure setup, got %+v", signals)
	}

	traces := TraceSetupEvaluations(SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
	})
	if len(traces) != 1 || traces[0].Setup != "no_trade_no_structure_setup" || traces[0].Eligible {
		t.Fatalf("expected no_trade_no_structure_setup trace, got %+v", traces)
	}
	if traces[0].Primary.Score < scoring.LongThreshold {
		t.Fatalf("test fixture should still have strong score evidence, got %+v", traces[0])
	}
}

func TestSetupSignalEngineUsesStructureSetupBeforeScoreThreshold(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.SelectedFactors = []string{"trend"}
	scoring.FactorWeights = map[string]float64{"trend": 1}
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = nil
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {
				{Name: "price", Value: 100},
				{Name: "price", Timeframe: "15m", Value: 100},
				{Name: "price", Timeframe: "5m", Value: 100},
			},
			"ema": {
				{Name: "ema", Timeframe: "15m", Period: 20, Value: 101},
				{Name: "ema", Timeframe: "15m", Period: 50, Value: 99},
				{Name: "ema", Timeframe: "5m", Period: 20, Value: 101},
				{Name: "ema", Timeframe: "5m", Period: 50, Value: 99},
			},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "15m", Value: 1},
				{Name: "macd_histogram", Timeframe: "5m", Value: 1},
			},
			"atr": {
				{Name: "atr", Timeframe: "15m", Period: 14, Value: 1},
				{Name: "atr", Timeframe: "5m", Period: 14, Value: 1},
			},
		},
		Structures: map[string][]market.StructureSnapshot{
			"setup": {{
				Name:      "setup",
				Timeframe: "15m",
				Valid:     true,
				Setup:     "trend_continuation_long",
				Direction: "long",
				Signals:   []string{"HH/HL trend", "current leg supports continuation"},
			}},
			"market_structure": {{
				Name:      "market_structure",
				Timeframe: "15m",
				Valid:     true,
				Direction: "up",
				KeyLevels: map[string]float64{"support": 95, "resistance": 120},
			}},
		},
	}

	signals, err := NewSetupSignalEngine().Generate(context.Background(), SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
		Now: time.Unix(4, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected structure setup signal despite score below old threshold, got %d", len(signals))
	}
	if signals[0].RuleID != "trend_continuation_long" || signals[0].Action != "open_long" {
		t.Fatalf("expected structure-driven trend continuation, got %+v", signals[0])
	}
	setup, ok := signals[0].Evidence["setup"].(SetupEvaluationTrace)
	if !ok || setup.Primary.Score >= scoring.LongThreshold {
		t.Fatalf("expected setup evidence with sub-threshold score, got %#v", signals[0].Evidence["setup"])
	}
}

func TestPreferredStructureSetupUsesEntryTriggerWhenPrimaryWaits(t *testing.T) {
	snapshot := &market.FactorSnapshot{
		Structures: map[string][]market.StructureSnapshot{
			"setup": {
				{
					Name:      "setup",
					Timeframe: "15m",
					Valid:     false,
					Setup:     "no_trade_wait_trigger",
					Direction: "long",
					Signals:   []string{"HH/HL trend", "waiting for entry trigger"},
				},
				{
					Name:      "setup",
					Timeframe: "5m",
					Valid:     true,
					Setup:     "trend_pullback_long",
					Direction: "long",
					Signals:   []string{"HH/HL trend", "pullback near latest support"},
				},
			},
		},
	}

	setup, ok := preferredStructureSetup(snapshot, TimeframeRoleTrace{Primary: "15m", Entry: "5m"})
	if !ok {
		t.Fatal("expected preferred setup")
	}
	if setup.Setup != "trend_pullback_long" || setup.Timeframe != "5m" {
		t.Fatalf("expected entry trigger to outrank primary wait state, got %+v", setup)
	}
}

func TestPreferredStructureSetupKeepsPrimaryInvalidationAsHardBlock(t *testing.T) {
	snapshot := &market.FactorSnapshot{
		Structures: map[string][]market.StructureSnapshot{
			"setup": {
				{
					Name:      "setup",
					Timeframe: "15m",
					Valid:     false,
					Setup:     "no_trade_structure_invalidated",
					Phase:     "invalidated",
				},
				{
					Name:      "setup",
					Timeframe: "5m",
					Valid:     true,
					Setup:     "trend_pullback_long",
					Direction: "long",
				},
			},
		},
	}

	setup, ok := preferredStructureSetup(snapshot, TimeframeRoleTrace{Primary: "15m", Entry: "5m"})
	if !ok {
		t.Fatal("expected preferred setup")
	}
	if setup.Setup != "no_trade_structure_invalidated" || setup.Timeframe != "15m" {
		t.Fatalf("expected primary invalidation to remain a hard block, got %+v", setup)
	}
}

func TestPreferredStructureSetupDoesNotUseConfirmationWhenPrimaryEntryAreNoTrade(t *testing.T) {
	snapshot := &market.FactorSnapshot{
		Structures: map[string][]market.StructureSnapshot{
			"setup": {
				{
					Name:      "setup",
					Timeframe: "15m",
					Valid:     false,
					Setup:     "no_trade_chop",
					Direction: "neutral",
				},
				{
					Name:      "setup",
					Timeframe: "5m",
					Valid:     false,
					Setup:     "no_trade_chop",
					Direction: "neutral",
				},
				{
					Name:      "setup",
					Timeframe: "1h",
					Valid:     true,
					Setup:     "failed_breakout_short",
					Direction: "short",
				},
			},
		},
	}

	setup, ok := preferredStructureSetup(snapshot, TimeframeRoleTrace{Primary: "15m", Entry: "5m", Confirmations: []string{"1h"}})
	if !ok {
		t.Fatal("expected preferred setup")
	}
	if setup.Setup != "no_trade_chop" || setup.Timeframe != "15m" {
		t.Fatalf("expected primary no-trade setup to block confirmation-only trade, got %+v", setup)
	}
}

func TestScoresSupportStructureSetupRejectsStrongOppositeReversalEvidence(t *testing.T) {
	if scoresSupportStructureSetup("open_short", "failed_breakout_short", ScoringEvaluationTrace{Score: 41}, ScoringEvaluationTrace{Score: -10}) {
		t.Fatal("expected strongly bullish primary score to reject failed-breakout short")
	}
	if !scoresSupportStructureSetup("open_short", "failed_breakout_short", ScoringEvaluationTrace{Score: 10}, ScoringEvaluationTrace{Score: -10}) {
		t.Fatal("expected neutral primary score with bearish entry to allow failed-breakout short")
	}
	if scoresSupportStructureSetup("open_long", "range_reversal_long", ScoringEvaluationTrace{Score: -41}, ScoringEvaluationTrace{Score: 10}) {
		t.Fatal("expected strongly bearish primary score to reject reversal long")
	}
	if !scoresSupportStructureSetup("open_long", "range_reversal_long", ScoringEvaluationTrace{Score: -10}, ScoringEvaluationTrace{Score: 10}) {
		t.Fatal("expected neutral primary score with bullish entry to allow reversal long")
	}
}

func TestTrendScoreUsesTimeframePrice(t *testing.T) {
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {
				{Name: "price", Value: 100},
				{Name: "price", Timeframe: "5m", Value: 120},
			},
			"ema": {
				{Name: "ema", Timeframe: "5m", Period: 20, Value: 110},
				{Name: "ema", Timeframe: "5m", Period: 50, Value: 115},
			},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "5m", Value: 1},
			},
		},
	}

	score, ok := trendScore("5m", snapshot)
	if !ok {
		t.Fatalf("expected 5m trend score to be available")
	}
	if score <= 0 {
		t.Fatalf("expected 5m timeframe price to produce bullish trend score, got %.2f", score)
	}
}

func testScoringStrategy() *ScoringStrategy {
	return &ScoringStrategy{
		Enabled: true,
		SelectedFactors: []string{
			"trend",
			"momentum",
			"structure",
			"derivatives",
		},
		FactorWeights: map[string]float64{
			"trend":       1,
			"momentum":    1,
			"structure":   1,
			"derivatives": 0.5,
		},
		LongThreshold:           60,
		ShortThreshold:          -60,
		MinAvailableWeightRatio: 0.5,
		MinConfidence:           70,
		Timeframe:               "3m",
		Execution: RuleExecution{
			Leverage:        2,
			PositionSizeUSD: 100,
			StopLossPct:     2,
			TakeProfitPct:   4,
			Confidence:      70,
		},
	}
}

func testNeutralMultiTimeframeSnapshot() *market.FactorSnapshot {
	return &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {{Name: "price", Value: 100}},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "5m", Value: 0},
				{Name: "macd_histogram", Timeframe: "15m", Value: 0},
				{Name: "macd_histogram", Timeframe: "1h", Value: 0},
			},
			"rsi": {
				{Name: "rsi", Timeframe: "5m", Period: 14, Value: 50},
				{Name: "rsi", Timeframe: "15m", Period: 14, Value: 50},
				{Name: "rsi", Timeframe: "1h", Period: 14, Value: 50},
			},
		},
	}
}

func testScoringSnapshot(includeTrend, includeMomentum bool) *market.FactorSnapshot {
	technical := map[string][]market.IndicatorPoint{
		"price": {{Name: "price", Value: 100}},
		"atr":   {{Name: "atr", Timeframe: "3m", Period: 14, Value: 2}},
	}
	if includeTrend {
		technical["ema"] = []market.IndicatorPoint{
			{Name: "ema", Timeframe: "3m", Period: 20, Value: 90},
			{Name: "ema", Timeframe: "3m", Period: 50, Value: 80},
		}
		technical["macd_histogram"] = []market.IndicatorPoint{
			{Name: "macd_histogram", Timeframe: "3m", Value: 1},
		}
	}
	if includeMomentum {
		technical["rsi"] = []market.IndicatorPoint{
			{Name: "rsi", Timeframe: "3m", Period: 14, Value: 60},
		}
	}
	snapshot := &market.FactorSnapshot{
		Symbol:    "BTCUSDT",
		AsOf:      time.Unix(1, 0).UTC(),
		Technical: technical,
	}
	if includeMomentum {
		snapshot.Structures = map[string][]market.StructureSnapshot{
			"support_resistance": {{
				Name:      "support_resistance",
				Timeframe: "3m",
				Valid:     true,
				KeyLevels: map[string]float64{"support": 99, "resistance": 130},
			}},
		}
	}
	return snapshot
}

func testMultiTimeframeSetupSnapshot() *market.FactorSnapshot {
	return &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {{Name: "price", Value: 100}},
			"ema": {
				{Name: "ema", Timeframe: "5m", Period: 20, Value: 92},
				{Name: "ema", Timeframe: "5m", Period: 50, Value: 88},
				{Name: "ema", Timeframe: "15m", Period: 20, Value: 90},
				{Name: "ema", Timeframe: "15m", Period: 50, Value: 82},
				{Name: "ema", Timeframe: "1h", Period: 20, Value: 94},
				{Name: "ema", Timeframe: "1h", Period: 50, Value: 86},
			},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "5m", Value: 1},
				{Name: "macd_histogram", Timeframe: "15m", Value: 1},
				{Name: "macd_histogram", Timeframe: "1h", Value: 1},
			},
			"rsi": {
				{Name: "rsi", Timeframe: "5m", Period: 14, Value: 60},
				{Name: "rsi", Timeframe: "15m", Period: 14, Value: 60},
				{Name: "rsi", Timeframe: "1h", Period: 14, Value: 58},
			},
			"atr": {
				{Name: "atr", Timeframe: "5m", Period: 14, Value: 2},
				{Name: "atr", Timeframe: "15m", Period: 14, Value: 3},
				{Name: "atr", Timeframe: "1h", Period: 14, Value: 4},
			},
		},
		Structures: map[string][]market.StructureSnapshot{
			"setup": {{
				Name:      "setup",
				Timeframe: "15m",
				Valid:     true,
				Setup:     "trend_continuation_long",
				Direction: "long",
				Signals:   []string{"HH/HL trend", "current leg supports continuation"},
			}},
			"market_structure": {{
				Name:      "market_structure",
				Timeframe: "15m",
				Valid:     true,
				Direction: "up",
				KeyLevels: map[string]float64{"support": 95, "resistance": 130},
			}},
		},
	}
}

func testBearishMultiTimeframeSetupSnapshot() *market.FactorSnapshot {
	return &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {{Name: "price", Value: 100}},
			"ema": {
				{Name: "ema", Timeframe: "5m", Period: 20, Value: 108},
				{Name: "ema", Timeframe: "5m", Period: 50, Value: 112},
				{Name: "ema", Timeframe: "15m", Period: 20, Value: 110},
				{Name: "ema", Timeframe: "15m", Period: 50, Value: 118},
				{Name: "ema", Timeframe: "1h", Period: 20, Value: 115},
				{Name: "ema", Timeframe: "1h", Period: 50, Value: 122},
			},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "5m", Value: -1},
				{Name: "macd_histogram", Timeframe: "15m", Value: -1},
				{Name: "macd_histogram", Timeframe: "1h", Value: -1},
			},
			"rsi": {
				{Name: "rsi", Timeframe: "5m", Period: 14, Value: 40},
				{Name: "rsi", Timeframe: "15m", Period: 14, Value: 40},
				{Name: "rsi", Timeframe: "1h", Period: 14, Value: 42},
			},
			"atr": {
				{Name: "atr", Timeframe: "5m", Period: 14, Value: 2},
				{Name: "atr", Timeframe: "15m", Period: 14, Value: 3},
				{Name: "atr", Timeframe: "1h", Period: 14, Value: 4},
			},
		},
		Structures: map[string][]market.StructureSnapshot{
			"setup": {{
				Name:      "setup",
				Timeframe: "15m",
				Valid:     true,
				Setup:     "trend_continuation_short",
				Direction: "short",
				Signals:   []string{"LL/LH trend", "current leg supports continuation"},
			}},
			"market_structure": {{
				Name:      "market_structure",
				Timeframe: "15m",
				Valid:     true,
				Direction: "down",
				KeyLevels: map[string]float64{"support": 70, "resistance": 105},
			}},
		},
	}
}
