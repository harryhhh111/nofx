package market

// ADXModule calculates ADX, +DI, -DI and derived directional/trend signals.
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

	// Direction: +1 bullish, -1 bearish, 0 neutral.
	diDirection := 0.0
	if plusDI > minusDI {
		diDirection = 1
	} else if plusDI < minusDI {
		diDirection = -1
	}

	// Trending threshold fixed at 25.
	trending := 0.0
	if adx > 25 {
		trending = 1
	}

	// Strength levels: [0,20) weak / [20,40) developing / [40,60) strong / [60,+) extreme.
	var strength float64
	switch {
	case adx < 20:
		strength = 0
	case adx < 40:
		strength = 1
	case adx < 60:
		strength = 2
	default:
		strength = 3
	}

	return []IndicatorPoint{
		{Name: "adx", Timeframe: ctx.Timeframe, Period: period, Value: adx, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "plus_di", Timeframe: ctx.Timeframe, Period: period, Value: plusDI, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "minus_di", Timeframe: ctx.Timeframe, Period: period, Value: minusDI, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "di_direction", Timeframe: ctx.Timeframe, Period: period, Value: diDirection, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "adx_trending", Timeframe: ctx.Timeframe, Period: period, Value: trending, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "adx_strength_level", Timeframe: ctx.Timeframe, Period: period, Value: strength, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	}, nil
}
