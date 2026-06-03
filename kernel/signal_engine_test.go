package kernel

import (
	"context"
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

func testScoringSnapshot(includeTrend, includeMomentum bool) *market.FactorSnapshot {
	technical := map[string][]market.IndicatorPoint{
		"price": {{Name: "price", Value: 100}},
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
	return &market.FactorSnapshot{
		Symbol:    "BTCUSDT",
		AsOf:      time.Unix(1, 0).UTC(),
		Technical: technical,
	}
}
