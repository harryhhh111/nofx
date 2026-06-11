package trader

import (
	"math"
	"nofx/kernel"
	"nofx/store"
	"sync"
	"testing"
	"time"
)

type breakevenSetStopLossCall struct {
	symbol       string
	positionSide string
	quantity     float64
	stopPrice    float64
}

type breakevenMockTrader struct {
	positions      []map[string]interface{}
	openOrders     map[string][]OpenOrder
	setStopLosses  []breakevenSetStopLossCall
	cancelSLCalls  int
	openOrdersErr  error
	cancelSLErr    error
	setStopLossErr error
}

func (m *breakevenMockTrader) GetBalance() (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *breakevenMockTrader) GetPositions() ([]map[string]interface{}, error) {
	return m.positions, nil
}

func (m *breakevenMockTrader) OpenLong(string, float64, int) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *breakevenMockTrader) OpenShort(string, float64, int) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *breakevenMockTrader) CloseLong(string, float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *breakevenMockTrader) CloseShort(string, float64) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *breakevenMockTrader) SetLeverage(string, int) error {
	return nil
}

func (m *breakevenMockTrader) SetMarginMode(string, bool) error {
	return nil
}

func (m *breakevenMockTrader) GetMarketPrice(string) (float64, error) {
	return 0, nil
}

func (m *breakevenMockTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	if m.setStopLossErr != nil {
		return m.setStopLossErr
	}
	m.setStopLosses = append(m.setStopLosses, breakevenSetStopLossCall{
		symbol:       symbol,
		positionSide: positionSide,
		quantity:     quantity,
		stopPrice:    stopPrice,
	})
	return nil
}

func (m *breakevenMockTrader) SetTakeProfit(string, string, float64, float64) error {
	return nil
}

func (m *breakevenMockTrader) CancelStopLossOrders(string) error {
	if m.cancelSLErr != nil {
		return m.cancelSLErr
	}
	m.cancelSLCalls++
	return nil
}

func (m *breakevenMockTrader) CancelTakeProfitOrders(string) error {
	return nil
}

func (m *breakevenMockTrader) CancelAllOrders(string) error {
	return nil
}

func (m *breakevenMockTrader) CancelStopOrders(string) error {
	return nil
}

func (m *breakevenMockTrader) FormatQuantity(string, float64) (string, error) {
	return "", nil
}

func (m *breakevenMockTrader) GetOrderStatus(string, string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *breakevenMockTrader) GetClosedPnL(time.Time, int) ([]ClosedPnLRecord, error) {
	return nil, nil
}

func (m *breakevenMockTrader) GetOpenOrders(symbol string) ([]OpenOrder, error) {
	if m.openOrdersErr != nil {
		return nil, m.openOrdersErr
	}
	return m.openOrders[symbol], nil
}

func newBreakevenTestAutoTrader(mockTrader *breakevenMockTrader, triggerPct float64) *AutoTrader {
	strategyConfig := &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{
			BreakevenProtection: &store.BreakevenProtectionConfig{
				Enabled:    true,
				TriggerPct: triggerPct,
			},
		},
	}

	return &AutoTrader{
		name:                  "test",
		exchange:              "binance",
		trader:                mockTrader,
		strategyEngine:        kernel.NewStrategyEngine(strategyConfig),
		breakevenSteps:        make(map[string]int),
		breakevenStepsMutex:   sync.RWMutex{},
		positionFirstSeenTime: make(map[string]int64),
	}
}

func TestCheckBreakevenPromotionInfersActiveSLAfterRestart(t *testing.T) {
	mockTrader := &breakevenMockTrader{
		positions: []map[string]interface{}{
			{
				"symbol":      "BTCUSDT",
				"side":        "long",
				"entryPrice":  100.0,
				"markPrice":   100.4,
				"leverage":    10.0,
				"positionAmt": 2.0,
			},
		},
		openOrders: map[string][]OpenOrder{
			"BTCUSDT": {
				{
					Symbol:       "BTCUSDT",
					Side:         "SELL",
					PositionSide: "LONG",
					Type:         "STOP_MARKET",
					StopPrice:    100.2,
					Quantity:     2,
					Status:       "NEW",
				},
			},
		},
	}
	autoTrader := newBreakevenTestAutoTrader(mockTrader, 1.0)

	autoTrader.checkBreakevenPromotion(mockTrader.positions)

	if len(mockTrader.setStopLosses) != 1 {
		t.Fatalf("set stop-loss calls = %d, want 1", len(mockTrader.setStopLosses))
	}
	call := mockTrader.setStopLosses[0]
	if call.positionSide != "LONG" {
		t.Fatalf("positionSide = %q, want LONG", call.positionSide)
	}
	if math.Abs(call.stopPrice-100.3) > 1e-9 {
		t.Fatalf("stopPrice = %.10f, want 100.3", call.stopPrice)
	}
	if autoTrader.breakevenSteps["BTCUSDT_long"] != 4 {
		t.Fatalf("memory step = %d, want 4", autoTrader.breakevenSteps["BTCUSDT_long"])
	}
}

func TestCheckBreakevenPromotionDoesNotRetreatActiveSL(t *testing.T) {
	mockTrader := &breakevenMockTrader{
		positions: []map[string]interface{}{
			{
				"symbol":      "BTCUSDT",
				"side":        "long",
				"entryPrice":  100.0,
				"markPrice":   100.2,
				"leverage":    10.0,
				"positionAmt": 2.0,
			},
		},
		openOrders: map[string][]OpenOrder{
			"BTCUSDT": {
				{
					Symbol:       "BTCUSDT",
					Side:         "SELL",
					PositionSide: "LONG",
					Type:         "STOP_MARKET",
					StopPrice:    100.3,
					Quantity:     2,
					Status:       "NEW",
				},
			},
		},
	}
	autoTrader := newBreakevenTestAutoTrader(mockTrader, 1.0)

	autoTrader.checkBreakevenPromotion(mockTrader.positions)

	if len(mockTrader.setStopLosses) != 0 {
		t.Fatalf("set stop-loss calls = %d, want 0", len(mockTrader.setStopLosses))
	}
	if mockTrader.cancelSLCalls != 0 {
		t.Fatalf("cancel SL calls = %d, want 0", mockTrader.cancelSLCalls)
	}
}

func TestCheckBreakevenPromotionEpsilonDoesNotTriggerEarly(t *testing.T) {
	mockTrader := &breakevenMockTrader{
		positions: []map[string]interface{}{
			{
				"symbol":      "BTCUSDT",
				"side":        "long",
				"entryPrice":  100.0,
				"markPrice":   100.095,
				"leverage":    10.0,
				"positionAmt": 2.0,
			},
		},
		openOrders: map[string][]OpenOrder{"BTCUSDT": nil},
	}
	autoTrader := newBreakevenTestAutoTrader(mockTrader, 1.0)

	autoTrader.checkBreakevenPromotion(mockTrader.positions)

	if len(mockTrader.setStopLosses) != 0 {
		t.Fatalf("set stop-loss calls = %d, want 0", len(mockTrader.setStopLosses))
	}
	if mockTrader.cancelSLCalls != 0 {
		t.Fatalf("cancel SL calls = %d, want 0", mockTrader.cancelSLCalls)
	}
}

func TestFindActiveStopLossPriceTrustsClosingSideWhenPositionSideMismatches(t *testing.T) {
	price, ok := findActiveStopLossPrice([]OpenOrder{
		{
			Symbol:       "BTCUSDT",
			Side:         "SELL",
			PositionSide: "SHORT",
			Type:         "STOP_MARKET",
			StopPrice:    100.2,
			Quantity:     2,
			Status:       "NEW",
		},
	}, "long")

	if !ok {
		t.Fatal("expected active stop-loss to be found")
	}
	if price != 100.2 {
		t.Fatalf("active SL = %.4f, want 100.2", price)
	}
}
