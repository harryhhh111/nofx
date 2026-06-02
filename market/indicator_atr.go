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
		if err := requireBarsExclusive(ctx.Symbol, indicatorKey("atr", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name:        "atr",
			Timeframe:   ctx.Timeframe,
			Period:      period,
			Value:       calculateATR(ctx.Klines, period),
			SourceTime:  ctx.SourceTime,
			AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
