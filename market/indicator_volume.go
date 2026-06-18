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

		// Basic volume indicators are emitted only when EnableVolume is set.
		if req.EnableVolume {
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

		// Volume spike indicators are emitted only when EnableVolumeSpike is set.
		if req.EnableVolumeSpike {
			multiplier := req.VolumeSpikeMultiplier
			if multiplier <= 0 {
				multiplier = 4.0
			}
			if avg > 0 {
				ratio := current / avg
				if ratio >= multiplier {
					points = append(points, IndicatorPoint{
						Name: "volume_spike", Timeframe: ctx.Timeframe, Period: period,
						Params: map[string]float64{"multiplier": multiplier},
						Value:  1, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
					})
				}
				if spikeHigh, ok := findLastVolumeSpikeHigh(ctx.Klines, period, multiplier); ok {
					points = append(points,
						IndicatorPoint{Name: "last_volume_spike_high", Timeframe: ctx.Timeframe, Period: period, Value: spikeHigh, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
					)
					closePrice := ctx.Klines[len(ctx.Klines)-1].Close
					breakVal := 0.0
					if closePrice > spikeHigh {
						breakVal = 1
					}
					points = append(points, IndicatorPoint{
						Name: "break_last_volume_spike_high", Timeframe: ctx.Timeframe, Period: period, Value: breakVal,
						SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
					})
				}
			}
		}
	}
	return points, nil
}
