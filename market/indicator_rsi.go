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
		if len(ctx.Klines) < period+1 {
			continue
		}
		val := calculateRSI(ctx.Klines, period)
		points = append(points, IndicatorPoint{
			Name: "rsi", Timeframe: ctx.Timeframe, Period: period,
			Value: val,
		})
	}
	return points, nil
}
