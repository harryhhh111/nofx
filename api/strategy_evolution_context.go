package api

import "nofx/store"

func (s *Server) buildStrategyEvolutionPerformance(userID, strategyID string) map[string]interface{} {
	if s.store == nil || userID == "" || strategyID == "" {
		return map[string]interface{}{"source": "none", "reason": "store, user_id, or strategy_id missing"}
	}
	traders, err := s.store.Trader().List(userID)
	if err != nil {
		return map[string]interface{}{"source": "none", "reason": err.Error()}
	}
	items := make([]map[string]interface{}, 0)
	totalTrades := 0
	var calibration map[string]interface{}
	if report, reportErr := s.store.SignalCalibration().BuildReport(strategyID, 1000); reportErr == nil && report != nil {
		calibration = buildEvolutionCalibrationSummary(report)
	}
	for _, trader := range traders {
		if trader == nil || trader.StrategyID != strategyID {
			continue
		}
		stats, _ := s.store.Position().GetFullStats(trader.ID)
		recentTrades, _ := s.store.Position().GetRecentTrades(trader.ID, 20)
		traderPerf := map[string]interface{}{
			"trader_id":     trader.ID,
			"trader_name":   trader.Name,
			"is_running":    trader.IsRunning,
			"total_trades":  0,
			"recent_trades": recentTrades,
		}
		if stats != nil {
			traderPerf["stats"] = stats
			traderPerf["total_trades"] = stats.TotalTrades
			totalTrades += stats.TotalTrades
		}
		items = append(items, traderPerf)
	}
	enoughHistory := totalTrades >= 10
	if calibration != nil {
		if enough, ok := calibration["enough_outcomes"].(bool); ok && enough {
			enoughHistory = true
		}
	}
	return map[string]interface{}{
		"source":              "trader_position_history+signal_calibration",
		"strategy_id":         strategyID,
		"trader_count":        len(items),
		"total_closed_trades": totalTrades,
		"traders":             items,
		"calibration":         calibration,
		"has_enough_history":  enoughHistory,
	}
}

func buildEvolutionCalibrationSummary(report *store.SignalCalibrationReport) map[string]interface{} {
	if report == nil {
		return nil
	}
	setupStats := make([]map[string]interface{}, 0, len(report.SetupStats))
	weakSetups := make([]map[string]interface{}, 0)
	for _, stat := range report.SetupStats {
		item := map[string]interface{}{
			"setup":           stat.Setup,
			"samples":         stat.Samples,
			"eligible":        stat.Eligible,
			"approved":        stat.Approved,
			"risk_rejected":   stat.RiskRejected,
			"review_rejected": stat.ReviewRejected,
			"no_signal":       stat.NoSignal,
			"closed_trades":   stat.ClosedTrades,
			"wins":            stat.Wins,
			"losses":          stat.Losses,
			"win_rate":        stat.WinRate,
			"total_pnl":       stat.TotalPnL,
			"average_pnl":     stat.AveragePnL,
		}
		setupStats = append(setupStats, item)
		if stat.ClosedTrades >= 5 && (stat.WinRate < 0.4 || stat.TotalPnL < 0) {
			weakSetups = append(weakSetups, item)
		}
	}
	return map[string]interface{}{
		"strategy_id":           report.StrategyID,
		"strategy_version":      report.StrategyVersion,
		"sample_count":          report.SampleCount,
		"signal_count":          report.SignalCount,
		"setup_count":           report.SetupCount,
		"eligible_count":        report.EligibleCount,
		"approved_count":        report.ApprovedCount,
		"no_signal_count":       report.NoSignalCount,
		"closed_trade_count":    report.ClosedTradeCount,
		"winning_trade_count":   report.WinningTradeCount,
		"losing_trade_count":    report.LosingTradeCount,
		"win_rate":              report.WinRate,
		"total_pnl":             report.TotalPnL,
		"average_pnl":           report.AveragePnL,
		"min_required_samples":  report.MinRequiredSamples,
		"min_required_outcomes": report.MinRequiredOutcomes,
		"enough_samples":        report.EnoughSamples,
		"enough_outcomes":       report.EnoughOutcomes,
		"quality_gate":          report.QualityGate,
		"recommendation":        report.Recommendation,
		"risk_status_counts":    report.RiskStatusCounts,
		"review_status_counts":  report.ReviewStatusCounts,
		"setup_stats":           setupStats,
		"weak_setups":           weakSetups,
	}
}

func mergeEvolutionPerformance(requested map[string]interface{}, generated map[string]interface{}) map[string]interface{} {
	if len(requested) == 0 {
		return generated
	}
	out := make(map[string]interface{}, len(generated)+len(requested)+1)
	for key, value := range generated {
		out[key] = value
	}
	for key, value := range requested {
		out[key] = value
	}
	out["server_generated_context"] = generated
	return out
}
