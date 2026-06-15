package kernel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nofx/market"
)

func TestScoreSignalEngineBlocksSingleAvailableFactor(t *testing.T) {
	scoring := testScoringStrategy()
	snapshot := testScoringSnapshot(true, false)

	signals, err := NewScoreSignalEngine().Generate(context.Background(), SignalRequest{
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
	if len(signals) != 0 {
		t.Fatalf("expected no signal with one available scoring factor, got %d", len(signals))
	}

	traces := TraceScoringEvaluations(SignalRequest{
		Candidates: []CandidateCoin{{Symbol: "BTCUSDT"}},
		Scoring:    scoring,
		FactorSnapshot: map[string]*market.FactorSnapshot{
			"BTCUSDT": snapshot,
		},
	})
	if len(traces) != 1 {
		t.Fatalf("expected one scoring trace, got %d", len(traces))
	}
	if traces[0].Eligible {
		t.Fatalf("expected scoring trace to be ineligible: %+v", traces[0])
	}
	if traces[0].AvailableFactorCount != 1 || traces[0].RequiredFactorCount != 2 {
		t.Fatalf("unexpected factor counts: %+v", traces[0])
	}
}

func TestScoreSignalEngineGeneratesWhenEvidenceIsSufficient(t *testing.T) {
	scoring := testScoringStrategy()
	snapshot := testScoringSnapshot(true, true)

	signals, err := NewScoreSignalEngine().Generate(context.Background(), SignalRequest{
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
		t.Fatalf("expected one signal with trend and momentum evidence, got %d", len(signals))
	}
	if signals[0].Action != "open_long" {
		t.Fatalf("expected open_long signal, got %s", signals[0].Action)
	}
	scoringEvidence, ok := signals[0].Evidence["scoring"].(ScoringEvaluationTrace)
	if !ok {
		t.Fatalf("expected scoring evidence trace, got %#v", signals[0].Evidence["scoring"])
	}
	if !scoringEvidence.Eligible || scoringEvidence.AvailableWeightRatio < scoring.MinAvailableWeightRatio {
		t.Fatalf("unexpected scoring evidence: %+v", scoringEvidence)
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

func TestTraceSetupEvaluationsExplainsProtectiveFilter(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testMultiTimeframeSetupSnapshot()
	snapshot.Structures = map[string][]market.StructureSnapshot{
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
	if traces[0].Eligible {
		t.Fatalf("expected protective filter to mark setup ineligible: %+v", traces[0])
	}
	if !strings.Contains(traces[0].Reason, "protective filter") || !strings.Contains(traces[0].Reason, "risk/reward") {
		t.Fatalf("expected protective filter reason, got %+v", traces[0])
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

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0)
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

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0)
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
	_, err := calculateProtectiveLevels("trend_continuation_long", "open_long", 100, execution, &market.FactorSnapshot{}, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0)
	if err == nil {
		t.Fatalf("expected missing ATR to fail market-based protective level calculation")
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

	_, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0)
	if !errors.Is(err, errSignalRejected) {
		t.Fatalf("expected non-trend setup without structure target to be rejected, got %v", err)
	}
}

func TestCandidateSignalAllowsPercentTargetFallbackForTrendSetup(t *testing.T) {
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

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "5m"}, 0)
	if err != nil {
		t.Fatalf("buildCandidateSignal returned error: %v", err)
	}
	levels, ok := signal.Evidence["protective_levels"].(ProtectiveLevelTrace)
	if !ok {
		t.Fatalf("expected protective level trace, got %#v", signal.Evidence["protective_levels"])
	}
	if levels.TargetSource != "execution_take_profit_pct" || signal.TakeProfit != 105 {
		t.Fatalf("expected trend setup to use percent target fallback, got signal=%+v levels=%+v", signal, levels)
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

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "15m"}, 3)
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

	signal, err := buildCandidateSignal(rule, "BTCUSDT", 100, "test", time.Unix(2, 0).UTC(), snapshot, TimeframeRoleTrace{Entry: "5m", Primary: "15m"}, 3)
	if !errors.Is(err, errSignalRejected) {
		t.Fatalf("expected technical target to remain unchanged and be rejected for low RR, got signal=%+v err=%v", signal, err)
	}
}

func TestTraceSetupEvaluationsLabelsNoTradeChop(t *testing.T) {
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
	if traces[0].Setup != "no_trade_chop" {
		t.Fatalf("expected no_trade_chop setup, got %+v", traces[0])
	}
}

func TestSetupSignalEngineLabelsBreakout(t *testing.T) {
	scoring := testScoringStrategy()
	scoring.Timeframe = "15m"
	scoring.EntryTimeframe = "5m"
	scoring.ConfirmationTimeframes = []string{"1h"}
	snapshot := testMultiTimeframeSetupSnapshot()
	snapshot.Technical["break_above_donchian"] = []market.IndicatorPoint{
		{Name: "break_above_donchian", Timeframe: "15m", Period: 20, Value: 1},
	}

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

func TestSupportResistanceBounceUsesTimeframePrice(t *testing.T) {
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {
				{Name: "price", Value: 100},
				{Name: "price", Timeframe: "5m", Value: 120},
			},
		},
		Structures: map[string][]market.StructureSnapshot{
			"support_resistance": {
				{
					Name:      "support_resistance",
					Timeframe: "5m",
					Valid:     true,
					KeyLevels: map[string]float64{"support": 119},
				},
			},
		},
	}

	if !hasSupportResistanceBounce("long", "5m", snapshot) {
		t.Fatalf("expected 5m support bounce to use 5m price")
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
	}
}
