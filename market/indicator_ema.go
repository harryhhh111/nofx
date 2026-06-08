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
		if len(ctx.Klines) < period {
			continue
		}
		val := calculateEMA(ctx.Klines, period)
		points = append(points, IndicatorPoint{
			Name: "ema", Timeframe: ctx.Timeframe, Period: period,
			Value: val,
		})
	}
	return points, nil
}
