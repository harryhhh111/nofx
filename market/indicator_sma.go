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
		if len(ctx.Klines) < period {
			continue
		}
		val := calculateSMA(ctx.Klines, period)
		points = append(points, IndicatorPoint{
			Name: "sma", Timeframe: ctx.Timeframe, Period: period,
			Value: val,
		})
	}
	return points, nil
}
