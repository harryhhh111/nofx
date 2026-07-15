package kernel

import "testing"

func TestApplyRiskBasedPositionSizingUsesStopDistance(t *testing.T) {
	signals := []CandidateSignal{
		{
			Symbol:     "BTCUSDT",
			Action:     "open_long",
			EntryPrice: 100,
			StopLoss:   98,
			Leverage:   5,
		},
	}

	applyRiskBasedPositionSizing(signals, AccountInfo{TotalEquity: 10000, AvailableBalance: 10000}, &PositionSizingConfig{
		RiskPerTradePct:              1,
		MinPositionSizeUSD:           12,
		MaxMarginUsage:               0.9,
		BTCETHMaxPositionValueRatio:  5,
		AltcoinMaxPositionValueRatio: 1,
	})

	if got, want := signals[0].PositionSizeUSD, 5000.0; got != want {
		t.Fatalf("expected risk-based notional %.2f, got %.2f", want, got)
	}
}

func TestApplyRiskBasedPositionSizingCapsByMargin(t *testing.T) {
	signals := []CandidateSignal{
		{
			Symbol:     "BTCUSDT",
			Action:     "open_long",
			EntryPrice: 100,
			StopLoss:   99,
			Leverage:   5,
		},
	}

	applyRiskBasedPositionSizing(signals, AccountInfo{TotalEquity: 10000, AvailableBalance: 500}, &PositionSizingConfig{
		RiskPerTradePct:              1,
		MinPositionSizeUSD:           12,
		MaxMarginUsage:               0.9,
		BTCETHMaxPositionValueRatio:  5,
		AltcoinMaxPositionValueRatio: 1,
	})

	if got, want := signals[0].PositionSizeUSD, 2250.0; got != want {
		t.Fatalf("expected margin-capped notional %.2f, got %.2f", want, got)
	}
}

func TestApplyRiskBasedPositionSizingKeepsNonOpenSignals(t *testing.T) {
	signals := []CandidateSignal{
		{
			Symbol:          "BTCUSDT",
			Action:          "wait",
			EntryPrice:      100,
			StopLoss:        98,
			PositionSizeUSD: 100,
		},
	}

	applyRiskBasedPositionSizing(signals, AccountInfo{TotalEquity: 10000, AvailableBalance: 10000}, &PositionSizingConfig{
		RiskPerTradePct: 1,
	})

	if got, want := signals[0].PositionSizeUSD, 100.0; got != want {
		t.Fatalf("expected wait signal size to remain %.2f, got %.2f", want, got)
	}
}

func TestApplyRiskBasedPositionSizingRejectsMinimumThatExceedsRiskBudget(t *testing.T) {
	signals := []CandidateSignal{{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		EntryPrice:      100,
		StopLoss:        50,
		Leverage:        5,
		PositionSizeUSD: 1000,
	}}

	applyRiskBasedPositionSizing(signals, AccountInfo{TotalEquity: 100, AvailableBalance: 100}, &PositionSizingConfig{
		RiskPerTradePct:             1,
		MinPositionSizeUSD:          12,
		MaxMarginUsage:              0.9,
		BTCETHMaxPositionValueRatio: 5,
	})

	if signals[0].PositionSizeUSD != 0 {
		t.Fatalf("expected signal to become non-executable instead of exceeding risk budget, got %.2f", signals[0].PositionSizeUSD)
	}
}
