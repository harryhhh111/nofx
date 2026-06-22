package api

import (
	"fmt"
	"net/http"
	"nofx/kernel"
	"nofx/store"
	"strings"

	"github.com/gin-gonic/gin"
)

type strategyDependencyCheck struct {
	Required []string `json:"required"`
	Missing  []string `json:"missing"`
}

func (s *Server) handleStrategyMetadata(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"timeframes": []gin.H{
			{"value": "1m", "label": "1m", "category": "scalp"},
			{"value": "3m", "label": "3m", "category": "scalp"},
			{"value": "5m", "label": "5m", "category": "scalp"},
			{"value": "15m", "label": "15m", "category": "intraday"},
			{"value": "30m", "label": "30m", "category": "intraday"},
			{"value": "1h", "label": "1h", "category": "intraday"},
			{"value": "2h", "label": "2h", "category": "swing"},
			{"value": "4h", "label": "4h", "category": "swing"},
			{"value": "6h", "label": "6h", "category": "swing"},
			{"value": "8h", "label": "8h", "category": "swing"},
			{"value": "12h", "label": "12h", "category": "swing"},
			{"value": "1d", "label": "1D", "category": "position"},
			{"value": "3d", "label": "3D", "category": "position"},
			{"value": "1w", "label": "1W", "category": "position"},
		},
		"technical_indicators": []gin.H{
			{"key": "enable_ema", "label": "ema", "desc": "emaDesc", "color": "#F0B90B", "period_key": "ema_periods", "default_periods": []int{20, 50}, "operands": []string{"ema"}},
			{"key": "enable_sma", "label": "sma", "desc": "smaDesc", "color": "#4ade80", "period_key": "sma_periods", "default_periods": []int{5, 20, 50}, "operands": []string{"sma", "sma_slope", "price_above_sma", "price_distance_pct", "sma_cross_up_fast_slow", "sma_cross_down_fast_slow"}},
			{"key": "enable_macd", "label": "macd", "desc": "macdDesc", "color": "#a855f7", "operands": []string{"macd", "macd_signal", "macd_histogram"}},
			{"key": "enable_rsi", "label": "rsi", "desc": "rsiDesc", "color": "#F6465D", "period_key": "rsi_periods", "default_periods": []int{7, 14}, "operands": []string{"rsi"}},
			{"key": "enable_atr", "label": "atr", "desc": "atrDesc", "color": "#60a5fa", "period_key": "atr_periods", "default_periods": []int{14}, "operands": []string{"atr"}},
			{"key": "enable_adx", "label": "adx", "desc": "adxDesc", "color": "#f97316", "operands": []string{"adx", "plus_di", "minus_di"}},
			{"key": "enable_sar", "label": "sar", "desc": "sarDesc", "color": "#06b6d4", "operands": []string{"sar", "sar_uptrend", "sar_flip_up", "sar_flip_down"}},
			{"key": "enable_boll", "label": "boll", "desc": "bollDesc", "color": "#ec4899", "period_key": "boll_periods", "default_periods": []int{20}, "multiplier_key": "boll_multiplier", "operands": []string{"boll_upper", "boll_middle", "boll_lower"}},
			{"key": "enable_session", "label": "session", "desc": "sessionDesc", "color": "#84cc16", "operands": []string{"session_open", "session_high", "session_low", "session_close", "session_volume", "bars_since_session_open", "prev_session_high", "prev_session_low", "prev_session_close", "prev_session_volume", "break_above_prev_session_high", "break_below_prev_session_low"}},
			{"key": "enable_opening_range", "label": "opening_range", "desc": "openingRangeDesc", "color": "#38bdf8", "period_key": "opening_range_minutes", "operands": []string{"opening_range_high", "opening_range_low", "opening_range_mid", "opening_range_width_pct", "opening_range_ready", "break_opening_range_high", "break_opening_range_low"}},
			{"key": "enable_rbreaker", "label": "rbreaker", "desc": "rbreakerDesc", "color": "#fb923c", "operands": []string{"rbreaker_pivot", "rbreaker_break_buy", "rbreaker_setup_sell", "rbreaker_reverse_sell", "rbreaker_reverse_buy", "rbreaker_setup_buy", "rbreaker_break_sell", "rbreaker_breakout_long", "rbreaker_breakout_short", "rbreaker_reverse_to_long", "rbreaker_reverse_to_short", "rbreaker_setup_sell_hit", "rbreaker_setup_buy_hit"}},
			{"key": "enable_mtsi", "label": "mtsi", "desc": "mtsiDesc", "color": "#c084fc", "operands": []string{"mtsi", "mtsi_abs", "close_vwap_distance_pct", "close_above_vwap", "close_below_vwap"}},
			{"key": "enable_vwap", "label": "vwap", "desc": "vwapDesc", "color": "#22d3ee", "period_key": "vwap_periods", "default_periods": []int{20}, "operands": []string{"vwap"}},
			{"key": "enable_donchian", "label": "donchian", "desc": "donchianDesc", "color": "#f472b6", "period_key": "donchian_periods", "default_periods": []int{20}, "operands": []string{"donchian_upper", "donchian_lower", "donchian_middle", "break_above_donchian", "break_below_donchian", "channel_width_pct"}},
			{"key": "enable_volume", "label": "volume", "desc": "volumeDesc", "color": "#8b5cf6", "period_key": "volume_periods", "default_periods": []int{20}, "operands": []string{"volume", "volume_avg", "volume_ratio"}},
			{"key": "enable_volume_spike", "label": "volumeSpike", "desc": "volumeSpikeDesc", "color": "#a78bfa", "period_key": "volume_periods", "default_periods": []int{20}, "multiplier_key": "volume_spike_multiplier", "operands": []string{"volume_spike", "last_volume_spike_high", "break_last_volume_spike_high"}},
			{"key": "enable_rolling_percentile", "label": "rolling_percentile", "desc": "rollingPercentileDesc", "color": "#34d399", "period_key": "rolling_percentile_periods", "default_periods": []int{20}, "operands": []string{"rolling_percentile", "z_score"}},
		},
		"always_calculated_indicators": []string{"price", "price_change", "realized_vol"},
		"indicator_operands":           kernel.SupportedIndicatorOperands(),
		"structure_operands":           kernel.SupportedStructureOperands(),
		"external_factors":             kernel.SupportedExternalFactors(),
		"external_factor_prefixes":     kernel.SupportedExternalFactorPrefixes(),
		"scoring_factors":              kernel.SupportedScoringFactors(),
	})
}

func strategyDependencyStatus(config *store.StrategyConfig) strategyDependencyCheck {
	if config == nil {
		return strategyDependencyCheck{}
	}
	required := map[string]bool{}
	missing := map[string]bool{}
	for _, rule := range config.CompiledRules {
		for _, condition := range rule.Conditions {
			collectOperandRequirement(config, condition.Left, required, missing)
			collectOperandRequirement(config, condition.Right, required, missing)
		}
	}
	return strategyDependencyCheck{
		Required: sortedKeys(required),
		Missing:  sortedKeys(missing),
	}
}

func collectOperandRequirement(config *store.StrategyConfig, operand store.CompiledRuleOperand, required, missing map[string]bool) {
	if operand.Kind != "indicator" {
		return
	}
	name := strings.TrimSpace(operand.Name)
	if name == "" || name == "price" {
		return
	}
	key := indicatorRequirementKey(name, operand.Timeframe, operand.Period)
	required[key] = true
	if !indicatorAvailableInConfig(config, operand) {
		missing[key] = true
	}
}

func indicatorRequirementKey(name, timeframe string, period int) string {
	if timeframe == "" && period <= 0 {
		return name
	}
	if period > 0 {
		return fmt.Sprintf("%s:%s:%d", name, timeframe, period)
	}
	return fmt.Sprintf("%s:%s", name, timeframe)
}

func indicatorAvailableInConfig(config *store.StrategyConfig, operand store.CompiledRuleOperand) bool {
	if config == nil {
		return false
	}
	if operand.Timeframe != "" && !timeframeSelected(config, operand.Timeframe) {
		return false
	}
	indicators := config.Indicators
	switch indicatorGroup(operand.Name) {
	case "ema":
		return indicators.EnableEMA && containsInt(indicators.EMAPeriods, operand.Period)
	case "sma":
		if !indicators.EnableSMA {
			return false
		}
		if operand.Name == "sma_cross_up_fast_slow" || operand.Name == "sma_cross_down_fast_slow" {
			// Cross signals are generated only when at least two distinct SMA
			// periods are configured; they are not bound to a single period.
			return len(indicators.SMAPeriods) >= 2 && operand.Period == 0
		}
		return containsInt(indicators.SMAPeriods, operand.Period)
	case "rsi":
		return indicators.EnableRSI && containsInt(indicators.RSIPeriods, operand.Period)
	case "atr":
		return indicators.EnableATR && containsInt(indicators.ATRPeriods, operand.Period)
	case "adx":
		return indicators.EnableADX && (operand.Period == 0 || operand.Period == indicators.ADXPeriod)
	case "sar":
		return indicators.EnableSAR
	case "boll":
		return indicators.EnableBOLL && containsInt(indicators.BOLLPeriods, operand.Period)
	case "macd":
		return indicators.EnableMACD
	case "volume":
		return indicators.EnableVolume
	case "volume_spike":
		return indicators.EnableVolumeSpike
	case "vwap":
		return indicators.EnableVWAP
	case "donchian":
		return indicators.EnableDonchian
	case "rolling_percentile":
		return indicators.EnableRollingPercentile
	case "session":
		return indicators.EnableSession
	case "opening_range":
		return indicators.EnableOpeningRange
	case "rbreaker":
		return indicators.EnableRBreaker
	case "mtsi":
		return indicators.EnableMTSI
	case "always":
		return true
	default:
		return false
	}
}

func indicatorGroup(name string) string {
	switch name {
	case "ema":
		return "ema"
	case "sma", "sma_slope", "price_above_sma", "price_distance_pct",
		"sma_cross_up_fast_slow", "sma_cross_down_fast_slow":
		return "sma"
	case "rsi":
		return "rsi"
	case "atr":
		return "atr"
	case "adx", "plus_di", "minus_di":
		return "adx"
	case "sar", "sar_uptrend", "sar_flip_up", "sar_flip_down":
		return "sar"
	case "boll_upper", "boll_middle", "boll_lower":
		return "boll"
	case "macd", "macd_signal", "macd_histogram":
		return "macd"
	case "volume", "volume_avg", "volume_ratio":
		return "volume"
	case "volume_spike", "last_volume_spike_high", "break_last_volume_spike_high":
		return "volume_spike"
	case "session_open", "session_high", "session_low", "session_close", "session_volume",
		"bars_since_session_open", "prev_session_high", "prev_session_low", "prev_session_close",
		"prev_session_volume", "break_above_prev_session_high", "break_below_prev_session_low":
		return "session"
	case "opening_range_high", "opening_range_low", "opening_range_mid",
		"opening_range_width_pct", "opening_range_ready",
		"break_opening_range_high", "break_opening_range_low":
		return "opening_range"
	case "rbreaker_pivot", "rbreaker_break_buy", "rbreaker_setup_sell",
		"rbreaker_reverse_sell", "rbreaker_reverse_buy", "rbreaker_setup_buy", "rbreaker_break_sell",
		"rbreaker_breakout_long", "rbreaker_breakout_short",
		"rbreaker_reverse_to_long", "rbreaker_reverse_to_short",
		"rbreaker_setup_sell_hit", "rbreaker_setup_buy_hit":
		return "rbreaker"
	case "mtsi", "mtsi_abs", "close_vwap_distance_pct", "close_above_vwap", "close_below_vwap":
		return "mtsi"
	case "vwap":
		return "vwap"
	case "donchian_upper", "donchian_lower", "donchian_middle",
		"break_above_donchian", "break_below_donchian", "channel_width_pct":
		return "donchian"
	case "rolling_percentile", "z_score":
		return "rolling_percentile"
	case "price_change",
		"return_1h", "return_4h", "return_24h", "return_3d",
		"first_cross_20pct_3d", "first_cross_25pct_3d",
		"realized_vol":
		return "always"
	default:
		return ""
	}
}

func timeframeSelected(config *store.StrategyConfig, timeframe string) bool {
	if timeframe == "" {
		return true
	}
	for _, tf := range config.Indicators.Klines.SelectedTimeframes {
		if tf == timeframe {
			return true
		}
	}
	if config.Indicators.Klines.PrimaryTimeframe == timeframe {
		return true
	}
	if config.Indicators.Klines.LongerTimeframe == timeframe {
		return true
	}
	return false
}

func containsInt(values []int, target int) bool {
	if target <= 0 {
		return len(values) > 0
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sortStrings(out)
	return out
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for j >= 0 && values[j] > value {
			values[j+1] = values[j]
			j--
		}
		values[j+1] = value
	}
}
