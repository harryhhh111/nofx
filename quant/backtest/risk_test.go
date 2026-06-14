package backtest

import (
	"strings"
	"testing"

	"nofx/market"
)

func TestEngineRiskPerTradeSizing(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 106, 99, 105),
	}
	engine := Engine{
		Config: Config{
			RunID:           "risk-sizing",
			Symbol:          "BTCUSDT",
			Timeframe:       "5m",
			InitialEquity:   10000,
			RiskPerTradePct: 0.0025,
		},
		Strategy: signalOnce(0, StrategySignal{
			Side:       SideLong,
			StopLoss:   95,
			TakeProfit: 105,
		}),
	}

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	assertClose(t, trade.Quantity, 5, 1e-9)
	assertClose(t, trade.RiskDecision.RiskUSDT, 25, 1e-9)
	if !hasReason(trade.RiskDecision.Reasons, "risk_per_trade") {
		t.Fatalf("expected risk sizing reason, got %+v", trade.RiskDecision.Reasons)
	}
}

func TestEngineDailyTradeLimitBlocksNextSignal(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 106, 99, 105),
		testBar(2, 105, 106, 104, 105),
		testBar(3, 105, 111, 104, 110),
	}
	engine := testEngine(1, 0, 0, signalsAt(map[int]StrategySignal{
		0: {Side: SideLong, StopLoss: 95, TakeProfit: 105},
		2: {Side: SideLong, StopLoss: 100, TakeProfit: 110},
	}))
	engine.Config.MaxDailyTrades = 1

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.Trades) != 1 {
		t.Fatalf("expected one allowed trade, got %d", len(result.Trades))
	}
	if len(result.RejectedSignals) != 1 {
		t.Fatalf("expected one rejected signal, got %d", len(result.RejectedSignals))
	}
	if !hasExactReason(result.RejectedSignals[0].RiskDecision.Reasons, "daily_trade_limit") {
		t.Fatalf("expected daily trade limit rejection, got %+v", result.RejectedSignals[0].RiskDecision.Reasons)
	}
}

func TestEngineDailyLossLimitBlocksNextSignal(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 101, 94, 95),
		testBar(2, 95, 96, 94, 95),
		testBar(3, 95, 101, 94, 100),
	}
	engine := testEngine(10, 0, 0, signalsAt(map[int]StrategySignal{
		0: {Side: SideLong, StopLoss: 95, TakeProfit: 110},
		2: {Side: SideLong, StopLoss: 90, TakeProfit: 100},
	}))
	engine.Config.MaxDailyLossPct = 0.004

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.Trades) != 1 {
		t.Fatalf("expected one allowed trade, got %d", len(result.Trades))
	}
	if len(result.RejectedSignals) != 1 {
		t.Fatalf("expected one rejected signal, got %d", len(result.RejectedSignals))
	}
	if !hasExactReason(result.RejectedSignals[0].RiskDecision.Reasons, "daily_loss_limit") {
		t.Fatalf("expected daily loss rejection, got %+v", result.RejectedSignals[0].RiskDecision.Reasons)
	}
}

func TestEngineCooldownBlocksAfterConsecutiveLoss(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 101, 94, 95),
		testBar(2, 95, 96, 94, 95),
		testBar(3, 95, 101, 94, 100),
	}
	engine := testEngine(1, 0, 0, signalsAt(map[int]StrategySignal{
		0: {Side: SideLong, StopLoss: 95, TakeProfit: 110},
		2: {Side: SideLong, StopLoss: 90, TakeProfit: 100},
	}))
	engine.Config.CooldownLosses = 1
	engine.Config.CooldownBars = 2

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.Trades) != 1 {
		t.Fatalf("expected one allowed trade, got %d", len(result.Trades))
	}
	if len(result.RejectedSignals) != 1 {
		t.Fatalf("expected one rejected signal, got %d", len(result.RejectedSignals))
	}
	reasons := result.RejectedSignals[0].RiskDecision.Reasons
	if !hasExactReason(reasons, "side_cooldown") || !hasExactReason(reasons, "symbol_side_cooldown") {
		t.Fatalf("expected side and symbol-side cooldown reasons, got %+v", reasons)
	}
}

func TestEngineTimeStop(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 102, 99, 101),
		testBar(2, 101, 103, 100, 102),
	}
	engine := testEngine(1, 0, 0, signalOnce(0, StrategySignal{
		Side:       SideLong,
		StopLoss:   90,
		TakeProfit: 120,
	}))
	engine.Config.TimeStopBars = 2

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	if trade.ExitReason != ExitReasonTimeStop {
		t.Fatalf("exit reason = %s, want %s", trade.ExitReason, ExitReasonTimeStop)
	}
	assertClose(t, trade.ExitPrice, 102, 1e-9)
}

func TestEngineBreakevenStop(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 103, 101, 102),
		testBar(2, 102, 103, 100, 101),
	}
	engine := testEngine(1, 0, 0, signalOnce(0, StrategySignal{
		Side:       SideLong,
		StopLoss:   95,
		TakeProfit: 120,
	}))
	engine.Config.BreakevenTriggerPct = 2

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	if trade.ExitReason != ExitReasonBreakeven {
		t.Fatalf("exit reason = %s, want %s", trade.ExitReason, ExitReasonBreakeven)
	}
	assertClose(t, trade.ExitPrice, 100, 1e-9)
}

func TestEngineTrailingStop(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 110, 106, 109),
		testBar(2, 109, 110, 105, 106),
	}
	engine := testEngine(1, 0, 0, signalOnce(0, StrategySignal{
		Side:       SideLong,
		StopLoss:   95,
		TakeProfit: 120,
	}))
	engine.Config.TrailingStartPct = 5
	engine.Config.TrailingDistancePct = 4

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	if trade.ExitReason != ExitReasonTrailing {
		t.Fatalf("exit reason = %s, want %s", trade.ExitReason, ExitReasonTrailing)
	}
	assertClose(t, trade.ExitPrice, 105.6, 1e-9)
}

func signalsAt(signals map[int]StrategySignal) Strategy {
	return StrategyFunc{
		StrategyName: "multi_signal_strategy",
		Fn: func(ctx BarContext) ([]StrategySignal, error) {
			signal, ok := signals[ctx.BarIndex]
			if !ok {
				return nil, nil
			}
			return []StrategySignal{signal}, nil
		},
	}
}

func hasReason(reasons []string, prefix string) bool {
	for _, reason := range reasons {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}

func hasExactReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
