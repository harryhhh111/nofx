package market

// RealizedVolModule calculates realized volatility.
type RealizedVolModule struct{}

func (m *RealizedVolModule) Name() string { return "realized_vol" }

func (m *RealizedVolModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.RealizedVolPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.RealizedVolPeriods) {
		if err := requireBarsExclusive(ctx.Symbol, indicatorKey("realized_vol", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name: "realized_vol", Timeframe: ctx.Timeframe, Period: period,
			Value: calculateRealizedVol(ctx.Klines, period),
			SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
