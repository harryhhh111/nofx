package market

// ADXModule calculates Average Directional Index with +DI/-DI.
type ADXModule struct{}

func (m *ADXModule) Name() string { return "adx" }

func (m *ADXModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.ADX == nil || req.ADX.Period <= 0 {
		return nil, nil
	}
	period := req.ADX.Period
	if err := requireBars(ctx.Symbol, "adx", len(ctx.Klines), 2*period+1); err != nil {
		return nil, err
	}
	adx, plusDI, minusDI := calculateADX(ctx.Klines, period)
	return []IndicatorPoint{
		{Name: "adx", Timeframe: ctx.Timeframe, Period: period, Value: adx},
		{Name: "plus_di", Timeframe: ctx.Timeframe, Period: period, Value: plusDI},
		{Name: "minus_di", Timeframe: ctx.Timeframe, Period: period, Value: minusDI},
	}, nil
}
