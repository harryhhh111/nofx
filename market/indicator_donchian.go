package market

// DonchianModule calculates Donchian Channel using the Turtle Trading convention:
// the channel is computed from the preceding N bars (excluding the current bar).
// This prevents the current price from polluting breakout detection.
type DonchianModule struct{}

func (m *DonchianModule) Name() string { return "donchian" }

func (m *DonchianModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.DonchianPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.DonchianPeriods) {
		// Need at least period+1 bars: N bars for the channel + 1 current bar for breakout detection
		if err := requireBars(ctx.Symbol, indicatorKey("donchian", ctx.Timeframe), len(ctx.Klines), period+1); err != nil {
			return nil, err
		}
		upper, lower := calculateDonchian(ctx.Klines, period, true)
		middle := (upper + lower) / 2
		currentPrice := ctx.Klines[len(ctx.Klines)-1].Close

		breakAbove := currentPrice > upper
		breakBelow := currentPrice < lower
		var channelWidthPct float64
		if lower != 0 {
			channelWidthPct = (upper - lower) / lower * 100
		}

		points = append(points,
			IndicatorPoint{Name: "donchian_upper", Timeframe: ctx.Timeframe, Period: period, Value: upper, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "donchian_lower", Timeframe: ctx.Timeframe, Period: period, Value: lower, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "donchian_middle", Timeframe: ctx.Timeframe, Period: period, Value: middle, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "break_above_donchian", Timeframe: ctx.Timeframe, Period: period, Value: boolToFloat(breakAbove), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "break_below_donchian", Timeframe: ctx.Timeframe, Period: period, Value: boolToFloat(breakBelow), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "channel_width_pct", Timeframe: ctx.Timeframe, Period: period, Value: channelWidthPct, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}
	return points, nil
}
