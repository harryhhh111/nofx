package market

// ATRModule calculates Average True Range.
type ATRModule struct{}

func (m *ATRModule) Name() string { return "atr" }

func (m *ATRModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.ATRPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.ATRPeriods) {
		if len(ctx.Klines) < period+1 {
			continue
		}
		val := calculateATR(ctx.Klines, period)
		points = append(points, IndicatorPoint{
			Name: "atr", Timeframe: ctx.Timeframe, Period: period,
			Value: val,
		})
	}
	return points, nil
}
