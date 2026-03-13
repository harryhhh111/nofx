package api

import (
	"net/http"
	"nofx/logger"
	"time"

	"github.com/gin-gonic/gin"
)

// handlePaperReset resets the paper trading account for a trader.
// Deletes all paper positions and orders, restoring the account to its initial state.
// POST /api/traders/:id/paper/reset
func (s *Server) handlePaperReset(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	// Verify trader belongs to user
	_, err := s.store.Trader().GetFullConfig(userID, traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader not found"})
		return
	}

	logger.Infof("[Paper] User %s requested paper account reset for trader %s", userID, traderID)

	// Delete all paper positions (both OPEN and CLOSED)
	posDB := s.store.Position()
	if err := posDB.DeletePaperPositions(traderID); err != nil {
		logger.Errorf("[Paper] Failed to delete paper positions for trader %s: %v", traderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset paper positions"})
		return
	}

	// Delete all paper orders
	if err := s.store.Order().DeletePaperOrders(traderID); err != nil {
		logger.Errorf("[Paper] Failed to delete paper orders for trader %s: %v", traderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset paper orders"})
		return
	}

	logger.Infof("[Paper] Paper account reset complete for trader %s", traderID)
	c.JSON(http.StatusOK, gin.H{
		"message":   "Paper trading account reset successfully",
		"trader_id": traderID,
		"reset_at":  time.Now().UTC().UnixMilli(),
	})
}

// handlePaperSummary returns paper trading performance summary for a trader.
// GET /api/traders/:id/paper/summary
func (s *Server) handlePaperSummary(c *gin.Context) {
	userID := c.GetString("user_id")
	traderID := c.Param("id")

	// Verify trader belongs to user and get config (for initial balance)
	fullConfig, err := s.store.Trader().GetFullConfig(userID, traderID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Trader not found"})
		return
	}

	initialBalance := fullConfig.Trader.InitialBalance

	// Get current cash and locked margin (restart-safe computation)
	cash, lockedMargin, err := s.store.Position().GetPaperBalance(traderID, initialBalance)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute paper balance"})
		return
	}

	// Get all closed paper positions for statistics
	closedPositions, err := s.store.Position().GetClosedPositionsBySource(traderID, "paper", 0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query paper positions"})
		return
	}

	// Compute statistics
	totalTrades := len(closedPositions)
	winTrades := 0
	totalPnL := 0.0
	totalFee := 0.0
	for _, pos := range closedPositions {
		totalPnL += pos.RealizedPnL
		totalFee += pos.Fee
		if pos.RealizedPnL > 0 {
			winTrades++
		}
	}

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(winTrades) / float64(totalTrades) * 100
	}

	// Get open positions count and unrealized PnL
	openPositions, _ := s.store.Position().GetOpenPositionsBySource(traderID, "paper")
	openCount := len(openPositions)

	totalEquity := cash + lockedMargin // unrealized PnL not included here (would need live prices)
	returnPct := 0.0
	if initialBalance > 0 {
		returnPct = (totalEquity - initialBalance) / initialBalance * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"trader_id":       traderID,
		"initial_balance": initialBalance,
		"total_equity":    totalEquity,
		"available_cash":  cash,
		"locked_margin":   lockedMargin,
		"return_pct":      returnPct,
		"total_pnl":       totalPnL,
		"total_fee":       totalFee,
		"total_trades":    totalTrades,
		"win_trades":      winTrades,
		"win_rate":        winRate,
		"open_positions":  openCount,
	})
}
