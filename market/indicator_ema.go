package market

// EMAModule calculates Exponential Moving Average.
type EMAModule struct{}

func (m *EMAModule) Name() string { return "ema" }

func (m *EMAModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.EMAPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.EMAPeriods) {
		if err := requireBars(ctx.Symbol, indicatorKey("ema", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name:        "ema",
			Timeframe:   ctx.Timeframe,
			Period:      period,
			Value:       calculateEMA(ctx.Klines, period),
			SourceTime:  ctx.SourceTime,
			AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
