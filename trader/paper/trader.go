package paper

import (
	"context"
	"fmt"
	"math"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"nofx/trader/types"
	"strconv"
	"strings"
	"sync"
	"time"
)

const takerFeeRate = 0.0004

// priceCache avoids repeated HTTP calls when GetPositions is polled every 15s
type priceCacheEntry struct {
	price     float64
	timestamp time.Time
}

var (
	priceCache    = make(map[string]*priceCacheEntry)
	priceCacheMu  sync.RWMutex
	priceCacheTTL = 30 * time.Second
)

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
	store          *store.Store
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
		store:          st,
		initialBalance: initialBalance,
		walletBalance:  initialBalance,
		positions:      make(map[string]*Position),
		orders:         make(map[string]*Order),
		nextOrderID:    time.Now().UnixMilli(),
	}
	t.loadOpenPositions(st)
	t.loadOpenStopOrders(st)
	return t
}

func (t *PaperTrader) loadOpenPositions(st *store.Store) {
	if st == nil || t.traderID == "" {
		logger.Infof("⚠️ [PaperTrader %s] loadOpenPositions skipped: st=%v, traderID=%s", t.traderID, st != nil, t.traderID)
		return
	}
	closedPositions, err := st.Position().GetClosedPositions(t.traderID, 10000)
	if err == nil {
		for _, p := range closedPositions {
			t.walletBalance += p.RealizedPnL - p.Fee
		}
		logger.Infof("✓ [PaperTrader %s] Loaded %d closed positions, wallet balance adjusted", t.traderID, len(closedPositions))
	} else {
		logger.Infof("⚠️ [PaperTrader %s] Failed to load closed positions: %v", t.traderID, err)
	}
	positions, err := st.Position().GetOpenPositions(t.traderID)
	if err != nil {
		logger.Infof("⚠️ [PaperTrader %s] Failed to load open positions: %v", t.traderID, err)
		return
	}
	logger.Infof("✓ [PaperTrader %s] Loading %d open positions into memory", t.traderID, len(positions))
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
		logger.Infof("  → [PaperTrader %s] Loaded position: %s %s @ %.2f x%d", t.traderID, p.Symbol, side, p.EntryPrice, p.Leverage)
	}

	// Pre-warm price cache for all open positions so first API call is fast
	if len(positions) > 0 {
		go func() {
			for _, p := range positions {
				_, _ = marketPrice(p.Symbol)
			}
			logger.Infof("✓ [PaperTrader %s] Price cache pre-warmed for %d positions", t.traderID, len(positions))
		}()
	}
}

func (t *PaperTrader) loadOpenStopOrders(st *store.Store) {
	if st == nil || t.traderID == "" {
		return
	}
	orders, err := st.Order().GetOpenProtectiveOrders(t.traderID)
	if err != nil {
		logger.Infof("⚠️ [PaperTrader %s] Failed to load protective orders: %v", t.traderID, err)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, order := range orders {
		if order.ExchangeOrderID == "" || order.StopPrice <= 0 || order.Quantity <= 0 {
			continue
		}
		t.stopOrders = append(t.stopOrders, types.OpenOrder{
			OrderID:      order.ExchangeOrderID,
			Symbol:       market.Normalize(order.Symbol),
			Side:         strings.ToUpper(order.Side),
			PositionSide: strings.ToUpper(order.PositionSide),
			Type:         strings.ToUpper(order.Type),
			Price:        order.Price,
			StopPrice:    order.StopPrice,
			Quantity:     order.Quantity,
			Status:       order.Status,
		})
	}
	if len(orders) > 0 {
		logger.Infof("✓ [PaperTrader %s] Loaded %d protective order(s)", t.traderID, len(orders))
	}
}

func positionKey(symbol, side string) string {
	return market.Normalize(symbol) + ":" + strings.ToLower(side)
}

func (t *PaperTrader) GetBalance() (map[string]interface{}, error) {
	t.evaluateStopOrders()

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
	t.evaluateStopOrders()

	t.mu.RLock()
	defer t.mu.RUnlock()

	// Concurrently fetch prices for all positions to avoid serial delays
	type priceResult struct {
		symbol string
		price  float64
		err    error
	}

	priceMap := make(map[string]float64)
	if len(t.positions) > 0 {
		resCh := make(chan priceResult, len(t.positions))
		for _, p := range t.positions {
			go func(s string) {
				price, err := marketPrice(s)
				resCh <- priceResult{symbol: s, price: price, err: err}
			}(p.Symbol)
		}
		for i := 0; i < len(t.positions); i++ {
			res := <-resCh
			if res.err != nil {
				logger.Infof("⚠️ [PaperTrader] Failed to get price for %s: %v", res.symbol, res.err)
			}
			priceMap[res.symbol] = res.price
		}
	}

	result := make([]map[string]interface{}, 0, len(t.positions))
	for _, p := range t.positions {
		markPrice := priceMap[p.Symbol]
		if markPrice <= 0 {
			markPrice = p.EntryPrice // fallback so position is still visible
		}
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
		t.cancelProtectiveOrdersLocked(normalized, strings.ToUpper(side))
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
	t.evaluateStopOrders()

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

func (t *PaperTrader) evaluateStopOrders() {
	t.mu.RLock()
	if len(t.stopOrders) == 0 {
		t.mu.RUnlock()
		return
	}
	orders := make([]types.OpenOrder, 0, len(t.stopOrders))
	symbolSet := make(map[string]struct{})
	for _, order := range t.stopOrders {
		if order.Status != "NEW" && order.Status != "" {
			continue
		}
		orders = append(orders, order)
		symbolSet[market.Normalize(order.Symbol)] = struct{}{}
	}
	t.mu.RUnlock()
	if len(orders) == 0 {
		return
	}

	prices := make(map[string]float64, len(symbolSet))
	for symbol := range symbolSet {
		price, err := marketPrice(symbol)
		if err != nil {
			logger.Infof("⚠️ [PaperTrader] Failed to evaluate protective orders for %s: %v", symbol, err)
			continue
		}
		prices[symbol] = price
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, order := range orders {
		symbol := market.Normalize(order.Symbol)
		price := prices[symbol]
		if price <= 0 || !paperStopTriggered(order, price) {
			continue
		}
		t.triggerStopOrderLocked(order, price)
	}
}

func paperStopTriggered(order types.OpenOrder, markPrice float64) bool {
	stopPrice := order.StopPrice
	if stopPrice <= 0 || markPrice <= 0 {
		return false
	}
	positionSide := strings.ToUpper(order.PositionSide)
	orderType := strings.ToUpper(order.Type)
	switch orderType {
	case "STOP_MARKET", "STOP":
		if positionSide == "SHORT" {
			return markPrice >= stopPrice
		}
		return markPrice <= stopPrice
	case "TAKE_PROFIT_MARKET", "TAKE_PROFIT":
		if positionSide == "SHORT" {
			return markPrice <= stopPrice
		}
		return markPrice >= stopPrice
	default:
		return false
	}
}

func (t *PaperTrader) triggerStopOrderLocked(order types.OpenOrder, markPrice float64) {
	symbol := market.Normalize(order.Symbol)
	positionSide := strings.ToUpper(order.PositionSide)
	side := "long"
	if positionSide == "SHORT" {
		side = "short"
	}
	key := positionKey(symbol, side)
	pos := t.positions[key]
	if pos == nil {
		t.removeStopOrderLocked(order.OrderID)
		t.markProtectiveOrderCanceled(order.OrderID)
		return
	}

	quantity := order.Quantity
	if quantity <= 0 || quantity > pos.Quantity {
		quantity = pos.Quantity
	}
	fillPrice := order.StopPrice
	if fillPrice <= 0 {
		fillPrice = markPrice
	}
	fee := fillPrice * quantity * takerFeeRate
	realized := pnl(pos.Side, pos.EntryPrice, fillPrice, quantity)
	t.walletBalance += realized - fee
	pos.Quantity -= quantity
	if pos.Quantity <= 1e-12 {
		delete(t.positions, key)
	}

	t.orders[order.OrderID] = &Order{
		ID:         order.OrderID,
		Symbol:     symbol,
		Status:     "FILLED",
		AvgPrice:   fillPrice,
		Quantity:   quantity,
		Commission: fee,
		CreatedAt:  time.Now(),
	}

	t.removeStopOrderLocked(order.OrderID)
	t.cancelProtectiveOrdersLocked(symbol, positionSide)
	t.recordTriggeredProtectiveOrder(order, symbol, positionSide, fillPrice, quantity, fee, realized)
	logger.Infof("✓ [PaperTrader] Protective order triggered: %s %s %s @ %.8f qty=%.8f pnl=%.4f",
		symbol, positionSide, order.Type, fillPrice, quantity, realized)
}

func (t *PaperTrader) removeStopOrderLocked(orderID string) {
	filtered := t.stopOrders[:0]
	for _, order := range t.stopOrders {
		if order.OrderID == orderID {
			continue
		}
		filtered = append(filtered, order)
	}
	t.stopOrders = filtered
}

func (t *PaperTrader) cancelProtectiveOrdersLocked(symbol, positionSide string) {
	filtered := t.stopOrders[:0]
	for _, order := range t.stopOrders {
		if market.Normalize(order.Symbol) == symbol && strings.EqualFold(order.PositionSide, positionSide) {
			continue
		}
		filtered = append(filtered, order)
	}
	t.stopOrders = filtered
	if t.store != nil && t.traderID != "" {
		if err := t.store.Order().CancelOpenProtectiveOrders(t.traderID, symbol, positionSide); err != nil {
			logger.Infof("⚠️ [PaperTrader] Failed to cancel protective order records: %v", err)
		}
	}
}

func (t *PaperTrader) markProtectiveOrderCanceled(orderID string) {
	if t.store == nil || t.traderID == "" || orderID == "" {
		return
	}
	orderRecord, err := t.store.Order().GetOrderByTraderAndExchangeOrderID(t.traderID, orderID)
	if err != nil || orderRecord == nil {
		if err != nil {
			logger.Infof("⚠️ [PaperTrader] Failed to find protective order %s: %v", orderID, err)
		}
		return
	}
	if err := t.store.Order().UpdateOrderStatus(orderRecord.ID, "CANCELED", 0, 0, 0); err != nil {
		logger.Infof("⚠️ [PaperTrader] Failed to mark protective order canceled: %v", err)
	}
}

func (t *PaperTrader) recordTriggeredProtectiveOrder(order types.OpenOrder, symbol, positionSide string, fillPrice, quantity, fee, realized float64) {
	if t.store == nil || t.traderID == "" || order.OrderID == "" {
		return
	}
	orderRecord, err := t.store.Order().GetOrderByTraderAndExchangeOrderID(t.traderID, order.OrderID)
	if err != nil {
		logger.Infof("⚠️ [PaperTrader] Failed to find triggered protective order %s: %v", order.OrderID, err)
		return
	}
	if orderRecord == nil {
		return
	}
	if err := t.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", quantity, fillPrice, fee); err != nil {
		logger.Infof("⚠️ [PaperTrader] Failed to update triggered protective order: %v", err)
	}

	closeReason := "stop_loss"
	if strings.Contains(strings.ToUpper(order.Type), "TAKE_PROFIT") {
		closeReason = "take_profit"
	}
	if err := t.store.Position().SetPendingCloseReason(t.traderID, symbol, positionSide, closeReason, order.OrderID); err != nil {
		logger.Infof("⚠️ [PaperTrader] Failed to tag protective close reason: %v", err)
	}

	action := "close_long"
	fillSide := "SELL"
	if positionSide == "SHORT" {
		action = "close_short"
		fillSide = "BUY"
	}
	fill := &store.TraderFill{
		TraderID:        t.traderID,
		ExchangeID:      orderRecord.ExchangeID,
		ExchangeType:    orderRecord.ExchangeType,
		OrderID:         orderRecord.ID,
		ExchangeOrderID: order.OrderID,
		ExchangeTradeID: fmt.Sprintf("%s-%d", order.OrderID, time.Now().UnixNano()),
		Symbol:          symbol,
		Side:            fillSide,
		Price:           fillPrice,
		Quantity:        quantity,
		QuoteQuantity:   fillPrice * quantity,
		Commission:      fee,
		CommissionAsset: "USDT",
		RealizedPnL:     realized,
		IsMaker:         false,
		CreatedAt:       time.Now().UTC().UnixMilli(),
	}
	if err := t.store.Order().CreateFill(fill); err != nil {
		logger.Infof("⚠️ [PaperTrader] Failed to record protective fill: %v", err)
	}
	posBuilder := store.NewPositionBuilder(t.store.Position())
	if err := posBuilder.ProcessTrade(t.traderID, orderRecord.ExchangeID, orderRecord.ExchangeType, symbol, positionSide, action, quantity, fillPrice, fee, realized, time.Now().UTC().UnixMilli(), order.OrderID); err != nil {
		logger.Infof("⚠️ [PaperTrader] Failed to close position from protective order: %v", err)
	}
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
	type exposureResult struct {
		unrealized float64
		marginUsed float64
	}

	if len(t.positions) == 0 {
		return 0, 0
	}

	// Concurrently fetch prices to avoid serial delays
	resCh := make(chan exposureResult, len(t.positions))
	for _, p := range t.positions {
		go func(pos *Position) {
			markPrice, _ := marketPrice(pos.Symbol)
			if markPrice <= 0 {
				markPrice = pos.EntryPrice // fallback
			}
			lev := pos.Leverage
			if lev <= 0 {
				lev = 1
			}
			resCh <- exposureResult{
				unrealized: pnl(pos.Side, pos.EntryPrice, markPrice, pos.Quantity),
				marginUsed: markPrice * pos.Quantity / float64(lev),
			}
		}(p)
	}

	for i := 0; i < len(t.positions); i++ {
		res := <-resCh
		unrealized += res.unrealized
		marginUsed += res.marginUsed
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
	symbol = market.Normalize(symbol)

	// Check cache first
	priceCacheMu.RLock()
	if entry, ok := priceCache[symbol]; ok && time.Since(entry.timestamp) < priceCacheTTL {
		priceCacheMu.RUnlock()
		return entry.price, nil
	}
	priceCacheMu.RUnlock()

	// Prefer live public ticker sources. Klines are only a public fallback when
	// all ticker endpoints are temporarily unavailable.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type result struct {
		price float64
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		price, source, priceErr := market.GetPublicTickerPrice(ctx, "binance", symbol, true)
		if priceErr == nil && price > 0 {
			if source != "binance" {
				logger.Warnf("[PaperTrader] Binance ticker unavailable for %s, using %s public ticker", symbol, source)
			}
			resCh <- result{price: price}
			return
		}
		logger.Warnf("[PaperTrader] Public ticker price failed for %s, falling back to public kline: %v", symbol, priceErr)

		klines, klineSource, klineErr := market.GetPublicKlines(ctx, "binance", symbol, "1m", 1, true)
		if klineErr != nil {
			resCh <- result{err: fmt.Errorf("public ticker failed: %v; public kline fallback failed: %w", priceErr, klineErr)}
			return
		}
		if len(klines) == 0 {
			resCh <- result{err: fmt.Errorf("no price data for %s from public ticker or public kline", symbol)}
			return
		}
		if klineSource != "binance" {
			logger.Warnf("[PaperTrader] Binance kline unavailable for %s, using %s public kline", symbol, klineSource)
		}
		resCh <- result{price: klines[len(klines)-1].Close}
	}()

	select {
	case res := <-resCh:
		if res.err != nil {
			return 0, res.err
		}
		// Update cache
		priceCacheMu.Lock()
		priceCache[symbol] = &priceCacheEntry{price: res.price, timestamp: time.Now()}
		priceCacheMu.Unlock()
		return res.price, nil
	case <-ctx.Done():
		return 0, fmt.Errorf("market price fetch timeout for %s", symbol)
	}
}

func pnl(side string, entryPrice, markPrice, quantity float64) float64 {
	if strings.EqualFold(side, "short") {
		return (entryPrice - markPrice) * quantity
	}
	return (markPrice - entryPrice) * quantity
}
