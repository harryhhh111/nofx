package market

// BOLLModule calculates Bollinger Bands.
type BOLLModule struct{}

func (m *BOLLModule) Name() string { return "boll" }

func (m *BOLLModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.BOLLPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, spec := range sanitizeBOLLSpecs(req.BOLLPeriods) {
		if err := requireBars(ctx.Symbol, indicatorKey("boll", ctx.Timeframe), len(ctx.Klines), spec.Period); err != nil {
			return nil, err
		}
		upper, middle, lower := calculateBOLL(ctx.Klines, spec.Period, spec.Multiplier)
		params := map[string]float64{"multiplier": spec.Multiplier}
		points = append(points,
			IndicatorPoint{Name: "boll_upper", Timeframe: ctx.Timeframe, Period: spec.Period, Params: params, Value: upper, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "boll_middle", Timeframe: ctx.Timeframe, Period: spec.Period, Params: params, Value: middle, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "boll_lower", Timeframe: ctx.Timeframe, Period: spec.Period, Params: params, Value: lower, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}
	return points, nil
}
