package kernel

import (
	"encoding/json"
	"testing"
)

func TestDecisionUnmarshalJSONAcceptsNumericStrings(t *testing.T) {
	input := []byte(`{
		"symbol": "ETHUSDT",
		"action": "open_long",
		"leverage": 7,
		"position_size_usd": "750",
		"stop_loss": "2290.00",
		"take_profit": "2336.00",
		"confidence": 75,
		"risk_usd": "6.51",
		"reasoning": "test"
	}`)

	var decision Decision
	if err := json.Unmarshal(input, &decision); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decision.PositionSizeUSD != 750 {
		t.Fatalf("PositionSizeUSD = %v, want 750", decision.PositionSizeUSD)
	}
	if decision.StopLoss != 2290 {
		t.Fatalf("StopLoss = %v, want 2290", decision.StopLoss)
	}
	if decision.TakeProfit != 2336 {
		t.Fatalf("TakeProfit = %v, want 2336", decision.TakeProfit)
	}
	if decision.RiskUSD != 6.51 {
		t.Fatalf("RiskUSD = %v, want 6.51", decision.RiskUSD)
	}
}
