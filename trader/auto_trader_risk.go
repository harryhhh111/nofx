package trader

import (
	"fmt"
	"math"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

// startDrawdownMonitor starts drawdown monitoring
func (at *AutoTrader) startDrawdownMonitor() {
	at.monitorWg.Add(1)
	go func() {
		defer at.monitorWg.Done()

		ticker := time.NewTicker(1 * time.Minute) // Check every minute
		defer ticker.Stop()

		logger.Info("📊 Started position drawdown monitoring (check every minute)")

		for {
			select {
			case <-ticker.C:
				at.checkPositionDrawdown()
			case <-at.stopMonitorCh:
				logger.Info("⏹ Stopped position drawdown monitoring")
				return
			}
		}
	}()
}

// checkPositionDrawdown checks position drawdown situation
func (at *AutoTrader) checkPositionDrawdown() {
	// Read drawdown config from strategy engine (with safe defaults)
	minProfitPct := 5.0
	triggerPct := 40.0
	useAI := false
	enabled := true
	if at.strategyEngine != nil {
		cfg := at.strategyEngine.GetConfig()
		rc := cfg.RiskControl
		if rc.DrawdownCloseMinProfitPct > 0 {
			minProfitPct = rc.DrawdownCloseMinProfitPct
		}
		if rc.DrawdownCloseTriggerPct > 0 {
			triggerPct = rc.DrawdownCloseTriggerPct
		}
		useAI = rc.DrawdownCloseUseAI
		// DrawdownCloseEnabled defaults to true; only disable when explicitly set to false
		// AND both thresholds are non-zero (i.e., config has been saved at least once).
		if rc.DrawdownCloseMinProfitPct > 0 || rc.DrawdownCloseTriggerPct > 0 {
			enabled = rc.DrawdownCloseEnabled
		}
	}
	if !enabled {
		return
	}

	// Get current positions
	positions, err := at.trader.GetPositions()
	if err != nil {
		logger.Infof("❌ Drawdown monitoring: failed to get positions: %v", err)
		return
	}

	// Breakeven protection runs first so SL reflects "this minute's latest profit"
	// before drawdown check uses peak-cache math.
	at.checkBreakevenPromotion(positions)

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // Short position quantity is negative, convert to positive
		}

		// Guard: skip if entry price is zero (prevents division by zero panic)
		if entryPrice <= 0 {
			logger.Warnf("⚠️ Drawdown monitoring: %s %s has zero entry price, skipping", symbol, side)
			continue
		}

		// Calculate current P&L percentage
		leverage := 10 // Default value
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		var currentPnLPct float64
		if side == "long" {
			currentPnLPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			currentPnLPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		// Construct unique position identifier (distinguish long/short)
		posKey := symbol + "_" + side

		// Get historical peak profit for this position
		at.peakPnLCacheMutex.RLock()
		peakPnLPct, exists := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		if !exists {
			// If no historical peak record, use current P&L as initial value
			peakPnLPct = currentPnLPct
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		} else {
			// Update peak cache
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		}

		// Calculate drawdown (magnitude of decline from peak)
		var drawdownPct float64
		if peakPnLPct > 0 && currentPnLPct < peakPnLPct {
			drawdownPct = ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
		}

		// Check close position condition
		if currentPnLPct > minProfitPct && drawdownPct >= triggerPct {
			logger.Infof("🚨 Drawdown condition triggered: %s %s | Current profit: %.2f%% | Peak: %.2f%% | Drawdown: %.2f%% | mode=%s",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct, map[bool]string{true: "ai-decide", false: "auto-close"}[useAI])

			if useAI {
				// AI-decide mode: queue an alert into the next AI cycle instead of closing immediately
				normalizedSymbol := market.Normalize(symbol)
				openingReason := ""
				if at.store != nil {
					sideUpper := strings.ToUpper(side)
					if dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, sideUpper); err == nil && dbPos != nil {
						openingReason = dbPos.OpeningReasoning
					}
				}
				alert := kernel.DrawdownAlert{
					Symbol:        normalizedSymbol,
					Side:          side,
					CurrentPnLPct: currentPnLPct,
					PeakPnLPct:    peakPnLPct,
					DrawdownPct:   drawdownPct,
					OpeningReason: openingReason,
				}
				at.pendingDrawdownAlertsMu.Lock()
				// Deduplicate: replace existing alert for same symbol+side
				replaced := false
				for i, existing := range at.pendingDrawdownAlerts {
					if existing.Symbol == alert.Symbol && existing.Side == alert.Side {
						at.pendingDrawdownAlerts[i] = alert
						replaced = true
						break
					}
				}
				if !replaced {
					at.pendingDrawdownAlerts = append(at.pendingDrawdownAlerts, alert)
				}
				at.pendingDrawdownAlertsMu.Unlock()
				logger.Infof("📋 [%s] Drawdown alert queued for AI decision: %s %s", at.name, symbol, side)
			} else {
				// Auto-close mode: close immediately
				if err := at.emergencyClosePosition(symbol, side); err != nil {
					logger.Infof("❌ Drawdown close position failed (%s %s): %v", symbol, side, err)
				} else {
					logger.Infof("✅ Drawdown close position succeeded: %s %s", symbol, side)
					at.ClearPeakPnLCache(symbol, side)
					at.ClearBreakevenSteps(symbol, side)
					at.saveRiskCloseDecision(symbol, side, currentPnLPct, peakPnLPct, drawdownPct)
				}
			}
		} else if currentPnLPct > minProfitPct {
			// Record situations close to close position condition (for debugging)
			logger.Infof("📊 Drawdown monitoring: %s %s | Profit: %.2f%% | Peak: %.2f%% | Drawdown: %.2f%%",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct)
		}
	}
}

// emergencyClosePosition emergency close position function
func (at *AutoTrader) emergencyClosePosition(symbol, side string) error {
	normalizedSymbol := market.Normalize(symbol)
	switch side {
	case "long":
		order, err := at.trader.CloseLong(symbol, 0) // 0 = close all
		if err != nil {
			return err
		}
		if at.store != nil {
			oid := store.FormatExchangeOrderIDFromMap(order)
			if err := at.store.Position().SetPendingCloseReason(at.id, normalizedSymbol, "LONG", "risk", oid); err != nil {
				logger.Warnf("SetPendingCloseReason(risk) failed trader=%s symbol=%s side=LONG: %v", at.id, normalizedSymbol, err)
			}
		}
		// Mark position as recently closed so the next AI cycle ignores stale exchange data
		at.recentlyClosedByRiskMu.Lock()
		at.recentlyClosedByRisk[normalizedSymbol+"_long"] = time.Now()
		at.recentlyClosedByRiskMu.Unlock()
		logger.Infof("✅ Emergency close long position succeeded, order ID: %v", order["orderId"])
	case "short":
		order, err := at.trader.CloseShort(symbol, 0) // 0 = close all
		if err != nil {
			return err
		}
		if at.store != nil {
			oid := store.FormatExchangeOrderIDFromMap(order)
			if err := at.store.Position().SetPendingCloseReason(at.id, normalizedSymbol, "SHORT", "risk", oid); err != nil {
				logger.Warnf("SetPendingCloseReason(risk) failed trader=%s symbol=%s side=SHORT: %v", at.id, normalizedSymbol, err)
			}
		}
		// Mark position as recently closed so the next AI cycle ignores stale exchange data
		at.recentlyClosedByRiskMu.Lock()
		at.recentlyClosedByRisk[normalizedSymbol+"_short"] = time.Now()
		at.recentlyClosedByRiskMu.Unlock()
		logger.Infof("✅ Emergency close short position succeeded, order ID: %v", order["orderId"])
	default:
		return fmt.Errorf("unknown position direction: %s", side)
	}

	return nil
}

// GetPeakPnLCache gets peak profit cache
func (at *AutoTrader) GetPeakPnLCache() map[string]float64 {
	at.peakPnLCacheMutex.RLock()
	defer at.peakPnLCacheMutex.RUnlock()

	// Return a copy of the cache
	cache := make(map[string]float64)
	for k, v := range at.peakPnLCache {
		cache[k] = v
	}
	return cache
}

// UpdatePeakPnL updates peak profit cache
func (at *AutoTrader) UpdatePeakPnL(symbol, side string, currentPnLPct float64) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	if peak, exists := at.peakPnLCache[posKey]; exists {
		// Update peak (if long, take larger value; if short, currentPnLPct is negative, also compare)
		if currentPnLPct > peak {
			at.peakPnLCache[posKey] = currentPnLPct
		}
	} else {
		// First time recording
		at.peakPnLCache[posKey] = currentPnLPct
	}
}

// ClearPeakPnLCache clears peak cache for specified position
func (at *AutoTrader) ClearPeakPnLCache(symbol, side string) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	delete(at.peakPnLCache, posKey)
}

// ClearBreakevenSteps clears breakeven promotion state for a position.
// Called from every position close path (risk-triggered, AI-decide, grid).
func (at *AutoTrader) ClearBreakevenSteps(symbol, side string) {
	at.breakevenStepsMutex.Lock()
	defer at.breakevenStepsMutex.Unlock()

	posKey := symbol + "_" + side
	delete(at.breakevenSteps, posKey)
}

// saveRiskCloseDecision saves a decision record for a risk-monitor-triggered position close.
// This makes the close visible in the frontend decision card history.
func (at *AutoTrader) saveRiskCloseDecision(symbol, side string, currentPnLPct, peakPnLPct, drawdownPct float64) {
	if at.store == nil {
		return
	}

	action := "close_long"
	if side == "short" {
		action = "close_short"
	}
	normalizedSymbol := market.Normalize(symbol)
	reasoning := fmt.Sprintf(
		"[风控自动平仓] %s %s — 当前收益 %.2f%%，峰值收益 %.2f%%，回撤幅度 %.2f%%，超过40%%阈值，触发强制平仓。",
		normalizedSymbol, strings.ToUpper(side), currentPnLPct, peakPnLPct, drawdownPct,
	)

	actionRecord := store.DecisionAction{
		Action:    action,
		Symbol:    normalizedSymbol,
		Reasoning: reasoning,
		Timestamp: time.Now().UTC(),
		Success:   true,
	}
	record := &store.DecisionRecord{
		TraderID:     at.id,
		CycleNumber:  0, // 0 indicates a system-generated (non-AI) record
		Timestamp:    time.Now().UTC(),
		CotSummary:   reasoning,
		ExecutionLog: []string{fmt.Sprintf("✅ 风控平仓: %s %s", normalizedSymbol, strings.ToUpper(side))},
		Decisions:    []store.DecisionAction{actionRecord},
		Success:      true,
	}

	if err := at.store.Decision().LogDecision(record); err != nil {
		logger.Warnf("⚠️ [%s] Failed to save risk-close decision record for %s %s: %v", at.name, symbol, side, err)
	} else {
		logger.Infof("📝 [%s] Risk-close decision record saved for %s %s", at.name, symbol, side)
	}
}

// ============================================================================
// Risk Control Helpers
// ============================================================================

// isBTCETH checks if a symbol is BTC or ETH
func isBTCETH(symbol string) bool {
	symbol = strings.ToUpper(symbol)
	return strings.HasPrefix(symbol, "BTC") || strings.HasPrefix(symbol, "ETH")
}

// enforcePositionValueRatio checks and enforces position value ratio limits (CODE ENFORCED)
// Returns the adjusted position size (capped if necessary) and whether the position was capped
// positionSizeUSD: the original position size in USD
// equity: the account equity
// symbol: the trading symbol
func (at *AutoTrader) enforcePositionValueRatio(positionSizeUSD float64, equity float64, symbol string) (float64, bool) {
	if at.config.StrategyConfig == nil {
		return positionSizeUSD, false
	}

	riskControl := at.config.StrategyConfig.RiskControl

	// Get the appropriate position value ratio limit
	var maxPositionValueRatio float64
	if isBTCETH(symbol) {
		maxPositionValueRatio = riskControl.BTCETHMaxPositionValueRatio
		if maxPositionValueRatio <= 0 {
			maxPositionValueRatio = 5.0 // Default: 5x for BTC/ETH
		}
	} else {
		maxPositionValueRatio = riskControl.AltcoinMaxPositionValueRatio
		if maxPositionValueRatio <= 0 {
			maxPositionValueRatio = 1.0 // Default: 1x for altcoins
		}
	}

	// Calculate max allowed position value = equity × ratio
	maxPositionValue := equity * maxPositionValueRatio

	// Check if position size exceeds limit
	if positionSizeUSD > maxPositionValue {
		logger.Infof("  ⚠️ [RISK CONTROL] Position %.2f USDT exceeds limit (equity %.2f × %.1fx = %.2f USDT max for %s), capping",
			positionSizeUSD, equity, maxPositionValueRatio, maxPositionValue, symbol)
		return maxPositionValue, true
	}

	return positionSizeUSD, false
}

// enforceMinPositionSize checks minimum position size (CODE ENFORCED)
func (at *AutoTrader) enforceMinPositionSize(positionSizeUSD float64) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	minSize := at.config.StrategyConfig.RiskControl.MinPositionSize
	if minSize <= 0 {
		minSize = 12 // Default: 12 USDT
	}

	if positionSizeUSD < minSize {
		return fmt.Errorf("❌ [RISK CONTROL] Position %.2f USDT below minimum (%.2f USDT)", positionSizeUSD, minSize)
	}
	return nil
}

// enforceMaxPositions checks maximum positions count (CODE ENFORCED)
func (at *AutoTrader) enforceMaxPositions(currentPositionCount int) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	maxPositions := at.config.StrategyConfig.RiskControl.MaxPositions
	if maxPositions <= 0 {
		maxPositions = 3 // Default: 3 positions
	}

	if currentPositionCount >= maxPositions {
		return fmt.Errorf("❌ [RISK CONTROL] Already at max positions (%d/%d)", currentPositionCount, maxPositions)
	}
	return nil
}

// getSideFromAction converts order action to side (BUY/SELL)
func getSideFromAction(action string) string {
	switch action {
	case "open_long", "close_short":
		return "BUY"
	case "open_short", "close_long":
		return "SELL"
	default:
		return "BUY"
	}
}

// checkBreakevenPromotion progressively promotes the stop-loss in the
// favorable direction as float profit grows. At most one step per
// ticker, never retreats. See docs/plans/2026-06-10 §2.
func (at *AutoTrader) checkBreakevenPromotion(positions []map[string]interface{}) {
	if at.strategyEngine == nil || at.trader == nil {
		return
	}
	rc := at.strategyEngine.GetConfig().RiskControl
	bp := rc.BreakevenProtection
	if bp == nil || !bp.Enabled {
		return
	}
	if len(positions) == 0 {
		return
	}

	// Exchanges where CancelStopLossOrders is unsafe or broken for BE:
	// - hyperliquid: cannot distinguish SL/TP (would wipe TP)
	// - lighter: same limitation
	// - aster: CancelStopLossOrders consistently fails with "API-key format invalid"
	//   (HTTP 401 / Binance code -2014), so BE promotion cannot complete safely.
	exch := strings.ToLower(at.exchange)
	if exch == "hyperliquid" || exch == "lighter" || exch == "aster" {
		return
	}

	// Phase 1: detect hedged same-symbol positions (long+short) and skip.
	// CancelStopLossOrders is symbol-level on the exchanges we support
	// (binance, aster), so cancelling could misfire on the other side.
	sidesBySymbol := make(map[string]map[string]bool, len(positions))
	for _, pos := range positions {
		sym, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if sym == "" {
			continue
		}
		if sidesBySymbol[sym] == nil {
			sidesBySymbol[sym] = make(map[string]bool)
		}
		sidesBySymbol[sym][side] = true
	}
	hedgeSkip := make(map[string]bool, len(sidesBySymbol))
	for sym, sides := range sidesBySymbol {
		if sides["long"] && sides["short"] {
			hedgeSkip[sym] = true
		}
	}

	// Epsilon keeps floating-point boundary values from missing a step
	// (e.g. currentPnLPct == exactly trigger_pct due to IEEE-754 noise)
	// without materially moving the trigger threshold earlier.
	const epsilonPctOfTrigger = 1e-6

	openOrdersBySymbol := make(map[string][]OpenOrder, len(positions))
	openOrdersErrBySymbol := make(map[string]error, len(positions))

	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if symbol == "" || side == "" {
			continue
		}
		if hedgeSkip[symbol] {
			logger.Infof("🛡️ [%s] BE skip: hedged same-symbol position %s (long+short)", at.name, symbol)
			continue
		}

		entryPrice, _ := pos["entryPrice"].(float64)
		markPrice, _ := pos["markPrice"].(float64)
		if entryPrice <= 0 || markPrice <= 0 {
			continue
		}

		leverage := 10.0
		if lev, ok := pos["leverage"].(float64); ok && lev > 0 {
			leverage = lev
		}

		// currentPnLPct in leveraged PnL% (matches DrawdownClose metric).
		var currentPnLPct float64
		if side == "long" {
			currentPnLPct = ((markPrice - entryPrice) / entryPrice) * leverage * 100
		} else {
			currentPnLPct = ((entryPrice - markPrice) / entryPrice) * leverage * 100
		}
		if currentPnLPct <= 0 {
			continue
		}

		orders, ok := openOrdersBySymbol[symbol]
		if !ok {
			var err error
			orders, err = at.trader.GetOpenOrders(symbol)
			if err != nil {
				openOrdersErrBySymbol[symbol] = err
				openOrdersBySymbol[symbol] = nil
			} else {
				openOrdersBySymbol[symbol] = orders
			}
		}
		if err := openOrdersErrBySymbol[symbol]; err != nil {
			logger.Warnf("🛡️ [%s] BE skip: failed to read active SL for %s %s: %v", at.name, symbol, side, err)
			continue
		}

		posKey := symbol + "_" + side

		// Compute step parameters.
		triggerPct := bp.TriggerPct
		if triggerPct <= 0 {
			triggerPct = 1.0
		}
		priceStep := entryPrice * (triggerPct / 100) / leverage
		if priceStep <= 0 {
			continue
		}
		// Apply epsilon so float-pct == trigger_pct exactly still triggers.
		targetSteps := int((currentPnLPct + triggerPct*epsilonPctOfTrigger) / triggerPct)
		if targetSteps <= 0 {
			continue
		}

		// Read memory step.
		at.breakevenStepsMutex.RLock()
		memorySteps := at.breakevenSteps[posKey]
		at.breakevenStepsMutex.RUnlock()

		activeSL, hasActiveSL := findActiveStopLossPrice(orders, side)
		currentSteps := memorySteps
		if hasActiveSL {
			inferredSteps := inferBreakevenStepsFromSL(side, entryPrice, priceStep, activeSL)
			if inferredSteps > currentSteps {
				currentSteps = inferredSteps
				at.breakevenStepsMutex.Lock()
				if at.breakevenSteps[posKey] < inferredSteps {
					at.breakevenSteps[posKey] = inferredSteps
				}
				at.breakevenStepsMutex.Unlock()
			}
		}
		if targetSteps <= currentSteps {
			continue
		}

		// P1.2: at most one step per ticker, per symbol/side. Catch-up
		// to targetSteps is deferred to subsequent ticks.
		nextStep := currentSteps + 1
		var newSLPrice float64
		if side == "long" {
			newSLPrice = entryPrice + float64(nextStep-1)*priceStep
		} else {
			newSLPrice = entryPrice - float64(nextStep-1)*priceStep
		}

		// Half-step safety buffer so new SL never lands on or past mark.
		minTick := priceStep * 0.5

		// Skip if new SL would cross mark (would trigger immediately).
		if side == "long" && newSLPrice >= markPrice-minTick {
			logger.Warnf("🛡️ [%s] BE skip step %d: new SL %.4f ≥ mark-minTick %.4f for %s %s",
				at.name, nextStep, newSLPrice, markPrice-minTick, symbol, side)
			continue
		}
		if side == "short" && newSLPrice <= markPrice+minTick {
			logger.Warnf("🛡️ [%s] BE skip step %d: new SL %.4f ≤ mark+minTick %.4f for %s %s",
				at.name, nextStep, newSLPrice, markPrice+minTick, symbol, side)
			continue
		}
		if hasActiveSL && !isStopLossImprovement(side, activeSL, newSLPrice) {
			logger.Warnf("🛡️ [%s] BE skip step %d: new SL %.4f would not improve active SL %.4f for %s %s",
				at.name, nextStep, newSLPrice, activeSL, symbol, side)
			continue
		}

		quantity, _ := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}

		// P1.1: SetStopLoss takes LONG/SHORT (uppercase). All supported
		// adapters derive the order direction from this string and treat
		// anything-but-"LONG" as the short branch (Binance, Hyperliquid)
		// or treat the comparison as "SHORT" (Aster). Lowercase will
		// yield the wrong order side and place the SL in the wrong
		// direction.
		adapterSide := strings.ToUpper(side)

		// P2.1: cancel must succeed before setting a new one. If cancel
		// fails, the existing SL stays in place; setting another SL on
		// top can leave duplicate protective orders on the book (some
		// exchanges would treat them as separate orders, exposing
		// double-quantity risk on trigger). Skip this tick; retry next.
		if err := at.trader.CancelStopLossOrders(symbol); err != nil {
			logger.Warnf("⚠️ [%s] BE step %d cancel SL failed for %s %s: %v (skipping this tick; will retry)",
				at.name, nextStep, symbol, side, err)
			continue
		}

		if err := at.trader.SetStopLoss(symbol, adapterSide, quantity, newSLPrice); err != nil {
			logger.Warnf("⚠️ [%s] BE step %d SetStopLoss failed for %s %s: %v (memory step NOT advanced; will retry)",
				at.name, nextStep, symbol, side, err)
			continue
		}

		logger.Infof("🛡️ [%s] BE promotion step %d: %s %s 浮盈 %.2f%% (≥ %.2f%%), SL → %.4f",
			at.name, nextStep, symbol, side, currentPnLPct, float64(nextStep)*triggerPct, newSLPrice)

		at.breakevenStepsMutex.Lock()
		at.breakevenSteps[posKey] = nextStep
		at.breakevenStepsMutex.Unlock()
	}
}

func findActiveStopLossPrice(orders []OpenOrder, side string) (float64, bool) {
	positionSide := strings.ToUpper(side)
	expectedOrderSide := "SELL"
	if positionSide == "SHORT" {
		expectedOrderSide = "BUY"
	}

	var bestPrice float64
	found := false
	for _, order := range orders {
		orderType := strings.ToUpper(order.Type)
		if !strings.Contains(orderType, "STOP") || strings.Contains(orderType, "TAKE_PROFIT") {
			continue
		}
		if order.StopPrice <= 0 {
			continue
		}

		orderSide := strings.ToUpper(order.Side)
		if orderSide != "" {
			if orderSide != expectedOrderSide {
				continue
			}
		} else {
			orderPositionSide := strings.ToUpper(order.PositionSide)
			if orderPositionSide != "" && orderPositionSide != "BOTH" && orderPositionSide != positionSide {
				continue
			}
		}

		if !found {
			bestPrice = order.StopPrice
			found = true
			continue
		}
		if positionSide == "LONG" && order.StopPrice > bestPrice {
			bestPrice = order.StopPrice
		}
		if positionSide == "SHORT" && order.StopPrice < bestPrice {
			bestPrice = order.StopPrice
		}
	}

	return bestPrice, found
}

func inferBreakevenStepsFromSL(side string, entryPrice, priceStep, stopLossPrice float64) int {
	if priceStep <= 0 {
		return 0
	}
	if side == "long" {
		if stopLossPrice < entryPrice {
			return 0
		}
		return int(math.Floor((stopLossPrice-entryPrice)/priceStep+1e-9)) + 1
	}
	if stopLossPrice > entryPrice {
		return 0
	}
	return int(math.Floor((entryPrice-stopLossPrice)/priceStep+1e-9)) + 1
}

func isStopLossImprovement(side string, oldStopLossPrice, newStopLossPrice float64) bool {
	if side == "long" {
		return newStopLossPrice > oldStopLossPrice
	}
	return newStopLossPrice < oldStopLossPrice
}
