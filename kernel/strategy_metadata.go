package kernel

import "strings"

func SupportedIndicatorOperands() []string {
	return []string{
		"price",
		"ema", "sma", "rsi", "atr", "adx", "plus_di", "minus_di",
		"sar", "sar_uptrend", "sar_flip_up", "sar_flip_down",
		"boll_upper", "boll_middle", "boll_lower",
		"macd", "macd_signal", "macd_histogram",
		"volume", "volume_avg", "volume_ratio",
		"vwap",
		"donchian_upper", "donchian_lower", "donchian_middle",
		"break_above_donchian", "break_below_donchian",
		"price_change", "realized_vol",
		"session_open", "session_high", "session_low", "session_close", "session_volume",
		"bars_since_session_open",
		"prev_session_high", "prev_session_low", "prev_session_close", "prev_session_volume",
		"break_above_prev_session_high", "break_below_prev_session_low",
		"opening_range_high", "opening_range_low", "opening_range_mid",
		"opening_range_width_pct", "opening_range_ready",
		"break_opening_range_high", "break_opening_range_low",
		"rbreaker_pivot", "rbreaker_break_buy", "rbreaker_setup_sell",
		"rbreaker_reverse_sell", "rbreaker_reverse_buy", "rbreaker_setup_buy", "rbreaker_break_sell",
		"rbreaker_breakout_long", "rbreaker_breakout_short",
		"rbreaker_reverse_to_long", "rbreaker_reverse_to_short",
		"rbreaker_setup_sell_hit", "rbreaker_setup_buy_hit",
	}
}

func SupportedStructureOperands() []string {
	return []string{"fibonacci", "support_resistance"}
}

func SupportedExternalFactorPrefixes() []string {
	return []string{
		"quant_price_change_", "quant_oi_", "quant_oi_delta_", "quant_netflow_",
		"oi_ranking_", "netflow_ranking_", "price_ranking_",
	}
}

func SupportedExternalFactors() []string {
	return []string{"open_interest", "funding_rate", "oi_top_candidate"}
}

func SupportedScoringFactors() []string {
	return []string{"trend", "momentum", "structure", "derivatives"}
}

func IsSupportedIndicatorOperand(name string) bool {
	return containsString(SupportedIndicatorOperands(), strings.TrimSpace(name))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
