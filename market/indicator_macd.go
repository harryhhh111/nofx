package market

// MACDModule calculates Moving Average Convergence Divergence.
type MACDModule struct{}

func (m *MACDModule) Name() string { return "macd" }

func (m *MACDModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.MACD == nil {
		return nil, nil
	}
	if err := requireBars(ctx.Symbol, "macd", len(ctx.Klines), 26); err != nil {
		return nil, err
	}
	val := calculateMACD(ctx.Klines)
	return []IndicatorPoint{{
		Name: "macd", Timeframe: ctx.Timeframe,
		Value: val,
	}}, nil
}
