package paper

import (
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/trader/types"
	"sync"
	"time"
)

const (
	defaultFeeRate = 0.0005 // 0.05% taker fee (standard futures rate)
	paperSource    = "paper"
	paperSideOpen  = "OPEN"
)

// PaperTrader implements the trader.Trader interface for paper (simulated) trading.
// All orders are virtual - no exchange API calls are made.
// Positions and orders are persisted to the database using source="paper", enabling
// full restart-safety: virtual cash is recomputed from DB records on every call.
type PaperTrader struct {
	traderID       string
	initialBalance float64
	feeRate        float64

	mu          sync.RWMutex
	leverageMap map[string]int     // symbol -> last set leverage (in-memory hint)
	sl          map[string]float64 // key=symbol+"_"+side -> stop price
	tp          map[string]float64 // key=symbol+"_"+side -> take profit price

	store *store.Store
}

// NewPaperTrader creates a PaperTrader with the given trader ID, initial balance, and store.
// The initial balance is stored in config; the paper trader recomputes available cash
// from DB records on every GetBalance() call, ensuring restart safety.
func NewPaperTrader(traderID string, initialBalance float64, st *store.Store) *PaperTrader {
	logger.Infof("[Paper] Initialized paper trader %s with initial balance %.2f USDT (fee rate %.4f%%)",
		traderID, initialBalance, defaultFeeRate*100)
	return &PaperTrader{
		traderID:       traderID,
		initialBalance: initialBalance,
		feeRate:        defaultFeeRate,
		leverageMap:    make(map[string]int),
		sl:             make(map[string]float64),
		tp:             make(map[string]float64),
		store:          st,
	}
}

// ============================================================================
// Trader Interface Implementation
// ============================================================================

// GetBalance returns the virtual account balance.
// Cash is recomputed from DB records every call (restart-safe).
// Formula: cash = initialBalance + Σ(closed.realized_pnl) - Σ(all.fee) - Σ(open.margin)
func (p *PaperTrader) GetBalance() (map[string]interface{}, error) {
	cash, lockedMargin, err := p.store.Position().GetPaperBalance(p.traderID, p.initialBalance)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to compute balance: %w", err)
	}

	// Compute unrealized PnL from open positions
	openPositions, err := p.store.Position().GetOpenPositionsBySource(p.traderID, paperSource)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to get open positions: %w", err)
	}

	unrealizedPnL := 0.0
	for _, pos := range openPositions {
		currentPrice, err := market.GetCurrentPrice(pos.Symbol)
		if err != nil {
			logger.Warnf("[Paper] Failed to get price for %s: %v", pos.Symbol, err)
			continue
		}
		if pos.Side == "LONG" {
			unrealizedPnL += (currentPrice - pos.EntryPrice) * pos.Quantity
		} else {
			unrealizedPnL += (pos.EntryPrice - currentPrice) * pos.Quantity
		}
	}

	totalEquity := cash + lockedMargin + unrealizedPnL

	return map[string]interface{}{
		"total_equity":      totalEquity,
		"available_balance": cash,
		"unrealized_pnl":    unrealizedPnL,
		"margin_used":       lockedMargin,
		"source":            paperSource,
	}, nil
}

// GetPositions returns all open paper positions with live mark-to-market prices.
func (p *PaperTrader) GetPositions() ([]map[string]interface{}, error) {
	openPositions, err := p.store.Position().GetOpenPositionsBySource(p.traderID, paperSource)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to get positions: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(openPositions))
	for _, pos := range openPositions {
		currentPrice, err := market.GetCurrentPrice(pos.Symbol)
		if err != nil {
			logger.Warnf("[Paper] Failed to get price for %s: %v", pos.Symbol, err)
			currentPrice = pos.EntryPrice
		}

		var unrealizedPnL float64
		if pos.Side == "LONG" {
			unrealizedPnL = (currentPrice - pos.EntryPrice) * pos.Quantity
		} else {
			unrealizedPnL = (pos.EntryPrice - currentPrice) * pos.Quantity
		}

		var pnlPct float64
		margin := 0.0
		if pos.Leverage > 0 && pos.EntryPrice > 0 {
			margin = pos.EntryPrice * pos.Quantity / float64(pos.Leverage)
			if margin > 0 {
				pnlPct = unrealizedPnL / margin * 100
			}
		}

		result = append(result, map[string]interface{}{
			"symbol":          pos.Symbol,
			"side":            pos.Side,
			"positionSide":    pos.Side,
			"entryPrice":      pos.EntryPrice,
			"markPrice":       currentPrice,
			"positionAmt":     pos.Quantity,
			"unrealizedProfit": unrealizedPnL,
			"pnlPct":          pnlPct,
			"leverage":        pos.Leverage,
			"marginUsed":      margin,
			"source":          paperSource,
		})
	}

	return result, nil
}

// OpenLong opens a paper long position.
func (p *PaperTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return p.openPosition(symbol, "LONG", quantity, leverage)
}

// OpenShort opens a paper short position.
func (p *PaperTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return p.openPosition(symbol, "SHORT", quantity, leverage)
}

// openPosition handles the common logic for opening paper positions.
func (p *PaperTrader) openPosition(symbol, side string, quantity float64, leverage int) (map[string]interface{}, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("[Paper] quantity must be positive, got %f", quantity)
	}
	if leverage <= 0 {
		leverage = 1
	}

	currentPrice, err := market.GetCurrentPrice(symbol)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to get market price for %s: %w", symbol, err)
	}

	notional := currentPrice * quantity
	margin := notional / float64(leverage)
	fee := notional * p.feeRate

	// Check available cash (restart-safe computation)
	cash, _, err := p.store.Position().GetPaperBalance(p.traderID, p.initialBalance)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to compute available cash: %w", err)
	}
	if cash < margin+fee {
		return nil, fmt.Errorf("[Paper] insufficient balance: need %.4f USDT (margin=%.4f + fee=%.4f), available=%.4f",
			margin+fee, margin, fee, cash)
	}

	nowMs := time.Now().UTC().UnixMilli()
	pos := &store.TraderPosition{
		TraderID:           p.traderID,
		ExchangeID:         "paper-" + p.traderID,
		ExchangeType:       "paper",
		ExchangePositionID: fmt.Sprintf("paper-%s-%s-%d", symbol, side, nowMs),
		Symbol:             symbol,
		Side:               side,
		EntryQuantity:      quantity,
		Quantity:           quantity,
		EntryPrice:         currentPrice,
		EntryTime:          nowMs,
		Fee:                fee,
		Leverage:           leverage,
		Status:             "OPEN",
		Source:             paperSource,
		CreatedAt:          nowMs,
		UpdatedAt:          nowMs,
	}

	if err := p.store.Position().Create(pos); err != nil {
		return nil, fmt.Errorf("[Paper] failed to save position: %w", err)
	}

	logger.Infof("[Paper] Opened %s %s qty=%.4f @ %.4f leverage=%dx fee=%.4f USDT (posID=%d)",
		side, symbol, quantity, currentPrice, leverage, fee, pos.ID)

	return map[string]interface{}{
		"orderId":       fmt.Sprintf("paper-%d", nowMs),
		"symbol":        symbol,
		"side":          side,
		"quantity":      quantity,
		"price":         currentPrice,
		"leverage":      leverage,
		"fee":           fee,
		"margin":        margin,
		"position_id":   pos.ID,
		"source":        paperSource,
	}, nil
}

// CloseLong closes a paper long position. quantity=0 means close all.
func (p *PaperTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return p.closePosition(symbol, "LONG", quantity)
}

// CloseShort closes a paper short position. quantity=0 means close all.
func (p *PaperTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return p.closePosition(symbol, "SHORT", quantity)
}

// closePosition handles the common logic for closing paper positions.
func (p *PaperTrader) closePosition(symbol, side string, quantity float64) (map[string]interface{}, error) {
	pos, err := p.store.Position().GetOpenPositionBySymbolAndSource(p.traderID, symbol, side, paperSource)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to find open %s position for %s: %w", side, symbol, err)
	}
	if pos == nil {
		return nil, fmt.Errorf("[Paper] no open %s position found for %s", side, symbol)
	}

	currentPrice, err := market.GetCurrentPrice(symbol)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to get market price for %s: %w", symbol, err)
	}

	// Determine quantity to close
	closeQty := quantity
	if closeQty <= 0 || closeQty >= pos.Quantity {
		closeQty = pos.Quantity
	}

	// Realized PnL (before fees)
	var grossPnL float64
	if side == "LONG" {
		grossPnL = (currentPrice - pos.EntryPrice) * closeQty
	} else {
		grossPnL = (pos.EntryPrice - currentPrice) * closeQty
	}

	closeFee := currentPrice * closeQty * p.feeRate
	// Proportional opening fee for this close
	var openFeeProration float64
	if pos.EntryQuantity > 0 {
		openFeeProration = pos.Fee * (closeQty / pos.EntryQuantity)
	}
	totalFee := closeFee + openFeeProration
	netPnL := grossPnL - totalFee

	nowMs := time.Now().UTC().UnixMilli()

	if closeQty >= pos.Quantity-0.0001 {
		// Full close
		if err := p.store.Position().ClosePosition(pos.ID, currentPrice, fmt.Sprintf("paper-%d", nowMs), netPnL, totalFee, "paper_close"); err != nil {
			return nil, fmt.Errorf("[Paper] failed to close position: %w", err)
		}
		logger.Infof("[Paper] Closed %s %s qty=%.4f @ %.4f PnL=%.4f USDT fee=%.4f USDT",
			side, symbol, closeQty, currentPrice, netPnL, totalFee)
	} else {
		// Partial close: pass addFee=0 so close fee doesn't accumulate into pos.Fee.
		// The close fee is already embedded in the net PnL (grossPnL-closeFee).
		// pos.Fee retains only the opening fee, matching GetPaperBalance's openFees deduction.
		if err := p.store.Position().ReducePositionQuantity(pos.ID, closeQty, currentPrice, 0, grossPnL-closeFee); err != nil {
			return nil, fmt.Errorf("[Paper] failed to reduce position: %w", err)
		}
		logger.Infof("[Paper] Partially closed %s %s qty=%.4f/%.4f @ %.4f PnL=%.4f USDT fee=%.4f USDT",
			side, symbol, closeQty, pos.Quantity, currentPrice, netPnL, totalFee)
	}

	// Clear any stop/tp for this position
	p.mu.Lock()
	delete(p.sl, symbol+"_"+side)
	delete(p.tp, symbol+"_"+side)
	p.mu.Unlock()

	return map[string]interface{}{
		"orderId":       fmt.Sprintf("paper-%d", nowMs),
		"symbol":        symbol,
		"side":          side,
		"closeQty":      closeQty,
		"exitPrice":     currentPrice,
		"grossPnL":      grossPnL,
		"netPnL":        netPnL,
		"fee":           totalFee,
		"source":        paperSource,
	}, nil
}

// SetLeverage records leverage for a symbol (in-memory only, no exchange call).
func (p *PaperTrader) SetLeverage(symbol string, leverage int) error {
	p.mu.Lock()
	p.leverageMap[symbol] = leverage
	p.mu.Unlock()
	return nil
}

// SetMarginMode is a no-op for paper trading.
func (p *PaperTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	return nil
}

// GetMarketPrice fetches real market price (market data is always real).
func (p *PaperTrader) GetMarketPrice(symbol string) (float64, error) {
	return market.GetCurrentPrice(symbol)
}

// SetStopLoss records a virtual stop-loss price in memory.
// AutoTrader will check position PnL on next cycle and trigger close if price breaches stop.
func (p *PaperTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	p.mu.Lock()
	p.sl[symbol+"_"+positionSide] = stopPrice
	p.mu.Unlock()
	logger.Infof("[Paper] Set stop-loss for %s %s @ %.4f", symbol, positionSide, stopPrice)
	return nil
}

// SetTakeProfit records a virtual take-profit price in memory.
func (p *PaperTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	p.mu.Lock()
	p.tp[symbol+"_"+positionSide] = takeProfitPrice
	p.mu.Unlock()
	logger.Infof("[Paper] Set take-profit for %s %s @ %.4f", symbol, positionSide, takeProfitPrice)
	return nil
}

// CancelStopLossOrders clears the stop-loss for a symbol.
func (p *PaperTrader) CancelStopLossOrders(symbol string) error {
	p.mu.Lock()
	delete(p.sl, symbol+"_LONG")
	delete(p.sl, symbol+"_SHORT")
	p.mu.Unlock()
	return nil
}

// CancelTakeProfitOrders clears the take-profit for a symbol.
func (p *PaperTrader) CancelTakeProfitOrders(symbol string) error {
	p.mu.Lock()
	delete(p.tp, symbol+"_LONG")
	delete(p.tp, symbol+"_SHORT")
	p.mu.Unlock()
	return nil
}

// CancelAllOrders clears all virtual stop/tp orders for a symbol.
func (p *PaperTrader) CancelAllOrders(symbol string) error {
	p.mu.Lock()
	delete(p.sl, symbol+"_LONG")
	delete(p.sl, symbol+"_SHORT")
	delete(p.tp, symbol+"_LONG")
	delete(p.tp, symbol+"_SHORT")
	p.mu.Unlock()
	return nil
}

// CancelStopOrders clears stop-loss and take-profit orders for a symbol.
func (p *PaperTrader) CancelStopOrders(symbol string) error {
	return p.CancelAllOrders(symbol)
}

// FormatQuantity returns a formatted quantity string (uses simple 4 decimal precision).
func (p *PaperTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

// GetOrderStatus always returns FILLED for paper orders (instant execution).
func (p *PaperTrader) GetOrderStatus(symbol string, orderID string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":      "FILLED",
		"avgPrice":    0.0,
		"executedQty": 0.0,
		"commission":  0.0,
	}, nil
}

// GetClosedPnL returns closed paper positions as ClosedPnLRecord list.
func (p *PaperTrader) GetClosedPnL(startTime time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	startMs := startTime.UTC().UnixMilli()
	positions, err := p.store.Position().GetClosedPositionsBySource(p.traderID, paperSource, startMs, limit)
	if err != nil {
		return nil, fmt.Errorf("[Paper] failed to query closed positions: %w", err)
	}

	records := make([]types.ClosedPnLRecord, 0, len(positions))
	for _, pos := range positions {
		records = append(records, types.ClosedPnLRecord{
			Symbol:      pos.Symbol,
			Side:        pos.Side,
			EntryPrice:  pos.EntryPrice,
			ExitPrice:   pos.ExitPrice,
			Quantity:    pos.Quantity,
			RealizedPnL: pos.RealizedPnL,
			Fee:         pos.Fee,
			Leverage:    pos.Leverage,
			EntryTime:   time.UnixMilli(pos.EntryTime).UTC(),
			ExitTime:    time.UnixMilli(pos.ExitTime).UTC(),
			CloseType:   pos.CloseReason,
			ExchangeID:  paperSource,
		})
	}
	return records, nil
}

// GetOpenOrders returns virtual stop/tp orders from in-memory state.
func (p *PaperTrader) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var orders []types.OpenOrder
	for _, side := range []string{"LONG", "SHORT"} {
		key := symbol + "_" + side
		if stopPrice, ok := p.sl[key]; ok {
			closeSide := "SELL"
			if side == "SHORT" {
				closeSide = "BUY"
			}
			orders = append(orders, types.OpenOrder{
				OrderID:      "paper-sl-" + key,
				Symbol:       symbol,
				Side:         closeSide,
				PositionSide: side,
				Type:         "STOP_MARKET",
				StopPrice:    stopPrice,
				Status:       "NEW",
			})
		}
		if tpPrice, ok := p.tp[key]; ok {
			closeSide := "SELL"
			if side == "SHORT" {
				closeSide = "BUY"
			}
			orders = append(orders, types.OpenOrder{
				OrderID:      "paper-tp-" + key,
				Symbol:       symbol,
				Side:         closeSide,
				PositionSide: side,
				Type:         "TAKE_PROFIT_MARKET",
				StopPrice:    tpPrice,
				Status:       "NEW",
			})
		}
	}
	return orders, nil
}
