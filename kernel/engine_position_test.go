package kernel

import "testing"

func TestValidateDecisionUsesStructuralStopAnchorForRiskReward(t *testing.T) {
	decision := Decision{
		Symbol:              "BTCUSDT",
		Action:              "open_long",
		Leverage:            1,
		PositionSizeUSD:     100,
		StopLoss:            75,
		StopLossAnchor:      95,
		TakeProfit:          108,
		ProtectiveATR:       10,
		ProtectiveATRBuffer: 2,
	}
	decisions := []Decision{decision}

	rejected := validateDecisions(
		decisions,
		1000,
		5,
		5,
		1,
		1,
		1.5,
		map[string]float64{"BTCUSDT": 100},
		map[string]float64{"BTCUSDT": 10},
	)

	if rejected != 0 || decisions[0].Action != "open_long" {
		t.Fatalf("expected structural RR to pass despite wider execution stop, rejected=%d decision=%+v", rejected, decisions[0])
	}
}

func TestValidateDecisionFallsBackToExecutionStopWhenAnchorMissing(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        1,
		PositionSizeUSD: 100,
		StopLoss:        75,
		TakeProfit:      108,
	}
	decisions := []Decision{decision}

	rejected := validateDecisions(
		decisions,
		1000,
		5,
		5,
		1,
		1,
		1.5,
		map[string]float64{"BTCUSDT": 100},
		map[string]float64{"BTCUSDT": 10},
	)

	if rejected != 1 || decisions[0].Action != "wait" {
		t.Fatalf("expected missing anchor to fall back to execution stop RR and reject, rejected=%d decision=%+v", rejected, decisions[0])
	}
}
