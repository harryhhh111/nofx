package trader

import (
	"context"
	"fmt"
	"math"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

// executeDecisionWithRecord executes AI decision and records detailed information
func (at *AutoTrader) executeDecisionWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "hold", "wait":
		// No execution needed, just record
		return nil
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// protectiveOrderRetryDelay is the wait between the first failed protective-order
// attempt and the single retry.
const protectiveOrderRetryDelay = 500 * time.Millisecond

func (at *AutoTrader) requiredExecutionPrice(symbol string) (float64, error) {
	price, err := at.executionMarketPriceWithRetry(symbol)
	if err == nil && price > 0 {
		return price, nil
	}
	logger.Warnf("  ⚠️  Failed to get pinned ticker price for %s, falling back to pinned kline price: %v", symbol, err)

	klines, _, fallbackErr := market.GetPublicKlines(context.Background(), at.exchange, symbol, "1m", 1, false)
	if fallbackErr == nil && len(klines) > 0 && klines[len(klines)-1].Close > 0 {
		return klines[len(klines)-1].Close, nil
	}
	return 0, fmt.Errorf("failed to get execution price for %s: ticker=%v; kline=%v", symbol, err, fallbackErr)
}

func (at *AutoTrader) executionMarketPriceWithRetry(symbol string) (float64, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		price, err := at.pinnedExecutionMarketPrice(symbol)
		if err == nil && price > 0 {
			return price, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("non-positive market price %.8f", price)
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
		}
	}
	return 0, lastErr
}

func (at *AutoTrader) pinnedExecutionMarketPrice(symbol string) (float64, error) {
	price, _, err := market.GetPublicTickerPrice(context.Background(), at.exchange, symbol, false)
	if err == nil && price > 0 {
		return price, nil
	}

	traderPrice, traderErr := at.trader.GetMarketPrice(symbol)
	if traderErr == nil && traderPrice > 0 {
		return traderPrice, nil
	}
	if err != nil {
		return 0, fmt.Errorf("public pinned ticker failed: %v; trader ticker failed: %w", err, traderErr)
	}
	return 0, traderErr
}

func (at *AutoTrader) optionalExecutionPrice(symbol string) float64 {
	price, err := at.requiredExecutionPrice(symbol)
	if err != nil {
		logger.Warnf("  ⚠️  Failed to get reference price for %s; continuing close order without price: %v", symbol, err)
		return 0
	}
	return price
}

func (at *AutoTrader) freshOpenPositionSize(decision *kernel.Decision, currentPrice, equity float64) (float64, error) {
	if decision == nil || (decision.Action != "open_long" && decision.Action != "open_short") {
		return 0, fmt.Errorf("fresh execution risk requires an open decision")
	}
	if currentPrice <= 0 || equity <= 0 || decision.StopLoss <= 0 || decision.TakeProfit <= 0 {
		return 0, fmt.Errorf("fresh execution risk has invalid price, equity, or protective levels")
	}

	structuralStop := decision.StopLossAnchor
	if structuralStop <= 0 {
		return 0, fmt.Errorf("fresh execution risk requires a structural stop anchor")
	}
	var structuralRisk, reward float64
	if decision.Action == "open_long" {
		if decision.StopLoss >= currentPrice || structuralStop >= currentPrice || decision.TakeProfit <= currentPrice {
			return 0, fmt.Errorf("long signal is stale at execution price %.8f (SL %.8f, anchor %.8f, TP %.8f)", currentPrice, decision.StopLoss, structuralStop, decision.TakeProfit)
		}
		structuralRisk = currentPrice - structuralStop
		reward = decision.TakeProfit - currentPrice
	} else {
		if decision.StopLoss <= currentPrice || structuralStop <= currentPrice || decision.TakeProfit >= currentPrice {
			return 0, fmt.Errorf("short signal is stale at execution price %.8f (SL %.8f, anchor %.8f, TP %.8f)", currentPrice, decision.StopLoss, structuralStop, decision.TakeProfit)
		}
		structuralRisk = structuralStop - currentPrice
		reward = currentPrice - decision.TakeProfit
	}

	minRR := store.DefaultMinRiskRewardRatio
	riskPct := store.DefaultRiskPerTradePct
	if at != nil && at.config.StrategyConfig != nil {
		risk := at.config.StrategyConfig.RiskControl
		if risk.MinRiskRewardRatio > 0 {
			minRR = risk.MinRiskRewardRatio
		}
		if risk.RiskPerTradePct > 0 {
			riskPct = risk.RiskPerTradePct
		}
	}
	structuralRR := reward / structuralRisk
	if structuralRisk <= 0 || structuralRR < minRR {
		return 0, fmt.Errorf("signal is stale at execution: structural risk/reward %.2f is below %.2f", structuralRR, minRR)
	}

	executionRiskRatio := math.Abs(currentPrice-decision.StopLoss) / currentPrice
	if executionRiskRatio <= 0 {
		return 0, fmt.Errorf("execution stop distance is not positive")
	}
	maxRiskSizedNotional := equity * riskPct / 100 / executionRiskRatio
	positionSize := decision.PositionSizeUSD
	if positionSize <= 0 {
		return 0, fmt.Errorf("position size must be positive")
	}
	if positionSize > maxRiskSizedNotional {
		logger.Infof("  ⚠️ Fresh execution risk reduced position %.2f -> %.2f USDT at price %.8f", positionSize, maxRiskSizedNotional, currentPrice)
		positionSize = maxRiskSizedNotional
	}
	return positionSize, nil
}

// placeProtectiveOrders attaches stop-loss and take-profit orders to a freshly
// opened position.
//
// Stop-loss is mandatory: a position without a working stop-loss is a naked,
// unbounded-risk position. If the stop-loss cannot be placed even after one
// retry (or the stop price is invalid), the position is rolled back (closed at
// market) and an error is returned so the caller treats the open as failed.
//
// Take-profit is best-effort: a missing take-profit does not endanger capital,
// so a failure here only logs a warning and keeps the (stop-protected) position.
//
// side must be "LONG" or "SHORT".
func (at *AutoTrader) placeProtectiveOrders(decision *kernel.Decision, side string, quantity float64) error {
	sideLower := "long"
	if side == "SHORT" {
		sideLower = "short"
	}

	rollback := func(cause error) error {
		logger.Warnf("  🚨 No stop-loss protection for %s %s; rolling back position to avoid a naked position", decision.Symbol, side)
		if rbErr := at.emergencyClosePosition(decision.Symbol, sideLower); rbErr != nil {
			return fmt.Errorf("stop loss failed for %s and rollback failed: stop_loss_err=[%v] rollback_err=%w", decision.Symbol, cause, rbErr)
		}
		return fmt.Errorf("stop loss failed for %s, position rolled back: %w", decision.Symbol, cause)
	}

	if decision.StopLoss <= 0 {
		return rollback(fmt.Errorf("invalid stop loss price %.8f", decision.StopLoss))
	}

	slErr := at.trader.SetStopLoss(decision.Symbol, side, quantity, decision.StopLoss)
	if slErr != nil {
		logger.Warnf("  ⚠ Failed to set stop loss for %s (%v), retrying once...", decision.Symbol, slErr)
		time.Sleep(protectiveOrderRetryDelay)
		slErr = at.trader.SetStopLoss(decision.Symbol, side, quantity, decision.StopLoss)
	}
	if slErr != nil {
		return rollback(slErr)
	}
	at.recordProtectiveOrder(decision.Symbol, side, "STOP_MARKET", "stop_loss", quantity, decision.StopLoss)

	// Take-profit is non-fatal.
	var tpErr error
	if decision.TakeProfit > 0 {
		if tpErr = at.trader.SetTakeProfit(decision.Symbol, side, quantity, decision.TakeProfit); tpErr != nil {
			logger.Warnf("  ⚠ Failed to set take profit for %s (%v), retrying once...", decision.Symbol, tpErr)
			time.Sleep(protectiveOrderRetryDelay)
			if tpErr = at.trader.SetTakeProfit(decision.Symbol, side, quantity, decision.TakeProfit); tpErr != nil {
				logger.Warnf("  ⚠ Take profit not set for %s after retry (%v); position retains stop-loss protection", decision.Symbol, tpErr)
			}
		}
	}

	if decision.TakeProfit > 0 && tpErr == nil {
		at.recordProtectiveOrder(decision.Symbol, side, "TAKE_PROFIT_MARKET", "take_profit", quantity, decision.TakeProfit)
	}

	return nil
}

func (at *AutoTrader) recordProtectiveOrder(symbol, positionSide, orderType, orderAction string, quantity, stopPrice float64) {
	if at.store == nil || stopPrice <= 0 || quantity <= 0 {
		return
	}
	openOrders, err := at.trader.GetOpenOrders(symbol)
	if err != nil {
		logger.Warnf("  ⚠️ Failed to query protective order for local backup: %v", err)
		return
	}

	normalizedSymbol := market.Normalize(symbol)
	var matchedID string
	var matchedSide string
	for _, order := range openOrders {
		if market.Normalize(order.Symbol) != normalizedSymbol {
			continue
		}
		if !strings.EqualFold(order.PositionSide, positionSide) {
			continue
		}
		if !strings.EqualFold(order.Type, orderType) {
			continue
		}
		if !closeFloat(order.StopPrice, stopPrice) {
			continue
		}
		if order.Quantity > 0 && !closeFloat(order.Quantity, quantity) {
			continue
		}
		matchedID = strings.TrimSpace(order.OrderID)
		matchedSide = strings.ToUpper(order.Side)
		break
	}
	if matchedID == "" {
		logger.Warnf("  ⚠️ Protective order placed but no matching open order found for backup: %s %s %s @ %.8f", normalizedSymbol, positionSide, orderType, stopPrice)
		return
	}
	if existing, err := at.store.Order().GetOrderByTraderAndExchangeOrderID(at.id, matchedID); err == nil && existing != nil {
		return
	}

	if matchedSide == "" {
		matchedSide = "SELL"
		if strings.EqualFold(positionSide, "SHORT") {
			matchedSide = "BUY"
		}
	}

	var relatedPositionID int64
	if pos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, strings.ToUpper(positionSide)); err == nil && pos != nil {
		relatedPositionID = pos.ID
	}

	nowMs := time.Now().UTC().UnixMilli()
	orderRecord := &store.TraderOrder{
		TraderID:          at.id,
		ExchangeID:        at.exchangeID,
		ExchangeType:      at.exchange,
		ExchangeOrderID:   matchedID,
		Symbol:            normalizedSymbol,
		Side:              matchedSide,
		PositionSide:      strings.ToUpper(positionSide),
		Type:              strings.ToUpper(orderType),
		TimeInForce:       "GTC",
		Quantity:          quantity,
		Price:             0,
		StopPrice:         stopPrice,
		Status:            "NEW",
		CommissionAsset:   "USDT",
		ReduceOnly:        true,
		ClosePosition:     true,
		WorkingType:       "CONTRACT_PRICE",
		OrderAction:       orderAction,
		RelatedPositionID: relatedPositionID,
		CreatedAt:         nowMs,
		UpdatedAt:         nowMs,
	}
	if err := at.store.Order().CreateOrder(orderRecord); err != nil {
		logger.Warnf("  ⚠️ Failed to record protective order backup: %v", err)
		return
	}
	logger.Infof("  🛡️ Protective order recorded: %s %s %s @ %.8f", normalizedSymbol, positionSide, orderAction, stopPrice)
}

func closeFloat(a, b float64) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	return math.Abs(a-b) <= math.Max(math.Abs(a), math.Abs(b))*1e-8
}

// executeOpenLongWithRecord executes open long position and records detailed information
func (at *AutoTrader) executeOpenLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📈 Open long: %s", decision.Symbol)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if _, ok := matchingOpenPositionQuantity(pos, market.Normalize(decision.Symbol), "long"); ok {
			return fmt.Errorf("❌ %s already has long position, close it first", decision.Symbol)
		}
	}

	// [CODE ENFORCED] Defensive leverage re-check (kernel risk gate already validates)
	if err := at.enforceLeverage(decision.Leverage, decision.Symbol); err != nil {
		return err
	}

	// Get execution price from the exchange ticker/mark price. Klines are only a fallback.
	currentPrice, err := at.requiredExecutionPrice(decision.Symbol)
	if err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance // Fallback to available balance
	}

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	freshRiskSize, err := at.freshOpenPositionSize(decision, currentPrice, equity)
	if err != nil {
		return err
	}
	decision.PositionSizeUSD = freshRiskSize

	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(decision.Leverage) + 0.001
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		// Use 98% of max to leave buffer for price fluctuation
		adjustedSize := maxAffordablePositionSize * 0.98
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD, decision.Symbol); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / currentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = currentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	executionAnalyticsID := at.startExecutionAnalytics(decision, "open_long", currentPrice, quantity)
	order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		at.markExecutionFailed(executionAnalyticsID, err)
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_long", quantity, currentPrice, decision.Leverage, 0, executionAnalyticsID)

	// Record position opening time
	posKey := decision.Symbol + "_long"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Set protective orders: stop-loss is mandatory (rollback on failure to avoid
	// a naked position), take-profit is best-effort.
	if err := at.placeProtectiveOrders(decision, "LONG", quantity); err != nil {
		return err
	}

	return nil
}

// executeOpenShortWithRecord executes open short position and records detailed information
func (at *AutoTrader) executeOpenShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📉 Open short: %s", decision.Symbol)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if _, ok := matchingOpenPositionQuantity(pos, market.Normalize(decision.Symbol), "short"); ok {
			return fmt.Errorf("❌ %s already has short position, close it first", decision.Symbol)
		}
	}

	// [CODE ENFORCED] Defensive leverage re-check (kernel risk gate already validates)
	if err := at.enforceLeverage(decision.Leverage, decision.Symbol); err != nil {
		return err
	}

	// Get execution price from the exchange ticker/mark price. Klines are only a fallback.
	currentPrice, err := at.requiredExecutionPrice(decision.Symbol)
	if err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance // Fallback to available balance
	}

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	freshRiskSize, err := at.freshOpenPositionSize(decision, currentPrice, equity)
	if err != nil {
		return err
	}
	decision.PositionSizeUSD = freshRiskSize

	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(decision.Leverage) + 0.001
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		// Use 98% of max to leave buffer for price fluctuation
		adjustedSize := maxAffordablePositionSize * 0.98
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD, decision.Symbol); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / currentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = currentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	executionAnalyticsID := at.startExecutionAnalytics(decision, "open_short", currentPrice, quantity)
	order, err := at.trader.OpenShort(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		at.markExecutionFailed(executionAnalyticsID, err)
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_short", quantity, currentPrice, decision.Leverage, 0, executionAnalyticsID)

	// Record position opening time
	posKey := decision.Symbol + "_short"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Set protective orders: stop-loss is mandatory (rollback on failure to avoid
	// a naked position), take-profit is best-effort.
	if err := at.placeProtectiveOrders(decision, "SHORT", quantity); err != nil {
		return err
	}

	return nil
}

// executeCloseLongWithRecord executes close long position and records detailed information
func (at *AutoTrader) executeCloseLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close long: %s", decision.Symbol)

	// Reference price is only for records/analytics. It must not block a market close.
	currentPrice := at.optionalExecutionPrice(decision.Symbol)
	actionRecord.Price = currentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "LONG"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok && amt > 0 {
						quantity = amt
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position
	executionAnalyticsID := at.startExecutionAnalytics(decision, "close_long", currentPrice, quantity)
	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = close all
	if err != nil {
		at.markExecutionFailed(executionAnalyticsID, err)
		return err
	}

	if at.store != nil {
		oid := store.FormatExchangeOrderIDFromMap(order)
		if err := at.store.Position().SetPendingCloseReason(at.id, normalizedSymbol, "LONG", "ai", oid); err != nil {
			logger.Warnf("SetPendingCloseReason(ai) failed trader=%s symbol=%s side=LONG: %v", at.id, normalizedSymbol, err)
		}
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_long", quantity, currentPrice, 0, entryPrice, executionAnalyticsID)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// executeCloseShortWithRecord executes close short position and records detailed information
func (at *AutoTrader) executeCloseShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close short: %s", decision.Symbol)

	// Reference price is only for records/analytics. It must not block a market close.
	currentPrice := at.optionalExecutionPrice(decision.Symbol)
	actionRecord.Price = currentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "SHORT"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok {
						quantity = -amt // positionAmt is negative for short
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Close position
	executionAnalyticsID := at.startExecutionAnalytics(decision, "close_short", currentPrice, quantity)
	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = close all
	if err != nil {
		at.markExecutionFailed(executionAnalyticsID, err)
		return err
	}

	if at.store != nil {
		oid := store.FormatExchangeOrderIDFromMap(order)
		if err := at.store.Position().SetPendingCloseReason(at.id, normalizedSymbol, "SHORT", "ai", oid); err != nil {
			logger.Warnf("SetPendingCloseReason(ai) failed trader=%s symbol=%s side=SHORT: %v", at.id, normalizedSymbol, err)
		}
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_short", quantity, currentPrice, 0, entryPrice, executionAnalyticsID)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}
