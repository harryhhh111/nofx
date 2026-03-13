// Package paper provides an in-memory simulated exchange for paper trading.
// It satisfies the types.Trader interface without touching real funds.
// Positions are stored in memory; balance starts at a configurable virtual USDT amount.
// Market prices are fetched from Binance public REST API (no API key required).
package paper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/logger"
	"nofx/trader/types"
)

const (
	defaultVirtualBalance = 10000.0 // default paper trading initial balance in USDT
	binanceTickerURL      = "https://fapi.binance.com/fapi/v1/ticker/price?symbol="
)

// paperPosition holds the state of a single simulated position.
type paperPosition struct {
	Symbol        string
	Side          string  // "LONG" or "SHORT"
	EntryPrice    float64
	Quantity      float64
	Leverage      int
	StopLoss      float64
	TakeProfit    float64
	OpenTime      time.Time
	UnrealizedPnL float64 // refreshed on GetPositions
}

// PaperExchange is an in-memory exchange that simulates futures trading.
// Thread-safe via mu.
type PaperExchange struct {
	mu             sync.RWMutex
	positions      map[string]*paperPosition // key: "SYMBOL_SIDE"
	virtualBalance float64
	orderSeq       int64
}

// NewPaperExchange creates a PaperExchange with the default virtual balance.
func NewPaperExchange() *PaperExchange {
	return NewPaperExchangeWithBalance(defaultVirtualBalance)
}

// NewPaperExchangeWithBalance creates a PaperExchange with a custom initial balance.
func NewPaperExchangeWithBalance(initialBalance float64) *PaperExchange {
	logger.Infof("[PaperExchange] Initialized with virtual balance %.2f USDT", initialBalance)
	return &PaperExchange{
		positions:      make(map[string]*paperPosition),
		virtualBalance: initialBalance,
	}
}

// posKey returns the map key for a position.
func posKey(symbol, side string) string {
	return strings.ToUpper(symbol) + "_" + strings.ToUpper(side)
}

// nextOrderID generates a sequential fake order ID.
func (p *PaperExchange) nextOrderID() string {
	p.orderSeq++
	return fmt.Sprintf("PAPER-%d", p.orderSeq)
}

// fetchMarketPrice calls Binance public REST to get current mark price.
func fetchMarketPrice(symbol string) (float64, error) {
	url := binanceTickerURL + strings.ToUpper(symbol)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return 0, fmt.Errorf("PaperExchange: http get %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("PaperExchange: read body: %w", err)
	}
	var result struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("PaperExchange: unmarshal ticker: %w", err)
	}
	price, err := strconv.ParseFloat(result.Price, 64)
	if err != nil {
		return 0, fmt.Errorf("PaperExchange: parse price %q: %w", result.Price, err)
	}
	return price, nil
}

// ── types.Trader interface implementation ─────────────────────────────────────

// GetBalance returns virtual account balance.
// Both snake_case and camelCase keys are returned so that buildTradingContext
// (which reads camelCase) and other callers (which read snake_case) both work.
func (p *PaperExchange) GetBalance() (map[string]interface{}, error) {
	p.mu.RLock()
	balance := p.virtualBalance
	p.mu.RUnlock()
	return map[string]interface{}{
		// camelCase — expected by buildTradingContext in auto_trader.go
		"totalEquity":        balance,
		"totalWalletBalance": balance,
		"availableBalance":   balance,
		// snake_case — for CreateTrader balance fetch and other callers
		"total_equity":      balance,
		"wallet_balance":    balance,
		"available_balance": balance,
		"balance":           balance,
	}, nil
}

// SetVirtualBalance sets the virtual balance (used when restoring from DB after restart,
// so that account total_pnl matches position history realized PnL).
func (p *PaperExchange) SetVirtualBalance(balance float64) {
	p.mu.Lock()
	p.virtualBalance = balance
	p.mu.Unlock()
	logger.Infof("[PaperExchange] Virtual balance set to %.2f USDT", balance)
}

// GetPositions returns all open paper positions with live unrealized PnL.
func (p *PaperExchange) GetPositions() ([]map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]map[string]interface{}, 0, len(p.positions))
	for _, pos := range p.positions {
		markPrice, err := fetchMarketPrice(pos.Symbol)
		if err != nil {
			logger.Warnf("[PaperExchange] GetPositions: cannot fetch price for %s: %v", pos.Symbol, err)
			markPrice = pos.EntryPrice // fall back to entry price
		}

		var pnl float64
		if strings.ToUpper(pos.Side) == "LONG" {
			pnl = (markPrice - pos.EntryPrice) * pos.Quantity * float64(pos.Leverage)
		} else {
			pnl = (pos.EntryPrice - markPrice) * pos.Quantity * float64(pos.Leverage)
		}
		pos.UnrealizedPnL = pnl

		result = append(result, map[string]interface{}{
			"symbol":           pos.Symbol,
			"side":             strings.ToLower(pos.Side),
			"positionSide":     strings.ToUpper(pos.Side),
			"entryPrice":       pos.EntryPrice,
			"markPrice":        markPrice,
			"positionAmt":      pos.Quantity,
			"leverage":         float64(pos.Leverage), // float64 so type-assert in auto_trader works
			// auto_trader.go reads "unRealizedProfit" (capital R) — match exactly
			"unRealizedProfit": pnl,
			"unrealizedProfit": pnl, // extra alias for other callers
			"isolatedMargin":   float64(0),
			"liquidationPrice": float64(0),
			"stopLoss":         pos.StopLoss,
			"takeProfit":       pos.TakeProfit,
		})
	}
	return result, nil
}

// RestorePosition restores a position from DB into memory (used after service restart).
// Does not call the market; entry price and quantity are taken from the persisted record.
// If a position with the same symbol+side already exists, it is skipped to avoid duplicate.
func (p *PaperExchange) RestorePosition(symbol, side string, entryPrice, quantity float64, leverage int) {
	if quantity <= 0 {
		return
	}
	side = strings.ToUpper(side)
	if side != "LONG" && side != "SHORT" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := posKey(symbol, side)
	if _, ok := p.positions[key]; ok {
		return // already present (e.g. from same run)
	}
	p.positions[key] = &paperPosition{
		Symbol:     strings.ToUpper(symbol),
		Side:       side,
		EntryPrice: entryPrice,
		Quantity:   quantity,
		Leverage:   leverage,
		OpenTime:   time.Now(),
	}
	logger.Infof("[PaperExchange] Restored position %s %s qty=%.4f entry=%.4f lev=%dx", symbol, side, quantity, entryPrice, leverage)
}

// OpenLong opens a simulated long position. If one already exists, quantity is added.
func (p *PaperExchange) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	markPrice, err := fetchMarketPrice(symbol)
	if err != nil {
		return nil, fmt.Errorf("PaperExchange OpenLong: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := posKey(symbol, "LONG")
	if existing, ok := p.positions[key]; ok {
		// Average in
		totalQty := existing.Quantity + quantity
		existing.EntryPrice = (existing.EntryPrice*existing.Quantity + markPrice*quantity) / totalQty
		existing.Quantity = totalQty
		existing.Leverage = leverage
	} else {
		p.positions[key] = &paperPosition{
			Symbol:     strings.ToUpper(symbol),
			Side:       "LONG",
			EntryPrice: markPrice,
			Quantity:   quantity,
			Leverage:   leverage,
			OpenTime:   time.Now(),
		}
	}

	orderID := p.nextOrderID()
	logger.Infof("[PaperExchange] OpenLong %s qty=%.4f lev=%dx price=%.4f orderID=%s",
		symbol, quantity, leverage, markPrice, orderID)
	return map[string]interface{}{
		"orderId":          orderID,
		"symbol":           strings.ToUpper(symbol),
		"side":             "BUY",
		"executedQty":      fmt.Sprintf("%.4f", quantity),
		"avgPrice":         fmt.Sprintf("%.4f", markPrice),
		"status":           "FILLED",
	}, nil
}

// OpenShort opens a simulated short position. If one already exists, quantity is added.
func (p *PaperExchange) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	markPrice, err := fetchMarketPrice(symbol)
	if err != nil {
		return nil, fmt.Errorf("PaperExchange OpenShort: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := posKey(symbol, "SHORT")
	if existing, ok := p.positions[key]; ok {
		totalQty := existing.Quantity + quantity
		existing.EntryPrice = (existing.EntryPrice*existing.Quantity + markPrice*quantity) / totalQty
		existing.Quantity = totalQty
		existing.Leverage = leverage
	} else {
		p.positions[key] = &paperPosition{
			Symbol:     strings.ToUpper(symbol),
			Side:       "SHORT",
			EntryPrice: markPrice,
			Quantity:   quantity,
			Leverage:   leverage,
			OpenTime:   time.Now(),
		}
	}

	orderID := p.nextOrderID()
	logger.Infof("[PaperExchange] OpenShort %s qty=%.4f lev=%dx price=%.4f orderID=%s",
		symbol, quantity, leverage, markPrice, orderID)
	return map[string]interface{}{
		"orderId":     orderID,
		"symbol":      strings.ToUpper(symbol),
		"side":        "SELL",
		"executedQty": fmt.Sprintf("%.4f", quantity),
		"avgPrice":    fmt.Sprintf("%.4f", markPrice),
		"status":      "FILLED",
	}, nil
}

// CloseLong closes a simulated long position (quantity=0 closes all).
func (p *PaperExchange) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	markPrice, err := fetchMarketPrice(symbol)
	if err != nil {
		return nil, fmt.Errorf("PaperExchange CloseLong: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := posKey(symbol, "LONG")
	pos, ok := p.positions[key]
	if !ok {
		return nil, fmt.Errorf("PaperExchange CloseLong: no LONG position for %s", symbol)
	}

	closeQty := quantity
	if closeQty <= 0 || closeQty >= pos.Quantity {
		closeQty = pos.Quantity
		delete(p.positions, key)
	} else {
		pos.Quantity -= closeQty
	}

	pnl := (markPrice - pos.EntryPrice) * closeQty * float64(pos.Leverage)
	p.virtualBalance += pnl
	orderID := p.nextOrderID()
	logger.Infof("[PaperExchange] CloseLong %s qty=%.4f exitPrice=%.4f pnl=%+.4f orderID=%s",
		symbol, closeQty, markPrice, pnl, orderID)
	return map[string]interface{}{
		"orderId":      orderID,
		"symbol":       strings.ToUpper(symbol),
		"side":         "SELL",
		"executedQty":  fmt.Sprintf("%.4f", closeQty),
		"avgPrice":     fmt.Sprintf("%.4f", markPrice),
		"realizedPnl":  fmt.Sprintf("%.4f", pnl),
		"status":       "FILLED",
	}, nil
}

// CloseShort closes a simulated short position (quantity=0 closes all).
func (p *PaperExchange) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	markPrice, err := fetchMarketPrice(symbol)
	if err != nil {
		return nil, fmt.Errorf("PaperExchange CloseShort: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := posKey(symbol, "SHORT")
	pos, ok := p.positions[key]
	if !ok {
		return nil, fmt.Errorf("PaperExchange CloseShort: no SHORT position for %s", symbol)
	}

	closeQty := quantity
	if closeQty <= 0 || closeQty >= pos.Quantity {
		closeQty = pos.Quantity
		delete(p.positions, key)
	} else {
		pos.Quantity -= closeQty
	}

	pnl := (pos.EntryPrice - markPrice) * closeQty * float64(pos.Leverage)
	p.virtualBalance += pnl
	orderID := p.nextOrderID()
	logger.Infof("[PaperExchange] CloseShort %s qty=%.4f exitPrice=%.4f pnl=%+.4f orderID=%s",
		symbol, closeQty, markPrice, pnl, orderID)
	return map[string]interface{}{
		"orderId":     orderID,
		"symbol":      strings.ToUpper(symbol),
		"side":        "BUY",
		"executedQty": fmt.Sprintf("%.4f", closeQty),
		"avgPrice":    fmt.Sprintf("%.4f", markPrice),
		"realizedPnl": fmt.Sprintf("%.4f", pnl),
		"status":      "FILLED",
	}, nil
}

// SetLeverage is a no-op for paper trading; leverage is applied on open.
func (p *PaperExchange) SetLeverage(symbol string, leverage int) error {
	logger.Infof("[PaperExchange] SetLeverage %s %dx (no-op)", symbol, leverage)
	return nil
}

// SetMarginMode is a no-op for paper trading.
func (p *PaperExchange) SetMarginMode(symbol string, isCrossMargin bool) error {
	return nil
}

// GetMarketPrice fetches the latest price from Binance public REST.
func (p *PaperExchange) GetMarketPrice(symbol string) (float64, error) {
	return fetchMarketPrice(symbol)
}

// SetStopLoss stores stop-loss on the in-memory position.
func (p *PaperExchange) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := posKey(symbol, positionSide)
	if pos, ok := p.positions[key]; ok {
		pos.StopLoss = stopPrice
		logger.Infof("[PaperExchange] SetStopLoss %s %s sl=%.4f", symbol, positionSide, stopPrice)
	}
	return nil
}

// SetTakeProfit stores take-profit on the in-memory position.
func (p *PaperExchange) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := posKey(symbol, positionSide)
	if pos, ok := p.positions[key]; ok {
		pos.TakeProfit = takeProfitPrice
		logger.Infof("[PaperExchange] SetTakeProfit %s %s tp=%.4f", symbol, positionSide, takeProfitPrice)
	}
	return nil
}

// CancelStopLossOrders clears stop-loss from the position.
func (p *PaperExchange) CancelStopLossOrders(symbol string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for side := range map[string]struct{}{"LONG": {}, "SHORT": {}} {
		if pos, ok := p.positions[posKey(symbol, side)]; ok {
			pos.StopLoss = 0
		}
	}
	return nil
}

// CancelTakeProfitOrders clears take-profit from the position.
func (p *PaperExchange) CancelTakeProfitOrders(symbol string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for side := range map[string]struct{}{"LONG": {}, "SHORT": {}} {
		if pos, ok := p.positions[posKey(symbol, side)]; ok {
			pos.TakeProfit = 0
		}
	}
	return nil
}

// CancelAllOrders clears SL/TP from the position (paper trading has no real orders).
func (p *PaperExchange) CancelAllOrders(symbol string) error {
	return p.CancelStopLossOrders(symbol)
}

// CancelStopOrders clears both SL and TP.
func (p *PaperExchange) CancelStopOrders(symbol string) error {
	_ = p.CancelStopLossOrders(symbol)
	_ = p.CancelTakeProfitOrders(symbol)
	return nil
}

// FormatQuantity returns the quantity formatted to 4 decimal places.
func (p *PaperExchange) FormatQuantity(symbol string, quantity float64) (string, error) {
	return fmt.Sprintf("%.4f", quantity), nil
}

// GetOrderStatus returns a synthetic FILLED status (orders are instant in paper trading).
func (p *PaperExchange) GetOrderStatus(symbol string, orderID string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":      "FILLED",
		"orderId":     orderID,
		"symbol":      strings.ToUpper(symbol),
		"executedQty": "0",
		"avgPrice":    "0",
	}, nil
}

// GetClosedPnL returns an empty list; paper trading does not persist closed records.
func (p *PaperExchange) GetClosedPnL(startTime time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	return []types.ClosedPnLRecord{}, nil
}

// GetOpenOrders returns empty list; paper trading has no pending orders.
func (p *PaperExchange) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	return []types.OpenOrder{}, nil
}
