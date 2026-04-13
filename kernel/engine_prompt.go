package kernel

import (
	"fmt"
	"nofx/market"
	"nofx/provider/nofxos"
	"nofx/store"
	"strings"
	"time"
)

// ============================================================================
// Prompt Building - System Prompt
// ============================================================================

// BuildSystemPrompt builds System Prompt according to strategy configuration
func (e *StrategyEngine) BuildSystemPrompt(accountEquity float64, variant string) string {
	var sb strings.Builder
	riskControl := e.config.RiskControl
	promptSections := e.config.PromptSections
	closeConfidence := riskControl.MinCloseConfidence
	if closeConfidence <= 0 {
		closeConfidence = store.DefaultMinCloseConfidence
	}

	// 0. Data Dictionary & Schema (ensure AI understands all fields)
	lang := e.GetLanguage()
	schemaPrompt := GetSchemaPrompt(lang)
	sb.WriteString(schemaPrompt)
	sb.WriteString("\n\n")
	sb.WriteString("---\n\n")

	// 1. Role definition (editable)
	if promptSections.RoleDefinition != "" {
		sb.WriteString(promptSections.RoleDefinition)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# You are a professional cryptocurrency trading AI\n\n")
		sb.WriteString("Your task is to make trading decisions based on provided market data.\n\n")
	}

	// 2. Trading mode variant
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "aggressive":
		sb.WriteString("## Mode: Aggressive\n- Prioritize capturing trend breakouts, can build positions in batches when confidence ≥ 70\n- Allow higher positions, but must strictly set stop-loss and explain risk-reward ratio\n\n")
	case "conservative":
		sb.WriteString("## Mode: Conservative\n- Only open positions when multiple signals resonate\n- Prioritize cash preservation, must pause for multiple periods after consecutive losses\n\n")
	case "scalping":
		sb.WriteString("## Mode: Scalping\n- Focus on short-term momentum, smaller profit targets but require quick action\n- If price doesn't move as expected within two bars, immediately reduce position or stop-loss\n\n")
	}

	// 3. Hard constraints (risk control)
	btcEthPosValueRatio := riskControl.BTCETHMaxPositionValueRatio
	if btcEthPosValueRatio <= 0 {
		btcEthPosValueRatio = 5.0
	}
	altcoinPosValueRatio := riskControl.AltcoinMaxPositionValueRatio
	if altcoinPosValueRatio <= 0 {
		altcoinPosValueRatio = 1.0
	}

	sb.WriteString("# Hard Constraints (Risk Control)\n\n")
	sb.WriteString("## CODE ENFORCED (Backend validation, cannot be bypassed):\n")
	sb.WriteString(fmt.Sprintf("- Max Positions: %d coins simultaneously\n", riskControl.MaxPositions))
	sb.WriteString(fmt.Sprintf("- Position Value Limit (Altcoins): max %.0f USDT (= equity %.0f × %.1fx)\n",
		accountEquity*altcoinPosValueRatio, accountEquity, altcoinPosValueRatio))
	sb.WriteString(fmt.Sprintf("- Position Value Limit (BTC/ETH): max %.0f USDT (= equity %.0f × %.1fx)\n",
		accountEquity*btcEthPosValueRatio, accountEquity, btcEthPosValueRatio))
	sb.WriteString(fmt.Sprintf("- Max Margin Usage: ≤%.0f%%\n", riskControl.MaxMarginUsage*100))
	sb.WriteString(fmt.Sprintf("- Min Position Size: ≥%.0f USDT\n\n", riskControl.MinPositionSize))

	sb.WriteString("## AI GUIDED (Recommended, you should follow):\n")
	sb.WriteString(fmt.Sprintf("- Trading Leverage: Altcoins max %dx | BTC/ETH max %dx\n",
		riskControl.AltcoinMaxLeverage, riskControl.BTCETHMaxLeverage))
	sb.WriteString(fmt.Sprintf("- Risk-Reward Ratio: target ≥1:%.1f. Formula: reward = |take_profit - entry|, risk = |entry - stop_loss|, ratio = reward / risk.\n", riskControl.MinRiskRewardRatio))
	sb.WriteString(fmt.Sprintf("  **You MUST calculate and write the ratio in your reasoning before opening any position.** Example: \"entry 2244, SL 2218, TP 2300 → risk=26, reward=56, ratio=2.15 (target ≥%.1f ✓)\"\n", riskControl.MinRiskRewardRatio))
	sb.WriteString(fmt.Sprintf("  If ratio < %.1f, strongly consider skipping. If ratio is close (within 80%% of target, i.e. ≥%.1f), acceptable with strong signals.\n", riskControl.MinRiskRewardRatio, riskControl.MinRiskRewardRatio*0.8))
	sb.WriteString(fmt.Sprintf("- Min Confidence: ≥%d to open position\n\n", riskControl.MinConfidence))

	// Stop-loss ATR buffer guidance (mode-aware)
	atrBuffer := riskControl.StopLossATRBuffer
	if atrBuffer <= 0 {
		// Default by mode
		switch strings.ToLower(strings.TrimSpace(variant)) {
		case "conservative":
			atrBuffer = 1.5
		case "aggressive":
			atrBuffer = 0.5
		case "scalping":
			atrBuffer = 0.3
		default: // balanced
			atrBuffer = 1.0
		}
	}
	sb.WriteString("## Stop-Loss & Entry Quality\n")
	sb.WriteString("- **IMPORTANT: Use 15m or 1h ATR14 for stop-loss evaluation, NOT the 3m ATR** (3m ATR is too small for meaningful stop-loss distances)\n")
	sb.WriteString(fmt.Sprintf("- Stop-loss buffer: set stop-loss at least %.1f × ATR14(15m or 1h) BEYOND the support/resistance level (not right at it)\n", atrBuffer))
	sb.WriteString("  Example (long): support at 2088, 15m ATR14=9.14 → stop = 2088 - 9.14×")
	sb.WriteString(fmt.Sprintf("%.1f", atrBuffer))
	sb.WriteString(fmt.Sprintf(" = %.1f\n", 2088-9.14*atrBuffer))
	sb.WriteString("- Entry quality: only enter at ① near key support/resistance (reversal) or ② after breakout confirmation (trend)\n")
	sb.WriteString("  Do NOT enter in the middle zone between support and resistance — poor stop/target geometry\n")
	sb.WriteString(fmt.Sprintf("- If your stop distance (entry to stop) < %.1f × ATR14(15m or 1h), the trade setup is too tight — skip or wait for better entry\n\n", atrBuffer))

	// Position sizing guidance — correct order: technicals first, then size
	sb.WriteString("## Opening Decision Flow (MUST follow this order)\n\n")
	sb.WriteString("**Step 1: Technical levels FIRST (ignore leverage at this stage)**\n")
	sb.WriteString("- Find support/resistance on your analysis timeframe\n")
	sb.WriteString("- Set SL = support - ATR buffer (long) or resistance + ATR buffer (short)\n")
	sb.WriteString("- Set TP = target level from chart structure\n")
	sb.WriteString("- Calculate R:R ratio = |TP - entry| / |entry - SL|\n")
	sb.WriteString(fmt.Sprintf("- If R:R < %.1f → **SKIP the trade**, do NOT shrink SL to force a better ratio\n\n", riskControl.MinRiskRewardRatio))

	sb.WriteString("**Step 2: Position size based on risk budget (leverage matters HERE)**\n")
	maxRiskPct := 2.0
	maxRiskUSD := accountEquity * maxRiskPct / 100
	sb.WriteString(fmt.Sprintf("- Max risk per trade = equity × %.0f%% = %.0f × %.0f%% = **%.2f USDT**\n", maxRiskPct, accountEquity, maxRiskPct, maxRiskUSD))
	sb.WriteString("- SL distance %% = |entry - SL| / entry\n")
	sb.WriteString("- position_size_usd = max_risk / (SL_distance%% × leverage)\n")
	sb.WriteString(fmt.Sprintf("- Example: SL distance=1.5%%, leverage=%dx → position = %.2f / (0.015 × %d) = %.0f USDT\n",
		riskControl.BTCETHMaxLeverage, maxRiskUSD, riskControl.BTCETHMaxLeverage,
		maxRiskUSD/(0.015*float64(riskControl.BTCETHMaxLeverage))))
	sb.WriteString(fmt.Sprintf("- Position Value Limits: BTC/ETH max %.0f USDT | Altcoins max %.0f USDT\n",
		accountEquity*btcEthPosValueRatio, accountEquity*altcoinPosValueRatio))
	sb.WriteString("- If calculated size < min position size → trade not viable at this leverage, **reduce leverage or skip**\n\n")

	sb.WriteString("**FORBIDDEN**: Shrinking SL distance to fit a larger position. SL is determined by technicals, NOT by how much you want to trade.\n")
	sb.WriteString("**FORBIDDEN**: Setting SL based on leverage (e.g., \"10x so I'll use -0.5% SL\"). SL must be based on chart structure + ATR buffer.\n\n")

	// 4. Trading frequency (editable)
	if promptSections.TradingFrequency != "" {
		sb.WriteString(promptSections.TradingFrequency)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("# ⏱️ Trading Frequency Awareness\n\n")
		sb.WriteString("- Excellent traders: 2-4 trades/day ≈ 0.1-0.2 trades/hour\n")
		sb.WriteString("- >2 trades/hour = Overtrading\n")
		sb.WriteString("- Single position hold time ≥ 30-60 minutes\n")
		sb.WriteString("If you find yourself trading every period → standards too low; if closing positions < 30 minutes → too impatient.\n\n")
	}

	// 5. Entry standards (editable)
	if promptSections.EntryStandards != "" {
		sb.WriteString(promptSections.EntryStandards)
		sb.WriteString("\n\nYou have the following indicator data:\n")
		e.writeAvailableIndicators(&sb)
		sb.WriteString(fmt.Sprintf("\n**Confidence ≥ %d** required to open positions.\n\n", riskControl.MinConfidence))
	} else {
		sb.WriteString("# 🎯 Entry Standards (Strict)\n\n")
		sb.WriteString("Only open positions when multiple signals resonate. You have:\n")
		e.writeAvailableIndicators(&sb)
		sb.WriteString(fmt.Sprintf("\nFeel free to use any effective analysis method, but **confidence ≥ %d** required to open positions; avoid low-quality behaviors such as single indicators, contradictory signals, sideways consolidation, reopening immediately after closing, etc.\n\n", riskControl.MinConfidence))
	}

	// 6. Decision process (editable)
	if promptSections.DecisionProcess != "" {
		sb.WriteString(promptSections.DecisionProcess)
		sb.WriteString("\n\n")
	} else {
		if lang == LangChinese {
			sb.WriteString("# 📋 决策流程\n\n")
			sb.WriteString("1. 先检查持仓 → 任何平仓前必须完成平仓检查清单\n")
			sb.WriteString("2. 再扫描候选币种 + 多时间框架 → 是否存在强信号\n")
			sb.WriteString("3. 先写思考过程，再输出结构化 JSON\n")
			sb.WriteString("**禁止**：用低于开仓周期的时间框架作为平仓依据。\n")
			sb.WriteString("**禁止**：在开仓论点仍有效、且 Net PnL < 手续费×2 时提前止盈平仓。\n\n")
		} else {
			sb.WriteString("# 📋 Decision Process\n\n")
			sb.WriteString("1. Check positions → Apply MANDATORY Close-Position Checklist before any close decision\n")
			sb.WriteString("2. Scan candidate coins + multi-timeframe → Are there strong signals\n")
			sb.WriteString("3. Write chain of thought first, then output structured JSON\n")
			sb.WriteString("**FORBIDDEN**: Closing a position based on a shorter timeframe than it was opened on.\n")
			sb.WriteString("**FORBIDDEN**: Closing a position where Net PnL < fees×2 while the opening thesis is still valid.\n\n")
		}
	}

	// 6.5. Thesis-driven position management + close checklist (always included, not editable)
	if lang == LangChinese {
		sb.WriteString("# ⚠️ 基于论点的持仓管理（硬性规则）\n\n")
		sb.WriteString("## 时间框架锚定（禁止违反）\n")
		sb.WriteString("开仓时，你的 reasoning 必须包含一个 [THESIS] 块：\n")
		sb.WriteString("`[THESIS] timeframe=15m | invalidation=15m 收盘跌破 2080 且 OI 转负 | min_target=+1.5% [/THESIS]`\n")
		sb.WriteString("这个 thesis 会成为后续周期中的硬性约束。你必须：\n")
		sb.WriteString("- 只使用相同或更高的时间框架来评估持仓\n")
		sb.WriteString("- **禁止**：因为 3m/5m 噪音而平掉基于 15m 开的仓位\n")
		sb.WriteString("- 给 thesis 足够的发挥时间（15m thesis → 至少持有 30 分钟；1h thesis → 至少持有 1-3 小时）\n\n")
		sb.WriteString("## ⛔ 强制平仓检查清单\n\n")
		sb.WriteString("在做出任何平仓动作前，你必须在 <reasoning> 中回答全部 6 项。若无法回答 → 输出 HOLD：\n\n")
		sb.WriteString("1. **[TIMEFRAME]** 开仓周期是什么？我是否在使用相同或更高周期的数据？（禁止更低周期）\n")
		sb.WriteString("2. **[THESIS]** 开仓失效条件是什么？它是否已在开仓周期上被触发？\n")
		sb.WriteString("3. **[FEES]** 当前毛利润是多少？往返手续费（已付 + 预估平仓）是多少？Net PnL = 毛利润 - 手续费？\n")
		sb.WriteString("4. **[MIN PROFIT]** 如果是止盈：Net PnL 是否 ≥ 手续费 × 2？若否且 thesis 仍有效 → 必须 HOLD\n")
		sb.WriteString("5. **[CLOSE CONFIDENCE]** 你有多大把握“现在平仓”比“等待止损/止盈自动触发”更好？评分 0-100。\n")
		sb.WriteString(fmt.Sprintf("   - 提前平仓要求 confidence ≥ %d。若 < %d，必须 HOLD。\n", closeConfidence, closeConfidence))
		sb.WriteString(fmt.Sprintf("   - “小利润可能回吐” = 低信心（30-50）；“开仓周期上趋势已反转，且多重确认” = 高信心（%d+）。\n", closeConfidence))
		sb.WriteString(fmt.Sprintf("6. **[VERDICT]** thesis 已失效（confidence ≥%d）→ CLOSE | 硬止损触发 → CLOSE | Net PnL ≥ fees×2 且到达止盈 → CLOSE | 其余或 confidence < %d → **MUST HOLD**\n\n", closeConfidence, closeConfidence))
	} else {
		sb.WriteString("# ⚠️ Thesis-Driven Position Management (BINDING RULES)\n\n")
		sb.WriteString("## Timeframe Anchoring (FORBIDDEN to violate)\n")
		sb.WriteString("When you open a position, your reasoning MUST include a [THESIS] block:\n")
		sb.WriteString("`[THESIS] timeframe=15m | invalidation=15m close below 2080 AND OI turns negative | min_target=+1.5% [/THESIS]`\n")
		sb.WriteString("This thesis becomes a BINDING RULE. In future cycles you MUST:\n")
		sb.WriteString("- Evaluate the position using the SAME or HIGHER timeframe only\n")
		sb.WriteString("- FORBIDDEN: closing a 15m-based position because of 3m/5m noise\n")
		sb.WriteString("- Give the thesis time to play out (15m thesis → hold ≥30min; 1h thesis → hold ≥1-3h)\n\n")
		sb.WriteString("## ⛔ MANDATORY Close-Position Checklist\n\n")
		sb.WriteString("Before ANY close action, you MUST answer ALL 6 items in <reasoning>. If you cannot → output HOLD:\n\n")
		sb.WriteString("1. **[TIMEFRAME]** Opening timeframe? Am I using same-or-higher TF data? (FORBIDDEN: lower TF)\n")
		sb.WriteString("2. **[THESIS]** What was the invalidation condition? Is it triggered on the opening TF?\n")
		sb.WriteString("3. **[FEES]** Gross PnL? Round-trip fees (paid + est. close)? Net PnL = gross - fees?\n")
		sb.WriteString("4. **[MIN PROFIT]** If profit-taking: Net PnL ≥ fees × 2? If NO and thesis valid → MUST HOLD\n")
		sb.WriteString("5. **[CLOSE CONFIDENCE]** How confident are you that closing NOW is better than letting SL/TP trigger? Score 0-100.\n")
		sb.WriteString(fmt.Sprintf("   - Closing requires confidence ≥ %d. If < %d, you MUST HOLD.\n", closeConfidence, closeConfidence))
		sb.WriteString(fmt.Sprintf("   - \"Small profit might disappear\" = low confidence (30-50). \"Trend has reversed on opening TF with multiple confirmations\" = high confidence (%d+).\n", closeConfidence))
		sb.WriteString(fmt.Sprintf("6. **[VERDICT]** Thesis invalidated (confidence ≥%d) → CLOSE | Hard SL hit → CLOSE | Net PnL ≥ fees×2 + TP reached → CLOSE | None or confidence < %d → **MUST HOLD**\n\n", closeConfidence, closeConfidence))
	}

	// 7. Output format
	if lang == LangChinese {
		sb.WriteString("# 输出格式（严格遵守）\n\n")
		sb.WriteString("**必须**使用 XML 标签 <reasoning>、<reasoning_summary> 和 <decision> 分隔思考过程、摘要和决策 JSON。\n\n")
		sb.WriteString("## 格式要求\n\n")
		sb.WriteString("<reasoning>\n")
		sb.WriteString("你的详细思考过程...\n")
		sb.WriteString("- 简要分析你的判断逻辑\n")
		sb.WriteString("</reasoning>\n\n")
		sb.WriteString("<reasoning_summary>\n")
		sb.WriteString("2-4 句：写出关键结论和主要原因。不要在 summary 中写数据源名称（如 ai500、OITop），只写推理结论。\n")
		sb.WriteString("</reasoning_summary>\n\n")
		sb.WriteString("<decision>\n")
		sb.WriteString("JSON 决策数组\n\n")
		sb.WriteString("```json\n[\n")
		// Use the actual configured position value ratio for BTC/ETH in the example
		examplePositionSize := accountEquity * btcEthPosValueRatio
		sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"risk_usd\": 300, \"reasoning\": \"[THESIS] timeframe=15m | invalidation=15m 收盘重新站上 97500 且 OI 转负 | min_target=+1.8%% [/THESIS] BTC 跌破 97500 支撑，OI 增加 +2.1%%，空头压力得到确认，目标看向 91000 一带。\"},\n",
			riskControl.BTCETHMaxLeverage, examplePositionSize))
		sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"reasoning\": \"[TIMEFRAME] 开仓周期为 15m，当前使用 15m 数据判断。 [THESIS] 失效条件为跌破 2100 且 OI 转负；当前 15m 已满足。 [FEES] 净盈亏已重新计算。 [MIN PROFIT] 非止盈场景。 [CLOSE CONFIDENCE] 90。 [VERDICT] 开仓论点失效，执行平仓。\"}\n")
		sb.WriteString("]\n```\n")
		sb.WriteString("</decision>\n\n")
		sb.WriteString("## 字段说明\n\n")
		sb.WriteString("- `action`: open_long | open_short | close_long | close_short | hold | wait\n")
		sb.WriteString(fmt.Sprintf("- `confidence`: 0-100（开仓建议 ≥ %d）\n", riskControl.MinConfidence))
		sb.WriteString("- 开仓时必填：leverage, position_size_usd, stop_loss, take_profit, confidence, risk_usd, reasoning\n")
		sb.WriteString("- `reasoning`: **所有动作都必填**\n")
		sb.WriteString("  - **开仓时（open_long/open_short）**：必须以 `[THESIS] timeframe=X | invalidation=condition | min_target=+X%% [/THESIS]` 开头，后面再写分析。这个 thesis 会成为后续周期的硬性约束。\n")
		sb.WriteString("  - **平仓时**：必须包含平仓检查清单全部 6 项（[TIMEFRAME], [THESIS], [FEES], [MIN PROFIT], [CLOSE CONFIDENCE], [VERDICT]）\n")
		sb.WriteString("  - **持有/等待时**：用 1-2 句简要说明原因\n")
		sb.WriteString("- **重要**：所有数值都必须是计算后的结果，不要写公式/表达式（例如写 `27.76`，不要写 `3000 * 0.01`）\n\n")
	} else {
		sb.WriteString("# Output Format (Strictly Follow)\n\n")
		sb.WriteString("**Must use XML tags <reasoning>, <reasoning_summary> and <decision> to separate chain of thought, summary and decision JSON**\n\n")
		sb.WriteString("## Format Requirements\n\n")
		sb.WriteString("<reasoning>\n")
		sb.WriteString("Your chain of thought analysis...\n")
		sb.WriteString("- Briefly analyze your thinking process \n")
		sb.WriteString("</reasoning>\n\n")
		sb.WriteString("<reasoning_summary>\n")
		sb.WriteString("2-4 sentences: key conclusion and main reason for the decision. Do NOT include data source names (e.g. ai500, OITop) in the summary—only reasoning and conclusions.\n")
		sb.WriteString("</reasoning_summary>\n\n")
		sb.WriteString("<decision>\n")
		sb.WriteString("JSON decision array\n\n")
		sb.WriteString("```json\n[\n")
		// Use the actual configured position value ratio for BTC/ETH in the example
		examplePositionSize := accountEquity * btcEthPosValueRatio
		sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97000, \"take_profit\": 91000, \"confidence\": 85, \"risk_usd\": 300, \"reasoning\": \"BTC broke below 97500 support with OI increasing +2.1%%, confirming bearish pressure. Target 91000 resistance turned support.\"},\n",
			riskControl.BTCETHMaxLeverage, examplePositionSize))
		sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"reasoning\": \"Opening thesis invalidated: price dropped below key support 2100 and OI turned negative.\"}\n")
		sb.WriteString("]\n```\n")
		sb.WriteString("</decision>\n\n")
		sb.WriteString("## Field Description\n\n")
		sb.WriteString("- `action`: open_long | open_short | close_long | close_short | hold | wait\n")
		sb.WriteString(fmt.Sprintf("- `confidence`: 0-100 (opening recommended ≥ %d)\n", riskControl.MinConfidence))
		sb.WriteString("- Required when opening: leverage, position_size_usd, stop_loss, take_profit, confidence, risk_usd, reasoning\n")
		sb.WriteString("- `reasoning`: **REQUIRED for ALL actions**\n")
		sb.WriteString("  - **When opening (open_long/open_short)**: MUST begin with `[THESIS] timeframe=X | invalidation=condition | min_target=+X% [/THESIS]` followed by analysis. This becomes a BINDING RULE for future cycles.\n")
		sb.WriteString("  - **When closing**: MUST include all 6 items of the Close-Position Checklist ([TIMEFRAME], [THESIS], [FEES], [MIN PROFIT], [CLOSE CONFIDENCE], [VERDICT])\n")
		sb.WriteString("  - **When holding/waiting**: Brief explanation of why (1-2 sentences)\n")
		sb.WriteString("- **IMPORTANT**: All numeric values must be calculated numbers, NOT formulas/expressions (e.g., use `27.76` not `3000 * 0.01`)\n\n")
	}

	// 8. Custom Prompt
	if e.config.CustomPrompt != "" {
		sb.WriteString("# 📌 Personalized Trading Strategy\n\n")
		sb.WriteString(e.config.CustomPrompt)
		sb.WriteString("\n\n")
		sb.WriteString("Note: The above personalized strategy is a supplement to the basic rules and cannot violate the basic risk control principles.\n")
	}

	return sb.String()
}

func (e *StrategyEngine) writeAvailableIndicators(sb *strings.Builder) {
	indicators := e.config.Indicators
	kline := indicators.Klines

	sb.WriteString(fmt.Sprintf("- %s price series", kline.PrimaryTimeframe))
	if kline.EnableMultiTimeframe {
		sb.WriteString(fmt.Sprintf(" + %s K-line series\n", kline.LongerTimeframe))
	} else {
		sb.WriteString("\n")
	}

	if indicators.EnableEMA {
		sb.WriteString("- EMA indicators")
		if len(indicators.EMAPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.EMAPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableMACD {
		sb.WriteString("- MACD indicators\n")
	}

	if indicators.EnableRSI {
		sb.WriteString("- RSI indicators")
		if len(indicators.RSIPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.RSIPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableATR {
		sb.WriteString("- ATR indicators")
		if len(indicators.ATRPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.ATRPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableBOLL {
		sb.WriteString("- Bollinger Bands (BOLL) - Upper/Middle/Lower bands")
		if len(indicators.BOLLPeriods) > 0 {
			sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.BOLLPeriods))
		}
		sb.WriteString("\n")
	}

	if indicators.EnableVolume {
		sb.WriteString("- Volume data\n")
	}

	if indicators.EnableOI {
		sb.WriteString("- Open Interest (OI) data\n")
	}

	if indicators.EnableFundingRate {
		sb.WriteString("- Funding rate\n")
	}

	if len(e.config.CoinSource.StaticCoins) > 0 || e.config.CoinSource.UseAI500 || e.config.CoinSource.UseOITop {
		sb.WriteString("- AI500 / OI_Top filter tags (if available)\n")
	}

	if indicators.EnableQuantData {
		sb.WriteString("- Quantitative data (institutional/retail fund flow, position changes, multi-period price changes)\n")
	}
}

// ============================================================================
// Prompt Building - User Prompt
// ============================================================================

// BuildUserPrompt builds User Prompt based on strategy configuration
func (e *StrategyEngine) BuildUserPrompt(ctx *Context) string {
	var sb strings.Builder
	lang := e.GetLanguage()
	closeConfidence := e.config.RiskControl.MinCloseConfidence
	if closeConfidence <= 0 {
		closeConfidence = store.DefaultMinCloseConfidence
	}

	// System status
	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("时间: %s | 周期: #%d | 运行时长: %d 分钟\n\n",
			ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))
	} else {
		sb.WriteString(fmt.Sprintf("Time: %s | Period: #%d | Runtime: %d minutes\n\n",
			ctx.CurrentTime, ctx.CallCount, ctx.RuntimeMinutes))
	}

	// BTC market
	if btcData, hasBTC := ctx.MarketDataMap["BTCUSDT"]; hasBTC {
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("BTC: %.2f（1h: %+.2f%%, 4h: %+.2f%%） | MACD: %.4f | RSI: %.2f\n\n",
				btcData.CurrentPrice, btcData.PriceChange1h, btcData.PriceChange4h,
				btcData.CurrentMACD, btcData.CurrentRSI7))
		} else {
			sb.WriteString(fmt.Sprintf("BTC: %.2f (1h: %+.2f%%, 4h: %+.2f%%) | MACD: %.4f | RSI: %.2f\n\n",
				btcData.CurrentPrice, btcData.PriceChange1h, btcData.PriceChange4h,
				btcData.CurrentMACD, btcData.CurrentRSI7))
		}
	}

	// Account information
	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("账户: 净值 %.2f | 可用余额 %.2f (%.1f%%) | PnL %+.2f%% | 保证金 %.1f%% | 持仓 %d\n\n",
			ctx.Account.TotalEquity,
			ctx.Account.AvailableBalance,
			(ctx.Account.AvailableBalance/ctx.Account.TotalEquity)*100,
			ctx.Account.TotalPnLPct,
			ctx.Account.MarginUsedPct,
			ctx.Account.PositionCount))
	} else {
		sb.WriteString(fmt.Sprintf("Account: Equity %.2f | Balance %.2f (%.1f%%) | PnL %+.2f%% | Margin %.1f%% | Positions %d\n\n",
			ctx.Account.TotalEquity,
			ctx.Account.AvailableBalance,
			(ctx.Account.AvailableBalance/ctx.Account.TotalEquity)*100,
			ctx.Account.TotalPnLPct,
			ctx.Account.MarginUsedPct,
			ctx.Account.PositionCount))
	}

	// Recently completed orders (placed before positions to ensure visibility)
	if len(ctx.RecentOrders) > 0 {
		if lang == LangChinese {
			sb.WriteString("## 最近已完成交易\n")
		} else {
			sb.WriteString("## Recent Completed Trades\n")
		}
		for i, order := range ctx.RecentOrders {
			resultStr := "Profit"
			if order.RealizedPnL < 0 {
				resultStr = "Loss"
			}
			if lang == LangChinese {
				if order.RealizedPnL < 0 {
					resultStr = "亏损"
				} else {
					resultStr = "盈利"
				}
				sb.WriteString(fmt.Sprintf("%d. %s %s | 开仓 %.4f 平仓 %.4f | %s: %+.2f USDT (%+.2f%%) | %s→%s (%s)\n",
					i+1, order.Symbol, order.Side,
					order.EntryPrice, order.ExitPrice,
					resultStr, order.RealizedPnL, order.PnLPct,
					order.EntryTime, order.ExitTime, order.HoldDuration))
			} else {
				sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Exit %.4f | %s: %+.2f USDT (%+.2f%%) | %s→%s (%s)\n",
					i+1, order.Symbol, order.Side,
					order.EntryPrice, order.ExitPrice,
					resultStr, order.RealizedPnL, order.PnLPct,
					order.EntryTime, order.ExitTime, order.HoldDuration))
			}
		}
		sb.WriteString("\n")
	}

	// Historical trading statistics (helps AI understand past performance)
	if ctx.TradingStats != nil && ctx.TradingStats.TotalTrades > 0 {
		// Win/Loss ratio
		var winLossRatio float64
		if ctx.TradingStats.AvgLoss > 0 {
			winLossRatio = ctx.TradingStats.AvgWin / ctx.TradingStats.AvgLoss
		}

		if lang == LangChinese {
			sb.WriteString("## 历史交易统计\n")
			sb.WriteString(fmt.Sprintf("总交易: %d 笔 | 盈利因子: %.2f | 夏普比率: %.2f | 盈亏比: %.2f\n",
				ctx.TradingStats.TotalTrades,
				ctx.TradingStats.ProfitFactor,
				ctx.TradingStats.SharpeRatio,
				winLossRatio))
			sb.WriteString(fmt.Sprintf("总盈亏: %+.2f USDT | 平均盈利: +%.2f | 平均亏损: -%.2f | 最大回撤: %.1f%%\n",
				ctx.TradingStats.TotalPnL,
				ctx.TradingStats.AvgWin,
				ctx.TradingStats.AvgLoss,
				ctx.TradingStats.MaxDrawdownPct))

			// Performance hints based on profit factor, sharpe, and drawdown
			if ctx.TradingStats.ProfitFactor >= 1.5 && ctx.TradingStats.SharpeRatio >= 1 {
				sb.WriteString("表现: 良好 - 保持当前策略\n")
			} else if ctx.TradingStats.ProfitFactor < 1 {
				sb.WriteString("表现: 需改进 - 提高盈亏比，优化止盈止损\n")
			} else if ctx.TradingStats.MaxDrawdownPct > 30 {
				sb.WriteString("表现: 风险偏高 - 减少仓位，控制回撤\n")
			} else {
				sb.WriteString("表现: 正常 - 有优化空间\n")
			}
		} else {
			sb.WriteString("## Historical Trading Statistics\n")
			sb.WriteString(fmt.Sprintf("Total Trades: %d | Profit Factor: %.2f | Sharpe: %.2f | Win/Loss Ratio: %.2f\n",
				ctx.TradingStats.TotalTrades,
				ctx.TradingStats.ProfitFactor,
				ctx.TradingStats.SharpeRatio,
				winLossRatio))
			sb.WriteString(fmt.Sprintf("Total PnL: %+.2f USDT | Avg Win: +%.2f | Avg Loss: -%.2f | Max Drawdown: %.1f%%\n",
				ctx.TradingStats.TotalPnL,
				ctx.TradingStats.AvgWin,
				ctx.TradingStats.AvgLoss,
				ctx.TradingStats.MaxDrawdownPct))

			// Performance hints based on profit factor, sharpe, and drawdown
			if ctx.TradingStats.ProfitFactor >= 1.5 && ctx.TradingStats.SharpeRatio >= 1 {
				sb.WriteString("Performance: GOOD - maintain current strategy\n")
			} else if ctx.TradingStats.ProfitFactor < 1 {
				sb.WriteString("Performance: NEEDS IMPROVEMENT - improve win/loss ratio, optimize TP/SL\n")
			} else if ctx.TradingStats.MaxDrawdownPct > 30 {
				sb.WriteString("Performance: HIGH RISK - reduce position size, control drawdown\n")
			} else {
				sb.WriteString("Performance: NORMAL - room for optimization\n")
			}
		}
		sb.WriteString("\n")
	}

	// Position information
	if len(ctx.Positions) > 0 {
		if lang == LangChinese {
			sb.WriteString("## 当前持仓\n")
		} else {
			sb.WriteString("## Current Positions\n")
		}
		for i, pos := range ctx.Positions {
			sb.WriteString(e.formatPositionInfo(i+1, pos, ctx))
		}
	} else {
		if lang == LangChinese {
			sb.WriteString("当前持仓: 无\n\n")
		} else {
			sb.WriteString("Current Positions: None\n\n")
		}
	}

	// Position management rules — binding constraints from opening thesis
	if len(ctx.PositionMemories) > 0 {
		if lang == LangChinese {
			sb.WriteString("## ⚠️ 持仓管理硬性规则\n\n")
			sb.WriteString("下面每个仓位都有开仓时设定的 thesis。要平仓，必须满足：\n")
			sb.WriteString(fmt.Sprintf("- 开仓失效条件已在开仓周期上触发，且平仓信心 ≥ %d；或\n", closeConfidence))
			sb.WriteString("- 触发硬止损（-5%）；或\n")
			sb.WriteString("- Net PnL（扣费后）≥ 手续费×2，且达到止盈目标\n")
			sb.WriteString(fmt.Sprintf("- **开仓相对容易（confidence ≥ 70），提前平仓更难（confidence ≥ %d）。** 这种不对称用于防止冲动离场。\n\n", closeConfidence))
			sb.WriteString("**重要**：如果仓位显示 🛡️ Exchange Orders（交易所已挂止损/止盈），它们会自动触发，作为你的安全网。\n")
			sb.WriteString("手动平仓前先问自己：**我现在平仓，是否比等待止损/止盈自动触发有更好的理由？**\n")
			sb.WriteString("- 若市场结构已经根本改变（且在开仓周期上确认反转）→ 可以提前平仓\n")
			sb.WriteString("- 若只是想“锁定一点小利润”或“避免一点回撤” → 这不是更好的理由，应让 SL/TP 发挥作用。\n\n")
		} else {
			sb.WriteString("## ⚠️ BINDING Position Management Rules\n\n")
			sb.WriteString("Each position below has a thesis set when it was opened. Closing requires:\n")
			sb.WriteString(fmt.Sprintf("- The INVALIDATION CONDITION is met (on the opening timeframe) with close confidence ≥ %d, OR\n", closeConfidence))
			sb.WriteString("- Hard stop-loss is hit (-5%), OR\n")
			sb.WriteString("- Net PnL (after fees) ≥ fees×2 AND take-profit target reached\n")
			sb.WriteString(fmt.Sprintf("- **Opening a position is easy (confidence ≥ 70), but closing early is HARD (confidence ≥ %d).** This asymmetry protects against impulsive exits.\n\n", closeConfidence))
			sb.WriteString("**IMPORTANT**: If a position shows 🛡️ Exchange Orders (SL/TP active on exchange), those orders will trigger automatically as your safety net.\n")
			sb.WriteString("Before manually closing, ask yourself: **do I have a BETTER reason to exit NOW than waiting for my SL/TP to trigger?**\n")
			sb.WriteString("- If the market structure has fundamentally changed (trend reversal confirmed on opening TF) → early close is justified\n")
			sb.WriteString("- If you just want to \"lock in small profit\" or \"avoid a small drawdown\" → that is NOT a better reason. Let the SL/TP work.\n\n")
		}

		for _, pm := range ctx.PositionMemories {
			reasoning := pm.OpeningReasoning
			if reasoning == "" {
				reasoning = pm.CotSummary
			}
			if reasoning == "" {
				continue
			}

			sb.WriteString(fmt.Sprintf("### %s %s\n", pm.Symbol, strings.ToUpper(pm.Side)))

			// Try to parse structured [THESIS] block
			if thesisStart := strings.Index(reasoning, "[THESIS]"); thesisStart >= 0 {
				thesisEnd := strings.Index(reasoning[thesisStart:], "[/THESIS]")
				var thesisBlock string
				if thesisEnd >= 0 {
					thesisBlock = strings.TrimSpace(reasoning[thesisStart+8 : thesisStart+thesisEnd])
				} else {
					// [THESIS] found but no closing tag — take until end of line
					endLine := strings.Index(reasoning[thesisStart+8:], "\n")
					if endLine >= 0 {
						thesisBlock = strings.TrimSpace(reasoning[thesisStart+8 : thesisStart+8+endLine])
					} else {
						thesisBlock = strings.TrimSpace(reasoning[thesisStart+8:])
					}
				}
				// Parse key-value pairs from thesis
				sb.WriteString("**ACTIVE CONSTRAINTS (from opening thesis):**\n")
				for _, part := range strings.Split(thesisBlock, "|") {
					part = strings.TrimSpace(part)
					if part != "" {
						sb.WriteString(fmt.Sprintf("  - %s\n", part))
					}
				}
				sb.WriteString("  - **RULE**: Only use the above timeframe or higher to evaluate this position. FORBIDDEN to close based on lower timeframe noise.\n")
				sb.WriteString("  - **RULE**: Net PnL must ≥ fees×2 for profit-taking. If not met and thesis valid → MUST HOLD.\n")
			} else {
				// Legacy unstructured format — still display as binding
				sb.WriteString(fmt.Sprintf("**Opening thesis (BINDING)**: %s\n", reasoning))
				sb.WriteString("  → You MUST verify these conditions still hold on the same timeframe before closing.\n")
			}
			sb.WriteString("\n")
		}

		hasReview := false
		for _, pm := range ctx.PositionMemories {
			if pm.LastReviewSummary != "" {
				hasReview = true
				break
			}
		}
		if hasReview {
			sb.WriteString("## Your Last Review of These Positions\n")
			for _, pm := range ctx.PositionMemories {
				if pm.LastReviewSummary != "" {
					sb.WriteString(fmt.Sprintf("- %s %s: %s\n", pm.Symbol, pm.Side, pm.LastReviewSummary))
				}
			}
			sb.WriteString("\n")
		}
	}

	// Candidate coins (exclude coins already in positions to avoid duplicate data)
	positionSymbols := make(map[string]bool)
	for _, pos := range ctx.Positions {
		// Normalize symbol to handle both "ETH" and "ETHUSDT" formats
		normalizedSymbol := market.Normalize(pos.Symbol)
		positionSymbols[normalizedSymbol] = true
	}

	sb.WriteString(fmt.Sprintf("## Candidate Coins (%d coins)\n\n", len(ctx.MarketDataMap)))
	displayedCount := 0
	for _, coin := range ctx.CandidateCoins {
		// Skip if this coin is already a position (data already shown in positions section)
		normalizedCoinSymbol := market.Normalize(coin.Symbol)
		if positionSymbols[normalizedCoinSymbol] {
			continue
		}

		marketData, hasData := ctx.MarketDataMap[coin.Symbol]
		if !hasData {
			continue
		}
		displayedCount++

		sourceTags := e.formatCoinSourceTag(coin.Sources)
		sb.WriteString(fmt.Sprintf("### %d. %s%s\n\n", displayedCount, coin.Symbol, sourceTags))
		sb.WriteString(e.formatMarketData(marketData))

		if ctx.QuantDataMap != nil {
			if quantData, hasQuant := ctx.QuantDataMap[coin.Symbol]; hasQuant {
				sb.WriteString(e.formatQuantData(quantData))
			}
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	// Get language for market data formatting
	nofxosLang := nofxos.LangEnglish
	if e.GetLanguage() == LangChinese {
		nofxosLang = nofxos.LangChinese
	}

	// OI Ranking data (market-wide open interest changes)
	if ctx.OIRankingData != nil {
		sb.WriteString(nofxos.FormatOIRankingForAI(ctx.OIRankingData, nofxosLang))
	}

	// NetFlow Ranking data (market-wide fund flow)
	if ctx.NetFlowRankingData != nil {
		sb.WriteString(nofxos.FormatNetFlowRankingForAI(ctx.NetFlowRankingData, nofxosLang))
	}

	// Price Ranking data (market-wide gainers/losers)
	if ctx.PriceRankingData != nil {
		sb.WriteString(nofxos.FormatPriceRankingForAI(ctx.PriceRankingData, nofxosLang))
	}

	sb.WriteString("---\n\n")
	// External data sources (Kronos predictions, etc.)
	for _, item := range ctx.ExternalDataItems {
		sb.WriteString(fmt.Sprintf("## External Data: %s\n", item.Label))
		if item.Description != "" {
			sb.WriteString(fmt.Sprintf("Context: %s\n", item.Description))
		}
		sb.WriteString(fmt.Sprintf("Data: %s\n\n", item.Data))
	}

	sb.WriteString("Now please analyze and output your decision (Chain of Thought + JSON)\n")

	return sb.String()
}

func (e *StrategyEngine) formatPositionInfo(index int, pos PositionInfo, ctx *Context) string {
	var sb strings.Builder
	lang := e.GetLanguage()

	holdingDuration := ""
	if pos.UpdateTime > 0 {
		durationMs := time.Now().UnixMilli() - pos.UpdateTime
		durationMin := durationMs / (1000 * 60)
		if durationMin < 60 {
			if lang == LangChinese {
				holdingDuration = fmt.Sprintf(" | 持仓时长 %d 分钟", durationMin)
			} else {
				holdingDuration = fmt.Sprintf(" | Holding Duration %d min", durationMin)
			}
		} else {
			durationHour := durationMin / 60
			durationMinRemainder := durationMin % 60
			if lang == LangChinese {
				holdingDuration = fmt.Sprintf(" | 持仓时长 %d小时%d分钟", durationHour, durationMinRemainder)
			} else {
				holdingDuration = fmt.Sprintf(" | Holding Duration %dh %dm", durationHour, durationMinRemainder)
			}
		}
	}

	positionValue := pos.Quantity * pos.MarkPrice
	if positionValue < 0 {
		positionValue = -positionValue
	}

	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("%d. %s %s | 开仓价 %.4f 当前价 %.4f | 数量 %.4f | 仓位价值 %.2f USDT | PnL%+.2f%% | 盈亏金额%+.2f USDT | 峰值收益%.2f%% | 杠杆 %dx | 保证金 %.0f | 强平价 %.4f%s\n",
			index, pos.Symbol, strings.ToUpper(pos.Side),
			pos.EntryPrice, pos.MarkPrice, pos.Quantity, positionValue, pos.UnrealizedPnLPct, pos.UnrealizedPnL, pos.PeakPnLPct,
			pos.Leverage, pos.MarginUsed, pos.LiquidationPrice, holdingDuration))
	} else {
		sb.WriteString(fmt.Sprintf("%d. %s %s | Entry %.4f Current %.4f | Qty %.4f | Position Value %.2f USDT | PnL%+.2f%% | PnL Amount%+.2f USDT | Peak PnL%.2f%% | Leverage %dx | Margin %.0f | Liq Price %.4f%s\n",
			index, pos.Symbol, strings.ToUpper(pos.Side),
			pos.EntryPrice, pos.MarkPrice, pos.Quantity, positionValue, pos.UnrealizedPnLPct, pos.UnrealizedPnL, pos.PeakPnLPct,
			pos.Leverage, pos.MarginUsed, pos.LiquidationPrice, holdingDuration))
	}

	// Show active conditional orders (SL/TP) on exchange
	if pos.StopLossPrice > 0 || pos.TakeProfitPrice > 0 {
		if lang == LangChinese {
			sb.WriteString("   🛡️ 交易所条件单: ")
		} else {
			sb.WriteString("   🛡️ Exchange Orders: ")
		}
		parts := []string{}
		if pos.StopLossPrice > 0 {
			if lang == LangChinese {
				parts = append(parts, fmt.Sprintf("止损 @ %.4f", pos.StopLossPrice))
			} else {
				parts = append(parts, fmt.Sprintf("Stop-Loss @ %.4f", pos.StopLossPrice))
			}
		}
		if pos.TakeProfitPrice > 0 {
			if lang == LangChinese {
				parts = append(parts, fmt.Sprintf("止盈 @ %.4f", pos.TakeProfitPrice))
			} else {
				parts = append(parts, fmt.Sprintf("Take-Profit @ %.4f", pos.TakeProfitPrice))
			}
		}
		sb.WriteString(strings.Join(parts, " | "))
		if lang == LangChinese {
			sb.WriteString("（已在交易所生效，会自动触发）\n")
		} else {
			sb.WriteString(" (ACTIVE on exchange — will trigger automatically)\n")
		}
	}

	// Fee and Net PnL display — critical for AI to understand true profitability
	roundTripFee := pos.AccumulatedFee + pos.EstimatedCloseFee
	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("   💰 已付手续费: %.4f USDT | 预估平仓费: %.4f USDT | 往返合计: %.4f USDT | Net PnL（扣费后）: %+.4f USDT\n",
			pos.AccumulatedFee, pos.EstimatedCloseFee, roundTripFee, pos.NetPnL))
	} else {
		sb.WriteString(fmt.Sprintf("   💰 Paid Fee: %.4f USDT | Est. Close Fee: %.4f USDT | Round-trip: %.4f USDT | Net PnL (after fees): %+.4f USDT\n",
			pos.AccumulatedFee, pos.EstimatedCloseFee, roundTripFee, pos.NetPnL))
	}
	if pos.NetPnL < 0 && pos.UnrealizedPnL > 0 {
		if lang == LangChinese {
			sb.WriteString("   ⚠️ 警告：毛利润为正，但扣除手续费后的 Net PnL 为负，现在平仓实际上会亏钱！\n")
		} else {
			sb.WriteString("   ⚠️ WARNING: Gross PnL is POSITIVE but Net PnL is NEGATIVE after fees — closing now LOSES money!\n")
		}
	}
	if roundTripFee > 0 && pos.UnrealizedPnL > 0 && pos.UnrealizedPnL < roundTripFee*2 {
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("   ⚠️ 毛利润 (%.4f) < 2× 手续费 (%.4f) —— 若 thesis 未失效，则禁止止盈平仓\n", pos.UnrealizedPnL, roundTripFee*2))
		} else {
			sb.WriteString(fmt.Sprintf("   ⚠️ Gross profit (%.4f) < 2× fees (%.4f) — profit-taking FORBIDDEN unless thesis invalidated\n", pos.UnrealizedPnL, roundTripFee*2))
		}
	}
	sb.WriteString("\n")

	if marketData, ok := ctx.MarketDataMap[pos.Symbol]; ok {
		sb.WriteString(e.formatMarketData(marketData))

		if ctx.QuantDataMap != nil {
			if quantData, hasQuant := ctx.QuantDataMap[pos.Symbol]; hasQuant {
				sb.WriteString(e.formatQuantData(quantData))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (e *StrategyEngine) formatCoinSourceTag(sources []string) string {
	if len(sources) > 1 {
		// Multiple signal source combination
		hasAI500 := false
		hasOITop := false
		hasOILow := false
		hasHyperAll := false
		hasHyperMain := false
		for _, s := range sources {
			switch s {
			case "ai500":
				hasAI500 = true
			case "oi_top":
				hasOITop = true
			case "oi_low":
				hasOILow = true
			case "hyper_all":
				hasHyperAll = true
			case "hyper_main":
				hasHyperMain = true
			}
		}
		if hasAI500 && hasOITop {
			return " (AI500+OI_Top dual signal)"
		}
		if hasAI500 && hasOILow {
			return " (AI500+OI_Low dual signal)"
		}
		if hasOITop && hasOILow {
			return " (OI_Top+OI_Low)"
		}
		if hasHyperMain && hasAI500 {
			return " (HyperMain+AI500)"
		}
		if hasHyperAll || hasHyperMain {
			return " (Hyperliquid)"
		}
		return " (Multiple sources)"
	} else if len(sources) == 1 {
		switch sources[0] {
		case "ai500":
			return " (AI500)"
		case "oi_top":
			return " (OI_Top OI increase)"
		case "oi_low":
			return " (OI_Low OI decrease)"
		case "static":
			return " (Manual selection)"
		case "hyper_all":
			return " (Hyperliquid All)"
		case "hyper_main":
			return " (Hyperliquid Top20)"
		}
	}
	return ""
}

// ============================================================================
// Market Data Formatting
// ============================================================================

func (e *StrategyEngine) formatMarketData(data *market.Data) string {
	var sb strings.Builder
	indicators := e.config.Indicators

	// Clearly label the coin symbol
	sb.WriteString(fmt.Sprintf("=== %s Market Data ===\n\n", data.Symbol))
	sb.WriteString(fmt.Sprintf("current_price = %.4f", data.CurrentPrice))

	if indicators.EnableEMA {
		sb.WriteString(fmt.Sprintf(", current_ema20 = %.3f", data.CurrentEMA20))
	}

	if indicators.EnableMACD {
		sb.WriteString(fmt.Sprintf(", current_macd = %.3f", data.CurrentMACD))
	}

	if indicators.EnableRSI {
		sb.WriteString(fmt.Sprintf(", current_rsi7 = %.3f", data.CurrentRSI7))
	}

	sb.WriteString("\n\n")

	if indicators.EnableOI || indicators.EnableFundingRate {
		sb.WriteString(fmt.Sprintf("Additional data for %s:\n\n", data.Symbol))

		if indicators.EnableOI && data.OpenInterest != nil {
			sb.WriteString(fmt.Sprintf("Open Interest: Latest: %.2f Average: %.2f\n\n",
				data.OpenInterest.Latest, data.OpenInterest.Average))
		}

		if indicators.EnableFundingRate {
			sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))
		}
	}

	if len(data.TimeframeData) > 0 {
		timeframeOrder := []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
		for _, tf := range timeframeOrder {
			if tfData, ok := data.TimeframeData[tf]; ok {
				sb.WriteString(fmt.Sprintf("=== %s Timeframe (oldest → latest) ===\n\n", strings.ToUpper(tf)))
				e.formatTimeframeSeriesData(&sb, tfData, indicators)
			}
		}
	} else {
		// Compatible with old data format
		if data.IntradaySeries != nil {
			klineConfig := indicators.Klines
			sb.WriteString(fmt.Sprintf("Intraday series (%s intervals, oldest → latest):\n\n", klineConfig.PrimaryTimeframe))

			if len(data.IntradaySeries.MidPrices) > 0 {
				sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
			}

			if indicators.EnableEMA && len(data.IntradaySeries.EMA20Values) > 0 {
				sb.WriteString(fmt.Sprintf("EMA indicators (20-period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
			}

			if indicators.EnableMACD && len(data.IntradaySeries.MACDValues) > 0 {
				sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
			}

			if indicators.EnableRSI {
				if len(data.IntradaySeries.RSI7Values) > 0 {
					sb.WriteString(fmt.Sprintf("RSI indicators (7-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
				}
				if len(data.IntradaySeries.RSI14Values) > 0 {
					sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
				}
			}

			if indicators.EnableVolume && len(data.IntradaySeries.Volume) > 0 {
				sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3m ATR (14-period): %.3f\n\n", data.IntradaySeries.ATR14))
			}
		}

		if data.LongerTermContext != nil && indicators.Klines.EnableMultiTimeframe {
			sb.WriteString(fmt.Sprintf("Longer-term context (%s timeframe):\n\n", indicators.Klines.LongerTimeframe))

			if indicators.EnableEMA {
				sb.WriteString(fmt.Sprintf("20-Period EMA: %.3f vs. 50-Period EMA: %.3f\n\n",
					data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3-Period ATR: %.3f vs. 14-Period ATR: %.3f\n\n",
					data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
			}

			if indicators.EnableVolume {
				sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
					data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))
			}

			if indicators.EnableMACD && len(data.LongerTermContext.MACDValues) > 0 {
				sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
			}

			if indicators.EnableRSI && len(data.LongerTermContext.RSI14Values) > 0 {
				sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
			}
		}
	}

	return sb.String()
}

func (e *StrategyEngine) formatTimeframeSeriesData(sb *strings.Builder, data *market.TimeframeSeriesData, indicators store.IndicatorConfig) {
	if len(data.Klines) > 0 {
		sb.WriteString("Time(UTC)      Open      High      Low       Close     Volume\n")
		for i, k := range data.Klines {
			t := time.Unix(k.Time/1000, 0).UTC()
			timeStr := t.Format("01-02 15:04")
			marker := ""
			if i == len(data.Klines)-1 {
				marker = "  <- current"
			}
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f %-12.2f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, k.Volume, marker))
		}
		sb.WriteString("\n")
	} else if len(data.MidPrices) > 0 {
		sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.MidPrices)))
		if indicators.EnableVolume && len(data.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.Volume)))
		}
	}

	if indicators.EnableEMA {
		if len(data.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA20: %s\n", formatFloatSlice(data.EMA20Values)))
		}
		if len(data.EMA50Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA50: %s\n", formatFloatSlice(data.EMA50Values)))
		}
	}

	if indicators.EnableMACD && len(data.MACDValues) > 0 {
		sb.WriteString(fmt.Sprintf("MACD: %s\n", formatFloatSlice(data.MACDValues)))
	}

	if indicators.EnableRSI {
		if len(data.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI7: %s\n", formatFloatSlice(data.RSI7Values)))
		}
		if len(data.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI14: %s\n", formatFloatSlice(data.RSI14Values)))
		}
	}

	if indicators.EnableATR && data.ATR14 > 0 {
		sb.WriteString(fmt.Sprintf("ATR14: %.4f\n", data.ATR14))
	}

	if indicators.EnableBOLL && len(data.BOLLUpper) > 0 {
		sb.WriteString(fmt.Sprintf("BOLL Upper: %s\n", formatFloatSlice(data.BOLLUpper)))
		sb.WriteString(fmt.Sprintf("BOLL Middle: %s\n", formatFloatSlice(data.BOLLMiddle)))
		sb.WriteString(fmt.Sprintf("BOLL Lower: %s\n", formatFloatSlice(data.BOLLLower)))
	}

	sb.WriteString("\n")
}

func (e *StrategyEngine) formatQuantData(data *QuantData) string {
	if data == nil {
		return ""
	}

	indicators := e.config.Indicators
	if !indicators.EnableQuantOI && !indicators.EnableQuantNetflow {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 %s Quantitative Data:\n", data.Symbol))

	if len(data.PriceChange) > 0 {
		sb.WriteString("Price Change: ")
		timeframes := []string{"5m", "15m", "1h", "4h", "12h", "24h"}
		parts := []string{}
		for _, tf := range timeframes {
			if v, ok := data.PriceChange[tf]; ok {
				parts = append(parts, fmt.Sprintf("%s: %+.4f%%", tf, v*100))
			}
		}
		sb.WriteString(strings.Join(parts, " | "))
		sb.WriteString("\n")
	}

	if indicators.EnableQuantNetflow && data.Netflow != nil {
		sb.WriteString("Fund Flow (Netflow):\n")
		timeframes := []string{"5m", "15m", "1h", "4h", "12h", "24h"}

		if data.Netflow.Institution != nil {
			if data.Netflow.Institution.Future != nil && len(data.Netflow.Institution.Future) > 0 {
				sb.WriteString("  Institutional Futures:\n")
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Institution.Future[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
			if data.Netflow.Institution.Spot != nil && len(data.Netflow.Institution.Spot) > 0 {
				sb.WriteString("  Institutional Spot:\n")
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Institution.Spot[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
		}

		if data.Netflow.Personal != nil {
			if data.Netflow.Personal.Future != nil && len(data.Netflow.Personal.Future) > 0 {
				sb.WriteString("  Retail Futures:\n")
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Personal.Future[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
			if data.Netflow.Personal.Spot != nil && len(data.Netflow.Personal.Spot) > 0 {
				sb.WriteString("  Retail Spot:\n")
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Personal.Spot[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
		}
	}

	if indicators.EnableQuantOI && len(data.OI) > 0 {
		for exchange, oiData := range data.OI {
			if len(oiData.Delta) > 0 {
				sb.WriteString(fmt.Sprintf("Open Interest (%s):\n", exchange))
				for _, tf := range []string{"5m", "15m", "1h", "4h", "12h", "24h"} {
					if d, ok := oiData.Delta[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %+.4f%% (%s)\n", tf, d.OIDeltaPercent, formatFlowValue(d.OIDeltaValue)))
					}
				}
			}
		}
	}

	return sb.String()
}

func formatFlowValue(v float64) string {
	sign := ""
	if v >= 0 {
		sign = "+"
	}
	absV := v
	if absV < 0 {
		absV = -absV
	}
	if absV >= 1e9 {
		return fmt.Sprintf("%s%.2fB", sign, v/1e9)
	} else if absV >= 1e6 {
		return fmt.Sprintf("%s%.2fM", sign, v/1e6)
	} else if absV >= 1e3 {
		return fmt.Sprintf("%s%.2fK", sign, v/1e3)
	}
	return fmt.Sprintf("%s%.2f", sign, v)
}

func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = fmt.Sprintf("%.4f", v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}
