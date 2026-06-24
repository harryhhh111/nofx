package kernel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClosedTradeOutcomeMarshalsProtectiveMetadata(t *testing.T) {
	outcome := ClosedTradeOutcome{
		Symbol:                 "BTCUSDT",
		Side:                   "LONG",
		PositionSizeUSD:        120,
		OpeningDecisionID:      42,
		StopLossSource:         "support_resistance.support",
		StopLossTimeframe:      "15m",
		TakeProfitSource:       "support_resistance.resistance",
		TakeProfitTimeframe:    "1h",
		ProtectiveATR:          100,
		ProtectiveATRTimeframe: "15m",
		ProtectiveATRBuffer:    2.5,
		ProtectiveRiskReward:   3,
	}

	data, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("marshal closed trade outcome: %v", err)
	}
	payload := string(data)
	for _, field := range []string{
		"position_size_usd",
		"opening_decision_id",
		"stop_loss_source",
		"stop_loss_timeframe",
		"take_profit_source",
		"take_profit_timeframe",
		"protective_atr",
		"protective_atr_timeframe",
		"protective_atr_buffer",
		"protective_risk_reward",
	} {
		if !strings.Contains(payload, field) {
			t.Fatalf("expected %s in closed trade memory payload: %s", field, payload)
		}
	}
}
