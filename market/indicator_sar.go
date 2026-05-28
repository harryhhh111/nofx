package market

// SARModule calculates Parabolic SAR.
type SARModule struct{}

func (m *SARModule) Name() string { return "sar" }

func (m *SARModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.SAR == nil || !req.SAR.Enabled {
		return nil, nil
	}
	if err := requireBars(ctx.Symbol, indicatorKey("sar", ctx.Timeframe), len(ctx.Klines), 2); err != nil {
		return nil, err
	}
	sar, isUptrend, flipUp, flipDown := calculateParabolicSAR(ctx.Klines)
	return []IndicatorPoint{
		{Name: "sar", Timeframe: ctx.Timeframe, Value: sar, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "sar_uptrend", Timeframe: ctx.Timeframe, Value: boolToFloat(isUptrend), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "sar_flip_up", Timeframe: ctx.Timeframe, Value: boolToFloat(flipUp), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "sar_flip_down", Timeframe: ctx.Timeframe, Value: boolToFloat(flipDown), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	}, nil
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
