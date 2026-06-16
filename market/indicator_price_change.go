package market

// PriceChangeModule calculates price change over bar windows and named time
// windows, plus first-cross-over-threshold detection.
type PriceChangeModule struct{}

func (m *PriceChangeModule) Name() string { return "price_change" }

func (m *PriceChangeModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	var points []IndicatorPoint

	// Existing bar-window price changes.
	for _, bars := range sanitizePeriods(req.PriceChangeWindows) {
		if err := requireBarsExclusive(ctx.Symbol, indicatorKey("price_change", ctx.Timeframe), len(ctx.Klines), bars); err != nil {
			return nil, err
		}
		points = append(points, IndicatorPoint{
			Name:   "price_change", Timeframe: ctx.Timeframe, Period: bars,
			Params:     map[string]float64{"bars": float64(bars)},
			Value:      calculatePriceChangeWindow(ctx.Klines, bars),
			SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})
	}

	// Named time windows: silently skip when bars are insufficient.
	for _, window := range req.PriceChangeNamedWindows {
		bars := namedWindowToBars(window, ctx.Timeframe)
		if bars <= 0 || !hasEnoughBars(ctx.Klines, bars) {
			continue
		}
		ret := calculatePriceChangeWindow(ctx.Klines, bars)
		points = append(points, IndicatorPoint{
			Name:        "return_" + window, Timeframe: ctx.Timeframe, Period: bars,
			Params:      map[string]float64{"bars": float64(bars)},
			Value:       ret,
			SourceTime:  ctx.SourceTime, AvailableAt: ctx.AsOf,
		})
	}

	// First-cross-over-threshold detection (always on for 3d window).
	for _, threshold := range []struct{ name string; pct float64 }{
		{"first_cross_20pct_3d", 0.20},
		{"first_cross_25pct_3d", 0.25},
	} {
		bars := namedWindowToBars("3d", ctx.Timeframe)
		if bars <= 0 || !hasEnoughBars(ctx.Klines, bars) {
			continue
		}
		if val, ok := detectFirstCross(ctx.Klines, bars, threshold.pct); ok {
			points = append(points, IndicatorPoint{
				Name:        threshold.name, Timeframe: ctx.Timeframe,
				Params:      map[string]float64{"bars": float64(bars), "threshold": threshold.pct},
				Value:       val,
				SourceTime:  ctx.SourceTime, AvailableAt: ctx.AsOf,
			})
		}
	}

	return points, nil
}
