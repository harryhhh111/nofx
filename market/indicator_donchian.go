package market

// DonchianModule calculates Donchian Channel upper and lower bands.
type DonchianModule struct{}

func (m *DonchianModule) Name() string { return "donchian" }

func (m *DonchianModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.DonchianPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.DonchianPeriods) {
		if err := requireBars(ctx.Symbol, indicatorKey("donchian", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		upper, lower := calculateDonchian(ctx.Klines, period)
		points = append(points,
			IndicatorPoint{Name: "donchian_upper", Timeframe: ctx.Timeframe, Period: period, Value: upper, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "donchian_lower", Timeframe: ctx.Timeframe, Period: period, Value: lower, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}
	return points, nil
}
