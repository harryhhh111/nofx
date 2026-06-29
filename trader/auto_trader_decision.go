package trader

import (
	"context"
	"fmt"
	"math"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/telemetry"
	"strconv"
	"strings"
	"time"
)

// saveEquitySnapshot saves equity snapshot independently (for drawing profit curve, decoupled from AI decision)
func (at *AutoTrader) saveEquitySnapshot(ctx *kernel.Context) {
	if at.store == nil || ctx == nil {
		return
	}

	snapshot := &store.EquitySnapshot{
		TraderID:      at.id,
		Timestamp:     time.Now().UTC(),
		TotalEquity:   ctx.Account.TotalEquity,
		Balance:       ctx.Account.TotalEquity - ctx.Account.UnrealizedPnL,
		UnrealizedPnL: ctx.Account.UnrealizedPnL,
		PositionCount: ctx.Account.PositionCount,
		MarginUsedPct: ctx.Account.MarginUsedPct,
	}

	if err := at.store.Equity().Save(snapshot); err != nil {
		logger.Infof("⚠️ Failed to save equity snapshot: %v", err)
	}
}

// saveDecision saves AI decision log to database (only records AI input/output, for debugging)
func (at *AutoTrader) saveDecision(record *store.DecisionRecord) error {
	if at.store == nil {
		return nil
	}

	at.cycleNumber++
	record.CycleNumber = at.cycleNumber
	record.TraderID = at.id

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	if err := at.store.Decision().LogDecision(record); err != nil {
		logger.Infof("⚠️ Failed to save decision record: %v", err)
		return err
	}

	logger.Infof("📝 Decision record saved: trader=%s, cycle=%d", at.id, at.cycleNumber)
	return nil
}

func (at *AutoTrader) saveSignalCalibrationSamples(decision *kernel.FullDecision, record *store.DecisionRecord) {
	if at.store == nil || decision == nil || record == nil || len(decision.CalibrationSamples) == 0 {
		return
	}
	version := at.currentStrategyVersion()
	rows := make([]*store.SignalCalibrationSample, 0, len(decision.CalibrationSamples))
	for _, sample := range decision.CalibrationSamples {
		strategyVersion := sample.StrategyVersion
		if strategyVersion == "" {
			strategyVersion = version
		}
		rows = append(rows, &store.SignalCalibrationSample{
			TraderID:                   at.id,
			StrategyID:                 at.config.StrategyID,
			StrategyVersion:            strategyVersion,
			DecisionID:                 record.ID,
			CycleNumber:                record.CycleNumber,
			Symbol:                     sample.Symbol,
			SampleKind:                 sample.SampleKind,
			SignalID:                   sample.SignalID,
			RuleID:                     sample.RuleID,
			Setup:                      sample.Setup,
			Action:                     sample.Action,
			Eligible:                   sample.Eligible,
			Timeframe:                  sample.Timeframe,
			PrimaryTimeframe:           sample.PrimaryTimeframe,
			EntryTimeframe:             sample.EntryTimeframe,
			ConfirmationTimeframesJSON: store.MarshalCalibrationJSON(sample.ConfirmationTimeframes),
			EntryPrice:                 sample.EntryPrice,
			Confidence:                 sample.Confidence,
			Score:                      sample.Score,
			PrimaryScore:               sample.PrimaryScore,
			EntryScore:                 sample.EntryScore,
			ReviewStatus:               sample.ReviewStatus,
			ReviewReasonsJSON:          store.MarshalCalibrationJSON(sample.ReviewReasons),
			RiskStatus:                 sample.RiskStatus,
			RiskReason:                 sample.RiskReason,
			FactorSnapshotJSON:         store.MarshalCalibrationJSON(sample.FactorSnapshot),
			SetupTraceJSON:             store.MarshalCalibrationJSON(sample.SetupTrace),
			EvidenceTraceJSON:          store.MarshalCalibrationJSON(sample.EvidenceTrace),
			SignalJSON:                 store.MarshalCalibrationJSON(sample.Signal),
			MarketContextJSON:          store.MarshalCalibrationJSON(sample.MarketContext),
			KlineWindowsJSON:           store.MarshalCalibrationJSON(sample.KlineWindows),
			AsOf:                       sample.AsOf,
		})
	}
	if err := at.store.SignalCalibration().CreateMany(rows); err != nil {
		logger.Warnf("[%s] failed to save signal calibration samples: %v", at.name, err)
		return
	}
	logger.Infof("[%s] saved %d signal calibration sample(s)", at.name, len(rows))
}

// saveBBMACDSignals stores BB MACD snapshots for later offline accuracy evaluation.
// These records are not injected into AI prompts and do not affect trading decisions.
func (at *AutoTrader) saveBBMACDSignals(ctx *kernel.Context) {
	if at.store == nil || ctx == nil || len(ctx.MarketDataMap) == 0 {
		return
	}

	now := time.Now().UTC()
	signals := make([]*store.BBMACDSignal, 0)
	for symbol, data := range ctx.MarketDataMap {
		if data == nil || data.CurrentPrice <= 0 || len(data.TimeframeData) == 0 {
			continue
		}
		normalizedSymbol := market.Normalize(symbol)
		for timeframe, tfData := range data.TimeframeData {
			if tfData == nil || tfData.BBMACD == nil {
				continue
			}
			bb := tfData.BBMACD
			signals = append(signals, &store.BBMACDSignal{
				TraderID:       at.id,
				CycleNumber:    at.callCount,
				Symbol:         normalizedSymbol,
				Timeframe:      timeframe,
				SignalTime:     now,
				Price:          data.CurrentPrice,
				Regime:         bb.Regime,
				State:          bb.State,
				Strength:       bb.Strength,
				FastPeriod:     bb.Params.Fast,
				SlowPeriod:     bb.Params.Slow,
				SignalPeriod:   bb.Params.Signal,
				BOLLPeriod:     bb.Params.BOLLPeriod,
				BOLLMultiplier: bb.Params.BOLLMultiplier,
				MACD:           bb.MACD,
				MACDSignal:     bb.Signal,
				Histogram:      bb.Histogram,
				UpperBand:      bb.Upper,
				MiddleBand:     bb.Middle,
				LowerBand:      bb.Lower,
			})
		}
	}

	if len(signals) == 0 {
		return
	}
	if err := at.store.BBMACDSignal().CreateMany(signals); err != nil {
		logger.Warnf("Failed to save BB MACD signals: %v", err)
		return
	}
	logger.Infof("Saved %d BB MACD signal snapshots", len(signals))
}

// syncClosedTradeMemories writes one AI-reviewed memory for each newly closed position.
// It is deliberately post-trade only: memory can influence future review, but it cannot
// rewrite the strategy that produced the trade.
func (at *AutoTrader) syncClosedTradeMemories(limit int) {
	if at.store == nil || at.mcpClient == nil {
		return
	}
	if limit <= 0 {
		limit = 3
	}
	closed, err := at.store.Position().GetClosedPositions(at.id, limit)
	if err != nil {
		logger.Warnf("[%s] failed to load closed positions for trade memory: %v", at.name, err)
		return
	}
	if len(closed) == 0 {
		return
	}

	memoryStore := kernel.NewStoreTradeMemory(at.store, at.id)
	summarizer := kernel.NewLLMTradeMemorySummarizer(at.mcpClient)
	for _, pos := range closed {
		exists, err := at.store.TradeMemory().ExistsForPosition(at.id, pos.ID)
		if err != nil {
			logger.Warnf("[%s] failed to check trade memory for position %d: %v", at.name, pos.ID, err)
			continue
		}
		if exists {
			continue
		}

		outcome := at.closedTradeOutcome(pos)
		reviewCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		summary, err := summarizer.SummarizeClosedTrade(reviewCtx, outcome)
		cancel()
		if err != nil {
			logger.Warnf("[%s] failed to summarize closed trade memory for %s position %d: %v", at.name, pos.Symbol, pos.ID, err)
			continue
		}

		record := kernel.BuildTradeMemoryRecord(outcome, *summary)
		if err := memoryStore.Record(context.Background(), record); err != nil {
			logger.Warnf("[%s] failed to save trade memory for %s position %d: %v", at.name, pos.Symbol, pos.ID, err)
			continue
		}
		logger.Infof("[%s] saved trade memory for %s position %d (%s, pnl %.4f)",
			at.name, pos.Symbol, pos.ID, summary.Result, pos.RealizedPnL)
	}
}

func (at *AutoTrader) saveOpeningSignalMetadata(record *store.DecisionRecord) {
	if at.store == nil || record == nil {
		return
	}
	for _, action := range record.Decisions {
		if !action.Success || (action.Action != "open_long" && action.Action != "open_short") {
			continue
		}
		if action.SignalID == "" && action.RuleID == "" && action.Setup == "" {
			continue
		}
		side := "LONG"
		if action.Action == "open_short" {
			side = "SHORT"
		}
		symbol := market.Normalize(action.Symbol)
		meta := store.PositionOpeningSignalMetadata{
			DecisionID:      record.ID,
			SignalID:        action.SignalID,
			RuleID:          action.RuleID,
			Setup:           action.Setup,
			StrategyID:      at.config.StrategyID,
			StrategyVersion: action.Version,
		}
		if meta.StrategyVersion == "" {
			meta.StrategyVersion = at.currentStrategyVersion()
		}
		if err := at.store.Position().UpdatePositionOpeningSignalMetadata(at.id, symbol, side, meta); err != nil {
			logger.Infof("🧭 [%s] Position not yet in DB for signal metadata %s %s, starting background retry", at.name, symbol, side)
			go at.retryOpeningSignalMetadata(symbol, side, meta)
		} else {
			logger.Infof("🧭 [%s] Saved opening signal metadata for %s %s signal=%s setup=%s", at.name, symbol, side, meta.SignalID, meta.Setup)
		}
	}
}

func (at *AutoTrader) retryOpeningSignalMetadata(symbol, side string, meta store.PositionOpeningSignalMetadata) {
	for i := 0; i < 12; i++ {
		time.Sleep(5 * time.Second)
		if at.store == nil {
			return
		}
		if err := at.store.Position().UpdatePositionOpeningSignalMetadata(at.id, symbol, side, meta); err == nil {
			logger.Infof("🧭 [%s] Background flush: saved opening signal metadata for %s %s (attempt %d)", at.name, symbol, side, i+1)
			return
		}
	}
	logger.Infof("⚠️ [%s] Background flush failed for opening signal metadata %s %s after 60s", at.name, symbol, side)
}

func (at *AutoTrader) closedTradeOutcome(pos *store.TraderPosition) kernel.ClosedTradeOutcome {
	if pos == nil {
		return kernel.ClosedTradeOutcome{}
	}
	entryQty := pos.EntryQuantity
	if entryQty <= 0 {
		entryQty = pos.Quantity
	}
	notional := entryQty * pos.EntryPrice
	pnlPct := 0.0
	if notional > 0 {
		pnlPct = pos.RealizedPnL / notional * 100
	}
	strategyVersion := pos.StrategyVersion
	if strategyVersion == "" {
		strategyVersion = at.currentStrategyVersion()
	}
	return kernel.ClosedTradeOutcome{
		TraderID:          at.id,
		StrategyID:        pos.StrategyID,
		StrategyVersion:   strategyVersion,
		SignalID:          pos.OpeningSignalID,
		RuleID:            pos.OpeningRuleID,
		Setup:             pos.OpeningSetup,
		PositionID:        pos.ID,
		Symbol:            pos.Symbol,
		Side:              pos.Side,
		EntryPrice:        pos.EntryPrice,
		ExitPrice:         pos.ExitPrice,
		Quantity:          entryQty,
		PositionSizeUSD:   notional,
		Leverage:          pos.Leverage,
		RealizedPnL:       pos.RealizedPnL,
		RealizedPnLPct:    pnlPct,
		Fee:               pos.Fee,
		EntryTimeMs:       pos.EntryTime,
		ExitTimeMs:        pos.ExitTime,
		HoldDuration:      formatMemoryDuration(pos.EntryTime, pos.ExitTime),
		CloseReason:       pos.CloseReason,
		OpeningReasoning:  pos.OpeningReasoning,
		LastReviewSummary: pos.LastReviewSummary,
		OpeningDecisionID: pos.OpeningDecisionID,

		StopLossSource:         pos.StopLossSource,
		StopLossTimeframe:      pos.StopLossTimeframe,
		StopLossAnchor:         pos.StopLossAnchor,
		TakeProfitSource:       pos.TakeProfitSource,
		TakeProfitTimeframe:    pos.TakeProfitTimeframe,
		TakeProfitAnchor:       pos.TakeProfitAnchor,
		ProtectiveATR:          pos.ProtectiveATR,
		ProtectiveATRTimeframe: pos.ProtectiveATRTimeframe,
		ProtectiveATRBuffer:    pos.ProtectiveATRBuffer,
		ProtectiveRiskReward:   pos.ProtectiveRiskReward,
	}
}

func (at *AutoTrader) currentStrategyVersion() string {
	if at == nil || at.strategyEngine == nil || at.strategyEngine.GetConfig() == nil {
		return ""
	}
	config := at.strategyEngine.GetConfig()
	if version := kernel.StrategyConfigFingerprint(config); version != "" {
		return version
	}
	for _, rule := range config.CompiledRules {
		if rule.Version != "" {
			return rule.Version
		}
	}
	return ""
}

func formatMemoryDuration(startMs, endMs int64) string {
	if startMs <= 0 || endMs <= startMs {
		return ""
	}
	d := time.Duration(endMs-startMs) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
}

// GetStatus gets system status (for API)
func (at *AutoTrader) GetStatus() map[string]interface{} {
	aiProvider := "DeepSeek"
	if at.config.UseQwen {
		aiProvider = "Qwen"
	}

	at.isRunningMutex.RLock()
	isRunning := at.isRunning
	at.isRunningMutex.RUnlock()

	result := map[string]interface{}{
		"trader_id":       at.id,
		"trader_name":     at.name,
		"ai_model":        at.aiModel,
		"exchange":        at.exchange,
		"is_running":      isRunning,
		"start_time":      at.startTime.Format(time.RFC3339),
		"runtime_minutes": int(time.Since(at.startTime).Minutes()),
		"call_count":      at.callCount,
		"initial_balance": at.initialBalance,
		"scan_interval":   at.config.ScanInterval.String(),
		"stop_until":      at.stopUntil.Format(time.RFC3339),
		"last_reset_time": at.lastResetTime.Format(time.RFC3339),
		"ai_provider":     aiProvider,
	}

	// Add strategy info
	if at.config.StrategyConfig != nil {
		result["strategy_type"] = at.config.StrategyConfig.StrategyType
		if at.config.StrategyConfig.GridConfig != nil {
			result["grid_symbol"] = at.config.StrategyConfig.GridConfig.Symbol
		}
	}

	return result
}

// GetAccountInfo gets account information (for API)
func (at *AutoTrader) GetAccountInfo() (map[string]interface{}, error) {
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Get account fields
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0
	totalEquity := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Use totalEquity directly if provided by trader (more accurate)
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		totalEquity = eq
	} else {
		// Fallback: Total Equity = Wallet balance + Unrealized profit
		totalEquity = totalWalletBalance + totalUnrealizedProfit
	}

	// Get positions to calculate total margin
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	totalMarginUsed := 0.0
	totalUnrealizedPnLCalculated := 0.0
	for _, pos := range positions {
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		totalUnrealizedPnLCalculated += unrealizedPnl

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed
	}

	// Verify unrealized P&L consistency (API value vs calculated from positions)
	// Note: Lighter API may return 0 for unrealized PnL, this is a known limitation
	diff := math.Abs(totalUnrealizedProfit - totalUnrealizedPnLCalculated)
	if diff > 5.0 { // Only warn if difference is significant (> 5 USDT)
		logger.Infof("⚠️ Unrealized P&L inconsistency (Lighter API limitation): API=%.4f, Calculated=%.4f, Diff=%.4f",
			totalUnrealizedProfit, totalUnrealizedPnLCalculated, diff)
	}

	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	} else {
		logger.Infof("⚠️ Initial Balance abnormal: %.2f, cannot calculate P&L percentage", at.initialBalance)
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	return map[string]interface{}{
		// Core fields
		"total_equity":      totalEquity,           // Account equity = wallet + unrealized
		"wallet_balance":    totalWalletBalance,    // Wallet balance (excluding unrealized P&L)
		"unrealized_profit": totalUnrealizedProfit, // Unrealized P&L (official value from exchange API)
		"available_balance": availableBalance,      // Available balance

		// P&L statistics
		"total_pnl":       totalPnL,          // Total P&L = equity - initial
		"total_pnl_pct":   totalPnLPct,       // Total P&L percentage
		"initial_balance": at.initialBalance, // Initial balance
		"daily_pnl":       at.dailyPnL,       // Daily P&L

		// Position information
		"position_count":  len(positions),  // Position count
		"margin_used":     totalMarginUsed, // Margin used
		"margin_used_pct": marginUsedPct,   // Margin usage rate
	}, nil
}

// GetPositions gets position list (for API)
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		// Calculate margin used
		marginUsed := (quantity * markPrice) / float64(leverage)

		// Calculate P&L percentage (based on margin)
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		result = append(result, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealizedPnl,
			"unrealized_pnl_pct": pnlPct,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
		})
	}

	return result, nil
}

// ClosePositionManually closes a position through the live trader instance used
// by this AutoTrader. Paper trading keeps open positions in memory, so manual
// closes must go through this instance instead of a temporary PaperTrader.
func (at *AutoTrader) ClosePositionManually(symbol, side string) (map[string]interface{}, float64, float64, error) {
	if at == nil || at.trader == nil {
		return nil, 0, 0, fmt.Errorf("trader is not available")
	}

	normalizedSymbol := market.Normalize(symbol)
	normalizedSide := strings.ToLower(strings.TrimSpace(side))
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to get positions: %w", err)
	}

	var posQty float64
	var entryPrice float64
	for _, pos := range positions {
		posSymbol, _ := pos["symbol"].(string)
		posSide, _ := pos["side"].(string)
		if market.Normalize(posSymbol) != normalizedSymbol || !strings.EqualFold(posSide, normalizedSide) {
			continue
		}
		if amt, ok := pos["positionAmt"].(float64); ok {
			posQty = math.Abs(amt)
		}
		if price, ok := pos["entryPrice"].(float64); ok {
			entryPrice = price
		}
		break
	}

	if posQty <= 0 {
		return nil, 0, 0, fmt.Errorf("no %s position for %s", normalizedSide, normalizedSymbol)
	}

	var result map[string]interface{}
	switch normalizedSide {
	case "long":
		result, err = at.trader.CloseLong(normalizedSymbol, 0)
	case "short":
		result, err = at.trader.CloseShort(normalizedSymbol, 0)
	default:
		return nil, 0, 0, fmt.Errorf("side must be LONG or SHORT")
	}
	if err != nil {
		return nil, 0, 0, err
	}

	return result, posQty, entryPrice, nil
}

// recordAndConfirmOrder polls order status for actual fill data and records position
// action: open_long, open_short, close_long, close_short
// entryPrice: entry price when closing (0 when opening)
func (at *AutoTrader) recordAndConfirmOrder(orderResult map[string]interface{}, symbol, action string, quantity float64, price float64, leverage int, entryPrice float64, executionAnalyticsID int64) {
	if at.store == nil {
		return
	}

	// Get order ID (supports multiple types)
	var orderID string
	switch v := orderResult["orderId"].(type) {
	case int64:
		orderID = fmt.Sprintf("%d", v)
	case float64:
		orderID = fmt.Sprintf("%.0f", v)
	case string:
		orderID = v
	default:
		orderID = fmt.Sprintf("%v", v)
	}

	if orderID == "" || orderID == "0" {
		logger.Infof("  ⚠️ Order ID is empty, skipping record")
		return
	}

	at.markExecutionOrderSubmitted(executionAnalyticsID, orderResult)

	// Determine positionSide
	var positionSide string
	switch action {
	case "open_long", "close_long":
		positionSide = "LONG"
	case "open_short", "close_short":
		positionSide = "SHORT"
	}

	var actualPrice = price
	var actualQty = quantity
	var fee float64
	submittedStatus, _ := orderResult["status"].(string)
	if avgPrice, ok := orderResultNumber(orderResult, "avgPrice"); ok && avgPrice > 0 {
		actualPrice = avgPrice
	}
	if execQty, ok := orderResultNumber(orderResult, "executedQty"); ok && execQty > 0 {
		actualQty = execQty
	}
	if commission, ok := orderResultNumber(orderResult, "commission"); ok {
		fee = commission
	}

	// Exchanges with OrderSync: Skip immediate order recording, let OrderSync handle it
	// This ensures accurate data from GetTrades API and avoids duplicate records
	switch at.exchange {
	case "binance", "lighter", "hyperliquid", "bybit", "okx", "bitget", "aster", "kucoin", "gate":
		logger.Infof("  📝 Order submitted (id: %s), will be synced by OrderSync", orderID)
		return
	}

	// For exchanges without OrderSync (e.g., Binance): record immediately and poll for fill data
	orderRecord := at.createOrderRecord(orderID, symbol, action, positionSide, actualQty, actualPrice, leverage)
	if err := at.store.Order().CreateOrder(orderRecord); err != nil {
		logger.Infof("  ⚠️ Failed to record order: %v", err)
	} else {
		logger.Infof("  📝 Order recorded: %s [%s] %s", orderID, action, symbol)
	}

	// Wait for order to be filled and get actual fill data
	fillRecorded := false
	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 5; i++ {
		status, err := at.trader.GetOrderStatus(symbol, orderID)
		if err == nil {
			statusStr, _ := status["status"].(string)
			if statusStr == "FILLED" {
				// Get actual fill price
				if avgPrice, ok := status["avgPrice"].(float64); ok && avgPrice > 0 {
					actualPrice = avgPrice
				}
				// Get actual executed quantity
				if execQty, ok := status["executedQty"].(float64); ok && execQty > 0 {
					actualQty = execQty
				}
				// Get commission/fee
				if commission, ok := status["commission"].(float64); ok {
					fee = commission
				}
				logger.Infof("  ✅ Order filled: avgPrice=%.6f, qty=%.6f, fee=%.6f", actualPrice, actualQty, fee)

				at.markExecutionFinal(executionAnalyticsID, action, price, quantity, actualPrice, actualQty, "filled")

				// Update order status to FILLED
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", actualQty, actualPrice, fee); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}

				// Record fill details
				at.recordOrderFill(orderRecord.ID, orderID, symbol, action, actualPrice, actualQty, fee)
				fillRecorded = true
				break
			} else if statusStr == "CANCELED" || statusStr == "EXPIRED" || statusStr == "REJECTED" {
				logger.Infof("  ⚠️ Order %s, skipping position record", statusStr)
				at.markExecutionFinal(executionAnalyticsID, action, price, quantity, actualPrice, actualQty, strings.ToLower(statusStr))

				// Update order status
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, statusStr, 0, 0, 0); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !fillRecorded && strings.EqualFold(submittedStatus, "FILLED") && actualQty > 0 && actualPrice > 0 {
		logger.Infof("  ✅ Order filled from submit response: avgPrice=%.6f, qty=%.6f, fee=%.6f", actualPrice, actualQty, fee)
		at.markExecutionFinal(executionAnalyticsID, action, price, quantity, actualPrice, actualQty, "filled")
		if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", actualQty, actualPrice, fee); err != nil {
			logger.Infof("  ⚠️ Failed to update order status: %v", err)
		}
		at.recordOrderFill(orderRecord.ID, orderID, symbol, action, actualPrice, actualQty, fee)
		fillRecorded = true
	}
	if actualQty <= 0 || actualPrice <= 0 {
		logger.Infof("  ⚠️ Filled order %s has invalid fill data (price=%.6f qty=%.6f), skipping position update", orderID, actualPrice, actualQty)
		return
	}

	// Normalize symbol for position record consistency
	normalizedSymbolForPosition := market.Normalize(symbol)

	logger.Infof("  📝 Recording position (ID: %s, action: %s, price: %.6f, qty: %.6f, fee: %.4f)",
		orderID, action, actualPrice, actualQty, fee)

	// Record position change with actual fill data (use normalized symbol)
	at.recordPositionChange(orderID, normalizedSymbolForPosition, positionSide, action, actualQty, actualPrice, leverage, entryPrice, fee)

	// Send anonymous trade statistics for experience improvement (async, non-blocking)
	// This helps us understand overall product usage across all deployments
	telemetry.TrackTrade(telemetry.TradeEvent{
		Exchange:  at.exchange,
		TradeType: action,
		Symbol:    symbol,
		AmountUSD: actualPrice * actualQty,
		Leverage:  leverage,
		UserID:    at.userID,
		TraderID:  at.id,
	})
}

func orderResultNumber(orderResult map[string]interface{}, key string) (float64, bool) {
	if orderResult == nil {
		return 0, false
	}
	switch v := orderResult[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// recordPositionChange records position change (create record on open, update record on close)
func (at *AutoTrader) recordPositionChange(orderID, symbol, side, action string, quantity, price float64, leverage int, entryPrice float64, fee float64) {
	if at.store == nil {
		return
	}

	switch action {
	case "open_long", "open_short":
		// Open position: create new position record
		nowMs := time.Now().UTC().UnixMilli()
		pos := &store.TraderPosition{
			TraderID:     at.id,
			ExchangeID:   at.exchangeID, // Exchange account UUID
			ExchangeType: at.exchange,   // Exchange type: binance/bybit/okx/etc
			Symbol:       symbol,
			Side:         side, // LONG or SHORT
			Quantity:     quantity,
			EntryPrice:   price,
			EntryOrderID: orderID,
			EntryTime:    nowMs,
			Leverage:     leverage,
			Status:       "OPEN",
			CreatedAt:    nowMs,
			UpdatedAt:    nowMs,
		}
		if err := at.store.Position().Create(pos); err != nil {
			logger.Infof("  ⚠️ Failed to record position: %v", err)
		} else {
			logger.Infof("  📊 Position recorded [%s] %s %s @ %.4f", at.id[:8], symbol, side, price)
		}

	case "close_long", "close_short":
		// Close position using PositionBuilder for consistent handling
		// PositionBuilder will handle both cases:
		// 1. If open position exists: close it properly
		// 2. If no open position (e.g., table cleared): create a closed position record
		posBuilder := store.NewPositionBuilder(at.store.Position())
		if err := posBuilder.ProcessTrade(
			at.id, at.exchangeID, at.exchange,
			symbol, side, action,
			quantity, price, fee, 0, // realizedPnL will be calculated
			time.Now().UTC().UnixMilli(), orderID,
		); err != nil {
			logger.Infof("  ⚠️ Failed to process close position: %v", err)
		} else {
			logger.Infof("  ✅ Position closed [%s] %s %s @ %.4f", at.id[:8], symbol, side, price)
		}
	}
}

// createOrderRecord creates an order record struct from order details
func (at *AutoTrader) createOrderRecord(orderID, symbol, action, positionSide string, quantity, price float64, leverage int) *store.TraderOrder {
	// Determine order type (market for auto trader)
	orderType := "MARKET"

	// Determine side (BUY/SELL)
	var side string
	switch action {
	case "open_long", "close_short":
		side = "BUY"
	case "open_short", "close_long":
		side = "SELL"
	}

	// Use action as orderAction directly (keep lowercase format)
	orderAction := action

	// Determine if it's a reduce only order
	reduceOnly := (action == "close_long" || action == "close_short")

	// Normalize symbol for consistency
	normalizedSymbol := market.Normalize(symbol)

	return &store.TraderOrder{
		TraderID:        at.id,
		ExchangeID:      at.exchangeID,
		ExchangeType:    at.exchange,
		ExchangeOrderID: orderID,
		Symbol:          normalizedSymbol,
		Side:            side,
		PositionSide:    positionSide,
		Type:            orderType,
		TimeInForce:     "GTC",
		Quantity:        quantity,
		Price:           price,
		Status:          "NEW",
		FilledQuantity:  0,
		AvgFillPrice:    0,
		Commission:      0,
		CommissionAsset: "USDT",
		Leverage:        leverage,
		ReduceOnly:      reduceOnly,
		ClosePosition:   reduceOnly,
		OrderAction:     orderAction,
		CreatedAt:       time.Now().UTC().UnixMilli(),
		UpdatedAt:       time.Now().UTC().UnixMilli(),
	}
}

// recordOrderFill records order fill/trade details
func (at *AutoTrader) recordOrderFill(orderRecordID int64, exchangeOrderID, symbol, action string, price, quantity, fee float64) {
	if at.store == nil {
		return
	}

	// Determine side (BUY/SELL)
	var side string
	switch action {
	case "open_long", "close_short":
		side = "BUY"
	case "open_short", "close_long":
		side = "SELL"
	}

	// Generate a simple trade ID (exchange doesn't always provide one)
	tradeID := fmt.Sprintf("%s-%d", exchangeOrderID, time.Now().UnixNano())

	// Normalize symbol for consistency
	normalizedSymbol := market.Normalize(symbol)

	fill := &store.TraderFill{
		TraderID:        at.id,
		ExchangeID:      at.exchangeID,
		ExchangeType:    at.exchange,
		OrderID:         orderRecordID,
		ExchangeOrderID: exchangeOrderID,
		ExchangeTradeID: tradeID,
		Symbol:          normalizedSymbol,
		Side:            side,
		Price:           price,
		Quantity:        quantity,
		QuoteQuantity:   price * quantity,
		Commission:      fee,
		CommissionAsset: "USDT",
		RealizedPnL:     0,     // Will be calculated for close orders
		IsMaker:         false, // Market orders are usually taker
		CreatedAt:       time.Now().UTC().UnixMilli(),
	}

	// Calculate realized PnL for close orders
	if action == "close_long" || action == "close_short" {
		// Try to get the entry price from the open position
		var positionSide string
		if action == "close_long" {
			positionSide = "LONG"
		} else {
			positionSide = "SHORT"
		}

		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, positionSide); err == nil && openPos != nil {
			if positionSide == "LONG" {
				fill.RealizedPnL = (price - openPos.EntryPrice) * quantity
			} else {
				fill.RealizedPnL = (openPos.EntryPrice - price) * quantity
			}
		}
	}

	if err := at.store.Order().CreateFill(fill); err != nil {
		logger.Infof("  ⚠️ Failed to record fill: %v", err)
	} else {
		logger.Infof("  📋 Fill recorded: %.4f @ %.6f, fee: %.4f", quantity, price, fee)
	}
}

// GetOpenOrders returns open orders (pending SL/TP) from exchange
func (at *AutoTrader) GetOpenOrders(symbol string) ([]OpenOrder, error) {
	return at.trader.GetOpenOrders(symbol)
}
