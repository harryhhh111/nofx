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

const defaultEstimatedCloseFeeRate = 0.00055

// runCycle runs one deterministic setup, risk, and execution cycle.
func (at *AutoTrader) runCycle() error {
	at.callCount++

	logger.Info("\n" + strings.Repeat("=", 70) + "\n")
	logger.Infof("⏰ %s - trading decision cycle #%d", time.Now().Format("2006-01-02 15:04:05"), at.callCount)
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
	record.AccountState = store.AccountSnapshot{
		TotalBalance:          ctx.Account.TotalEquity,
		AvailableBalance:      ctx.Account.AvailableBalance,
		TotalUnrealizedProfit: ctx.Account.UnrealizedPnL,
		PositionCount:         ctx.Account.PositionCount,
		MarginUsedPct:         ctx.Account.MarginUsedPct,
		InitialBalance:        at.initialBalance,
	}
	record.Positions = make([]store.PositionSnapshot, 0, len(ctx.Positions))
	for _, pos := range ctx.Positions {
		record.Positions = append(record.Positions, store.PositionSnapshot{
			Symbol:           pos.Symbol,
			Side:             pos.Side,
			PositionAmt:      pos.Quantity,
			EntryPrice:       pos.EntryPrice,
			MarkPrice:        pos.MarkPrice,
			UnrealizedProfit: pos.UnrealizedPnL,
			Leverage:         float64(pos.Leverage),
			LiquidationPrice: pos.LiquidationPrice,
		})
	}

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
		at.saveDecision(record)
		return nil
	}

	logger.Info(strings.Repeat("=", 70))
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
	}

	logger.Infof("📊 Account equity: %.2f USDT | Available: %.2f USDT | Positions: %d",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount)

	// 5. Run the deterministic strategy engine. AI is not in this synchronous path.
	logger.Infof("Running deterministic setup and risk evaluation... [Strategy Engine]")
	cycleDecision, err := kernel.EvaluateStrategy(ctx, at.strategyEngine, "balanced")
	at.saveBBMACDSignals(ctx)

	if cycleDecision != nil && cycleDecision.AIRequestDurationMs > 0 {
		record.AIRequestDurationMs = cycleDecision.AIRequestDurationMs
		logger.Infof("⏱️ Decision pipeline duration: %.2f seconds", float64(record.AIRequestDurationMs)/1000)
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("Decision pipeline duration: %d ms", record.AIRequestDurationMs))
	}

	// Save chain of thought, decisions, and input prompt even if there's an error (for debugging)
	if cycleDecision != nil {
		record.SystemPrompt = cycleDecision.SystemPrompt // Save system prompt
		record.InputPrompt = cycleDecision.UserPrompt
		record.CoTTrace = cycleDecision.CoTTrace
		record.CotSummary = cycleDecision.CoTSummary
		if cycleDecision.UserDecisionSummary != nil && cycleDecision.UserDecisionSummary.Headline != "" {
			record.CotSummary = cycleDecision.UserDecisionSummary.Headline
		}
		record.RawResponse = cycleDecision.RawResponse // Save raw AI response for debugging
		flowTrace := struct {
			Decisions           []kernel.Decision               `json:"decisions"`
			Signals             []kernel.CandidateSignal        `json:"signals"`
			SetupEvaluations    []kernel.SetupEvaluationTrace   `json:"setup_evaluations"`
			EvidenceEvaluations []kernel.ScoringEvaluationTrace `json:"evidence_evaluations"`
			RuleEvaluations     []kernel.RuleEvaluationTrace    `json:"rule_evaluations"`
			Reviews             []kernel.SignalReviewDecision   `json:"reviews,omitempty"`
			Risk                *kernel.RiskGateResult          `json:"risk,omitempty"`
			MarketContext       *kernel.MarketContext           `json:"market_context,omitempty"`
			InputAudit          *kernel.TradingInputAudit       `json:"input_audit,omitempty"`
			UserDecisionSummary *kernel.UserDecisionSummary     `json:"user_decision_summary,omitempty"`
		}{
			Decisions:           cycleDecision.Decisions,
			Signals:             cycleDecision.Signals,
			SetupEvaluations:    cycleDecision.SetupEvaluations,
			EvidenceEvaluations: cycleDecision.EvidenceEvaluations,
			RuleEvaluations:     cycleDecision.RuleEvaluations,
			Reviews:             cycleDecision.Reviews,
			Risk:                cycleDecision.Risk,
			MarketContext:       cycleDecision.MarketContext,
			InputAudit:          cycleDecision.InputAudit,
			UserDecisionSummary: cycleDecision.UserDecisionSummary,
		}
		if decisionJSON, jsonErr := json.MarshalIndent(flowTrace, "", "  "); jsonErr == nil {
			record.DecisionJSON = string(decisionJSON)
		}
	}

	if err != nil {
		at.consecutiveDecisionFailures++
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Trading decision pipeline failed: %v", err)
		record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("Trading decision pipeline failed: %v", err))

		// Activate safe mode after 3 consecutive failures
		if at.consecutiveDecisionFailures >= 3 && !at.safeMode {
			at.safeMode = true
			at.safeModeReason = fmt.Sprintf("decision pipeline failed %d consecutive times: %v", at.consecutiveDecisionFailures, err)
			logger.Errorf("🛡️ [%s] SAFE MODE ACTIVATED — decision pipeline failed %d times in a row. No new positions will be opened. Existing positions are protected with current stop-loss settings.",
				at.name, at.consecutiveDecisionFailures)
			logger.Errorf("🛡️ [%s] Reason: %v", at.name, err)
			logger.Errorf("🛡️ [%s] Action: Will retry the deterministic pipeline each cycle. Safe mode auto-deactivates after recovery.", at.name)
		}

		// Print system prompt and AI chain of thought (output even with errors for debugging)
		if cycleDecision != nil {
			logger.Info("\n" + strings.Repeat("=", 70) + "\n")
			logger.Infof("📋 System prompt (error case)")
			logger.Info(strings.Repeat("=", 70))
			logger.Info(cycleDecision.SystemPrompt)
			logger.Info(strings.Repeat("=", 70))

			if cycleDecision.CoTTrace != "" {
				logger.Info("\n" + strings.Repeat("-", 70) + "\n")
				logger.Info("💭 AI chain of thought analysis (error case):")
				logger.Info(strings.Repeat("-", 70))
				logger.Info(cycleDecision.CoTTrace)
				logger.Info(strings.Repeat("-", 70))
			}
		}

		at.saveDecision(record)

		// In safe mode, don't return error — keep the loop running to retry next cycle
		if at.safeMode {
			logger.Warnf("🛡️ [%s] Safe mode: skipping this cycle, will retry in %v", at.name, at.config.ScanInterval)
			return nil
		}

		return fmt.Errorf("trading decision pipeline failed: %w", err)
	}

	// Decision pipeline succeeded — reset failure counter and deactivate safe mode.
	if at.consecutiveDecisionFailures > 0 {
		logger.Infof("✅ [%s] Decision pipeline recovered after %d consecutive failures", at.name, at.consecutiveDecisionFailures)
	}
	at.consecutiveDecisionFailures = 0
	if at.safeMode {
		logger.Infof("🛡️ [%s] SAFE MODE DEACTIVATED — decision pipeline is healthy again. Resuming normal trading.", at.name)
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
	sortedDecisions := sortDecisionsByPriority(cycleDecision.Decisions)

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
			Action:                 d.Action,
			Symbol:                 d.Symbol,
			Quantity:               0,
			Leverage:               d.Leverage,
			Price:                  0,
			StopLoss:               d.StopLoss,
			TakeProfit:             d.TakeProfit,
			StopLossSource:         d.StopLossSource,
			StopLossTimeframe:      d.StopLossTF,
			StopLossAnchor:         d.StopLossAnchor,
			StopLossPolicy:         d.StopLossPolicy,
			TakeProfitSource:       d.TakeProfitSource,
			TakeProfitTimeframe:    d.TakeProfitTF,
			TakeProfitAnchor:       d.TakeProfitAnchor,
			TakeProfitPolicy:       d.TakeProfitPolicy,
			TakeProfitCandidates:   d.TakeProfitCandidates,
			TakeProfitMinRR:        d.TakeProfitMinRR,
			TakeProfitMinATRs:      d.TakeProfitMinATRs,
			TakeProfitSelectedRR:   d.TakeProfitSelectedRR,
			TakeProfitSelectedATRs: d.TakeProfitSelectedATRs,
			TakeProfitQualified:    d.TakeProfitQualified,
			NearestTakeProfit:      d.NearestTakeProfit,
			NearestTakeProfitRR:    d.NearestTakeProfitRR,
			NearestTakeProfitATRs:  d.NearestTakeProfitATRs,
			ProtectiveATR:          d.ProtectiveATR,
			ProtectiveATRTF:        d.ProtectiveATRTF,
			ProtectiveATRBuffer:    d.ProtectiveATRBuffer,
			ProtectiveRiskReward:   d.ProtectiveRiskReward,
			ExecutionRiskReward:    d.ExecutionRiskReward,
			Confidence:             d.Confidence,
			Reasoning:              d.Reasoning,
			SignalID:               d.SignalID,
			RuleID:                 d.RuleID,
			Setup:                  d.Setup,
			Version:                d.StrategyVersion,
			Timestamp:              time.Now().UTC(),
			Success:                false,
		}

		if skipped, reason, err := at.shouldSkipStopLossCooldownDecision(&d); err != nil {
			logger.Warnf("⚠️ [%s] Failed to pre-check stop-loss cooldown for %s %s: %v", at.name, d.Symbol, d.Action, err)
		} else if skipped {
			logger.Warnf("🛑 [%s] Risk cooldown: skipped %s %s: %s", at.name, d.Symbol, d.Action, reason)
			actionRecord.Success = true
			if actionRecord.Reasoning != "" {
				actionRecord.Reasoning = fmt.Sprintf("[SKIPPED_RISK_COOLDOWN] %s | Original: %s", reason, actionRecord.Reasoning)
			} else {
				actionRecord.Reasoning = fmt.Sprintf("[SKIPPED_RISK_COOLDOWN] %s", reason)
			}
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("🛑 %s %s skipped by risk cooldown: %s", d.Symbol, d.Action, reason))
			record.Decisions = append(record.Decisions, actionRecord)
			continue
		}

		if skipped, reason, err := at.shouldSkipDuplicateOpenDecision(&d); err != nil {
			logger.Warnf("⚠️ [%s] Failed to pre-check duplicate open decision for %s %s: %v", at.name, d.Symbol, d.Action, err)
		} else if skipped {
			logger.Infof("↩️ [%s] Skipped %s %s: %s", at.name, d.Symbol, d.Action, reason)
			actionRecord.Success = true
			if actionRecord.Reasoning != "" {
				actionRecord.Reasoning = fmt.Sprintf("[SKIPPED] %s | Original: %s", reason, actionRecord.Reasoning)
			} else {
				actionRecord.Reasoning = fmt.Sprintf("[SKIPPED] %s", reason)
			}
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("↩️ %s %s skipped: %s", d.Symbol, d.Action, reason))
			record.Decisions = append(record.Decisions, actionRecord)
			continue
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
				pending := pendingOpeningReasoning{
					EntryNotBefore: d.SignalGeneratedAt,
				}
				// Prefer per-position reasoning from JSON, fallback to cycle-level CoTSummary
				pending.Reasoning = d.Reasoning
				if pending.Reasoning == "" && cycleDecision != nil && cycleDecision.CoTSummary != "" {
					pending.Reasoning = cycleDecision.CoTSummary
				}
				if pending.Reasoning == "" {
					pending.Reasoning = fmt.Sprintf("[%s %s] reasoning not provided by AI", d.Symbol, d.Action)
				}

				// Try immediate write (works if position record already exists)
				if err := at.store.Position().UpdatePositionOpeningReasoning(at.id, normalizedSymbol, side, pending.EntryNotBefore, pending.Reasoning); err != nil {
					// Position not yet in DB (OrderSync hasn't run), start background retry
					at.pendingOpenReasoningMu.Lock()
					at.pendingOpenReasoning[pendingKey] = pending
					at.pendingOpenReasoningMu.Unlock()
					logger.Infof("📝 [%s] Position not yet in DB for %s %s, starting background retry", at.name, d.Symbol, side)
					go func(traderID, symbol, s, pk string, expected pendingOpeningReasoning) {
						for i := 0; i < 12; i++ { // retry every 5s for up to 60s
							time.Sleep(5 * time.Second)
							if err := at.store.Position().UpdatePositionOpeningReasoning(traderID, symbol, s, expected.EntryNotBefore, expected.Reasoning); err == nil {
								at.pendingOpenReasoningMu.Lock()
								if at.pendingOpenReasoning[pk] == expected {
									delete(at.pendingOpenReasoning, pk)
								}
								at.pendingOpenReasoningMu.Unlock()
								logger.Infof("📝 [%s] Background flush: saved opening reasoning for %s %s (attempt %d)", at.name, symbol, s, i+1)
								return
							}
						}
						logger.Infof("⚠️ [%s] Background flush failed for %s %s after 60s", at.name, symbol, s)
					}(at.id, normalizedSymbol, side, pendingKey, pending)
				} else {
					logger.Infof("📝 [%s] Saved opening reasoning for %s %s to DB", at.name, d.Symbol, side)
				}
			}
			if (d.Action == "open_long" || d.Action == "open_short") && at.store != nil {
				at.saveOpeningProtectiveMetadata(&d)
			}

			// Brief delay after successful execution
			time.Sleep(1 * time.Second)
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 9. Save decision record
	if err := at.saveDecision(record); err != nil {
		logger.Infof("⚠ Failed to save decision record: %v", err)
	} else {
		at.saveOpeningSignalMetadata(record)
		at.saveSignalCalibrationSamples(cycleDecision, record)
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
		at.recordPositionExcursion(symbol, side, markPrice, unrealizedPnl, pnlPct)

		// Get position open time from exchange (preferred) or fallback to local tracking
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true

		var updateTime int64
		var accumulatedFee float64
		var dbPosition *store.TraderPosition
		// Priority 1: Get from database (trader_positions table) - most accurate
		if at.store != nil {
			normalizedSymbol := market.Normalize(symbol)
			normalizedSide := strings.ToUpper(side)
			if dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, normalizedSide); err == nil && dbPos != nil {
				dbPosition = dbPos
				pendingKey := normalizedSymbol + "_" + normalizedSide
				at.pendingOpenReasoningMu.Lock()
				pending, hasPendingReasoning := at.pendingOpenReasoning[pendingKey]
				at.pendingOpenReasoningMu.Unlock()
				if hasPendingReasoning && dbPos.OpeningReasoning == "" {
					if err := at.store.Position().UpdatePositionOpeningReasoning(at.id, dbPos.Symbol, strings.ToUpper(side), pending.EntryNotBefore, pending.Reasoning); err == nil {
						dbPos.OpeningReasoning = pending.Reasoning
						at.pendingOpenReasoningMu.Lock()
						if at.pendingOpenReasoning[pendingKey] == pending {
							delete(at.pendingOpenReasoning, pendingKey)
						}
						at.pendingOpenReasoningMu.Unlock()
					}
				}
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
		at.resetPeakPnLForPosition(posKey, updateTime)

		// Get peak profit rate for this position
		at.peakPnLCacheMutex.RLock()
		peakPnlPct := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		// Use actual Aster fills and current account taker rate when available.
		positionNotional := quantity * markPrice
		feeSnapshot, feeErr := at.currentPositionFees(symbol, side, positionNotional, updateTime, dbPosition)
		if feeErr != nil {
			logger.Warnf("⚠️ [%s] Position fee lookup for %s %s: %v", at.name, symbol, side, feeErr)
		}
		accumulatedFee = feeSnapshot.AccumulatedFee
		estimatedCloseFee := feeSnapshot.EstimatedCloseFee
		netPnL := unrealizedPnl - accumulatedFee - estimatedCloseFee

		condEntry := activeCondOrders[posKey]

		positionInfo := kernel.PositionInfo{
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
			FeeSource:         feeSnapshot.Source,
			NetPnL:            netPnL,
			StopLossPrice:     condEntry.sl,
			TakeProfitPrice:   condEntry.tp,
		}
		if dbPosition != nil {
			positionInfo.OpeningSignalID = dbPosition.OpeningSignalID
			positionInfo.OpeningEpisodeID = dbPosition.OpeningEpisodeID
			positionInfo.OpeningRuleID = dbPosition.OpeningRuleID
			positionInfo.OpeningSetup = dbPosition.OpeningSetup
			positionInfo.OpeningThesisJSON = dbPosition.OpeningThesisJSON
			positionInfo.StrategyVersion = dbPosition.StrategyVersion
			positionInfo.OpeningReasoning = dbPosition.OpeningReasoning
			positionInfo.LastReviewSummary = dbPosition.LastReviewSummary
			positionInfo.StopLossAnchor = dbPosition.StopLossAnchor
			positionInfo.StopLossSource = dbPosition.StopLossSource
			positionInfo.StopLossTimeframe = dbPosition.StopLossTimeframe
			positionInfo.TakeProfitAnchor = dbPosition.TakeProfitAnchor
			positionInfo.TakeProfitSource = dbPosition.TakeProfitSource
			positionInfo.TakeProfitTF = dbPosition.TakeProfitTimeframe
		}
		positionInfos = append(positionInfos, positionInfo)
	}

	// Clean up closed position records
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}
	at.clearInactivePeakPnLCache(currentPositionKeys)

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
	strategyConfig.NormalizeForExecution()
	btcEthLeverage := strategyConfig.RiskControl.BTCETHMaxLeverage
	altcoinLeverage := strategyConfig.RiskControl.AltcoinMaxLeverage
	logger.Infof("📋 [%s] Strategy leverage config: BTC/ETH=%dx, Altcoin=%dx", at.name, btcEthLeverage, altcoinLeverage)

	// 6. Build context
	ctx := &kernel.Context{
		CurrentTime:     time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		RuntimeMinutes:  int(time.Since(at.startTime).Minutes()),
		CallCount:       at.callCount,
		Exchange:        at.exchange,
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

	// 12. Load external data sources (Kronos, etc.)
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

const (
	stopLossSymbolCooldown      = 2 * time.Hour
	stopLossTraderCooldown      = 6 * time.Hour
	stopLossTraderCooldownCount = 3
)

func (at *AutoTrader) shouldSkipStopLossCooldownDecision(decision *kernel.Decision) (bool, string, error) {
	if decision == nil || at.store == nil {
		return false, "", nil
	}
	if decision.Action != "open_long" && decision.Action != "open_short" {
		return false, "", nil
	}

	closed, err := at.store.Position().GetClosedPositions(at.id, 50)
	if err != nil {
		return false, "", err
	}
	if len(closed) == 0 {
		return false, "", nil
	}

	nowMs := time.Now().UTC().UnixMilli()
	targetSymbol := market.Normalize(decision.Symbol)
	symbolCooldownMs := int64(stopLossSymbolCooldown / time.Millisecond)
	traderCooldownMs := int64(stopLossTraderCooldown / time.Millisecond)

	recentStopLosses := 0
	var latestSymbolStop *store.TraderPosition
	for _, pos := range closed {
		if pos == nil || !strings.EqualFold(pos.CloseReason, "stop_loss") || pos.ExitTime <= 0 {
			continue
		}
		ageMs := nowMs - pos.ExitTime
		if ageMs < 0 {
			ageMs = 0
		}
		if ageMs <= traderCooldownMs {
			recentStopLosses++
		}
		if market.Normalize(pos.Symbol) == targetSymbol && ageMs <= symbolCooldownMs {
			if latestSymbolStop == nil || pos.ExitTime > latestSymbolStop.ExitTime {
				latestSymbolStop = pos
			}
		}
	}

	if latestSymbolStop != nil {
		remaining := time.Duration(symbolCooldownMs-(nowMs-latestSymbolStop.ExitTime)) * time.Millisecond
		if remaining < 0 {
			remaining = 0
		}
		return true, fmt.Sprintf("%s hit stop-loss %.0fm ago; symbol cooldown remaining %.0fm",
			targetSymbol,
			time.Duration(nowMs-latestSymbolStop.ExitTime).Minutes(),
			remaining.Minutes(),
		), nil
	}

	if recentStopLosses >= stopLossTraderCooldownCount {
		return true, fmt.Sprintf("%d stop-losses within %.0fh; trader cooldown active",
			recentStopLosses,
			stopLossTraderCooldown.Hours(),
		), nil
	}

	return false, "", nil
}

func (at *AutoTrader) shouldSkipDuplicateOpenDecision(decision *kernel.Decision) (bool, string, error) {
	if decision == nil {
		return false, "", nil
	}

	side := ""
	switch decision.Action {
	case "open_long":
		side = "long"
	case "open_short":
		side = "short"
	default:
		return false, "", nil
	}

	positions, err := at.trader.GetPositions()
	if err != nil {
		return false, "", err
	}

	targetSymbol := market.Normalize(decision.Symbol)
	for _, pos := range positions {
		if qty, ok := matchingOpenPositionQuantity(pos, targetSymbol, side); ok {
			return true, fmt.Sprintf("existing %s position is still open (qty=%.8f)", side, qty), nil
		}
	}

	return false, "", nil
}

func matchingOpenPositionQuantity(pos map[string]interface{}, normalizedSymbol, side string) (float64, bool) {
	posSymbol, _ := pos["symbol"].(string)
	posSide, _ := pos["side"].(string)
	if market.Normalize(posSymbol) != normalizedSymbol || !strings.EqualFold(posSide, side) {
		return 0, false
	}

	qty, ok := orderResultNumber(pos, "positionAmt")
	if !ok {
		qty, _ = orderResultNumber(pos, "size")
	}
	if qty < 0 {
		qty = -qty
	}
	return qty, qty > 0
}

func estimateCloseFee(positionNotional, accumulatedFee float64, dbPosition *store.TraderPosition) float64 {
	if positionNotional <= 0 {
		return 0
	}
	rate := defaultEstimatedCloseFeeRate
	if dbPosition != nil && accumulatedFee > 0 {
		entryQuantity := dbPosition.EntryQuantity
		if entryQuantity <= 0 {
			entryQuantity = dbPosition.Quantity
		}
		entryNotional := dbPosition.EntryPrice * entryQuantity
		if entryNotional > 0 {
			observedRate := accumulatedFee / entryNotional
			if observedRate > rate && observedRate <= 0.01 {
				rate = observedRate
			}
		}
	}
	return positionNotional * rate
}

func (at *AutoTrader) saveOpeningProtectiveMetadata(decision *kernel.Decision) {
	if at.store == nil || decision == nil {
		return
	}
	if decision.StopLossSource == "" && decision.TakeProfitSource == "" && decision.ProtectiveATR <= 0 {
		return
	}

	side := "LONG"
	if decision.Action == "open_short" {
		side = "SHORT"
	}
	symbol := market.Normalize(decision.Symbol)
	meta := store.PositionProtectiveLevelMetadata{
		EntryNotBefore:         decision.SignalGeneratedAt,
		StopLossSource:         decision.StopLossSource,
		StopLossTimeframe:      decision.StopLossTF,
		StopLossAnchor:         decision.StopLossAnchor,
		StopLossPolicy:         decision.StopLossPolicy,
		TakeProfitSource:       decision.TakeProfitSource,
		TakeProfitTimeframe:    decision.TakeProfitTF,
		TakeProfitAnchor:       decision.TakeProfitAnchor,
		TakeProfitPolicy:       decision.TakeProfitPolicy,
		TakeProfitCandidates:   decision.TakeProfitCandidates,
		TakeProfitMinRR:        decision.TakeProfitMinRR,
		TakeProfitMinATRs:      decision.TakeProfitMinATRs,
		TakeProfitSelectedRR:   decision.TakeProfitSelectedRR,
		TakeProfitSelectedATRs: decision.TakeProfitSelectedATRs,
		TakeProfitQualified:    decision.TakeProfitQualified,
		NearestTakeProfit:      decision.NearestTakeProfit,
		NearestTakeProfitRR:    decision.NearestTakeProfitRR,
		NearestTakeProfitATRs:  decision.NearestTakeProfitATRs,
		ProtectiveATR:          decision.ProtectiveATR,
		ProtectiveATRTimeframe: decision.ProtectiveATRTF,
		ProtectiveATRBuffer:    decision.ProtectiveATRBuffer,
		ProtectiveRiskReward:   decision.ProtectiveRiskReward,
		ExecutionRiskReward:    decision.ExecutionRiskReward,
	}

	if err := at.store.Position().UpdatePositionProtectiveLevelMetadata(at.id, symbol, side, meta); err != nil {
		logger.Infof("📌 [%s] Position not yet in DB for protective metadata %s %s, starting background retry", at.name, symbol, side)
		go func() {
			for i := 0; i < 12; i++ {
				time.Sleep(5 * time.Second)
				if err := at.store.Position().UpdatePositionProtectiveLevelMetadata(at.id, symbol, side, meta); err == nil {
					logger.Infof("📌 [%s] Background flush: saved protective metadata for %s %s (attempt %d)", at.name, symbol, side, i+1)
					return
				}
			}
			logger.Infof("⚠️ [%s] Background flush failed for protective metadata %s %s after 60s", at.name, symbol, side)
		}()
		return
	}
	logger.Infof("📌 [%s] Saved protective metadata for %s %s: SL=%s TP=%s RR=%.2f",
		at.name, symbol, side, meta.StopLossSource, meta.TakeProfitSource, meta.ProtectiveRiskReward)
}
