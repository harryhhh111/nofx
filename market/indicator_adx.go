package market

// ADXModule calculates ADX, +DI, and -DI using Wilder's smoothing.
type ADXModule struct{}

func (m *ADXModule) Name() string { return "adx" }

func (m *ADXModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.ADX == nil || req.ADX.Period <= 0 {
		return nil, nil
	}
	period := req.ADX.Period
	need := 2*period + 1
	if err := requireBars(ctx.Symbol, indicatorKey("adx", ctx.Timeframe), len(ctx.Klines), need); err != nil {
		return nil, err
	}
	adx, plusDI, minusDI := calculateADX(ctx.Klines, period)
	return []IndicatorPoint{
		{Name: "adx", Timeframe: ctx.Timeframe, Period: period, Value: adx, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "plus_di", Timeframe: ctx.Timeframe, Period: period, Value: plusDI, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "minus_di", Timeframe: ctx.Timeframe, Period: period, Value: minusDI, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	}, nil
}
