package trader

import (
	"nofx/kernel"
	"nofx/store"
	"testing"
)

func TestProfitProtectionTriggersAfterPeakActivationEvenIfCurrentProfitDropsBelowActivation(t *testing.T) {
	if !profitProtectionTriggered(6, 2, 5, 40) {
		t.Fatal("expected profit protection to trigger after armed peak drawdown")
	}
}

func TestProfitProtectionRequiresPeakActivation(t *testing.T) {
	if profitProtectionTriggered(4.9, -1, 5, 40) {
		t.Fatal("expected profit protection to stay inactive before peak reaches activation threshold")
	}
}

func TestProfitProtectionRequiresConfiguredDrawdown(t *testing.T) {
	if profitProtectionTriggered(6, 4.5, 5, 40) {
		t.Fatal("expected profit protection to ignore shallow pullbacks")
	}
}

func TestProfitProtectionDrawdownPct(t *testing.T) {
	got := profitProtectionDrawdownPct(6, 2)
	if got < 66.66 || got > 66.67 {
		t.Fatalf("unexpected drawdown pct: %.4f", got)
	}
	if got := profitProtectionDrawdownPct(6, 7); got != 0 {
		t.Fatalf("expected no drawdown above peak, got %.4f", got)
	}
}

func TestPeakPnLCacheFollowsPositionEntryLifecycle(t *testing.T) {
	at := &AutoTrader{
		peakPnLCache:         make(map[string]float64),
		peakPnLPositionEntry: make(map[string]int64),
	}
	const key = "BTCUSDT_long"

	at.resetPeakPnLForPosition(key, 100)
	at.UpdatePeakPnL("BTCUSDT", "long", 8)
	at.resetPeakPnLForPosition(key, 100)
	if got := at.GetPeakPnLCache()[key]; got != 8 {
		t.Fatalf("expected the same position to retain peak 8, got %.2f", got)
	}

	at.resetPeakPnLForPosition(key, 200)
	if _, exists := at.GetPeakPnLCache()[key]; exists {
		t.Fatal("expected a newly entered position to discard the previous position peak")
	}

	at.UpdatePeakPnL("BTCUSDT", "long", 3)
	at.clearInactivePeakPnLCache(map[string]bool{})
	if len(at.GetPeakPnLCache()) != 0 || len(at.peakPnLPositionEntry) != 0 {
		t.Fatalf("expected inactive position state to be cleared, peaks=%+v entries=%+v", at.GetPeakPnLCache(), at.peakPnLPositionEntry)
	}
}

func TestFreshOpenPositionSizeUsesExecutionStopForRiskAndAnchorForRR(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{RiskPerTradePct: 1, MinRiskRewardRatio: 1.5},
	}}}
	decision := &kernel.Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		PositionSizeUSD: 1000,
		StopLoss:        90,
		StopLossAnchor:  95,
		TakeProfit:      110,
	}

	size, err := at.freshOpenPositionSize(decision, 100, 1000)
	if err != nil {
		t.Fatalf("freshOpenPositionSize returned error: %v", err)
	}
	if size != 100 {
		t.Fatalf("expected actual stop distance to cap notional at 100, got %.2f", size)
	}
}

func TestFreshOpenPositionSizeRejectsStaleStructuralRR(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{RiskPerTradePct: 1, MinRiskRewardRatio: 1.5},
	}}}
	decision := &kernel.Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		PositionSizeUSD: 100,
		StopLoss:        90,
		StopLossAnchor:  95,
		TakeProfit:      110,
	}

	if _, err := at.freshOpenPositionSize(decision, 104, 1000); err == nil {
		t.Fatal("expected stale execution price to fail structural RR revalidation")
	}
}
