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

type realtimePositionFeeProvider interface {
	GetPositionTradingFees(symbol, side string, startTime time.Time) (float64, error)
	GetCommissionRates(symbol string) (maker, taker float64, err error)
}

type positionFeeSnapshot struct {
	AccumulatedFee    float64
	EstimatedCloseFee float64
	Source            string
}

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
	activePositionKeys := make(map[string]bool, len(positions))

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		posKey := symbol + "_" + side
		activePositionKeys[posKey] = true
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

		var grossPnLPct float64
		if side == "long" {
			grossPnLPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			grossPnLPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}
		unrealizedPnl := 0.0
		if v, ok := pos["unRealizedProfit"].(float64); ok {
			unrealizedPnl = v
		} else if side == "long" {
			unrealizedPnl = (markPrice - entryPrice) * quantity
		} else {
			unrealizedPnl = (entryPrice - markPrice) * quantity
		}
		at.recordPositionExcursion(symbol, side, markPrice, unrealizedPnl, grossPnLPct)

		var dbPosition *store.TraderPosition
		if at.store != nil {
			dbPosition, _ = at.store.Position().GetOpenPositionBySymbol(at.id, market.Normalize(symbol), strings.ToUpper(side))
		}
		entryTimeMs := int64(0)
		if dbPosition != nil {
			entryTimeMs = dbPosition.EntryTime
		}
		if entryTimeMs <= 0 {
			if createdTime, ok := pos["createdTime"].(int64); ok {
				entryTimeMs = createdTime
			} else if createdTime, ok := pos["createdTime"].(float64); ok {
				entryTimeMs = int64(createdTime)
			}
		}
		at.resetPeakPnLForPosition(posKey, entryTimeMs)
		fees, feeErr := at.currentPositionFees(symbol, side, quantity*markPrice, entryTimeMs, dbPosition)
		if feeErr != nil {
			logger.Warnf("⚠️ Drawdown monitoring fee lookup for %s %s: %v", symbol, side, feeErr)
		}
		netPnL := unrealizedPnl - fees.AccumulatedFee - fees.EstimatedCloseFee
		marginBasis := entryPrice * quantity / float64(leverage)
		currentPnLPct := grossPnLPct
		if marginBasis > 0 {
			currentPnLPct = netPnL / marginBasis * 100
		}

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

		drawdownPct := profitProtectionDrawdownPct(peakPnLPct, currentPnLPct)

		if profitProtectionTriggered(peakPnLPct, currentPnLPct, minProfitPct, triggerPct) {
			logger.Infof("🚨 Profit protection triggered: %s %s | Net profit: %.2f%% (%.2f USDT) | Fees: %.4f+%.4f | Peak: %.2f%% | Drawdown: %.2f%% | mode=%s",
				symbol, side, currentPnLPct, netPnL, fees.AccumulatedFee, fees.EstimatedCloseFee, peakPnLPct, drawdownPct, map[bool]string{true: "ai-decide", false: "auto-close"}[useAI])

			if useAI {
				// AI-decide mode: queue an alert into the next AI cycle instead of closing immediately
				normalizedSymbol := market.Normalize(symbol)
				openingReason := ""
				if dbPosition != nil {
					openingReason = dbPosition.OpeningReasoning
				}
				alert := kernel.DrawdownAlert{
					Symbol:            normalizedSymbol,
					Side:              side,
					CurrentPnLPct:     currentPnLPct,
					PeakPnLPct:        peakPnLPct,
					DrawdownPct:       drawdownPct,
					CurrentNetPnL:     netPnL,
					AccumulatedFee:    fees.AccumulatedFee,
					EstimatedCloseFee: fees.EstimatedCloseFee,
					FeeSource:         fees.Source,
					OpeningReason:     openingReason,
					ObservedAt:        time.Now().UTC().UnixMilli(),
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
					at.saveRiskCloseDecision(symbol, side, currentPnLPct, peakPnLPct, drawdownPct, triggerPct)
				}
			}
		} else if peakPnLPct >= minProfitPct {
			logger.Infof("📊 Profit protection armed: %s %s | Net profit: %.2f%% | Peak: %.2f%% | Drawdown: %.2f%%",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct)
		}
	}
	at.clearInactivePeakPnLCache(activePositionKeys)
}

func (at *AutoTrader) currentPositionFees(symbol, side string, positionNotional float64, entryTimeMs int64, dbPosition *store.TraderPosition) (positionFeeSnapshot, error) {
	snapshot := positionFeeSnapshot{}
	accumulatedSource := "estimated"
	closeSource := "estimated_rate"
	if dbPosition != nil {
		snapshot.AccumulatedFee = math.Abs(dbPosition.Fee)
		accumulatedSource = "database"
	}
	var lookupErrors []string
	if provider, ok := at.trader.(realtimePositionFeeProvider); ok {
		if entryTime := unixTimeFromStoredTimestamp(entryTimeMs); !entryTime.IsZero() {
			actualFee, err := provider.GetPositionTradingFees(symbol, side, entryTime)
			if err != nil {
				lookupErrors = append(lookupErrors, err.Error())
			} else {
				snapshot.AccumulatedFee = math.Abs(actualFee)
				accumulatedSource = "exchange_trades"
			}
		}
		_, takerRate, err := provider.GetCommissionRates(symbol)
		if err != nil {
			lookupErrors = append(lookupErrors, err.Error())
		} else if takerRate > 0 {
			snapshot.EstimatedCloseFee = positionNotional * takerRate
			closeSource = "exchange_rate"
		}
	}
	if snapshot.EstimatedCloseFee <= 0 {
		snapshot.EstimatedCloseFee = estimateCloseFee(positionNotional, snapshot.AccumulatedFee, dbPosition)
	}
	snapshot.Source = accumulatedSource + "_and_" + closeSource
	if len(lookupErrors) > 0 {
		return snapshot, fmt.Errorf("%s; using %s fee fallback", strings.Join(lookupErrors, "; "), snapshot.Source)
	}
	return snapshot, nil
}

func unixTimeFromStoredTimestamp(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value >= 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

func (at *AutoTrader) acknowledgeDrawdownAlerts(processed []kernel.DrawdownAlert) {
	if len(processed) == 0 {
		return
	}
	processedAt := make(map[string]int64, len(processed))
	for _, alert := range processed {
		symbol := market.Normalize(alert.Symbol)
		side := normalizeProtectivePositionSide(alert.Side)
		if symbol == "" || (side != "LONG" && side != "SHORT") {
			continue
		}
		key := symbol + "_" + side
		if alert.ObservedAt > processedAt[key] {
			processedAt[key] = alert.ObservedAt
		}
	}

	at.pendingDrawdownAlertsMu.Lock()
	defer at.pendingDrawdownAlertsMu.Unlock()
	retained := at.pendingDrawdownAlerts[:0]
	for _, alert := range at.pendingDrawdownAlerts {
		symbol := market.Normalize(alert.Symbol)
		side := normalizeProtectivePositionSide(alert.Side)
		key := symbol + "_" + side
		observedAt, ok := processedAt[key]
		if !ok || alert.ObservedAt > observedAt {
			retained = append(retained, alert)
		}
	}
	at.pendingDrawdownAlerts = retained
}

func profitProtectionTriggered(peakPnLPct, currentPnLPct, activationPct, triggerPct float64) bool {
	if peakPnLPct < activationPct {
		return false
	}
	return profitProtectionDrawdownPct(peakPnLPct, currentPnLPct) >= triggerPct
}

func profitProtectionDrawdownPct(peakPnLPct, currentPnLPct float64) float64 {
	if peakPnLPct <= 0 || currentPnLPct >= peakPnLPct {
		return 0
	}
	return ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
}

func (at *AutoTrader) recordPositionExcursion(symbol, side string, markPrice, unrealizedPnL, unrealizedPnLPct float64) {
	if at == nil || at.store == nil {
		return
	}
	normalizedSymbol := market.Normalize(symbol)
	normalizedSide := strings.ToUpper(side)
	if normalizedSymbol == "" || normalizedSide == "" {
		return
	}
	if err := at.store.Position().UpdatePositionExcursion(
		at.id,
		normalizedSymbol,
		normalizedSide,
		markPrice,
		unrealizedPnL,
		unrealizedPnLPct,
		time.Now().UTC().UnixMilli(),
	); err != nil {
		logger.Warnf("Update position excursion failed trader=%s symbol=%s side=%s: %v", at.id, normalizedSymbol, normalizedSide, err)
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
		at.recordAndConfirmOrder(order, symbol, "close_long", 0, 0, 0, 0, 0)
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
		at.recordAndConfirmOrder(order, symbol, "close_short", 0, 0, 0, 0, 0)
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

func (at *AutoTrader) resetPeakPnLForPosition(posKey string, entryTimeMs int64) {
	if posKey == "" || entryTimeMs <= 0 {
		return
	}
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()
	if previous := at.peakPnLPositionEntry[posKey]; previous > 0 && previous != entryTimeMs {
		delete(at.peakPnLCache, posKey)
	}
	at.peakPnLPositionEntry[posKey] = entryTimeMs
}

func (at *AutoTrader) clearInactivePeakPnLCache(active map[string]bool) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()
	for key := range at.peakPnLPositionEntry {
		if !active[key] {
			delete(at.peakPnLCache, key)
			delete(at.peakPnLPositionEntry, key)
		}
	}
}

// ClearPeakPnLCache clears peak cache for specified position
func (at *AutoTrader) ClearPeakPnLCache(symbol, side string) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	delete(at.peakPnLCache, posKey)
	delete(at.peakPnLPositionEntry, posKey)
}

// saveRiskCloseDecision saves a decision record for a risk-monitor-triggered position close.
// This makes the close visible in the frontend decision card history.
func (at *AutoTrader) saveRiskCloseDecision(symbol, side string, currentPnLPct, peakPnLPct, drawdownPct, triggerPct float64) {
	if at.store == nil {
		return
	}

	action := "close_long"
	if side == "short" {
		action = "close_short"
	}
	normalizedSymbol := market.Normalize(symbol)
	reasoning := fmt.Sprintf(
		"[持仓保护自动平仓] %s %s — 当前收益 %.2f%%，峰值收益 %.2f%%，回撤幅度 %.2f%%，达到 %.2f%% 保护阈值，触发强制平仓。",
		normalizedSymbol, strings.ToUpper(side), currentPnLPct, peakPnLPct, drawdownPct, triggerPct,
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
		ExecutionLog: []string{fmt.Sprintf("✅ 持仓保护平仓: %s %s", normalizedSymbol, strings.ToUpper(side))},
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
func (at *AutoTrader) enforceMinPositionSize(positionSizeUSD float64, symbol string) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	minSize := at.config.StrategyConfig.RiskControl.MinPositionSize
	if minSize <= 0 {
		minSize = 12 // Default: 12 USDT
	}
	if isBTCETH(symbol) && minSize < 60 {
		minSize = 60
	}

	if positionSizeUSD < minSize {
		return fmt.Errorf("❌ [RISK CONTROL] Position %.2f USDT below minimum (%.2f USDT)", positionSizeUSD, minSize)
	}
	return nil
}

// enforceLeverage is a defensive re-check that the requested leverage does not
// exceed the configured exchange leverage limit for the symbol class. The kernel
// risk gate already validates leverage; this guards against execution-layer drift
// or any path that bypasses the kernel. Returns an error (rejecting the open) on
// violation rather than silently capping, since a capped leverage would change
// the intended margin/sizing geometry.
func (at *AutoTrader) enforceLeverage(leverage int, symbol string) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	rc := at.config.StrategyConfig.RiskControl
	var maxLeverage int
	if isBTCETH(symbol) {
		maxLeverage = rc.BTCETHMaxLeverage
	} else {
		maxLeverage = rc.AltcoinMaxLeverage
	}
	if maxLeverage <= 0 {
		maxLeverage = 5 // Default matches store.RiskControlConfig defaults
	}

	if leverage > maxLeverage {
		return fmt.Errorf("❌ [RISK CONTROL] Leverage %dx exceeds limit (%dx) for %s", leverage, maxLeverage, symbol)
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
