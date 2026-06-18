package market

// RollingPercentileModule calculates rolling percentile and z-score of the
// closing price over trailing lookback windows. Outputs are skipped when
// insufficient data is available.
type RollingPercentileModule struct{}

func (m *RollingPercentileModule) Name() string { return "rolling_percentile" }

func (m *RollingPercentileModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if !req.EnableRollingPercentile || len(req.RollingPercentilePeriods) == 0 {
		return nil, nil
	}

	closes := make([]float64, len(ctx.Klines))
	for i, k := range ctx.Klines {
		closes[i] = k.Close
	}

	var points []IndicatorPoint
	for _, window := range sanitizePeriods(req.RollingPercentilePeriods) {
		if !hasEnoughBars(ctx.Klines, window) {
			continue
		}

		series := closes[len(closes)-window:]

		if percentile, ok := calculateRollingPercentile(series, window); ok {
			points = append(points, IndicatorPoint{
				Name: "rolling_percentile", Timeframe: ctx.Timeframe, Period: window,
				Value: percentile, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
			})
		}
		if zscore, ok := calculateZScore(series, window); ok {
			points = append(points, IndicatorPoint{
				Name: "z_score", Timeframe: ctx.Timeframe, Period: window,
				Value: zscore, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
			})
		}
	}
	return points, nil
}
