package market

// VolumeModule calculates current volume, average volume, and volume ratio.
type VolumeModule struct{}

func (m *VolumeModule) Name() string { return "volume" }

func (m *VolumeModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.VolumePeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.VolumePeriods) {
		if err := requireBars(ctx.Symbol, indicatorKey("volume", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		current := ctx.Klines[len(ctx.Klines)-1].Volume
		avg := calculateAverageVolume(ctx.Klines, period)
		points = append(points,
			IndicatorPoint{Name: "volume", Timeframe: ctx.Timeframe, Value: current, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "volume_avg", Timeframe: ctx.Timeframe, Period: period, Value: avg, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
		if avg > 0 {
			points = append(points, IndicatorPoint{
				Name: "volume_ratio", Timeframe: ctx.Timeframe, Period: period, Value: current / avg,
				SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
			})
		}
	}
	return points, nil
}
