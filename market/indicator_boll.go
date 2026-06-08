package market

// BOLLModule calculates Bollinger Bands.
type BOLLModule struct{}

func (m *BOLLModule) Name() string { return "boll" }

func (m *BOLLModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.BOLLPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, spec := range req.BOLLPeriods {
		if spec.Period <= 0 {
			continue
		}
		if len(ctx.Klines) < spec.Period {
			continue
		}
		mult := spec.Multiplier
		if mult <= 0 {
			mult = 2.0
		}
		upper, middle, lower := calculateBOLL(ctx.Klines, spec.Period, mult)
		points = append(points,
			IndicatorPoint{Name: "boll_upper", Timeframe: ctx.Timeframe, Period: spec.Period, Value: upper},
			IndicatorPoint{Name: "boll_middle", Timeframe: ctx.Timeframe, Period: spec.Period, Value: middle},
			IndicatorPoint{Name: "boll_lower", Timeframe: ctx.Timeframe, Period: spec.Period, Value: lower},
		)
	}
	return points, nil
}
