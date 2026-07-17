package kernel

import (
	"context"
	"testing"
	"time"

	"nofx/market"
)

func TestMarketContextSeparatesDirectionFromVolatility(t *testing.T) {
	snapshots := map[string]*market.FactorSnapshot{
		"BTCUSDT": bearishContextSnapshotWithVol("BTCUSDT", 0.80, 0.35, 1.0),
		"ETHUSDT": bearishContextSnapshotWithVol("ETHUSDT", 0.80, 0.35, 1.0),
	}
	ctx, err := NewDefaultMarketContextEngine().Build(context.Background(), MarketContextRequest{
		FactorSnapshot: snapshots,
		Now:            time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ctx.MarketRegime != "high_volatility" {
		t.Fatalf("expected high_volatility regime, got %+v", ctx)
	}
	if ctx.DirectionBias != "bearish" {
		t.Fatalf("expected bearish direction bias, got %+v", ctx)
	}
	if ctx.VolatilityRegime != "high_volatility" {
		t.Fatalf("expected high volatility regime field, got %+v", ctx)
	}
}

func TestMarketContextDoesNotTreatNormalPerBarVolAsHigh(t *testing.T) {
	snapshots := map[string]*market.FactorSnapshot{
		"BTCUSDT": bearishContextSnapshotWithVol("BTCUSDT", 0.05, 0.04, 0.12),
		"ETHUSDT": bearishContextSnapshotWithVol("ETHUSDT", 0.05, 0.04, 0.12),
	}
	ctx, err := NewDefaultMarketContextEngine().Build(context.Background(), MarketContextRequest{
		FactorSnapshot: snapshots,
		Now:            time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ctx.VolatilityRegime != "normal" {
		t.Fatalf("expected normal volatility for 0.05%% per-bar RV, got %+v", ctx)
	}
	if ctx.MarketRegime == "high_volatility" {
		t.Fatalf("normal per-bar RV should not dominate market regime, got %+v", ctx)
	}
}

func TestMarketContextDetectsRelativeVolatilityExpansion(t *testing.T) {
	snapshots := map[string]*market.FactorSnapshot{
		"BTCUSDT": bearishContextSnapshotWithVol("BTCUSDT", 0.30, 0.12, 0.35),
		"ETHUSDT": bearishContextSnapshotWithVol("ETHUSDT", 0.30, 0.12, 0.35),
	}
	ctx, err := NewDefaultMarketContextEngine().Build(context.Background(), MarketContextRequest{
		FactorSnapshot: snapshots,
		Now:            time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if ctx.VolatilityRegime != "high_volatility" {
		t.Fatalf("expected expanded realized volatility to be high, got %+v", ctx)
	}
}

func TestRiskGateRejectsLongAgainstBearishDirectionBias(t *testing.T) {
	gate := NewDefaultRiskGate(5, 5, 0.1, 0.05, 2, map[string]float64{"BTCUSDT": 100}, nil)
	result, err := gate.Validate(context.Background(), RiskGateRequest{
		Account: AccountInfo{TotalEquity: 10000},
		Signals: []CandidateSignal{{
			ID:              "sig-1",
			Symbol:          "BTCUSDT",
			Action:          "open_long",
			EntryPrice:      100,
			PositionSizeUSD: 100,
			Leverage:        2,
			StopLoss:        98,
			TakeProfit:      106,
			Confidence:      80,
			GeneratedAt:     time.Unix(1, 0).UTC(),
		}},
		Reviews:       []SignalReviewDecision{{SignalID: "sig-1", Status: "pass"}},
		MarketContext: &MarketContext{DirectionBias: "bearish", MarketRegime: "high_volatility"},
	})
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(result.Approved) != 0 || len(result.Rejected) != 1 {
		t.Fatalf("expected bearish direction bias to reject long, got %+v", result)
	}
	if result.Rejected[0].Reason != "market_context_rejected:direction_bias_bearish" {
		t.Fatalf("unexpected rejection reason: %+v", result.Rejected[0])
	}
}

func TestAssetTrendUsesDominantTimeframePrice(t *testing.T) {
	snapshot := &market.FactorSnapshot{
		Symbol: "BTCUSDT",
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {
				{Name: "price", Value: 100},
				{Name: "price", Timeframe: "15m", Value: 130},
			},
			"ema": {
				{Name: "ema", Timeframe: "15m", Period: 20, Value: 110},
				{Name: "ema", Timeframe: "15m", Period: 50, Value: 120},
			},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "15m", Value: 1},
			},
		},
	}

	if trend := assetTrend(snapshot); trend != "bullish" {
		t.Fatalf("expected dominant timeframe price to produce bullish trend, got %s", trend)
	}
}

func bearishContextSnapshot(symbol string) *market.FactorSnapshot {
	return bearishContextSnapshotWithVol(symbol, 0.80, 0.35, 1.0)
}

func bearishContextSnapshotWithVol(symbol string, rv20, rv60, atr14 float64) *market.FactorSnapshot {
	return &market.FactorSnapshot{
		Symbol: symbol,
		AsOf:   time.Unix(1, 0).UTC(),
		Technical: map[string][]market.IndicatorPoint{
			"price": {
				{Name: "price", Value: 100},
			},
			"ema": {
				{Name: "ema", Timeframe: "15m", Period: 20, Value: 110},
				{Name: "ema", Timeframe: "15m", Period: 50, Value: 120},
			},
			"macd_histogram": {
				{Name: "macd_histogram", Timeframe: "15m", Value: -1},
			},
			"realized_vol": {
				{Name: "realized_vol", Timeframe: "15m", Period: 20, Value: rv20},
				{Name: "realized_vol", Timeframe: "15m", Period: 60, Value: rv60},
			},
			"atr": {
				{Name: "atr", Timeframe: "15m", Period: 14, Value: atr14},
			},
		},
	}
}
