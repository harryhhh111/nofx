package market

// VolumeModule computes volume statistics (average volume over a period).
type VolumeModule struct{}

func (m *VolumeModule) Name() string { return "volume" }

func (m *VolumeModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.VolumePeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.VolumePeriods) {
		if len(ctx.Klines) < period {
			continue
		}
		sum := 0.0
		start := len(ctx.Klines) - period
		for i := start; i < len(ctx.Klines); i++ {
			sum += ctx.Klines[i].Volume
		}
		avg := sum / float64(period)
		lastVol := ctx.Klines[len(ctx.Klines)-1].Volume
		points = append(points,
			IndicatorPoint{Name: "volume_avg", Timeframe: ctx.Timeframe, Period: period, Value: avg},
			IndicatorPoint{Name: "volume_last", Timeframe: ctx.Timeframe, Period: period, Value: lastVol},
		)
	}
	return points, nil
}
