package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
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

	// Take-profit is non-fatal.
	if decision.TakeProfit > 0 {
		if tpErr := at.trader.SetTakeProfit(decision.Symbol, side, quantity, decision.TakeProfit); tpErr != nil {
			logger.Warnf("  ⚠ Failed to set take profit for %s (%v), retrying once...", decision.Symbol, tpErr)
			time.Sleep(protectiveOrderRetryDelay)
			if tpErr = at.trader.SetTakeProfit(decision.Symbol, side, quantity, decision.TakeProfit); tpErr != nil {
				logger.Warnf("  ⚠ Take profit not set for %s after retry (%v); position retains stop-loss protection", decision.Symbol, tpErr)
			}
		}
	}

	return nil
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
		if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
			return fmt.Errorf("❌ %s already has long position, close it first", decision.Symbol)
		}
	}

	// [CODE ENFORCED] Defensive leverage re-check (kernel risk gate already validates)
	if err := at.enforceLeverage(decision.Leverage, decision.Symbol); err != nil {
		return err
	}

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
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
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	executionAnalyticsID := at.startExecutionAnalytics(decision, "open_long", marketData.CurrentPrice, quantity)
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
	at.recordAndConfirmOrder(order, decision.Symbol, "open_long", quantity, marketData.CurrentPrice, decision.Leverage, 0, executionAnalyticsID)

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
		if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
			return fmt.Errorf("❌ %s already has short position, close it first", decision.Symbol)
		}
	}

	// [CODE ENFORCED] Defensive leverage re-check (kernel risk gate already validates)
	if err := at.enforceLeverage(decision.Leverage, decision.Symbol); err != nil {
		return err
	}

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
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
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	executionAnalyticsID := at.startExecutionAnalytics(decision, "open_short", marketData.CurrentPrice, quantity)
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
	at.recordAndConfirmOrder(order, decision.Symbol, "open_short", quantity, marketData.CurrentPrice, decision.Leverage, 0, executionAnalyticsID)

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

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

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
	executionAnalyticsID := at.startExecutionAnalytics(decision, "close_long", marketData.CurrentPrice, quantity)
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
	at.recordAndConfirmOrder(order, decision.Symbol, "close_long", quantity, marketData.CurrentPrice, 0, entryPrice, executionAnalyticsID)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// executeCloseShortWithRecord executes close short position and records detailed information
func (at *AutoTrader) executeCloseShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close short: %s", decision.Symbol)

	// Get current price
	marketData, err := market.GetWithExchange(decision.Symbol, at.exchange)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

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
	executionAnalyticsID := at.startExecutionAnalytics(decision, "close_short", marketData.CurrentPrice, quantity)
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
	at.recordAndConfirmOrder(order, decision.Symbol, "close_short", quantity, marketData.CurrentPrice, 0, entryPrice, executionAnalyticsID)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}
