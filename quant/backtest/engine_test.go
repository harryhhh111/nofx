package backtest

import (
	"math"
	"testing"
	"time"

	"nofx/market"
)

func TestEngineEntersAtNextBarOpenAndTakesProfit(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 110, 121, 109, 120),
	}
	engine := testEngine(2, 0, 0, signalOnce(0, StrategySignal{
		Side:       SideLong,
		StopLoss:   100,
		TakeProfit: 120,
	}))

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.Trades) != 1 {
		t.Fatalf("expected one trade, got %d", len(result.Trades))
	}
	trade := result.Trades[0]
	assertClose(t, trade.EntryPrice, 110, 1e-9)
	assertClose(t, trade.ExitPrice, 120, 1e-9)
	assertClose(t, trade.GrossPnL, 20, 1e-9)
	assertClose(t, trade.NetPnL, 20, 1e-9)
	if trade.ExitReason != ExitReasonTakeProfit {
		t.Fatalf("exit reason = %s, want %s", trade.ExitReason, ExitReasonTakeProfit)
	}
	if result.Metrics.TotalTrades != 1 || result.Metrics.WinningTrades != 1 {
		t.Fatalf("unexpected metrics: %+v", result.Metrics)
	}
}

func TestEngineUsesConservativeStopWhenStopAndTakeProfitHitSameBar(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 106, 97, 101),
	}
	engine := testEngine(1, 0, 0, signalOnce(0, StrategySignal{
		Side:       SideLong,
		StopLoss:   98,
		TakeProfit: 105,
	}))

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	if trade.ExitReason != ExitReasonStopLoss {
		t.Fatalf("exit reason = %s, want %s", trade.ExitReason, ExitReasonStopLoss)
	}
	assertClose(t, trade.ExitPrice, 98, 1e-9)
	assertClose(t, trade.NetPnL, -2, 1e-9)
}

func TestEngineSupportsShortTakeProfit(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 101, 94, 96),
	}
	engine := testEngine(3, 0, 0, signalOnce(0, StrategySignal{
		Side:       SideShort,
		StopLoss:   105,
		TakeProfit: 95,
	}))

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	if trade.Side != SideShort || trade.ExitReason != ExitReasonTakeProfit {
		t.Fatalf("unexpected trade: %+v", trade)
	}
	assertClose(t, trade.GrossPnL, 15, 1e-9)
	assertClose(t, trade.NetPnL, 15, 1e-9)
}

func TestEngineAppliesFeesAndSlippage(t *testing.T) {
	klines := []market.Kline{
		testBar(0, 100, 101, 99, 100),
		testBar(1, 100, 106, 99, 105),
	}
	engine := testEngine(1, 0.001, 10, signalOnce(0, StrategySignal{
		Side:       SideLong,
		StopLoss:   95,
		TakeProfit: 105,
	}))

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	assertClose(t, trade.EntryPrice, 100.1, 1e-9)
	assertClose(t, trade.ExitPrice, 104.895, 1e-9)
	assertClose(t, trade.GrossPnL, 4.795, 1e-9)
	assertClose(t, trade.Fees, 0.204995, 1e-9)
	assertClose(t, trade.NetPnL, 4.590005, 1e-9)
	assertClose(t, result.Metrics.FinalEquity, 10004.590005, 1e-9)
}

func TestEngineClosesOpenPositionAtEndOfData(t *testing.T) {
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

	result, err := engine.Run(klines)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	trade := result.Trades[0]
	if trade.ExitReason != ExitReasonEndOfData {
		t.Fatalf("exit reason = %s, want %s", trade.ExitReason, ExitReasonEndOfData)
	}
	assertClose(t, trade.ExitPrice, 102, 1e-9)
	assertClose(t, trade.NetPnL, 2, 1e-9)
}

func testEngine(quantity, feeRate, slippageBps float64, strategy Strategy) Engine {
	return Engine{
		Config: Config{
			RunID:         "test-run",
			Symbol:        "BTCUSDT",
			Timeframe:     "5m",
			InitialEquity: 10000,
			FixedQuantity: quantity,
			TakerFeeRate:  feeRate,
			SlippageBps:   slippageBps,
		},
		Strategy: strategy,
	}
}

func signalOnce(barIndex int, signal StrategySignal) Strategy {
	return StrategyFunc{
		StrategyName: "test_strategy",
		Fn: func(ctx BarContext) ([]StrategySignal, error) {
			if ctx.BarIndex != barIndex {
				return nil, nil
			}
			return []StrategySignal{signal}, nil
		},
	}
}

func testBar(offset int, open, high, low, close float64) market.Kline {
	openTime := time.Date(2026, 6, 14, 0, offset*5, 0, 0, time.UTC)
	return market.Kline{
		OpenTime:  openTime.UnixMilli(),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    1,
		CloseTime: openTime.Add(5*time.Minute - time.Millisecond).UnixMilli(),
	}
}

func assertClose(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("got %.12f, want %.12f", got, want)
	}
}
