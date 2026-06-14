package backtest

import (
	"fmt"
	"time"

	"nofx/market"
)

type Engine struct {
	Config   Config
	Strategy Strategy
}

type pendingOrder struct {
	signal   StrategySignal
	snapshot FeatureSnapshot
}

type openPosition struct {
	tradeID      string
	signal       StrategySignal
	snapshot     FeatureSnapshot
	riskDecision RiskDecision
	side         Side
	quantity     float64
	entryTime    time.Time
	entryPrice   float64
	entryFee     float64
	mfePct       float64
	maePct       float64
}

func (e Engine) Run(klines []market.Kline) (Result, error) {
	if err := e.validate(klines); err != nil {
		return Result{}, err
	}

	normSymbol := market.Normalize(e.Config.Symbol)
	normTF, _ := market.NormalizeTimeframe(e.Config.Timeframe)
	run := BacktestRun{
		RunID:         e.Config.RunID,
		Strategy:      e.Strategy.Name(),
		Symbol:        normSymbol,
		Timeframe:     normTF,
		StartTime:     time.UnixMilli(klines[0].OpenTime).UTC(),
		EndTime:       time.UnixMilli(klines[len(klines)-1].CloseTime).UTC(),
		InitialEquity: e.Config.InitialEquity,
		FeeModel:      "fixed_taker_fee",
		SlippageModel: "fixed_bps",
		ConfigVersion: e.Config.ConfigVersion,
	}
	if run.RunID == "" {
		run.RunID = fmt.Sprintf("%s-%s-%s", run.Strategy, run.Symbol, run.Timeframe)
	}
	if run.ConfigVersion == "" {
		run.ConfigVersion = "phase1"
	}

	result := Result{
		Run: run,
		EquityCurve: []EquityPoint{{
			Time:   run.StartTime,
			Equity: e.Config.InitialEquity,
		}},
	}

	equity := e.Config.InitialEquity
	var pending *pendingOrder
	var position *openPosition

	for i, bar := range klines {
		barTime := time.UnixMilli(bar.OpenTime).UTC()

		if pending != nil && position == nil {
			opened, err := e.openPosition(*pending, bar, len(result.Trades)+1)
			if err != nil {
				return Result{}, err
			}
			position = opened
			pending = nil
		}

		if position != nil {
			updateExcursions(position, bar)
			if exitPrice, reason, ok := e.exitTriggered(position, bar); ok {
				trade := e.closePosition(position, barTime, exitPrice, reason)
				result.Trades = append(result.Trades, trade)
				equity += trade.NetPnL
				result.EquityCurve = append(result.EquityCurve, EquityPoint{Time: trade.ExitTime, Equity: equity})
				position = nil
			}
		}

		if position != nil || pending != nil || i == len(klines)-1 {
			continue
		}

		ctx := BarContext{
			Run:      run,
			Bar:      bar,
			BarIndex: i,
			Time:     time.UnixMilli(bar.CloseTime).UTC(),
			Equity:   equity,
		}
		signals, err := e.Strategy.OnBar(ctx)
		if err != nil {
			return Result{}, err
		}
		signal, ok, err := firstExecutableSignal(signals, run, ctx.Time)
		if err != nil {
			return Result{}, err
		}
		if ok {
			pending = &pendingOrder{
				signal:   signal,
				snapshot: snapshotFromBar(run.Symbol, run.Timeframe, bar),
			}
		}
	}

	if position != nil {
		last := klines[len(klines)-1]
		exitTime := time.UnixMilli(last.CloseTime).UTC()
		trade := e.closePosition(position, exitTime, last.Close, ExitReasonEndOfData)
		result.Trades = append(result.Trades, trade)
		equity += trade.NetPnL
		result.EquityCurve = append(result.EquityCurve, EquityPoint{Time: trade.ExitTime, Equity: equity})
	}

	result.Metrics = CalculateMetrics(e.Config.InitialEquity, result.Trades, result.EquityCurve)
	return result, nil
}

func (e Engine) validate(klines []market.Kline) error {
	if e.Strategy == nil {
		return fmt.Errorf("strategy is required")
	}
	if len(klines) < 2 {
		return fmt.Errorf("at least two klines are required")
	}
	if e.Config.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if _, err := market.NormalizeTimeframe(e.Config.Timeframe); err != nil {
		return err
	}
	if e.Config.InitialEquity <= 0 {
		return fmt.Errorf("initial equity must be positive")
	}
	if e.Config.FixedQuantity <= 0 && e.Config.FixedNotionalUSDT <= 0 {
		return fmt.Errorf("fixed quantity or fixed notional must be positive")
	}
	if e.Config.TakerFeeRate < 0 {
		return fmt.Errorf("taker fee rate cannot be negative")
	}
	if e.Config.SlippageBps < 0 {
		return fmt.Errorf("slippage bps cannot be negative")
	}
	return nil
}

func (e Engine) openPosition(order pendingOrder, bar market.Kline, sequence int) (*openPosition, error) {
	entryPrice := applySlippage(order.signal.Side, true, bar.Open, e.Config.SlippageBps)
	if err := validateStops(order.signal, entryPrice); err != nil {
		return nil, err
	}

	quantity := e.Config.FixedQuantity
	if quantity <= 0 {
		quantity = e.Config.FixedNotionalUSDT / entryPrice
	}
	notional := entryPrice * quantity
	entryFee := notional * e.Config.TakerFeeRate

	return &openPosition{
		tradeID:      fmt.Sprintf("bt_%06d", sequence),
		signal:       order.signal,
		snapshot:     order.snapshot,
		riskDecision: RiskDecision{Action: "allow", PositionSize: quantity, Reasons: []string{"phase1_fixed_size"}},
		side:         order.signal.Side,
		quantity:     quantity,
		entryTime:    time.UnixMilli(bar.OpenTime).UTC(),
		entryPrice:   entryPrice,
		entryFee:     entryFee,
		mfePct:       0,
		maePct:       0,
	}, nil
}

func (e Engine) exitTriggered(position *openPosition, bar market.Kline) (float64, ExitReason, bool) {
	switch position.side {
	case SideLong:
		stopHit := bar.Low <= position.signal.StopLoss
		takeHit := bar.High >= position.signal.TakeProfit
		if stopHit {
			return position.signal.StopLoss, ExitReasonStopLoss, true
		}
		if takeHit {
			return position.signal.TakeProfit, ExitReasonTakeProfit, true
		}
	case SideShort:
		stopHit := bar.High >= position.signal.StopLoss
		takeHit := bar.Low <= position.signal.TakeProfit
		if stopHit {
			return position.signal.StopLoss, ExitReasonStopLoss, true
		}
		if takeHit {
			return position.signal.TakeProfit, ExitReasonTakeProfit, true
		}
	}
	return 0, "", false
}

func (e Engine) closePosition(position *openPosition, exitTime time.Time, rawExitPrice float64, reason ExitReason) BacktestTrade {
	exitPrice := applySlippage(position.side, false, rawExitPrice, e.Config.SlippageBps)
	gross := grossPnL(position.side, position.entryPrice, exitPrice, position.quantity)
	exitFee := exitPrice * position.quantity * e.Config.TakerFeeRate
	fees := position.entryFee + exitFee

	return BacktestTrade{
		TradeID:                  position.tradeID,
		Symbol:                   position.signal.Symbol,
		Strategy:                 position.signal.Strategy,
		Side:                     position.side,
		Quantity:                 position.quantity,
		EntryTime:                position.entryTime,
		EntryPrice:               position.entryPrice,
		ExitTime:                 exitTime,
		ExitPrice:                exitPrice,
		ExitReason:               reason,
		GrossPnL:                 gross,
		Fees:                     fees,
		Funding:                  0,
		NetPnL:                   gross - fees,
		MaxFavorableExcursionPct: position.mfePct,
		MaxAdverseExcursionPct:   position.maePct,
		Signal:                   position.signal,
		FeatureSnapshot:          position.snapshot,
		RiskDecision:             position.riskDecision,
	}
}

func firstExecutableSignal(signals []StrategySignal, run BacktestRun, signalTime time.Time) (StrategySignal, bool, error) {
	for _, signal := range signals {
		if signal.Symbol == "" {
			signal.Symbol = run.Symbol
		}
		signal.Symbol = market.Normalize(signal.Symbol)
		if signal.Symbol != run.Symbol {
			continue
		}
		if signal.Strategy == "" {
			signal.Strategy = run.Strategy
		}
		if signal.SignalTime.IsZero() {
			signal.SignalTime = signalTime
		}
		if signal.EntryType == "" {
			signal.EntryType = EntryTypeMarket
		}
		if signal.EntryType != EntryTypeMarket {
			continue
		}
		if signal.Side != SideLong && signal.Side != SideShort {
			return StrategySignal{}, false, fmt.Errorf("unsupported signal side %q", signal.Side)
		}
		if signal.StopLoss <= 0 || signal.TakeProfit <= 0 {
			return StrategySignal{}, false, fmt.Errorf("signal stop_loss and take_profit must be positive")
		}
		return signal, true, nil
	}
	return StrategySignal{}, false, nil
}

func validateStops(signal StrategySignal, entryPrice float64) error {
	switch signal.Side {
	case SideLong:
		if signal.StopLoss >= entryPrice {
			return fmt.Errorf("long stop_loss %.8f must be below entry %.8f", signal.StopLoss, entryPrice)
		}
		if signal.TakeProfit <= entryPrice {
			return fmt.Errorf("long take_profit %.8f must be above entry %.8f", signal.TakeProfit, entryPrice)
		}
	case SideShort:
		if signal.StopLoss <= entryPrice {
			return fmt.Errorf("short stop_loss %.8f must be above entry %.8f", signal.StopLoss, entryPrice)
		}
		if signal.TakeProfit >= entryPrice {
			return fmt.Errorf("short take_profit %.8f must be below entry %.8f", signal.TakeProfit, entryPrice)
		}
	default:
		return fmt.Errorf("unsupported side %q", signal.Side)
	}
	return nil
}

func applySlippage(side Side, isEntry bool, rawPrice, slippageBps float64) float64 {
	rate := slippageBps / 10000
	switch side {
	case SideLong:
		if isEntry {
			return rawPrice * (1 + rate)
		}
		return rawPrice * (1 - rate)
	case SideShort:
		if isEntry {
			return rawPrice * (1 - rate)
		}
		return rawPrice * (1 + rate)
	default:
		return rawPrice
	}
}

func grossPnL(side Side, entryPrice, exitPrice, quantity float64) float64 {
	switch side {
	case SideLong:
		return (exitPrice - entryPrice) * quantity
	case SideShort:
		return (entryPrice - exitPrice) * quantity
	default:
		return 0
	}
}

func updateExcursions(position *openPosition, bar market.Kline) {
	if position.entryPrice <= 0 {
		return
	}

	var favorablePct, adversePct float64
	switch position.side {
	case SideLong:
		favorablePct = (bar.High - position.entryPrice) / position.entryPrice * 100
		adversePct = (bar.Low - position.entryPrice) / position.entryPrice * 100
	case SideShort:
		favorablePct = (position.entryPrice - bar.Low) / position.entryPrice * 100
		adversePct = (position.entryPrice - bar.High) / position.entryPrice * 100
	}

	if favorablePct > position.mfePct {
		position.mfePct = favorablePct
	}
	if adversePct < position.maePct {
		position.maePct = adversePct
	}
}

func snapshotFromBar(symbol, timeframe string, bar market.Kline) FeatureSnapshot {
	return FeatureSnapshot{
		Symbol:    symbol,
		Time:      time.UnixMilli(bar.CloseTime).UTC(),
		Timeframe: timeframe,
		Open:      bar.Open,
		High:      bar.High,
		Low:       bar.Low,
		Close:     bar.Close,
		Volume:    bar.Volume,
	}
}
