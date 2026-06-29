package trader

import "testing"

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
