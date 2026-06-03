package paper

import (
	"context"
	"fmt"
	"math"
	"nofx/logger"
	"nofx/market"
	"nofx/provider/coinank/coinank_api"
	"nofx/provider/coinank/coinank_enum"
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
	apiClient     = market.NewAPIClient()
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
	initialBalance float64
	walletBalance  float64
	positions      map[string][]*Position
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
		positions:      make(map[string][]*Position),
		orders:         make(map[string]*Order),
		nextOrderID:    time.Now().UnixMilli(),
	}
	t.loadOpenPositions(st)
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
		key := positionKey(p.Symbol, side)
		t.positions[key] = append(t.positions[key], &Position{
			Symbol:     market.Normalize(p.Symbol),
			Side:       side,
			Quantity:   p.Quantity,
			EntryPrice: p.EntryPrice,
			Leverage:   p.Leverage,
			EntryTime:  time.UnixMilli(p.EntryTime),
		})
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

	// Concurrently fetch prices for all positions to avoid serial delays
	type priceResult struct {
		symbol string
		price  float64
		err    error
	}

	// Collect unique symbols for concurrent price fetch
	symbolSet := make(map[string]struct{})
	for _, group := range t.positions {
		for _, p := range group {
			symbolSet[p.Symbol] = struct{}{}
		}
	}

	priceMap := make(map[string]float64)
	if len(symbolSet) > 0 {
		resCh := make(chan priceResult, len(symbolSet))
		for sym := range symbolSet {
			go func(s string) {
				price, err := marketPrice(s)
				resCh <- priceResult{symbol: s, price: price, err: err}
			}(sym)
		}
		for i := 0; i < len(symbolSet); i++ {
			res := <-resCh
			if res.err != nil {
				logger.Infof("⚠️ [PaperTrader] Failed to get price for %s: %v", res.symbol, res.err)
			}
			priceMap[res.symbol] = res.price
		}
	}

	result := make([]map[string]interface{}, 0)
	for _, group := range t.positions {
		for _, p := range group {
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

	notional := price * quantity
	fee := notional * takerFeeRate
	unrealized, marginUsed := t.accountExposureLocked()
	available := t.walletBalance + unrealized - marginUsed
	required := notional/float64(leverage) + fee
	if required > available {
		return nil, fmt.Errorf("insufficient paper balance: required %.4f, available %.4f", required, available)
	}

	t.walletBalance -= fee
	t.positions[key] = append(t.positions[key], &Position{
		Symbol:     normalized,
		Side:       side,
		Quantity:   quantity,
		EntryPrice: price,
		Leverage:   leverage,
		EntryTime:  time.Now(),
	})

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
	group := t.positions[key]
	if len(group) == 0 {
		return nil, fmt.Errorf("no %s paper position for %s", side, normalized)
	}
	// Close the first position (FIFO)
	pos := group[0]
	if quantity <= 0 || quantity > pos.Quantity {
		quantity = pos.Quantity
	}

	realized := pnl(pos.Side, pos.EntryPrice, price, quantity)
	fee := price * quantity * takerFeeRate
	t.walletBalance += realized - fee
	pos.Quantity -= quantity
	if pos.Quantity <= 1e-12 {
		t.positions[key] = group[1:]
		if len(t.positions[key]) == 0 {
			delete(t.positions, key)
		}
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
	type exposureResult struct {
		unrealized float64
		marginUsed float64
	}

	// Flatten all positions into a slice
	allPositions := make([]*Position, 0)
	for _, group := range t.positions {
		allPositions = append(allPositions, group...)
	}

	if len(allPositions) == 0 {
		return 0, 0
	}

	// Concurrently fetch prices to avoid serial delays
	resCh := make(chan exposureResult, len(allPositions))
	for _, p := range allPositions {
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

	for i := 0; i < len(allPositions); i++ {
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

	// Fetch price via lightweight CoinAnk API (1 kline vs 200 klines in GetWithExchange)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type result struct {
		price float64
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		ts := time.Now().UnixMilli()
		klines, err := coinank_api.Kline(ctx, symbol, coinank_enum.Binance, ts, coinank_enum.To, 1, coinank_enum.Minute3)
		if err != nil {
			resCh <- result{err: err}
			return
		}
		if len(klines) == 0 {
			resCh <- result{err: fmt.Errorf("no kline data for %s", symbol)}
			return
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
