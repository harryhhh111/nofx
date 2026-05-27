package kernel

import (
	"fmt"
	"nofx/market"
	"nofx/provider/nofxos"
	"nofx/store"
	"strings"
	"time"
)

func (e *StrategyEngine) configuredAnalysisTimeframes() []string {
	timeframes := append([]string{}, e.config.Indicators.Klines.SelectedTimeframes...)
	if len(timeframes) == 0 {
		if primary := strings.TrimSpace(e.config.Indicators.Klines.PrimaryTimeframe); primary != "" {
			timeframes = append(timeframes, primary)
		}
		if longer := strings.TrimSpace(e.config.Indicators.Klines.LongerTimeframe); longer != "" {
			timeframes = append(timeframes, longer)
		}
	}
	return orderedTimeframesForPrompt(timeframes)
}

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
		if lang == LangChinese {
			sb.WriteString("# 你是一个专业的加密货币交易 AI\n\n")
			sb.WriteString("你的任务是根据提供的市场数据做出交易决策。\n\n")
		} else {
			sb.WriteString("# You are a professional cryptocurrency trading AI\n\n")
			sb.WriteString("Your task is to make trading decisions based on provided market data.\n\n")
		}
	}

	configuredTimeframes := e.configuredAnalysisTimeframes()
	if len(configuredTimeframes) > 0 {
		if lang == LangChinese {
			sb.WriteString("## 多周期分析要求\n")
			sb.WriteString(fmt.Sprintf("- 本轮已配置 K 线周期：%s\n", strings.Join(configuredTimeframes, ", ")))
			sb.WriteString("- 你必须对每个持仓和候选币逐个分析上述所有已配置周期，不能忽略任一周期\n")
			sb.WriteString("- 开仓、平仓、HOLD、WAIT 都必须综合全部已配置周期，而不是只盯某一个周期\n")
			sb.WriteString("- 高周期负责方向和结构，中周期负责形态确认，低周期只负责入场时机辅助\n")
			sb.WriteString("- 若某个已配置周期缺失数据，必须明确说明“该周期数据缺失”，禁止脑补结论\n\n")
		} else {
			sb.WriteString("## Multi-Timeframe Analysis Requirement\n")
			sb.WriteString(fmt.Sprintf("- Configured K-line timeframes for this run: %s\n", strings.Join(configuredTimeframes, ", ")))
			sb.WriteString("- You MUST analyze every configured timeframe for each position and candidate symbol; do not skip any configured timeframe\n")
			sb.WriteString("- OPEN, CLOSE, HOLD, and WAIT decisions must synthesize all configured timeframes, not just one anchor timeframe\n")
			sb.WriteString("- Higher timeframes define trend/structure, mid timeframes confirm setup quality, lower timeframes only assist entry timing\n")
			sb.WriteString("- If data for any configured timeframe is missing, state that explicitly and do not invent conclusions for that timeframe\n\n")
		}
	}

	// 2. Trading mode variant
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "aggressive":
		if lang == LangChinese {
			sb.WriteString("## 模式：激进\n- 优先捕捉趋势突破，信心 ≥ 70 时可分批建仓\n- 允许较高仓位，但必须严格设定止损并说明风险回报比\n\n")
		} else {
			sb.WriteString("## Mode: Aggressive\n- Prioritize capturing trend breakouts, can build positions in batches when confidence ≥ 70\n- Allow higher positions, but must strictly set stop-loss and explain risk-reward ratio\n\n")
		}
	case "conservative":
		if lang == LangChinese {
			sb.WriteString("## 模式：保守\n- 仅在多信号共振时开仓\n- 优先保全资金，连续亏损后必须暂停多个周期\n\n")
		} else {
			sb.WriteString("## Mode: Conservative\n- Only open positions when multiple signals resonate\n- Prioritize cash preservation, must pause for multiple periods after consecutive losses\n\n")
		}
	case "scalping":
		if lang == LangChinese {
			sb.WriteString("## 模式：超短线\n- 专注短期动量，较小的盈利目标但要求快速行动\n- 若价格在两根K线内未按预期移动，立即减仓或止损\n\n")
		} else {
			sb.WriteString("## Mode: Scalping\n- Focus on short-term momentum, smaller profit targets but require quick action\n- If price doesn't move as expected within two bars, immediately reduce position or stop-loss\n\n")
		}
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

	if lang == LangChinese {
		sb.WriteString("# \u786c\u6027\u7ea6\u675f\uff08\u98ce\u63a7\uff09\n\n")
		sb.WriteString("## \u4ee3\u7801\u5f3a\u5236\u6267\u884c\uff08\u540e\u7aef\u6821\u9a8c\uff0c\u65e0\u6cd5\u7ed5\u8fc7\uff09\uff1a\n")
		sb.WriteString(fmt.Sprintf("- \u6700\u5927\u6301\u4ed3\u6570\uff1a\u540c\u65f6 %d \u4e2a\u5e01\u79cd\n", riskControl.MaxPositions))
		sb.WriteString(fmt.Sprintf("- \u4ed3\u4f4d\u4ef7\u503c\u4e0a\u9650\uff08\u5c71\u5be8\u5e01\uff09\uff1a\u6700\u5927 %.0f USDT\uff08= \u51c0\u503c %.0f \u00d7 %.1f \u500d\uff09\n",
			accountEquity*altcoinPosValueRatio, accountEquity, altcoinPosValueRatio))
		sb.WriteString(fmt.Sprintf("- \u4ed3\u4f4d\u4ef7\u503c\u4e0a\u9650\uff08BTC/ETH\uff09\uff1a\u6700\u5927 %.0f USDT\uff08= \u51c0\u503c %.0f \u00d7 %.1f \u500d\uff09\n",
			accountEquity*btcEthPosValueRatio, accountEquity, btcEthPosValueRatio))
		sb.WriteString(fmt.Sprintf("- \u6700\u5927\u4fdd\u8bc1\u91d1\u4f7f\u7528\u7387\uff1a<= %.0f%%\n", riskControl.MaxMarginUsage*100))
		sb.WriteString(fmt.Sprintf("- \u6700\u5c0f\u4ed3\u4f4d\u5927\u5c0f\uff1a>= %.0f USDT\n\n", riskControl.MinPositionSize))

		sb.WriteString("## AI \u6307\u5bfc\uff08\u5efa\u8bae\u9075\u5b88\uff09\uff1a\n")
		sb.WriteString(fmt.Sprintf("- \u4ea4\u6613\u6760\u6746\uff1a\u5c71\u5be8\u5e01\u6700\u9ad8 %dx | BTC/ETH \u6700\u9ad8 %dx\n",
			riskControl.AltcoinMaxLeverage, riskControl.BTCETHMaxLeverage))
		sb.WriteString(fmt.Sprintf("- \u98ce\u9669\u56de\u62a5\u6bd4\uff1a\u76ee\u6807 >= 1:%.1f\u3002\u516c\u5f0f\uff1a\u6536\u76ca = |\u6b62\u76c8 - \u5165\u573a|\uff0c\u98ce\u9669 = |\u5165\u573a - \u6b62\u635f|\uff0c\u6bd4\u7387 = \u6536\u76ca / \u98ce\u9669\u3002\n", riskControl.MinRiskRewardRatio))
		sb.WriteString(fmt.Sprintf("  **\u5f00\u4ed3\u524d\u5fc5\u987b\u5728 reasoning \u4e2d\u8ba1\u7b97\u5e76\u5199\u51fa\u8be5\u6bd4\u7387\u3002** \u4f8b\u5982\uff1a\"entry {price}, SL {sl}, TP {tp} -> risk={entry-sl}, reward={tp-entry}, ratio={reward/risk}\uff08\u76ee\u6807 >= %.1f\uff09\"\n", riskControl.MinRiskRewardRatio))
		sb.WriteString(fmt.Sprintf("  \u82e5\u6bd4\u7387 < %.1f\uff0c\u5f3a\u70c8\u5efa\u8bae\u8df3\u8fc7\uff1b\u82e5\u63a5\u8fd1\u76ee\u6807\uff08\u8fbe\u5230 %.1f \u4ee5\u4e0a\uff09\uff0c\u9700\u8981\u66f4\u5f3a\u7684\u591a\u5468\u671f\u5171\u632f\u786e\u8ba4\u3002\n", riskControl.MinRiskRewardRatio, riskControl.MinRiskRewardRatio*0.8))
		sb.WriteString(fmt.Sprintf("- \u6700\u4f4e\u5f00\u4ed3\u4fe1\u5fc3\uff1a>= %d\n\n", riskControl.MinConfidence))
	} else {
		sb.WriteString("# Hard Constraints (Risk Control)\n\n")
		sb.WriteString("## CODE ENFORCED (Backend validation, cannot be bypassed):\n")
		sb.WriteString(fmt.Sprintf("- Max Positions: %d coins simultaneously\n", riskControl.MaxPositions))
		sb.WriteString(fmt.Sprintf("- Position Value Limit (Altcoins): max %.0f USDT (= equity %.0f x %.1f)\n",
			accountEquity*altcoinPosValueRatio, accountEquity, altcoinPosValueRatio))
		sb.WriteString(fmt.Sprintf("- Position Value Limit (BTC/ETH): max %.0f USDT (= equity %.0f x %.1f)\n",
			accountEquity*btcEthPosValueRatio, accountEquity, btcEthPosValueRatio))
		sb.WriteString(fmt.Sprintf("- Max Margin Usage: <= %.0f%%\n", riskControl.MaxMarginUsage*100))
		sb.WriteString(fmt.Sprintf("- Min Position Size: >= %.0f USDT\n\n", riskControl.MinPositionSize))

		sb.WriteString("## AI GUIDED (Recommended, you should follow):\n")
		sb.WriteString(fmt.Sprintf("- Trading Leverage: Altcoins max %dx | BTC/ETH max %dx\n",
			riskControl.AltcoinMaxLeverage, riskControl.BTCETHMaxLeverage))
		sb.WriteString(fmt.Sprintf("- Risk-Reward Ratio: target >= 1:%.1f. Formula: reward = |take_profit - entry|, risk = |entry - stop_loss|, ratio = reward / risk.\n", riskControl.MinRiskRewardRatio))
		sb.WriteString(fmt.Sprintf("  **You MUST calculate and write the ratio in your reasoning before opening any position.** Format: \"entry {price}, SL {sl}, TP {tp} -> risk={entry-sl}, reward={tp-entry}, ratio={reward/risk} (target >= %.1f)\"\n", riskControl.MinRiskRewardRatio))
		sb.WriteString(fmt.Sprintf("  If ratio < %.1f, strongly consider skipping. If ratio is close (within 80%% of target, i.e. >= %.1f), accept only with strong multi-timeframe confirmation.\n", riskControl.MinRiskRewardRatio, riskControl.MinRiskRewardRatio*0.8))
		sb.WriteString(fmt.Sprintf("- Min Confidence: >= %d to open position\n\n", riskControl.MinConfidence))
	}

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
	if lang == LangChinese {
		sb.WriteString("## 止损与入场质量\n")
		sb.WriteString("- **重要：使用 15m 或 1h ATR14 评估止损，不要用 3m ATR**（3m ATR 太小，止损距离没有意义）\n")
		sb.WriteString(fmt.Sprintf("- 止损缓冲：止损设在支撑/阻力位之外至少 %.1f × ATR14(15m 或 1h)（不要刚好设在支撑/阻力位上）\n", atrBuffer))
		sb.WriteString(fmt.Sprintf("  公式（做多）：止损 = 支撑 - ATR14(15m) × %.1f |（做空）：止损 = 阻力 + ATR14(15m) × %.1f\n", atrBuffer, atrBuffer))
		sb.WriteString("- 入场质量：仅在 ① 关键支撑/阻力位附近（反转）或 ② 突破确认后（趋势）入场\n")
		sb.WriteString("  不要在支撑和阻力之间的中间地带入场——止损/目标几何不佳\n")
		sb.WriteString(fmt.Sprintf("- 若止损距离（入场到止损）< %.1f × ATR14(15m 或 1h)，则交易设置太紧——跳过或等待更好的入场点\n\n", atrBuffer))

		sb.WriteString("## 开仓决策流程（必须按此顺序）\n\n")
		sb.WriteString("**第一步：先确定技术位（此阶段忽略杠杆和仓位大小）**\n")
		sb.WriteString("- 在你的分析时间框架上找到支撑/阻力\n")
		sb.WriteString("- 设 止损 = 支撑 - ATR 缓冲（做多）或 阻力 + ATR 缓冲（做空）\n")
		sb.WriteString("- 设 止盈 = 图表结构中的目标位\n")
		sb.WriteString("- 计算 R:R 比率 = |止盈 - 入场| / |入场 - 止损|\n")
		sb.WriteString(fmt.Sprintf("- 若 R:R < %.1f → **跳过该交易**。不要缩小止损来强行提高比率。\n\n", riskControl.MinRiskRewardRatio))

		sb.WriteString("**第二步：根据信心确定仓位大小（在配置限制内）**\n")
		sb.WriteString("- 高信心（≥85）：使用最大仓位价值限制的 80-100%%\n")
		sb.WriteString("- 中信心（70-84）：使用最大仓位价值限制的 50-80%%\n")
		sb.WriteString("- 低信心（60-69）：使用最大仓位价值限制的 30-50%%\n")
		sb.WriteString(fmt.Sprintf("- 仓位价值上限：BTC/ETH 最大 %.0f USDT | 山寨币最大 %.0f USDT\n",
			accountEquity*btcEthPosValueRatio, accountEquity*altcoinPosValueRatio))
		sb.WriteString("- 接受止损触发时的亏损金额——这是正常且预期的。\n\n")

		sb.WriteString("**⛔ 禁止**（违反 = 无效交易）：\n")
		sb.WriteString("- 缩小止损来减少潜在亏损。止损由图表结构 + ATR 决定，不由仓位大小或杠杆决定。\n")
		sb.WriteString("- 基于杠杆设止损（如「10 倍杠杆所以用 -0.5%% 止损」）。杠杆影响盈亏幅度，不影响止损位置。\n")
		sb.WriteString("- 先选止损再凑止盈来满足 R:R。止盈必须来自真实的图表目标。\n\n")
	} else {
		sb.WriteString("## Stop-Loss & Entry Quality\n")
		sb.WriteString("- **IMPORTANT: Use 15m or 1h ATR14 for stop-loss evaluation, NOT the 3m ATR** (3m ATR is too small for meaningful stop-loss distances)\n")
		sb.WriteString(fmt.Sprintf("- Stop-loss buffer: set stop-loss at least %.1f × ATR14(15m or 1h) BEYOND the support/resistance level (not right at it)\n", atrBuffer))
		sb.WriteString(fmt.Sprintf("  Formula (long): stop = support - ATR14(15m) × %.1f | (short): stop = resistance + ATR14(15m) × %.1f\n", atrBuffer, atrBuffer))
		sb.WriteString("- Entry quality: only enter at ① near key support/resistance (reversal) or ② after breakout confirmation (trend)\n")
		sb.WriteString("  Do NOT enter in the middle zone between support and resistance — poor stop/target geometry\n")
		sb.WriteString(fmt.Sprintf("- If your stop distance (entry to stop) < %.1f × ATR14(15m or 1h), the trade setup is too tight — skip or wait for better entry\n\n", atrBuffer))

		sb.WriteString("## Opening Decision Flow (MUST follow this order)\n\n")
		sb.WriteString("**Step 1: Technical levels FIRST (ignore leverage and position size at this stage)**\n")
		sb.WriteString("- Find support/resistance on your analysis timeframe\n")
		sb.WriteString("- Set SL = support - ATR buffer (long) or resistance + ATR buffer (short)\n")
		sb.WriteString("- Set TP = target level from chart structure\n")
		sb.WriteString("- Calculate R:R ratio = |TP - entry| / |entry - SL|\n")
		sb.WriteString(fmt.Sprintf("- If R:R < %.1f → **SKIP the trade**. Do NOT shrink SL to force a better ratio.\n\n", riskControl.MinRiskRewardRatio))

		sb.WriteString("**Step 2: Position size based on confidence (within configured limits)**\n")
		sb.WriteString("- High confidence (≥85): Use 80-100%% of max position value limit\n")
		sb.WriteString("- Medium confidence (70-84): Use 50-80%% of max position value limit\n")
		sb.WriteString("- Low confidence (60-69): Use 30-50%% of max position value limit\n")
		sb.WriteString(fmt.Sprintf("- Position Value Limits: BTC/ETH max %.0f USDT | Altcoins max %.0f USDT\n",
			accountEquity*btcEthPosValueRatio, accountEquity*altcoinPosValueRatio))
		sb.WriteString("- Accept the resulting loss amount if SL is hit — this is normal and expected.\n\n")

		sb.WriteString("**⛔ FORBIDDEN** (violation = invalid trade):\n")
		sb.WriteString("- Shrinking SL to reduce potential loss. SL is determined by chart structure + ATR, NOT by position size or leverage.\n")
		sb.WriteString("- Setting SL based on leverage (e.g., \"10x so I'll use -0.5%% SL\"). Leverage affects P&L magnitude, NOT where SL should be.\n")
		sb.WriteString("- Choosing SL first then fitting TP to meet R:R. TP must come from real chart targets.\n\n")
	}

	// 4. Trading frequency (editable)
	if promptSections.TradingFrequency != "" {
		sb.WriteString(promptSections.TradingFrequency)
		sb.WriteString("\n\n")
	} else {
		if lang == LangChinese {
			sb.WriteString("# ⏱️ 交易频率意识\n\n")
			sb.WriteString("- 优秀交易者：每天 2-4 笔交易 ≈ 每小时 0.1-0.2 笔\n")
			sb.WriteString("- 每小时 >2 笔 = 过度交易\n")
			sb.WriteString("- 单笔持仓时间 ≥ 30-60 分钟\n")
			sb.WriteString("如果你发现自己每个周期都在交易 → 标准太低；如果平仓时间 < 30 分钟 → 太急躁。\n\n")
		} else {
			sb.WriteString("# ⏱️ Trading Frequency Awareness\n\n")
			sb.WriteString("- Excellent traders: 2-4 trades/day ≈ 0.1-0.2 trades/hour\n")
			sb.WriteString("- >2 trades/hour = Overtrading\n")
			sb.WriteString("- Single position hold time ≥ 30-60 minutes\n")
			sb.WriteString("If you find yourself trading every period → standards too low; if closing positions < 30 minutes → too impatient.\n\n")
		}
	}

	// 5. Entry standards (editable)
	if promptSections.EntryStandards != "" {
		sb.WriteString(promptSections.EntryStandards)
		if lang == LangChinese {
			sb.WriteString("\n\n你拥有以下指标数据：\n")
		} else {
			sb.WriteString("\n\nYou have the following indicator data:\n")
		}
		e.writeAvailableIndicators(&sb)
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("\n**信心 ≥ %d** 才能开仓。\n\n", riskControl.MinConfidence))
		} else {
			sb.WriteString(fmt.Sprintf("\n**Confidence ≥ %d** required to open positions.\n\n", riskControl.MinConfidence))
		}
	} else {
		if lang == LangChinese {
			sb.WriteString("# 🎯 入场标准（严格）\n\n")
			sb.WriteString("仅在多信号共振时开仓。你拥有：\n")
			e.writeAvailableIndicators(&sb)
			sb.WriteString(fmt.Sprintf("\n可使用任何有效的分析方法，但**信心 ≥ %d** 才能开仓；避免单一指标、信号矛盾、横盘整理期入场、平仓后立即重新开仓等低质量行为。\n\n", riskControl.MinConfidence))
		} else {
			sb.WriteString("# 🎯 Entry Standards (Strict)\n\n")
			sb.WriteString("Only open positions when multiple signals resonate. You have:\n")
			e.writeAvailableIndicators(&sb)
			sb.WriteString(fmt.Sprintf("\nFeel free to use any effective analysis method, but **confidence ≥ %d** required to open positions; avoid low-quality behaviors such as single indicators, contradictory signals, sideways consolidation, reopening immediately after closing, etc.\n\n", riskControl.MinConfidence))
		}
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
		sb.WriteString("`[THESIS] timeframe={one configured timeframe} | invalidation={same-timeframe verifiable condition} | min_target=+1.5% [/THESIS]`\n")
		if len(configuredTimeframes) > 0 {
			sb.WriteString(fmt.Sprintf("Configured timeframes for this run: %s. Your opening analysis must consider the full multi-timeframe picture before choosing the thesis timeframe.\n", strings.Join(configuredTimeframes, ", ")))
		}
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
		sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97500.0, \"take_profit\": 96200.0, \"confidence\": 85, \"risk_usd\": 32.5, \"reasoning\": \"[THESIS] timeframe={分析周期} | invalidation={具体可验证的失效条件} | min_target={目标收益%%} [/THESIS] {基于实际数据的详细分析}\"},\n",
			riskControl.BTCETHMaxLeverage, examplePositionSize))
		sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"reasoning\": \"[TIMEFRAME] 开仓周期={TF}，当前使用同周期数据。 [THESIS] 失效条件={条件}；当前已触发。 [FEES] 毛利{X}，手续费{Y}，Net PnL={X-Y}。 [MIN PROFIT] {是否满足}。 [CLOSE CONFIDENCE] {0-100}。 [VERDICT] {结论}。\"}\n")
		sb.WriteString("]\n```\n")
		sb.WriteString("</decision>\n\n")
		sb.WriteString("## 字段说明\n\n")
		sb.WriteString("- `action`: open_long | open_short | close_long | close_short | hold | wait\n")
		sb.WriteString(fmt.Sprintf("- `confidence`: 0-100（开仓建议 ≥ %d）\n", riskControl.MinConfidence))
		sb.WriteString("- 开仓时必填：leverage, position_size_usd, stop_loss, take_profit, confidence, risk_usd, reasoning\n")
		sb.WriteString("- `stop_loss`、`take_profit`、`risk_usd` 必须是 JSON number，不能带引号\n")
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
		sb.WriteString(fmt.Sprintf("  {\"symbol\": \"BTCUSDT\", \"action\": \"open_short\", \"leverage\": %d, \"position_size_usd\": %.0f, \"stop_loss\": 97500.0, \"take_profit\": 96200.0, \"confidence\": 85, \"risk_usd\": 32.5, \"reasoning\": \"[THESIS] timeframe={TF} | invalidation={specific verifiable condition} | min_target={target%%} [/THESIS] {Detailed analysis based on actual data}\"},\n",
			riskControl.BTCETHMaxLeverage, examplePositionSize))
		sb.WriteString("  {\"symbol\": \"ETHUSDT\", \"action\": \"close_long\", \"reasoning\": \"[TIMEFRAME] Opened on {TF}, evaluating on same TF. [THESIS] Invalidation={condition}; currently triggered. [FEES] Gross {X}, fees {Y}, Net PnL {X-Y}. [MIN PROFIT] {met or N/A}. [CLOSE CONFIDENCE] {0-100}. [VERDICT] {conclusion}.\"}\n")
		sb.WriteString("]\n```\n")
		sb.WriteString("</decision>\n\n")
		sb.WriteString("## Field Description\n\n")
		sb.WriteString("- `action`: open_long | open_short | close_long | close_short | hold | wait\n")
		sb.WriteString(fmt.Sprintf("- `confidence`: 0-100 (opening recommended ≥ %d)\n", riskControl.MinConfidence))
		sb.WriteString("- Required when opening: leverage, position_size_usd, stop_loss, take_profit, confidence, risk_usd, reasoning\n")
		sb.WriteString("- `stop_loss`, `take_profit`, `risk_usd`: must be JSON numbers, not quoted strings\n")
		sb.WriteString("- `reasoning`: **REQUIRED for ALL actions**\n")
		sb.WriteString("  - **When opening (open_long/open_short)**: MUST begin with `[THESIS] timeframe=X | invalidation=condition | min_target=+X% [/THESIS]` followed by analysis. This becomes a BINDING RULE for future cycles.\n")
		sb.WriteString("  - **When closing**: MUST include all 6 items of the Close-Position Checklist ([TIMEFRAME], [THESIS], [FEES], [MIN PROFIT], [CLOSE CONFIDENCE], [VERDICT])\n")
		sb.WriteString("  - **When holding/waiting**: Brief explanation of why (1-2 sentences)\n")
		sb.WriteString("- **IMPORTANT**: All numeric values must be calculated numbers, NOT formulas/expressions (e.g., use `27.76` not `3000 * 0.01`)\n\n")
	}

	// 8. Custom Prompt
	if e.config.CustomPrompt != "" {
		if lang == LangChinese {
			sb.WriteString("# 📌 个性化交易策略\n\n")
			sb.WriteString(e.config.CustomPrompt)
			sb.WriteString("\n\n")
			sb.WriteString("注意：以上个性化策略是基本规则的补充，不能违反基本风控原则。\n")
		} else {
			sb.WriteString("# 📌 Personalized Trading Strategy\n\n")
			sb.WriteString(e.config.CustomPrompt)
			sb.WriteString("\n\n")
			sb.WriteString("Note: The above personalized strategy is a supplement to the basic rules and cannot violate the basic risk control principles.\n")
		}
	}

	return sb.String()
}

func (e *StrategyEngine) writeAvailableIndicators(sb *strings.Builder) {
	indicators := e.config.Indicators
	lang := e.GetLanguage()
	configuredTimeframes := e.configuredAnalysisTimeframes()

	if len(configuredTimeframes) > 0 {
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("- K\u7ebf\u5e8f\u5217\uff1a%s\n", strings.Join(configuredTimeframes, ", ")))
		} else {
			sb.WriteString(fmt.Sprintf("- K-line series: %s\n", strings.Join(configuredTimeframes, ", ")))
		}
	} else {
		kline := indicators.Klines
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("- %s K\u7ebf\u5e8f\u5217\n", kline.PrimaryTimeframe))
		} else {
			sb.WriteString(fmt.Sprintf("- %s price series\n", kline.PrimaryTimeframe))
		}
	}

	if indicators.EnableEMA {
		if lang == LangChinese {
			sb.WriteString("- EMA 指标")
		} else {
			sb.WriteString("- EMA indicators")
		}
		if len(indicators.EMAPeriods) > 0 {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("（周期：%v）", indicators.EMAPeriods))
			} else {
				sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.EMAPeriods))
			}
		}
		sb.WriteString("\n")
	}

	if indicators.EnableMACD {
		if lang == LangChinese {
			sb.WriteString("- MACD 指标\n")
		} else {
			sb.WriteString("- MACD indicators\n")
		}
	}

	if indicators.EnableRSI {
		if lang == LangChinese {
			sb.WriteString("- RSI 指标")
		} else {
			sb.WriteString("- RSI indicators")
		}
		if len(indicators.RSIPeriods) > 0 {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("（周期：%v）", indicators.RSIPeriods))
			} else {
				sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.RSIPeriods))
			}
		}
		sb.WriteString("\n")
	}

	if indicators.EnableATR {
		if lang == LangChinese {
			sb.WriteString("- ATR 指标")
		} else {
			sb.WriteString("- ATR indicators")
		}
		if len(indicators.ATRPeriods) > 0 {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("（周期：%v）", indicators.ATRPeriods))
			} else {
				sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.ATRPeriods))
			}
		}
		sb.WriteString("\n")
	}

	if indicators.EnableBOLL {
		if lang == LangChinese {
			sb.WriteString("- 布林带（BOLL）- 上轨/中轨/下轨")
		} else {
			sb.WriteString("- Bollinger Bands (BOLL) - Upper/Middle/Lower bands")
		}
		if len(indicators.BOLLPeriods) > 0 {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("（周期：%v）", indicators.BOLLPeriods))
			} else {
				sb.WriteString(fmt.Sprintf(" (periods: %v)", indicators.BOLLPeriods))
			}
		}
		sb.WriteString("\n")
	}

	if indicators.EnableVolume {
		if lang == LangChinese {
			sb.WriteString("- 成交量数据\n")
		} else {
			sb.WriteString("- Volume data\n")
		}
	}

	if indicators.EnableOI {
		if lang == LangChinese {
			sb.WriteString("- 持仓量（OI）数据\n")
		} else {
			sb.WriteString("- Open Interest (OI) data\n")
		}
	}

	if indicators.EnableFundingRate {
		if lang == LangChinese {
			sb.WriteString("- 资金费率\n")
		} else {
			sb.WriteString("- Funding rate\n")
		}
	}

	if len(e.config.CoinSource.StaticCoins) > 0 || e.config.CoinSource.UseAI500 || e.config.CoinSource.UseOITop {
		if lang == LangChinese {
			sb.WriteString("- AI500 / OI_Top 筛选标签（如可用）\n")
		} else {
			sb.WriteString("- AI500 / OI_Top filter tags (if available)\n")
		}
	}

	if indicators.EnableQuantData {
		if lang == LangChinese {
			sb.WriteString("- 量化数据（机构/散户资金流向、持仓变化、多周期价格变动）\n")
		} else {
			sb.WriteString("- Quantitative data (institutional/retail fund flow, position changes, multi-period price changes)\n")
		}
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

	configuredTimeframes := e.configuredAnalysisTimeframes()
	if len(configuredTimeframes) > 0 {
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("分析周期: %s\n", strings.Join(configuredTimeframes, ", ")))
			sb.WriteString("要求: 对每个持仓和候选币都必须逐个检查以上所有周期，并在最终结论中综合这些周期，不能只引用单一周期。\n\n")
		} else {
			sb.WriteString(fmt.Sprintf("Analysis timeframes: %s\n", strings.Join(configuredTimeframes, ", ")))
			sb.WriteString("Requirement: for every position and candidate symbol, you must check all listed timeframes above and synthesize them into the final decision instead of citing only one timeframe.\n\n")
		}
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
				if lang == LangChinese {
					sb.WriteString("**生效约束（来自开仓论点）：**\n")
				} else {
					sb.WriteString("**ACTIVE CONSTRAINTS (from opening thesis):**\n")
				}
				for _, part := range strings.Split(thesisBlock, "|") {
					part = strings.TrimSpace(part)
					if part != "" {
						sb.WriteString(fmt.Sprintf("  - %s\n", part))
					}
				}
				if lang == LangChinese {
					sb.WriteString("  - **规则**：仅使用上述时间框架或更高框架评估此仓位。禁止基于更低周期噪音平仓。\n")
					sb.WriteString("  - **规则**：止盈时 Net PnL 必须 ≥ 手续费×2。若未满足且 thesis 仍有效 → 必须 HOLD。\n")
				} else {
					sb.WriteString("  - **RULE**: Only use the above timeframe or higher to evaluate this position. FORBIDDEN to close based on lower timeframe noise.\n")
					sb.WriteString("  - **RULE**: Net PnL must ≥ fees×2 for profit-taking. If not met and thesis valid → MUST HOLD.\n")
				}
			} else {
				// Legacy unstructured format — still display as binding
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("**开仓论点（硬性约束）**：%s\n", reasoning))
					sb.WriteString("  → 平仓前必须验证这些条件在相同时间框架上仍然成立。\n")
				} else {
					sb.WriteString(fmt.Sprintf("**Opening thesis (BINDING)**: %s\n", reasoning))
					sb.WriteString("  → You MUST verify these conditions still hold on the same timeframe before closing.\n")
				}
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
			if lang == LangChinese {
				sb.WriteString("## 你上次对这些持仓的评估\n")
			} else {
				sb.WriteString("## Your Last Review of These Positions\n")
			}
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

	// Pre-calculate actual candidate count (excludes position coins already shown above)
	candidateDisplayCount := 0
	for _, coin := range ctx.CandidateCoins {
		if positionSymbols[market.Normalize(coin.Symbol)] {
			continue
		}
		if _, hasData := ctx.MarketDataMap[coin.Symbol]; hasData {
			candidateDisplayCount++
		}
	}

	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("## 候选币种（%d 个）\n\n", candidateDisplayCount))
	} else {
		sb.WriteString(fmt.Sprintf("## Candidate Coins (%d coins)\n\n", candidateDisplayCount))
	}
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
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("## 外部数据：%s\n", item.Label))
			if item.Description != "" {
				sb.WriteString(fmt.Sprintf("说明：%s\n", item.Description))
			}
		} else {
			sb.WriteString(fmt.Sprintf("## External Data: %s\n", item.Label))
			if item.Description != "" {
				sb.WriteString(fmt.Sprintf("Context: %s\n", item.Description))
			}
		}
		sb.WriteString(fmt.Sprintf("Data: %s\n\n", item.Data))
	}

	if lang == LangChinese {
		sb.WriteString("现在请分析并输出你的决策（思维链 + JSON）\n")
	} else {
		sb.WriteString("Now please analyze and output your decision (Chain of Thought + JSON)\n")
	}

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
	lang := e.GetLanguage()
	if len(sources) > 1 {
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
			if lang == LangChinese {
				return " (AI500+OI_Top 双信号)"
			}
			return " (AI500+OI_Top dual signal)"
		}
		if hasAI500 && hasOILow {
			if lang == LangChinese {
				return " (AI500+OI_Low 双信号)"
			}
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
		if lang == LangChinese {
			return " (多来源)"
		}
		return " (Multiple sources)"
	} else if len(sources) == 1 {
		switch sources[0] {
		case "ai500":
			return " (AI500)"
		case "oi_top":
			if lang == LangChinese {
				return " (OI_Top 高关注)"
			}
			return " (OI_Top high activity)"
		case "oi_low":
			if lang == LangChinese {
				return " (OI_Low 低关注)"
			}
			return " (OI_Low low activity)"
		case "static":
			if lang == LangChinese {
				return " (手动选择)"
			}
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
	lang := e.GetLanguage()

	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("=== %s 市场数据 ===\n\n", data.Symbol))
	} else {
		sb.WriteString(fmt.Sprintf("=== %s Market Data ===\n\n", data.Symbol))
	}
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
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("%s 附加数据：\n\n", data.Symbol))
		} else {
			sb.WriteString(fmt.Sprintf("Additional data for %s:\n\n", data.Symbol))
		}

		if indicators.EnableOI && data.OpenInterest != nil {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("持仓量：最新 %.2f 平均 %.2f\n\n",
					data.OpenInterest.Latest, data.OpenInterest.Average))
			} else {
				sb.WriteString(fmt.Sprintf("Open Interest: Latest: %.2f Average: %.2f\n\n",
					data.OpenInterest.Latest, data.OpenInterest.Average))
			}
		}

		if indicators.EnableFundingRate {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("资金费率：%.2e\n\n", data.FundingRate))
			} else {
				sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))
			}
		}
	}

	if len(data.TimeframeData) > 0 {
		timeframeOrder := []string{"1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "8h", "12h", "1d", "3d", "1w"}
		for _, tf := range timeframeOrder {
			if tfData, ok := data.TimeframeData[tf]; ok {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("=== %s 周期（从旧到新）===\n\n", strings.ToUpper(tf)))
				} else {
					sb.WriteString(fmt.Sprintf("=== %s Timeframe (oldest → latest) ===\n\n", strings.ToUpper(tf)))
				}
				e.formatTimeframeSeriesData(&sb, tfData, indicators)
			}
		}
	} else {
		if data.IntradaySeries != nil {
			klineConfig := indicators.Klines
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("日内序列（%s 间隔，从旧到新）：\n\n", klineConfig.PrimaryTimeframe))
			} else {
				sb.WriteString(fmt.Sprintf("Intraday series (%s intervals, oldest → latest):\n\n", klineConfig.PrimaryTimeframe))
			}

			if len(data.IntradaySeries.MidPrices) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("中间价：%s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
				} else {
					sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
				}
			}

			if indicators.EnableEMA && len(data.IntradaySeries.EMA20Values) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("EMA 指标（20周期）：%s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
				} else {
					sb.WriteString(fmt.Sprintf("EMA indicators (20-period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
				}
			}

			if indicators.EnableMACD && len(data.IntradaySeries.MACDValues) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("MACD 指标：%s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
				} else {
					sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
				}
			}

			if indicators.EnableRSI {
				if len(data.IntradaySeries.RSI7Values) > 0 {
					if lang == LangChinese {
						sb.WriteString(fmt.Sprintf("RSI 指标（7周期）：%s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
					} else {
						sb.WriteString(fmt.Sprintf("RSI indicators (7-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
					}
				}
				if len(data.IntradaySeries.RSI14Values) > 0 {
					if lang == LangChinese {
						sb.WriteString(fmt.Sprintf("RSI 指标（14周期）：%s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
					} else {
						sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
					}
				}
			}

			if indicators.EnableVolume && len(data.IntradaySeries.Volume) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("成交量：%s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
				} else {
					sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
				}
			}

			if indicators.EnableATR {
				sb.WriteString(fmt.Sprintf("3m ATR (14-period): %.3f\n\n", data.IntradaySeries.ATR14))
			}
		}

		if data.LongerTermContext != nil && indicators.Klines.EnableMultiTimeframe {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("长周期上下文（%s 周期）：\n\n", indicators.Klines.LongerTimeframe))
			} else {
				sb.WriteString(fmt.Sprintf("Longer-term context (%s timeframe):\n\n", indicators.Klines.LongerTimeframe))
			}

			if indicators.EnableEMA {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("20周期 EMA：%.3f vs 50周期 EMA：%.3f\n\n",
						data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
				} else {
					sb.WriteString(fmt.Sprintf("20-Period EMA: %.3f vs. 50-Period EMA: %.3f\n\n",
						data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
				}
			}

			if indicators.EnableATR {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("3周期 ATR：%.3f vs 14周期 ATR：%.3f\n\n",
						data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
				} else {
					sb.WriteString(fmt.Sprintf("3-Period ATR: %.3f vs. 14-Period ATR: %.3f\n\n",
						data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
				}
			}

			if indicators.EnableVolume {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("当前成交量：%.3f vs 平均成交量：%.3f\n\n",
						data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))
				} else {
					sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
						data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))
				}
			}

			if indicators.EnableMACD && len(data.LongerTermContext.MACDValues) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("MACD 指标：%s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
				} else {
					sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
				}
			}

			if indicators.EnableRSI && len(data.LongerTermContext.RSI14Values) > 0 {
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("RSI 指标（14周期）：%s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
				} else {
					sb.WriteString(fmt.Sprintf("RSI indicators (14-Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
				}
			}
		}
	}

	return sb.String()
}

func (e *StrategyEngine) formatTimeframeSeriesData(sb *strings.Builder, data *market.TimeframeSeriesData, indicators store.IndicatorConfig) {
	lang := e.GetLanguage()
	if len(data.Klines) > 0 {
		if lang == LangChinese {
			sb.WriteString("时间(UTC)       开盘      最高      最低      收盘      成交量\n")
		} else {
			sb.WriteString("Time(UTC)      Open      High      Low       Close     Volume\n")
		}
		for i, k := range data.Klines {
			t := time.Unix(k.Time/1000, 0).UTC()
			timeStr := t.Format("01-02 15:04")
			marker := ""
			if i == len(data.Klines)-1 {
				if lang == LangChinese {
					marker = "  <- 当前"
				} else {
					marker = "  <- current"
				}
			}
			sb.WriteString(fmt.Sprintf("%-14s %-9.4f %-9.4f %-9.4f %-9.4f %-12.2f%s\n",
				timeStr, k.Open, k.High, k.Low, k.Close, k.Volume, marker))
		}
		sb.WriteString("\n")
	} else if len(data.MidPrices) > 0 {
		if lang == LangChinese {
			sb.WriteString(fmt.Sprintf("中间价：%s\n\n", formatFloatSlice(data.MidPrices)))
		} else {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.MidPrices)))
		}
		if indicators.EnableVolume && len(data.Volume) > 0 {
			if lang == LangChinese {
				sb.WriteString(fmt.Sprintf("成交量：%s\n\n", formatFloatSlice(data.Volume)))
			} else {
				sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.Volume)))
			}
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

	lang := e.GetLanguage()
	var sb strings.Builder
	if lang == LangChinese {
		sb.WriteString(fmt.Sprintf("📊 %s 量化数据：\n", data.Symbol))
	} else {
		sb.WriteString(fmt.Sprintf("📊 %s Quantitative Data:\n", data.Symbol))
	}

	if len(data.PriceChange) > 0 {
		if lang == LangChinese {
			sb.WriteString("价格变动：")
		} else {
			sb.WriteString("Price Change: ")
		}
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
		if lang == LangChinese {
			sb.WriteString("资金流向（Netflow）：\n")
		} else {
			sb.WriteString("Fund Flow (Netflow):\n")
		}
		timeframes := []string{"5m", "15m", "1h", "4h", "12h", "24h"}

		if data.Netflow.Institution != nil {
			if data.Netflow.Institution.Future != nil && len(data.Netflow.Institution.Future) > 0 {
				if lang == LangChinese {
					sb.WriteString("  机构合约：\n")
				} else {
					sb.WriteString("  Institutional Futures:\n")
				}
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Institution.Future[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
			if data.Netflow.Institution.Spot != nil && len(data.Netflow.Institution.Spot) > 0 {
				if lang == LangChinese {
					sb.WriteString("  机构现货：\n")
				} else {
					sb.WriteString("  Institutional Spot:\n")
				}
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Institution.Spot[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
		}

		if data.Netflow.Personal != nil {
			if data.Netflow.Personal.Future != nil && len(data.Netflow.Personal.Future) > 0 {
				if lang == LangChinese {
					sb.WriteString("  散户合约：\n")
				} else {
					sb.WriteString("  Retail Futures:\n")
				}
				for _, tf := range timeframes {
					if v, ok := data.Netflow.Personal.Future[tf]; ok {
						sb.WriteString(fmt.Sprintf("    %s: %s\n", tf, formatFlowValue(v)))
					}
				}
			}
			if data.Netflow.Personal.Spot != nil && len(data.Netflow.Personal.Spot) > 0 {
				if lang == LangChinese {
					sb.WriteString("  散户现货：\n")
				} else {
					sb.WriteString("  Retail Spot:\n")
				}
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
				if lang == LangChinese {
					sb.WriteString(fmt.Sprintf("持仓量（%s）：\n", exchange))
				} else {
					sb.WriteString(fmt.Sprintf("Open Interest (%s):\n", exchange))
				}
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
