package paper

import (
	"fmt"
	"math"
	"nofx/market"
	"nofx/store"
	"nofx/trader/types"
	"strconv"
	"strings"
	"sync"
	"time"
)

const takerFeeRate = 0.0004

type Position struct {
	Symbol     string
	Side       string
	Quantity   float64
	EntryPrice float64
	Leverage   int
	EntryTime  time.Time
}

type Order struct {
	ID         string
	Symbol     string
	Status     string
	AvgPrice   float64
	Quantity   float64
	Commission float64
	CreatedAt  time.Time
}

type PaperTrader struct {
	mu             sync.RWMutex
	traderID       string
	initialBalance float64
	walletBalance  float64
	positions      map[string]*Position
	orders         map[string]*Order
	stopOrders     []types.OpenOrder
	nextOrderID    int64
}

func NewPaperTrader(initialBalance float64, st *store.Store, traderID string) *PaperTrader {
	if initialBalance <= 0 {
		initialBalance = 10000
	}
	t := &PaperTrader{
		traderID:       traderID,
		initialBalance: initialBalance,
		walletBalance:  initialBalance,
		positions:      make(map[string]*Position),
		orders:         make(map[string]*Order),
		nextOrderID:    time.Now().UnixMilli(),
	}
	t.loadOpenPositions(st)
	return t
}

func (t *PaperTrader) loadOpenPositions(st *store.Store) {
	if st == nil || t.traderID == "" {
		return
	}
	closedPositions, err := st.Position().GetClosedPositions(t.traderID, 10000)
	if err == nil {
		for _, p := range closedPositions {
			t.walletBalance += p.RealizedPnL - p.Fee
		}
	}
	positions, err := st.Position().GetOpenPositions(t.traderID)
	if err != nil {
		return
	}
	for _, p := range positions {
		side := strings.ToLower(p.Side)
		t.positions[positionKey(p.Symbol, side)] = &Position{
			Symbol:     market.Normalize(p.Symbol),
			Side:       side,
			Quantity:   p.Quantity,
			EntryPrice: p.EntryPrice,
			Leverage:   p.Leverage,
			EntryTime:  time.UnixMilli(p.EntryTime),
		}
	}
}

func positionKey(symbol, side string) string {
	return market.Normalize(symbol) + ":" + strings.ToLower(side)
}

func (t *PaperTrader) GetBalance() (map[string]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	unrealized, marginUsed := t.accountExposureLocked()
	totalEquity := t.walletBalance + unrealized
	available := totalEquity - marginUsed
	if available < 0 {
		available = 0
	}

	return map[string]interface{}{
		"totalWalletBalance":    t.walletBalance,
		"totalUnrealizedProfit": unrealized,
		"availableBalance":      available,
		"totalEquity":           totalEquity,
		"balance":               t.walletBalance,
	}, nil
}

func (t *PaperTrader) GetPositions() ([]map[string]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(t.positions))
	for _, p := range t.positions {
		markPrice, _ := marketPrice(p.Symbol)
		unrealized := pnl(p.Side, p.EntryPrice, markPrice, p.Quantity)
		result = append(result, map[string]interface{}{
			"symbol":           p.Symbol,
			"side":             p.Side,
			"entryPrice":       p.EntryPrice,
			"markPrice":        markPrice,
			"positionAmt":      p.Quantity,
			"unRealizedProfit": unrealized,
			"liquidationPrice": 0.0,
			"leverage":         float64(p.Leverage),
		})
	}
	return result, nil
}

func (t *PaperTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.open(symbol, "long", quantity, leverage)
}

func (t *PaperTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	return t.open(symbol, "short", quantity, leverage)
}

func (t *PaperTrader) open(symbol, side string, quantity float64, leverage int) (map[string]interface{}, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if leverage <= 0 {
		leverage = 1
	}

	normalized := market.Normalize(symbol)
	price, err := marketPrice(normalized)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	key := positionKey(normalized, side)
	if _, exists := t.positions[key]; exists {
		return nil, fmt.Errorf("%s already has %s paper position", normalized, side)
	}

	notional := price * quantity
	fee := notional * takerFeeRate
	unrealized, marginUsed := t.accountExposureLocked()
	available := t.walletBalance + unrealized - marginUsed
	required := notional/float64(leverage) + fee
	if required > available {
		return nil, fmt.Errorf("insufficient paper balance: required %.4f, available %.4f", required, available)
	}

	t.walletBalance -= fee
	t.positions[key] = &Position{
		Symbol:     normalized,
		Side:       side,
		Quantity:   quantity,
		EntryPrice: price,
		Leverage:   leverage,
		EntryTime:  time.Now(),
	}

	return t.recordFilledOrderLocked(normalized, price, quantity, fee), nil
}

func (t *PaperTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.close(symbol, "long", quantity)
}

func (t *PaperTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	return t.close(symbol, "short", quantity)
}

func (t *PaperTrader) close(symbol, side string, quantity float64) (map[string]interface{}, error) {
	normalized := market.Normalize(symbol)
	price, err := marketPrice(normalized)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	key := positionKey(normalized, side)
	pos := t.positions[key]
	if pos == nil {
		return nil, fmt.Errorf("no %s paper position for %s", side, normalized)
	}
	if quantity <= 0 || quantity > pos.Quantity {
		quantity = pos.Quantity
	}

	realized := pnl(pos.Side, pos.EntryPrice, price, quantity)
	fee := price * quantity * takerFeeRate
	t.walletBalance += realized - fee
	pos.Quantity -= quantity
	if pos.Quantity <= 1e-12 {
		delete(t.positions, key)
	}

	return t.recordFilledOrderLocked(normalized, price, quantity, fee), nil
}

func (t *PaperTrader) SetLeverage(symbol string, leverage int) error {
	return nil
}

func (t *PaperTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	return nil
}

func (t *PaperTrader) GetMarketPrice(symbol string) (float64, error) {
	return marketPrice(symbol)
}

func (t *PaperTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return t.addStopOrder(symbol, positionSide, "STOP_MARKET", quantity, stopPrice)
}

func (t *PaperTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	return t.addStopOrder(symbol, positionSide, "TAKE_PROFIT_MARKET", quantity, takeProfitPrice)
}

func (t *PaperTrader) CancelStopLossOrders(symbol string) error {
	return t.cancelStopOrders(symbol, "STOP_MARKET")
}

func (t *PaperTrader) CancelTakeProfitOrders(symbol string) error {
	return t.cancelStopOrders(symbol, "TAKE_PROFIT_MARKET")
}

func (t *PaperTrader) CancelAllOrders(symbol string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if strings.TrimSpace(symbol) == "" {
		t.stopOrders = nil
		return nil
	}
	normalized := market.Normalize(symbol)
	filtered := t.stopOrders[:0]
	for _, order := range t.stopOrders {
		if market.Normalize(order.Symbol) != normalized {
			filtered = append(filtered, order)
		}
	}
	t.stopOrders = filtered
	return nil
}

func (t *PaperTrader) CancelStopOrders(symbol string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	normalized := market.Normalize(symbol)
	filtered := t.stopOrders[:0]
	for _, order := range t.stopOrders {
		if market.Normalize(order.Symbol) != normalized {
			filtered = append(filtered, order)
		}
	}
	t.stopOrders = filtered
	return nil
}

func (t *PaperTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return strconv.FormatFloat(math.Round(quantity*1000000)/1000000, 'f', -1, 64), nil
}

func (t *PaperTrader) GetOrderStatus(symbol string, orderID string) (map[string]interface{}, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	order := t.orders[orderID]
	if order == nil {
		return nil, fmt.Errorf("paper order %s not found", orderID)
	}
	return map[string]interface{}{
		"status":      order.Status,
		"avgPrice":    order.AvgPrice,
		"executedQty": order.Quantity,
		"commission":  order.Commission,
	}, nil
}

func (t *PaperTrader) GetClosedPnL(startTime time.Time, limit int) ([]types.ClosedPnLRecord, error) {
	return nil, nil
}

func (t *PaperTrader) GetOpenOrders(symbol string) ([]types.OpenOrder, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	normalized := ""
	if strings.TrimSpace(symbol) != "" {
		normalized = market.Normalize(symbol)
	}
	orders := make([]types.OpenOrder, 0, len(t.stopOrders))
	for _, order := range t.stopOrders {
		if normalized == "" || market.Normalize(order.Symbol) == normalized {
			orders = append(orders, order)
		}
	}
	return orders, nil
}

func (t *PaperTrader) addStopOrder(symbol, positionSide, orderType string, quantity, stopPrice float64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	orderID := t.nextOrderIDLocked()
	side := "SELL"
	if strings.EqualFold(positionSide, "SHORT") {
		side = "BUY"
	}
	t.stopOrders = append(t.stopOrders, types.OpenOrder{
		OrderID:      orderID,
		Symbol:       market.Normalize(symbol),
		Side:         side,
		PositionSide: strings.ToUpper(positionSide),
		Type:         orderType,
		StopPrice:    stopPrice,
		Quantity:     quantity,
		Status:       "NEW",
	})
	return nil
}

func (t *PaperTrader) cancelStopOrders(symbol, orderType string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	normalized := market.Normalize(symbol)
	filtered := t.stopOrders[:0]
	for _, order := range t.stopOrders {
		if market.Normalize(order.Symbol) == normalized && order.Type == orderType {
			continue
		}
		filtered = append(filtered, order)
	}
	t.stopOrders = filtered
	return nil
}

func (t *PaperTrader) accountExposureLocked() (unrealized float64, marginUsed float64) {
	for _, p := range t.positions {
		markPrice, _ := marketPrice(p.Symbol)
		unrealized += pnl(p.Side, p.EntryPrice, markPrice, p.Quantity)
		leverage := p.Leverage
		if leverage <= 0 {
			leverage = 1
		}
		marginUsed += markPrice * p.Quantity / float64(leverage)
	}
	return unrealized, marginUsed
}

func (t *PaperTrader) recordFilledOrderLocked(symbol string, price, quantity, fee float64) map[string]interface{} {
	orderID := t.nextOrderIDLocked()
	t.orders[orderID] = &Order{
		ID:         orderID,
		Symbol:     symbol,
		Status:     "FILLED",
		AvgPrice:   price,
		Quantity:   quantity,
		Commission: fee,
		CreatedAt:  time.Now(),
	}
	return map[string]interface{}{
		"orderId":       orderID,
		"symbol":        symbol,
		"status":        "FILLED",
		"avgPrice":      price,
		"executedQty":   quantity,
		"commission":    fee,
		"transactTime":  time.Now().UnixMilli(),
		"paper_trading": true,
	}
}

func (t *PaperTrader) nextOrderIDLocked() string {
	t.nextOrderID++
	return fmt.Sprintf("paper-%d", t.nextOrderID)
}

func marketPrice(symbol string) (float64, error) {
	data, err := market.GetWithExchange(symbol, "binance")
	if err != nil {
		return 0, err
	}
	return data.CurrentPrice, nil
}

func pnl(side string, entryPrice, markPrice, quantity float64) float64 {
	if strings.EqualFold(side, "short") {
		return (entryPrice - markPrice) * quantity
	}
	return (markPrice - entryPrice) * quantity
}
