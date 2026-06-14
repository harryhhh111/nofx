package backtest

func CalculateMetrics(initialEquity float64, trades []BacktestTrade, curve []EquityPoint) Metrics {
	metrics := Metrics{
		InitialEquity: initialEquity,
		FinalEquity:   initialEquity,
		TotalTrades:   len(trades),
	}
	if len(curve) > 0 {
		metrics.FinalEquity = curve[len(curve)-1].Equity
	}
	if initialEquity > 0 {
		metrics.TotalReturnPct = (metrics.FinalEquity - initialEquity) / initialEquity * 100
	}

	var grossProfit, grossLossAbs float64
	var winSum, lossSum float64
	for _, trade := range trades {
		metrics.GrossPnL += trade.GrossPnL
		metrics.NetPnL += trade.NetPnL
		metrics.Fees += trade.Fees

		if trade.GrossPnL > 0 {
			grossProfit += trade.GrossPnL
		} else if trade.GrossPnL < 0 {
			grossLossAbs += -trade.GrossPnL
		}

		if trade.NetPnL > 0 {
			metrics.WinningTrades++
			winSum += trade.NetPnL
		} else if trade.NetPnL < 0 {
			metrics.LosingTrades++
			lossSum += trade.NetPnL
		}
	}

	if metrics.TotalTrades > 0 {
		metrics.WinRatePct = float64(metrics.WinningTrades) / float64(metrics.TotalTrades) * 100
	}
	if metrics.WinningTrades > 0 {
		metrics.AvgWin = winSum / float64(metrics.WinningTrades)
	}
	if metrics.LosingTrades > 0 {
		metrics.AvgLoss = lossSum / float64(metrics.LosingTrades)
	}
	if grossLossAbs > 0 {
		metrics.ProfitFactor = grossProfit / grossLossAbs
	}
	if grossProfit > 0 {
		metrics.FeeToGrossProfitPct = metrics.Fees / grossProfit * 100
	}
	metrics.MaxDrawdownPct = maxDrawdownPct(curve)

	return metrics
}

func maxDrawdownPct(curve []EquityPoint) float64 {
	if len(curve) == 0 {
		return 0
	}

	peak := curve[0].Equity
	maxDD := 0.0
	for _, point := range curve {
		if point.Equity > peak {
			peak = point.Equity
		}
		if peak <= 0 {
			continue
		}
		drawdown := (peak - point.Equity) / peak * 100
		if drawdown > maxDD {
			maxDD = drawdown
		}
	}
	return maxDD
}
