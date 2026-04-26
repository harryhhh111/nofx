package aster

import (
	"fmt"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"sort"
	"strings"
	"time"
)

func (t *AsterTrader) clearResidualOrdersForClosedSymbols(closedSymbols map[string]bool, positions []map[string]interface{}) {
	if len(closedSymbols) == 0 {
		return
	}

	activeSymbols := make(map[string]bool)
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		qty, _ := pos["positionAmt"].(float64)
		if symbol == "" || qty == 0 {
			continue
		}
		activeSymbols[market.Normalize(symbol)] = true
	}

	for symbol := range closedSymbols {
		if activeSymbols[symbol] {
			continue
		}
		if err := t.CancelAllOrders(symbol); err != nil {
			logger.Infof("  ⚠️ Failed to cancel residual Aster orders for closed symbol %s: %v", symbol, err)
			continue
		}
		logger.Infof("  ✅ Cleared residual Aster orders for closed symbol %s", symbol)
	}
}

// SyncOrdersFromAster syncs Aster exchange order history to local database
// Also creates/updates position records to ensure orders/fills/positions data consistency
// exchangeID: Exchange account UUID (from exchanges.id)
// exchangeType: Exchange type ("aster")
func (t *AsterTrader) SyncOrdersFromAster(traderID string, exchangeID string, exchangeType string, st *store.Store) error {
	if st == nil {
		return fmt.Errorf("store is nil")
	}

	// Get recent trades (last 24 hours)
	startTime := time.Now().Add(-24 * time.Hour)

	logger.Infof("🔄 Syncing Aster trades from: %s", startTime.Format(time.RFC3339))

	// Use GetTrades method to fetch trade records
	trades, err := t.GetTrades(startTime, 500)
	if err != nil {
		return fmt.Errorf("failed to get trades: %w", err)
	}

	logger.Infof("📥 Received %d trades from Aster", len(trades))

	// Sort trades by time ASC (oldest first) for proper position building
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Time.UnixMilli() < trades[j].Time.UnixMilli()
	})

	// Process trades one by one (no transaction to avoid deadlock)
	orderStore := st.Order()
	positionStore := st.Position()
	posBuilder := store.NewPositionBuilder(positionStore)
	syncedCount := 0
	closedSymbols := make(map[string]bool)

	for _, trade := range trades {
		// Check if this fill already exists. Aster exposes both orderId and fill id;
		// multiple fills can belong to the same order, so dedupe by trade/fill id.
		existingFill, err := orderStore.GetFillByExchangeTradeID(exchangeID, trade.TradeID)
		if err == nil && existingFill != nil {
			continue
		}

		// Normalize symbol
		symbol := market.Normalize(trade.Symbol)
		exchangeOrderID := trade.OrderID
		if exchangeOrderID == "" {
			exchangeOrderID = trade.TradeID
		}

		// Determine order action based on side, positionSide, and realizedPnL
		// Aster uses one-way position mode (BOTH), so we need to infer from PnL
		// - RealizedPnL != 0 means it's a close trade
		// - RealizedPnL == 0 means it's an open trade
		orderAction := deriveAsterOrderAction(trade.Side, trade.PositionSide, trade.RealizedPnL)
		if orderAction == "close_long" || orderAction == "close_short" {
			closedSymbols[symbol] = true
		}

		// Determine position side from order action
		positionSide := "LONG"
		if strings.Contains(orderAction, "short") {
			positionSide = "SHORT"
		}

		// Normalize side for storage
		side := strings.ToUpper(trade.Side)

		// Create order record - use Unix milliseconds UTC
		tradeTimeMs := trade.Time.UTC().UnixMilli()
		orderRecord := &store.TraderOrder{
			TraderID:        traderID,
			ExchangeID:      exchangeID,   // UUID
			ExchangeType:    exchangeType, // Exchange type
			ExchangeOrderID: exchangeOrderID,
			Symbol:          symbol,
			Side:            side,
			PositionSide:    "BOTH", // Aster uses one-way position mode
			Type:            "LIMIT",
			OrderAction:     orderAction,
			Quantity:        trade.Quantity,
			Price:           trade.Price,
			Status:          "FILLED",
			FilledQuantity:  trade.Quantity,
			AvgFillPrice:    trade.Price,
			Commission:      trade.Fee,
			FilledAt:        tradeTimeMs,
			CreatedAt:       tradeTimeMs,
			UpdatedAt:       tradeTimeMs,
		}

		// Insert order record
		if err := orderStore.CreateOrder(orderRecord); err != nil {
			logger.Infof("  ⚠️ Failed to sync trade %s: %v", trade.TradeID, err)
			continue
		}

		// Create fill record - use Unix milliseconds UTC
		fillRecord := &store.TraderFill{
			TraderID:        traderID,
			ExchangeID:      exchangeID,   // UUID
			ExchangeType:    exchangeType, // Exchange type
			OrderID:         orderRecord.ID,
			ExchangeOrderID: exchangeOrderID,
			ExchangeTradeID: trade.TradeID,
			Symbol:          symbol,
			Side:            side,
			Price:           trade.Price,
			Quantity:        trade.Quantity,
			QuoteQuantity:   trade.Price * trade.Quantity,
			Commission:      trade.Fee,
			CommissionAsset: "USDT",
			RealizedPnL:     trade.RealizedPnL,
			IsMaker:         false,
			CreatedAt:       tradeTimeMs,
		}

		if err := orderStore.CreateFill(fillRecord); err != nil {
			logger.Infof("  ⚠️ Failed to sync fill for trade %s: %v", trade.TradeID, err)
		}

		// Create/update position record using PositionBuilder
		if err := posBuilder.ProcessTrade(
			traderID, exchangeID, exchangeType,
			symbol, positionSide, orderAction,
			trade.Quantity, trade.Price, trade.Fee, trade.RealizedPnL,
			tradeTimeMs, exchangeOrderID,
		); err != nil {
			logger.Infof("  ⚠️ Failed to sync position for trade %s: %v", trade.TradeID, err)
		} else {
			logger.Infof("  📍 Position updated for trade: %s (action: %s, qty: %.6f)", trade.TradeID, orderAction, trade.Quantity)
		}

		syncedCount++
		logger.Infof("  ✅ Synced trade: %s %s %s qty=%.6f price=%.6f pnl=%.2f fee=%.6f action=%s",
			trade.TradeID, symbol, side, trade.Quantity, trade.Price, trade.RealizedPnL, trade.Fee, orderAction)
	}

	if len(closedSymbols) > 0 {
		positions, posErr := t.GetPositions()
		if posErr != nil {
			logger.Infof("  ⚠️ Failed to refresh Aster positions for order cleanup: %v", posErr)
		} else {
			t.clearResidualOrdersForClosedSymbols(closedSymbols, positions)
		}
	}

	logger.Infof("✅ Aster order sync completed: %d new trades synced", syncedCount)
	return nil
}

// deriveAsterOrderAction determines order action from trade details
// Aster uses one-way position mode (BOTH), so we infer from:
// - Side: BUY or SELL
// - RealizedPnL: non-zero means closing trade
func deriveAsterOrderAction(side, positionSide string, realizedPnL float64) string {
	side = strings.ToUpper(side)
	positionSide = strings.ToUpper(positionSide)

	// Check if this is a closing trade (has realized PnL)
	isClose := realizedPnL != 0

	if positionSide == "LONG" {
		if isClose {
			return "close_long"
		}
		return "open_long"
	} else if positionSide == "SHORT" {
		if isClose {
			return "close_short"
		}
		return "open_short"
	} else {
		// BOTH mode - infer from side and PnL
		if side == "BUY" {
			if isClose {
				return "close_short" // Buying to close short
			}
			return "open_long" // Buying to open long
		} else {
			if isClose {
				return "close_long" // Selling to close long
			}
			return "open_short" // Selling to open short
		}
	}
}

// StartOrderSync starts background order sync task for Aster
func (t *AsterTrader) StartOrderSync(traderID string, exchangeID string, exchangeType string, st *store.Store, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := t.SyncOrdersFromAster(traderID, exchangeID, exchangeType, st); err != nil {
				logger.Infof("⚠️  Aster order sync failed: %v", err)
			}
		}
	}()
	logger.Infof("🔄 Aster order sync started (interval: %v)", interval)
}
