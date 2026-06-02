package market

// RSIModule calculates Relative Strength Index.
type RSIModule struct{}

func (m *RSIModule) Name() string { return "rsi" }

func (m *RSIModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.RSIPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.RSIPeriods) {
		if err := requireBarsExclusive(ctx.Symbol, indicatorKey("rsi", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name:        "rsi",
			Timeframe:   ctx.Timeframe,
			Period:      period,
			Value:       calculateRSI(ctx.Klines, period),
			SourceTime:  ctx.SourceTime,
			AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
