package trader

import (
	"encoding/json"
	"fmt"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

// runCycle runs one trading cycle (using AI full decision-making)
func (at *AutoTrader) runCycle() error {
	at.callCount++

	logger.Info("\n" + strings.Repeat("=", 70) + "\n")
	logger.Infof("⏰ %s - AI decision cycle #%d", time.Now().Format("2006-01-02 15:04:05"), at.callCount)
	logger.Info(strings.Repeat("=", 70))

	// 0. Check if trader is stopped (early exit to prevent trades after Stop() is called)
	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		logger.Infof("⏹ Trader is stopped, aborting cycle #%d", at.callCount)
		return nil
	}


	// Create decision record
	record := &store.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
	}

	// 1. Check if trading needs to be stopped
	if time.Now().Before(at.stopUntil) {
		remaining := at.stopUntil.Sub(time.Now())
		logger.Infof("⏸ Risk control: Trading paused, remaining %.0f minutes", remaining.Minutes())
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Risk control paused, remaining %.0f minutes", remaining.Minutes())
		at.saveDecision(record)
		return nil
	}

	// 2. Reset daily P&L (reset every day)
	if time.Since(at.lastResetTime) > 24*time.Hour {
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		logger.Info("📅 Daily P&L reset")
	}

	// 4. Collect trading context
	ctx, err := at.buildTradingContext()
	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to build trading context: %v", err)
		at.saveDecision(record)
		return fmt.Errorf("failed to build trading context: %w", err)
	}

	// Save equity snapshot independently (decoupled from AI decision, used for drawing profit curve)
	// NOTE: Must be called BEFORE candidate coins check to ensure equity is always recorded
	at.saveEquitySnapshot(ctx)

	// If no candidate coins AND no open positions, log but do not error.
	// If there are open positions, AI still needs to manage them (close/stop-loss/etc.)
	// even when no new candidate coins are available.
	if len(ctx.CandidateCoins) == 0 && len(ctx.Positions) == 0 {
		logger.Infof("ℹ️  No candidate coins available and no open positions, skipping this cycle")
		record.Success = true // Not an error, just nothing to do
		record.ExecutionLog = append(record.ExecutionLog, "No candidate coins or open positions, cycle skipped")
		if len(ctx.DataFetchErrors) > 0 {
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("Data fetch errors: %v", ctx.DataFetchErrors))
			record.ErrorMessage = strings.Join(ctx.DataFetchErrors, "; ")
		}
		record.AccountState = store.AccountSnapshot{
			TotalBalance:          ctx.Account.TotalEquity,
			AvailableBalance:      ctx.Account.AvailableBalance,
			TotalUnrealizedProfit: ctx.Account.UnrealizedPnL,
			PositionCount:         ctx.Account.PositionCount,
			InitialBalance:        at.initialBalance,
		}
		at.saveDecision(record)
		return nil
	}

	logger.Info(strings.Repeat("=", 70))
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
	}

	logger.Infof("📊 Account equity: %.2f USDT | Available: %.2f USDT | Positions: %d",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount)

	// 5. Use strategy engine to call AI for decision
	logger.Infof("🤖 Requesting AI analysis and decision... [Strategy Engine]")
	aiDecision, err := kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, "balanced")
	at.saveBBMACDSignals(ctx)

	if aiDecision != nil && aiDecision.AIRequestDurationMs > 0 {
		record.AIRequestDurationMs = aiDecision.AIRequestDurationMs
		logger.Infof("⏱️ AI call duration: %.2f seconds", float64(record.AIRequestDurationMs)/1000)
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("AI call duration: %d ms", record.AIRequestDurationMs))
	}

	// Save chain of thought, decisions, and input prompt even if there's an error (for debugging)
	if aiDecision != nil {
		record.SystemPrompt = aiDecision.SystemPrompt // Save system prompt
		record.InputPrompt = aiDecision.UserPrompt
		record.CoTTrace = aiDecision.CoTTrace
		record.CotSummary = aiDecision.CoTSummary
		record.RawResponse = aiDecision.RawResponse // Save raw AI response for debugging
		if len(aiDecision.Decisions) > 0 {
			decisionJSON, _ := json.MarshalIndent(aiDecision.Decisions, "", "  ")
			record.DecisionJSON = string(decisionJSON)
		}
	}

	// Record AI charge (track cost regardless of decision outcome)
	if aiDecision != nil && at.store != nil {
		if chargeErr := at.store.AICharge().Record(at.id, at.aiModel, at.config.AIModel); chargeErr != nil {
			logger.Warnf("⚠️ Failed to record AI charge: %v", chargeErr)
		}
	}

	if err != nil {
		at.consecutiveAIFailures++
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to get AI decision [aiModel=%s aiProvider=%s]: %v", at.aiModel, at.config.AIModel, err)
		record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("AI call failed [aiModel=%s aiProvider=%s]: %v", at.aiModel, at.config.AIModel, err))

		// Activate safe mode after 3 consecutive failures
		if at.consecutiveAIFailures >= 3 && !at.safeMode {
			at.safeMode = true
			at.safeModeReason = fmt.Sprintf("AI failed %d consecutive times: %v", at.consecutiveAIFailures, err)
			logger.Errorf("🛡️ [%s] SAFE MODE ACTIVATED — AI failed %d times in a row. No new positions will be opened. Existing positions are protected with current stop-loss settings.",
				at.name, at.consecutiveAIFailures)
			logger.Errorf("🛡️ [%s] Reason: %v", at.name, err)
			logger.Errorf("🛡️ [%s] Action: Will keep trying AI each cycle. Safe mode auto-deactivates when AI recovers.", at.name)
		}

		// Print system prompt and AI chain of thought (output even with errors for debugging)
		if aiDecision != nil {
			logger.Info("\n" + strings.Repeat("=", 70) + "\n")
			logger.Infof("📋 System prompt (error case)")
			logger.Info(strings.Repeat("=", 70))
			logger.Info(aiDecision.SystemPrompt)
			logger.Info(strings.Repeat("=", 70))

			if aiDecision.CoTTrace != "" {
				logger.Info("\n" + strings.Repeat("-", 70) + "\n")
				logger.Info("💭 AI chain of thought analysis (error case):")
				logger.Info(strings.Repeat("-", 70))
				logger.Info(aiDecision.CoTTrace)
				logger.Info(strings.Repeat("-", 70))
			}
		}

		at.saveDecision(record)

		// In safe mode, don't return error — keep the loop running to retry next cycle
		if at.safeMode {
			logger.Warnf("🛡️ [%s] Safe mode: skipping this cycle, will retry in %v", at.name, at.config.ScanInterval)
			return nil
		}

		return fmt.Errorf("failed to get AI decision: %w", err)
	}

	// AI succeeded — reset failure counter and deactivate safe mode
	if at.consecutiveAIFailures > 0 {
		logger.Infof("✅ [%s] AI recovered after %d consecutive failures", at.name, at.consecutiveAIFailures)
	}
	at.consecutiveAIFailures = 0
	if at.safeMode {
		logger.Infof("🛡️ [%s] SAFE MODE DEACTIVATED — AI is working again. Resuming normal trading.", at.name)
		at.safeMode = false
		at.safeModeReason = ""
	}

	// // 5. Print system prompt
	// logger.Infof("\n" + strings.Repeat("=", 70))
	// logger.Infof("📋 System prompt [template: %s]", at.systemPromptTemplate)
	// logger.Info(strings.Repeat("=", 70))
	// logger.Info(decision.SystemPrompt)
	// logger.Infof(strings.Repeat("=", 70) + "\n")

	// 6. Print AI chain of thought
	// logger.Infof("\n" + strings.Repeat("-", 70))
	// logger.Info("💭 AI chain of thought analysis:")
	// logger.Info(strings.Repeat("-", 70))
	// logger.Info(decision.CoTTrace)
	// logger.Infof(strings.Repeat("-", 70) + "\n")

	// 7. Print AI decisions
	// logger.Infof("📋 AI decision list (%d items):\n", len(kernel.Decisions))
	// for i, d := range kernel.Decisions {
	//     logger.Infof("  [%d] %s: %s - %s", i+1, d.Symbol, d.Action, d.Reasoning)
	//     if d.Action == "open_long" || d.Action == "open_short" {
	//        logger.Infof("      Leverage: %dx | Position: %.2f USDT | Stop loss: %.4f | Take profit: %.4f",
	//           d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
	//     }
	// }
	logger.Info()
	logger.Info(strings.Repeat("-", 70))
	// 8. Sort decisions: ensure close positions first, then open positions (prevent position stacking overflow)
	logger.Info(strings.Repeat("-", 70))

	// 8. Sort decisions: ensure close positions first, then open positions (prevent position stacking overflow)
	sortedDecisions := sortDecisionsByPriority(aiDecision.Decisions)

	logger.Info("🔄 Execution order (optimized): Close positions first → Open positions later")
	for i, d := range sortedDecisions {
		logger.Infof("  [%d] %s %s", i+1, d.Symbol, d.Action)
	}
	logger.Info()

	// Check if trader is stopped before executing any decisions (prevent trades after Stop())
	at.isRunningMutex.RLock()
	running = at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		logger.Infof("⏹ Trader stopped before decision execution, aborting cycle #%d", at.callCount)
		return nil
	}

	// Safe mode: filter out open positions, only allow close/hold
	if at.safeMode {
		filtered := make([]kernel.Decision, 0)
		for _, d := range sortedDecisions {
			if d.Action == "open_long" || d.Action == "open_short" {
				logger.Warnf("🛡️ [%s] Safe mode: BLOCKED %s %s (no new positions allowed)", at.name, d.Action, d.Symbol)
				continue
			}
			filtered = append(filtered, d)
		}
		sortedDecisions = filtered
		if len(sortedDecisions) == 0 {
			logger.Infof("🛡️ [%s] Safe mode: all decisions were open positions, nothing to execute", at.name)
		}
	}

	// Execute decisions and record results
	for _, d := range sortedDecisions {
		// Check if trader is stopped before each decision (allow immediate stop during execution)
		at.isRunningMutex.RLock()
		running = at.isRunning
		at.isRunningMutex.RUnlock()
		if !running {
			logger.Infof("⏹ Trader stopped during decision execution, aborting remaining decisions")
			break
		}

		actionRecord := store.DecisionAction{
			Action:     d.Action,
			Symbol:     d.Symbol,
			Quantity:   0,
			Leverage:   d.Leverage,
			Price:      0,
			StopLoss:   d.StopLoss,
			TakeProfit: d.TakeProfit,
			Confidence: d.Confidence,
			Reasoning:  d.Reasoning,
			Timestamp:  time.Now().UTC(),
			Success:    false,
		}

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			logger.Infof("❌ Failed to execute decision (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("❌ %s %s failed: %v", d.Symbol, d.Action, err))
		} else {
			actionRecord.Success = true
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("✓ %s %s succeeded", d.Symbol, d.Action))

			// Save opening reasoning: try immediate DB write, fallback to cache + background retry
			if (d.Action == "open_long" || d.Action == "open_short") && at.store != nil {
				side := "LONG"
				if d.Action == "open_short" {
					side = "SHORT"
				}
				normalizedSymbol := market.Normalize(d.Symbol)
				pendingKey := normalizedSymbol + "_" + side
				// Prefer per-position reasoning from JSON, fallback to cycle-level CoTSummary
				reasoning := d.Reasoning
				if reasoning == "" && aiDecision != nil && aiDecision.CoTSummary != "" {
					reasoning = aiDecision.CoTSummary
				}
				if reasoning == "" {
					reasoning = fmt.Sprintf("[%s %s] reasoning not provided by AI", d.Symbol, d.Action)
				}

				// Try immediate write (works if position record already exists)
				if err := at.store.Position().UpdatePositionOpeningReasoning(at.id, normalizedSymbol, side, reasoning); err != nil {
					// Position not yet in DB (OrderSync hasn't run), start background retry
					at.pendingOpenReasoning[pendingKey] = reasoning
					logger.Infof("📝 [%s] Position not yet in DB for %s %s, starting background retry", at.name, d.Symbol, side)
					go func(traderID, symbol, s, r, pk string) {
						for i := 0; i < 12; i++ { // retry every 5s for up to 60s
							time.Sleep(5 * time.Second)
							if err := at.store.Position().UpdatePositionOpeningReasoning(traderID, symbol, s, r); err == nil {
								delete(at.pendingOpenReasoning, pk)
								logger.Infof("📝 [%s] Background flush: saved opening reasoning for %s %s (attempt %d)", at.name, symbol, s, i+1)
								return
							}
						}
						logger.Infof("⚠️ [%s] Background flush failed for %s %s after 60s", at.name, symbol, s)
					}(at.id, normalizedSymbol, side, reasoning, pendingKey)
				} else {
					logger.Infof("📝 [%s] Saved opening reasoning for %s %s to DB", at.name, d.Symbol, side)
				}
			}

			// Brief delay after successful execution
			time.Sleep(1 * time.Second)
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 9. Save decision record
	if err := at.saveDecision(record); err != nil {
		logger.Infof("⚠ Failed to save decision record: %v", err)
	}

	return nil
}

// buildTradingContext builds trading context
func (at *AutoTrader) buildTradingContext() (*kernel.Context, error) {
	// 1. Get account information
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to get account balance: %w", err)
	}

	// Get account fields
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0
	totalEquity := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Use totalEquity directly if provided by trader (more accurate)
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		totalEquity = eq
	} else {
		// Fallback: Total Equity = Wallet balance + Unrealized profit
		totalEquity = totalWalletBalance + totalUnrealizedProfit
	}

	// 2. Get position information
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	// Pre-fetch open/pending orders to find active SL/TP for each position
	type slTP struct{ sl, tp float64 }
	activeCondOrders := make(map[string]slTP)
	for _, pos := range positions {
		sym, _ := pos["symbol"].(string)
		sd, _ := pos["side"].(string)
		if sym == "" || sd == "" {
			continue
		}
		orders, oErr := at.trader.GetOpenOrders(sym)
		if oErr != nil {
			continue
		}
		key := sym + "_" + sd
		entry := activeCondOrders[key]
		for _, o := range orders {
			switch o.Type {
			case "STOP_MARKET", "STOP":
				if o.StopPrice > 0 {
					entry.sl = o.StopPrice
				}
			case "TAKE_PROFIT_MARKET", "TAKE_PROFIT":
				if o.StopPrice > 0 {
					entry.tp = o.StopPrice
				}
			}
		}
		activeCondOrders[key] = entry
	}

	var positionInfos []kernel.PositionInfo
	totalMarginUsed := 0.0

	// Current position key set (for cleaning up closed position records)
	currentPositionKeys := make(map[string]bool)

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // Short position quantity is negative, convert to positive
		}

		// Skip closed positions (quantity = 0), prevent "ghost positions" from being passed to AI
		if quantity == 0 {
			continue
		}

		// Skip positions recently closed by risk monitor — exchange API may still show them as active
		// for a short period after the close order fills.
		riskKey := symbol + "_" + side
		at.recentlyClosedByRiskMu.RLock()
		closedAt, wasRecentlyClosed := at.recentlyClosedByRisk[riskKey]
		at.recentlyClosedByRiskMu.RUnlock()
		if wasRecentlyClosed && time.Since(closedAt) < 10*time.Minute {
			logger.Infof("⚠️  [%s] Skipping stale position %s %s (risk-closed at %s, %.0fs ago)",
				at.name, symbol, side, closedAt.Format("15:04:05"), time.Since(closedAt).Seconds())
			continue
		}

		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		// Calculate margin used (estimated)
		leverage := 10 // Default value, should actually be fetched from position info
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed

		// Calculate P&L percentage (based on margin, considering leverage)
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		// Get position open time from exchange (preferred) or fallback to local tracking
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true

		var updateTime int64
		var accumulatedFee float64
		// Priority 1: Get from database (trader_positions table) - most accurate
		if at.store != nil {
			if dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, side); err == nil && dbPos != nil {
				if dbPos.EntryTime > 0 {
					updateTime = dbPos.EntryTime
				}
				accumulatedFee = dbPos.Fee
			}
		}
		// Priority 2: Get from exchange API (Bybit: createdTime, OKX: createdTime)
		if updateTime == 0 {
			if createdTime, ok := pos["createdTime"].(int64); ok && createdTime > 0 {
				updateTime = createdTime
			}
		}
		// Priority 3: Fallback to local tracking
		if updateTime == 0 {
			if _, exists := at.positionFirstSeenTime[posKey]; !exists {
				at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
			}
			updateTime = at.positionFirstSeenTime[posKey]
		}

		// Get peak profit rate for this position
		at.peakPnLCacheMutex.RLock()
		peakPnlPct := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		// Estimate closing fee and calculate net PnL
		positionNotional := quantity * markPrice
		estimatedCloseFee := positionNotional * 0.00055 // taker fee ~5.5bps
		netPnL := unrealizedPnl - accumulatedFee - estimatedCloseFee

		condEntry := activeCondOrders[posKey]

		positionInfos = append(positionInfos, kernel.PositionInfo{
			Symbol:            symbol,
			Side:              side,
			EntryPrice:        entryPrice,
			MarkPrice:         markPrice,
			Quantity:          quantity,
			Leverage:          leverage,
			UnrealizedPnL:     unrealizedPnl,
			UnrealizedPnLPct:  pnlPct,
			PeakPnLPct:        peakPnlPct,
			LiquidationPrice:  liquidationPrice,
			MarginUsed:        marginUsed,
			UpdateTime:        updateTime,
			AccumulatedFee:    accumulatedFee,
			EstimatedCloseFee: estimatedCloseFee,
			NetPnL:            netPnL,
			StopLossPrice:     condEntry.sl,
			TakeProfitPrice:   condEntry.tp,
		})
	}

	// Clean up closed position records
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}

	// Clean up stale entries from the risk-close cache (older than 10 minutes)
	at.recentlyClosedByRiskMu.Lock()
	for key, t := range at.recentlyClosedByRisk {
		if time.Since(t) > 10*time.Minute {
			delete(at.recentlyClosedByRisk, key)
		}
	}
	at.recentlyClosedByRiskMu.Unlock()

	// 3. Use strategy engine to get candidate coins (must have strategy engine)
	var candidateCoins []kernel.CandidateCoin
	var dataFetchErrors []string
	if at.strategyEngine == nil {
		logger.Infof("⚠️ [%s] No strategy engine configured, skipping candidate coins", at.name)
	} else {
		coins, err := at.strategyEngine.GetCandidateCoins()
		if err != nil {
			// Log warning but don't fail - equity snapshot should still be saved
			logger.Infof("⚠️ [%s] Failed to get candidate coins: %v (will use empty list)", at.name, err)
			dataFetchErrors = append(dataFetchErrors, fmt.Sprintf("GetCandidateCoins: %v", err))
		} else {
			candidateCoins = coins
			logger.Infof("📋 [%s] Strategy engine fetched candidate coins: %d", at.name, len(candidateCoins))
		}
	}

	// 4. Calculate total P&L
	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	// 5. Get leverage from strategy config
	strategyConfig := at.strategyEngine.GetConfig()
	btcEthLeverage := strategyConfig.RiskControl.BTCETHMaxLeverage
	altcoinLeverage := strategyConfig.RiskControl.AltcoinMaxLeverage
	logger.Infof("📋 [%s] Strategy leverage config: BTC/ETH=%dx, Altcoin=%dx", at.name, btcEthLeverage, altcoinLeverage)

	// 6. Build context
	ctx := &kernel.Context{
		CurrentTime:     time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		RuntimeMinutes:  int(time.Since(at.startTime).Minutes()),
		CallCount:       at.callCount,
		BTCETHLeverage:  btcEthLeverage,
		AltcoinLeverage: altcoinLeverage,
		Account: kernel.AccountInfo{
			TotalEquity:      totalEquity,
			AvailableBalance: availableBalance,
			UnrealizedPnL:    totalUnrealizedProfit,
			TotalPnL:         totalPnL,
			TotalPnLPct:      totalPnLPct,
			MarginUsed:       totalMarginUsed,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positionInfos),
		},
		Positions:       positionInfos,
		CandidateCoins:  candidateCoins,
		DataFetchErrors: dataFetchErrors,
	}

	// Inject pending drawdown alerts (AI-decide mode) and clear the queue
	at.pendingDrawdownAlertsMu.Lock()
	if len(at.pendingDrawdownAlerts) > 0 {
		ctx.DrawdownAlerts = make([]kernel.DrawdownAlert, len(at.pendingDrawdownAlerts))
		copy(ctx.DrawdownAlerts, at.pendingDrawdownAlerts)
		at.pendingDrawdownAlerts = at.pendingDrawdownAlerts[:0]
		logger.Infof("📋 [%s] Injected %d drawdown alert(s) into AI context", at.name, len(ctx.DrawdownAlerts))
	}
	at.pendingDrawdownAlertsMu.Unlock()

	// 7. Add recent closed trades (if store is available)
	if at.store != nil && strategyConfig.ShouldIncludeHistoricalContext() {
		// Get recent 10 closed trades for AI context
		recentTrades, err := at.store.Position().GetRecentTrades(at.id, 10)
		if err != nil {
			logger.Infof("⚠️ [%s] Failed to get recent trades: %v", at.name, err)
		} else {
			logger.Infof("📊 [%s] Found %d recent closed trades for AI context", at.name, len(recentTrades))
			for _, trade := range recentTrades {
				// Convert Unix timestamps to formatted strings for AI readability
				entryTimeStr := ""
				if trade.EntryTime > 0 {
					entryTimeStr = time.Unix(trade.EntryTime, 0).UTC().Format("01-02 15:04 UTC")
				}
				exitTimeStr := ""
				if trade.ExitTime > 0 {
					exitTimeStr = time.Unix(trade.ExitTime, 0).UTC().Format("01-02 15:04 UTC")
				}

				ctx.RecentOrders = append(ctx.RecentOrders, kernel.RecentOrder{
					Symbol:       trade.Symbol,
					Side:         trade.Side,
					EntryPrice:   trade.EntryPrice,
					ExitPrice:    trade.ExitPrice,
					RealizedPnL:  trade.RealizedPnL,
					PnLPct:       trade.PnLPct,
					EntryTime:    entryTimeStr,
					ExitTime:     exitTimeStr,
					HoldDuration: trade.HoldDuration,
				})
			}
		}
		// Get trading statistics for AI context
		stats, err := at.store.Position().GetFullStats(at.id)
		if err != nil {
			logger.Infof("⚠️ [%s] Failed to get trading stats: %v", at.name, err)
		} else if stats == nil {
			logger.Infof("⚠️ [%s] GetFullStats returned nil", at.name)
		} else if stats.TotalTrades == 0 {
			logger.Infof("⚠️ [%s] GetFullStats returned 0 trades (traderID=%s)", at.name, at.id)
		} else {
			ctx.TradingStats = &kernel.TradingStats{
				TotalTrades:    stats.TotalTrades,
				WinRate:        stats.WinRate,
				ProfitFactor:   stats.ProfitFactor,
				SharpeRatio:    stats.SharpeRatio,
				TotalPnL:       stats.TotalPnL,
				AvgWin:         stats.AvgWin,
				AvgLoss:        stats.AvgLoss,
				MaxDrawdownPct: stats.MaxDrawdownPct,
			}
			logger.Infof("📈 [%s] Trading stats: %d trades, %.1f%% win rate, PF=%.2f, Sharpe=%.2f, DD=%.1f%%",
				at.name, stats.TotalTrades, stats.WinRate, stats.ProfitFactor, stats.SharpeRatio, stats.MaxDrawdownPct)
		}
	} else {
		logger.Infof("⚠️ [%s] Store is nil, cannot get recent trades", at.name)
	}

	// 7b. Load position memories + flush pending reasoning
	if at.store != nil {
		for _, pos := range positionInfos {
			dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, pos.Symbol, strings.ToUpper(pos.Side))
			if err != nil || dbPos == nil {
				continue
			}
			memory := kernel.PositionMemory{
				Symbol: pos.Symbol,
				Side:   pos.Side,
			}

			// Flush pending opening reasoning to DB
			pendingKey := market.Normalize(pos.Symbol) + "_" + strings.ToUpper(pos.Side)
			if reasoning, ok := at.pendingOpenReasoning[pendingKey]; ok && dbPos.OpeningReasoning == "" {
				if err := at.store.Position().UpdatePositionOpeningReasoning(
					at.id, dbPos.Symbol, strings.ToUpper(pos.Side), reasoning,
				); err != nil {
					logger.Infof("⚠️ [%s] Failed to flush opening reasoning for %s: %v", at.name, pos.Symbol, err)
				} else {
					logger.Infof("📝 [%s] Flushed opening reasoning for %s %s to DB", at.name, pos.Symbol, pos.Side)
					dbPos.OpeningReasoning = reasoning
				}
				delete(at.pendingOpenReasoning, pendingKey)
			}

			memory.OpeningReasoning = dbPos.OpeningReasoning
			memory.LastReviewSummary = dbPos.LastReviewSummary

			if memory.OpeningReasoning != "" || memory.CotSummary != "" || memory.LastReviewSummary != "" {
				ctx.PositionMemories = append(ctx.PositionMemories, memory)
			}
		}
		if len(ctx.PositionMemories) > 0 {
			logger.Infof("🧠 [%s] Loaded position memories for %d open positions", at.name, len(ctx.PositionMemories))
		}
	}

	// 8. Get quantitative data (if enabled in strategy config)
	if strategyConfig.Indicators.EnableQuantData {
		// Collect symbols to query (candidate coins + position coins)
		symbolsToQuery := make(map[string]bool)
		for _, coin := range candidateCoins {
			symbolsToQuery[coin.Symbol] = true
		}
		for _, pos := range positionInfos {
			symbolsToQuery[pos.Symbol] = true
		}

		symbols := make([]string, 0, len(symbolsToQuery))
		for sym := range symbolsToQuery {
			symbols = append(symbols, sym)
		}

		logger.Infof("📊 [%s] Fetching quantitative data for %d symbols...", at.name, len(symbols))
		ctx.QuantDataMap = at.strategyEngine.FetchQuantDataBatch(symbols)
		logger.Infof("📊 [%s] Successfully fetched quantitative data for %d symbols", at.name, len(ctx.QuantDataMap))
	}

	// 9. Get OI ranking data (market-wide position changes)
	if strategyConfig.Indicators.EnableOIRanking {
		logger.Infof("📊 [%s] Fetching OI ranking data...", at.name)
		ctx.OIRankingData = at.strategyEngine.FetchOIRankingData()
		if ctx.OIRankingData != nil {
			logger.Infof("📊 [%s] OI ranking data ready: %d top, %d low positions",
				at.name, len(ctx.OIRankingData.TopPositions), len(ctx.OIRankingData.LowPositions))
		}
	}

	// 10. Get NetFlow ranking data (market-wide fund flow)
	if strategyConfig.Indicators.EnableNetFlowRanking {
		logger.Infof("💰 [%s] Fetching NetFlow ranking data...", at.name)
		ctx.NetFlowRankingData = at.strategyEngine.FetchNetFlowRankingData()
		if ctx.NetFlowRankingData != nil {
			logger.Infof("💰 [%s] NetFlow ranking data ready: inst_in=%d, inst_out=%d",
				at.name, len(ctx.NetFlowRankingData.InstitutionFutureTop), len(ctx.NetFlowRankingData.InstitutionFutureLow))
		}
	}

	// 11. Get Price ranking data (market-wide gainers/losers)
	if strategyConfig.Indicators.EnablePriceRanking {
		logger.Infof("📈 [%s] Fetching Price ranking data...", at.name)
		ctx.PriceRankingData = at.strategyEngine.FetchPriceRankingData()
		if ctx.PriceRankingData != nil {
			logger.Infof("📈 [%s] Price ranking data ready for %d durations",
				at.name, len(ctx.PriceRankingData.Durations))
		}
	}

	// 12. Get Long/Short ratio ranking data (market-wide sentiment)
	if strategyConfig.Indicators.EnableLongShortRanking {
		logger.Infof("📊 [%s] Fetching Long/Short ratio ranking data...", at.name)
		ctx.LongShortRankingData = at.strategyEngine.FetchLongShortRankingData()
		if ctx.LongShortRankingData != nil {
			logger.Infof("📊 [%s] Long/Short ranking data ready: %d entries",
				at.name, len(ctx.LongShortRankingData))
		}
	}

	// 13. Get Liquidation ranking data (market-wide forced liquidations)
	if strategyConfig.Indicators.EnableLiquidationRanking {
		logger.Infof("💥 [%s] Fetching Liquidation ranking data...", at.name)
		ctx.LiquidationRankingData = at.strategyEngine.FetchLiquidationRankingData()
		if ctx.LiquidationRankingData != nil {
			logger.Infof("💥 [%s] Liquidation ranking data ready: %d entries",
				at.name, len(ctx.LiquidationRankingData))
		}
	}

	// 14. Load external data sources (Kronos, etc.)
	if len(strategyConfig.Indicators.ExternalDataSources) > 0 {
		externalRaw, err := at.strategyEngine.FetchExternalData()
		if err != nil {
			logger.Infof("⚠️ [%s] Failed to fetch external data sources: %v", at.name, err)
		} else {
			for _, src := range strategyConfig.Indicators.ExternalDataSources {
				data, ok := externalRaw[src.Name]
				if !ok {
					continue
				}
				dataBytes, _ := json.Marshal(data)
				label := src.Name
				if src.ContextLabel != "" {
					label = src.ContextLabel
				}
				ctx.ExternalDataItems = append(ctx.ExternalDataItems, kernel.ExternalDataItem{
					Label:       label,
					Description: src.Description,
					Data:        string(dataBytes),
				})
			}
			if len(ctx.ExternalDataItems) > 0 {
				logger.Infof("🔌 [%s] Loaded %d external data items for AI context", at.name, len(ctx.ExternalDataItems))
			}
		}
	}

	return ctx, nil
}

// sortDecisionsByPriority sorts decisions: close positions first, then open positions, finally hold/wait
// This avoids position stacking overflow when changing positions
func sortDecisionsByPriority(decisions []kernel.Decision) []kernel.Decision {
	if len(decisions) <= 1 {
		return decisions
	}

	// Define priority
	getActionPriority := func(action string) int {
		switch action {
		case "close_long", "close_short":
			return 1 // Highest priority: close positions first
		case "open_long", "open_short":
			return 2 // Second priority: open positions later
		case "hold", "wait":
			return 3 // Lowest priority: wait
		default:
			return 999 // Unknown actions at the end
		}
	}

	// Copy decision list
	sorted := make([]kernel.Decision, len(decisions))
	copy(sorted, decisions)

	// Sort by priority
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getActionPriority(sorted[i].Action) > getActionPriority(sorted[j].Action) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

