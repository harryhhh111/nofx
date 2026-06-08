package market

// DonchianModule calculates Donchian Channel (highest high / lowest low over N bars).
type DonchianModule struct{}

func (m *DonchianModule) Name() string { return "donchian" }

func (m *DonchianModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.DonchianPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.DonchianPeriods) {
		if len(ctx.Klines) < period {
			continue
		}
		upper, lower := calculateDonchian(ctx.Klines, period)
		points = append(points,
			IndicatorPoint{Name: "donchian_upper", Timeframe: ctx.Timeframe, Period: period, Value: upper},
			IndicatorPoint{Name: "donchian_lower", Timeframe: ctx.Timeframe, Period: period, Value: lower},
		)
	}
	return points, nil
}
