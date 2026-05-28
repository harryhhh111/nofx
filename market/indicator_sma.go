package market

// SMAModule calculates Simple Moving Average.
type SMAModule struct{}

func (m *SMAModule) Name() string { return "sma" }

func (m *SMAModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.SMAPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.SMAPeriods) {
		if err := requireBars(ctx.Symbol, indicatorKey("sma", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name:        "sma",
			Timeframe:   ctx.Timeframe,
			Period:      period,
			Value:       calculateSMA(ctx.Klines, period),
			SourceTime:  ctx.SourceTime,
			AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
