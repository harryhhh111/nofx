package market

// SARModule calculates Parabolic SAR (Stop and Reverse).
type SARModule struct{}

func (m *SARModule) Name() string { return "sar" }

func (m *SARModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.SAR == nil || !req.SAR.Enabled {
		return nil, nil
	}
	if err := requireBars(ctx.Symbol, "sar", len(ctx.Klines), 2); err != nil {
		return nil, err
	}
	sar, isUptrend, flipUp, flipDown, af := calculateParabolicSAR(ctx.Klines)
	return []IndicatorPoint{
		{Name: "sar", Timeframe: ctx.Timeframe, Value: sar},
		{Name: "sar_uptrend", Timeframe: ctx.Timeframe, Value: boolToFloat64(isUptrend)},
		{Name: "sar_flip_up", Timeframe: ctx.Timeframe, Value: boolToFloat64(flipUp)},
		{Name: "sar_flip_down", Timeframe: ctx.Timeframe, Value: boolToFloat64(flipDown)},
		{Name: "sar_af", Timeframe: ctx.Timeframe, Value: af},
	}, nil
}

func boolToFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
