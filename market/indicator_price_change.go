package market

// PriceChangeModule calculates price change over a window of bars.
type PriceChangeModule struct{}

func (m *PriceChangeModule) Name() string { return "price_change" }

func (m *PriceChangeModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.PriceChangeWindows) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, bars := range sanitizePeriods(req.PriceChangeWindows) {
		if err := requireBarsExclusive(ctx.Symbol, indicatorKey("price_change", ctx.Timeframe), len(ctx.Klines), bars); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name:   "price_change", Timeframe: ctx.Timeframe, Period: bars,
			Params: map[string]float64{"bars": float64(bars)},
			Value:  calculatePriceChangeWindow(ctx.Klines, bars),
			SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
