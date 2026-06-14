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
	tradeID       string
	signal        StrategySignal
	snapshot      FeatureSnapshot
	riskDecision  RiskDecision
	side          Side
	quantity      float64
	entryTime     time.Time
	entryBarIndex int
	entryPrice    float64
	entryFee      float64
	stopPrice     float64
	stopReason    ExitReason
	mfePct        float64
	maePct        float64
	peakPrice     float64
	troughPrice   float64
}

type runState struct {
	currentDay          string
	dailyStartEquity    float64
	dailyPnL            float64
	dailyTrades         int
	lossStreakBySide    map[Side]int
	lossStreakBySymSide map[string]int
	cooldownBySide      map[Side]int
	cooldownBySymSide   map[string]int
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
	state := newRunState(run.StartTime, equity)

	for i, bar := range klines {
		barTime := time.UnixMilli(bar.OpenTime).UTC()
		state.ensureDay(barTime, equity)

		if pending != nil && position == nil {
			opened, rejected, err := e.openPosition(*pending, bar, i, len(result.Trades)+1, equity, state)
			if err != nil {
				return Result{}, err
			}
			if rejected != nil {
				result.RejectedSignals = append(result.RejectedSignals, *rejected)
			}
			if opened != nil {
				position = opened
				state.recordEntry()
			}
			pending = nil
		}

		if position != nil {
			updateExcursions(position, bar)
			if exitPrice, reason, ok := e.exitTriggered(position, bar, i); ok {
				trade := e.closePosition(position, barTime, exitPrice, reason)
				result.Trades = append(result.Trades, trade)
				equity += trade.NetPnL
				state.recordExit(trade)
				state.updateCooldowns(trade, i, e.Config)
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
		state.ensureDay(trade.ExitTime, equity-trade.NetPnL)
		state.recordExit(trade)
		state.updateCooldowns(trade, len(klines)-1, e.Config)
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
	if e.Config.FixedQuantity <= 0 && e.Config.FixedNotionalUSDT <= 0 && e.Config.RiskPerTradePct <= 0 {
		return fmt.Errorf("fixed quantity, fixed notional, or risk per trade must be positive")
	}
	if e.Config.RiskPerTradePct < 0 {
		return fmt.Errorf("risk per trade cannot be negative")
	}
	if e.Config.MaxDailyLossPct < 0 {
		return fmt.Errorf("max daily loss cannot be negative")
	}
	if e.Config.MaxDailyTrades < 0 {
		return fmt.Errorf("max daily trades cannot be negative")
	}
	if e.Config.TimeStopBars < 0 {
		return fmt.Errorf("time stop bars cannot be negative")
	}
	if e.Config.BreakevenTriggerPct < 0 {
		return fmt.Errorf("breakeven trigger cannot be negative")
	}
	if e.Config.TrailingStartPct < 0 || e.Config.TrailingDistancePct < 0 {
		return fmt.Errorf("trailing stop values cannot be negative")
	}
	if e.Config.TrailingStartPct > 0 && e.Config.TrailingDistancePct <= 0 {
		return fmt.Errorf("trailing distance must be positive when trailing start is enabled")
	}
	if e.Config.CooldownLosses < 0 || e.Config.CooldownBars < 0 {
		return fmt.Errorf("cooldown values cannot be negative")
	}
	if e.Config.TakerFeeRate < 0 {
		return fmt.Errorf("taker fee rate cannot be negative")
	}
	if e.Config.SlippageBps < 0 {
		return fmt.Errorf("slippage bps cannot be negative")
	}
	return nil
}

func (e Engine) openPosition(order pendingOrder, bar market.Kline, barIndex, sequence int, equity float64, state *runState) (*openPosition, *RejectedSignal, error) {
	entryPrice := applySlippage(order.signal.Side, true, bar.Open, e.Config.SlippageBps)
	if err := validateStops(order.signal, entryPrice); err != nil {
		return nil, nil, err
	}

	quantity, riskUSDT, reasons := e.positionSize(order.signal, entryPrice, equity)
	decision := state.riskDecision(order.signal, quantity, riskUSDT, reasons, e.Config, barIndex)
	if decision.Action == "block" {
		return nil, &RejectedSignal{
			Signal:          order.signal,
			FeatureSnapshot: order.snapshot,
			RiskDecision:    decision,
			Time:            time.UnixMilli(bar.OpenTime).UTC(),
			RawEntryPrice:   bar.Open,
		}, nil
	}
	notional := entryPrice * quantity
	entryFee := notional * e.Config.TakerFeeRate

	return &openPosition{
		tradeID:       fmt.Sprintf("bt_%06d", sequence),
		signal:        order.signal,
		snapshot:      order.snapshot,
		riskDecision:  decision,
		side:          order.signal.Side,
		quantity:      quantity,
		entryTime:     time.UnixMilli(bar.OpenTime).UTC(),
		entryBarIndex: barIndex,
		entryPrice:    entryPrice,
		entryFee:      entryFee,
		stopPrice:     order.signal.StopLoss,
		stopReason:    ExitReasonStopLoss,
		mfePct:        0,
		maePct:        0,
		peakPrice:     entryPrice,
		troughPrice:   entryPrice,
	}, nil, nil
}

func (e Engine) positionSize(signal StrategySignal, entryPrice, equity float64) (float64, float64, []string) {
	if e.Config.RiskPerTradePct > 0 {
		riskUSDT := equity * e.Config.RiskPerTradePct
		perUnitRisk := abs(entryPrice - signal.StopLoss)
		if perUnitRisk <= 0 {
			return 0, riskUSDT, []string{"invalid_stop_distance"}
		}
		return riskUSDT / perUnitRisk, riskUSDT, []string{fmt.Sprintf("risk_per_trade_%.4f_pct", e.Config.RiskPerTradePct*100)}
	}
	if e.Config.FixedQuantity > 0 {
		return e.Config.FixedQuantity, 0, []string{"phase1_fixed_quantity"}
	}
	return e.Config.FixedNotionalUSDT / entryPrice, 0, []string{"phase1_fixed_notional"}
}

func (e Engine) exitTriggered(position *openPosition, bar market.Kline, barIndex int) (float64, ExitReason, bool) {
	switch position.side {
	case SideLong:
		stopHit := bar.Low <= position.stopPrice
		takeHit := bar.High >= position.signal.TakeProfit
		if stopHit {
			return position.stopPrice, position.stopReason, true
		}
		if takeHit {
			return position.signal.TakeProfit, ExitReasonTakeProfit, true
		}
	case SideShort:
		stopHit := bar.High >= position.stopPrice
		takeHit := bar.Low <= position.signal.TakeProfit
		if stopHit {
			return position.stopPrice, position.stopReason, true
		}
		if takeHit {
			return position.signal.TakeProfit, ExitReasonTakeProfit, true
		}
	}
	if e.Config.TimeStopBars > 0 && barIndex-position.entryBarIndex+1 >= e.Config.TimeStopBars {
		return bar.Close, ExitReasonTimeStop, true
	}
	e.updateLifecycleStops(position, bar)
	return 0, "", false
}

func (e Engine) updateLifecycleStops(position *openPosition, bar market.Kline) {
	switch position.side {
	case SideLong:
		if bar.High > position.peakPrice {
			position.peakPrice = bar.High
		}
		favorablePct := (position.peakPrice - position.entryPrice) / position.entryPrice * 100
		if e.Config.BreakevenTriggerPct > 0 && favorablePct >= e.Config.BreakevenTriggerPct {
			if position.stopPrice < position.entryPrice {
				position.stopPrice = position.entryPrice
				position.stopReason = ExitReasonBreakeven
			}
		}
		if e.Config.TrailingStartPct > 0 && favorablePct >= e.Config.TrailingStartPct {
			trailingStop := position.peakPrice * (1 - e.Config.TrailingDistancePct/100)
			if trailingStop > position.stopPrice {
				position.stopPrice = trailingStop
				position.stopReason = ExitReasonTrailing
			}
		}
	case SideShort:
		if bar.Low < position.troughPrice {
			position.troughPrice = bar.Low
		}
		favorablePct := (position.entryPrice - position.troughPrice) / position.entryPrice * 100
		if e.Config.BreakevenTriggerPct > 0 && favorablePct >= e.Config.BreakevenTriggerPct {
			if position.stopPrice > position.entryPrice {
				position.stopPrice = position.entryPrice
				position.stopReason = ExitReasonBreakeven
			}
		}
		if e.Config.TrailingStartPct > 0 && favorablePct >= e.Config.TrailingStartPct {
			trailingStop := position.troughPrice * (1 + e.Config.TrailingDistancePct/100)
			if trailingStop < position.stopPrice {
				position.stopPrice = trailingStop
				position.stopReason = ExitReasonTrailing
			}
		}
	}
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

func newRunState(start time.Time, equity float64) *runState {
	return &runState{
		currentDay:          dayKey(start),
		dailyStartEquity:    equity,
		lossStreakBySide:    make(map[Side]int),
		lossStreakBySymSide: make(map[string]int),
		cooldownBySide:      make(map[Side]int),
		cooldownBySymSide:   make(map[string]int),
	}
}

func (s *runState) ensureDay(ts time.Time, equity float64) {
	key := dayKey(ts)
	if s.currentDay == key {
		return
	}
	s.currentDay = key
	s.dailyStartEquity = equity
	s.dailyPnL = 0
	s.dailyTrades = 0
}

func (s *runState) recordEntry() {
	s.dailyTrades++
}

func (s *runState) recordExit(trade BacktestTrade) {
	s.dailyPnL += trade.NetPnL

	symSide := symbolSideKey(trade.Symbol, trade.Side)
	if trade.NetPnL < 0 {
		s.lossStreakBySide[trade.Side]++
		s.lossStreakBySymSide[symSide]++
	} else if trade.NetPnL > 0 {
		s.lossStreakBySide[trade.Side] = 0
		s.lossStreakBySymSide[symSide] = 0
	}
}

func (s *runState) riskDecision(signal StrategySignal, quantity, riskUSDT float64, reasons []string, cfg Config, barIndex int) RiskDecision {
	decision := RiskDecision{
		Action:       "allow",
		PositionSize: quantity,
		RiskUSDT:     riskUSDT,
		Reasons:      append([]string{}, reasons...),
	}
	if quantity <= 0 {
		decision.Action = "block"
		decision.Reasons = append(decision.Reasons, "invalid_position_size")
	}
	if cfg.MaxDailyLossPct > 0 && s.dailyStartEquity > 0 {
		maxLoss := s.dailyStartEquity * cfg.MaxDailyLossPct
		if s.dailyPnL <= -maxLoss {
			decision.Action = "block"
			decision.Reasons = append(decision.Reasons, "daily_loss_limit")
		}
	}
	if cfg.MaxDailyTrades > 0 && s.dailyTrades >= cfg.MaxDailyTrades {
		decision.Action = "block"
		decision.Reasons = append(decision.Reasons, "daily_trade_limit")
	}
	if cfg.CooldownLosses > 0 && cfg.CooldownBars > 0 {
		sideUntil := s.cooldownBySide[signal.Side]
		if barIndex < sideUntil {
			decision.Action = "block"
			decision.Reasons = append(decision.Reasons, "side_cooldown")
		}
		symSideUntil := s.cooldownBySymSide[symbolSideKey(signal.Symbol, signal.Side)]
		if barIndex < symSideUntil {
			decision.Action = "block"
			decision.Reasons = append(decision.Reasons, "symbol_side_cooldown")
		}
	}
	return decision
}

func (s *runState) updateCooldowns(trade BacktestTrade, barIndex int, cfg Config) {
	if cfg.CooldownLosses <= 0 || cfg.CooldownBars <= 0 || trade.NetPnL >= 0 {
		return
	}
	symSide := symbolSideKey(trade.Symbol, trade.Side)
	if s.lossStreakBySide[trade.Side] >= cfg.CooldownLosses {
		s.cooldownBySide[trade.Side] = barIndex + cfg.CooldownBars + 1
	}
	if s.lossStreakBySymSide[symSide] >= cfg.CooldownLosses {
		s.cooldownBySymSide[symSide] = barIndex + cfg.CooldownBars + 1
	}
}

func dayKey(ts time.Time) string {
	return ts.UTC().Format("2006-01-02")
}

func symbolSideKey(symbol string, side Side) string {
	return symbol + ":" + string(side)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
