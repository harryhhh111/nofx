package trader

import (
	"math"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"time"
)

func (at *AutoTrader) startExecutionAnalytics(decision *kernel.Decision, action string, intendedPrice, intendedQty float64) int64 {
	if at == nil || at.store == nil || decision == nil {
		return 0
	}
	nowMs := time.Now().UTC().UnixMilli()
	signalGeneratedAt := decision.SignalGeneratedAt
	if signalGeneratedAt <= 0 {
		signalGeneratedAt = nowMs
	}
	bestBid, bestAsk := at.bestBidAsk(decision.Symbol)
	spreadBps := spreadBps(bestBid, bestAsk)
	expectedSlippage := 0.0
	if spreadBps > 0 {
		expectedSlippage = spreadBps / 2
	}
	record := &store.ExecutionAnalytics{
		TraderID:            at.id,
		ExchangeID:          at.exchangeID,
		ExchangeType:        at.exchange,
		Symbol:              market.Normalize(decision.Symbol),
		Action:              action,
		SignalGeneratedAt:   signalGeneratedAt,
		OrderSubmittedAt:    nowMs,
		IntendedPrice:       intendedPrice,
		IntendedQuantity:    intendedQty,
		SubmittedQuantity:   intendedQty,
		BestBid:             bestBid,
		BestAsk:             bestAsk,
		SpreadBps:           spreadBps,
		ExpectedSlippageBps: expectedSlippage,
		Status:              "submitted",
	}
	if err := at.store.ExecutionAnalytics().Create(record); err != nil {
		logger.Warnf("[%s] failed to create execution analytics for %s %s: %v", at.name, action, decision.Symbol, err)
		return 0
	}
	return record.ID
}

func (at *AutoTrader) markExecutionOrderSubmitted(id int64, order map[string]interface{}) {
	if at == nil || at.store == nil || id <= 0 {
		return
	}
	updates := map[string]interface{}{
		"status":             "submitted",
		"order_submitted_at": time.Now().UTC().UnixMilli(),
	}
	if orderID := store.FormatExchangeOrderIDFromMap(order); orderID != "" {
		updates["exchange_order_id"] = orderID
	}
	if err := at.store.ExecutionAnalytics().Update(id, updates); err != nil {
		logger.Warnf("[%s] failed to update execution analytics order submit: %v", at.name, err)
	}
}

func (at *AutoTrader) markExecutionFailed(id int64, err error) {
	if at == nil || at.store == nil || id <= 0 || err == nil {
		return
	}
	if updateErr := at.store.ExecutionAnalytics().Update(id, map[string]interface{}{
		"status":        "failed",
		"error_message": err.Error(),
	}); updateErr != nil {
		logger.Warnf("[%s] failed to update execution analytics failure: %v", at.name, updateErr)
	}
}

func (at *AutoTrader) markExecutionFinal(id int64, action string, intendedPrice, intendedQty, fillPrice, fillQty float64, status string) {
	if at == nil || at.store == nil || id <= 0 {
		return
	}
	nowMs := time.Now().UTC().UnixMilli()
	partialRatio := 0.0
	if intendedQty > 0 {
		partialRatio = fillQty / intendedQty
	}
	updates := map[string]interface{}{
		"status":             status,
		"first_fill_at":      nowMs,
		"final_fill_at":      nowMs,
		"avg_fill_price":     fillPrice,
		"filled_quantity":    fillQty,
		"partial_fill_ratio": partialRatio,
	}
	if intendedPrice > 0 && fillPrice > 0 {
		updates["realized_slippage_bps"] = realizedSlippageBps(action, intendedPrice, fillPrice)
	}
	if err := at.store.ExecutionAnalytics().Update(id, updates); err != nil {
		logger.Warnf("[%s] failed to update execution analytics final fill: %v", at.name, err)
	}
}

func (at *AutoTrader) bestBidAsk(symbol string) (float64, float64) {
	if at == nil || at.trader == nil {
		return 0, 0
	}
	bookTrader, ok := at.trader.(interface {
		GetOrderBook(symbol string, depth int) (bids, asks [][]float64, err error)
	})
	if !ok {
		return 0, 0
	}
	bids, asks, err := bookTrader.GetOrderBook(symbol, 1)
	if err != nil || len(bids) == 0 || len(asks) == 0 || len(bids[0]) == 0 || len(asks[0]) == 0 {
		return 0, 0
	}
	return bids[0][0], asks[0][0]
}

func spreadBps(bestBid, bestAsk float64) float64 {
	if bestBid <= 0 || bestAsk <= 0 || bestAsk < bestBid {
		return 0
	}
	mid := (bestBid + bestAsk) / 2
	if mid <= 0 {
		return 0
	}
	return (bestAsk - bestBid) / mid * 10000
}

func realizedSlippageBps(action string, intendedPrice, fillPrice float64) float64 {
	if intendedPrice <= 0 || fillPrice <= 0 {
		return 0
	}
	raw := (fillPrice - intendedPrice) / intendedPrice * 10000
	switch action {
	case "open_short", "close_long":
		raw = -raw
	}
	if math.Abs(raw) < 0.000001 {
		return 0
	}
	return raw
}
