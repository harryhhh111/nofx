package market

// SMAModule calculates Simple Moving Average and derived signals.
type SMAModule struct{}

func (m *SMAModule) Name() string { return "sma" }

func (m *SMAModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	periods := sanitizePeriods(req.SMAPeriods)
	if len(periods) == 0 {
		return nil, nil
	}

	n := len(ctx.Klines)
	currentClose := ctx.Klines[n-1].Close
	var points []IndicatorPoint

	// Crossover signal uses the smallest configured period as the fast line and
	// the largest configured period as the slow line. This convention is exposed
	// in metadata as sma_cross_up_fast_slow / sma_cross_down_fast_slow and does
	// not carry a period dimension (period == 0).
	fastPeriod, slowPeriod := 0, 0
	if len(periods) >= 2 {
		fastPeriod = periods[0]
		slowPeriod = periods[0]
		for _, p := range periods {
			if p < fastPeriod {
				fastPeriod = p
			}
			if p > slowPeriod {
				slowPeriod = p
			}
		}
	}

	for _, period := range periods {
		if err := requireBars(ctx.Symbol, indicatorKey("sma", ctx.Timeframe), n, period); err != nil {
			return nil, err
		}

		sma := calculateSMA(ctx.Klines, period)
		points = append(points, IndicatorPoint{
			Name:        "sma", Timeframe: ctx.Timeframe, Period: period,
			Value:       sma, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})

		// Price relative to SMA.
		if sma > 0 {
			above := 0.0
			if currentClose > sma {
				above = 1
			}
			points = append(points,
				IndicatorPoint{Name: "price_above_sma", Timeframe: ctx.Timeframe, Period: period, Value: above, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
				IndicatorPoint{Name: "price_distance_pct", Timeframe: ctx.Timeframe, Period: period, Value: (currentClose - sma) / sma * 100, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			)
		}

		// SMA slope (percentage change from previous bar's SMA).
		if n > period {
			prevSMA := calculateSMA(ctx.Klines[:n-1], period)
			if prevSMA != 0 {
				points = append(points, IndicatorPoint{
					Name: "sma_slope", Timeframe: ctx.Timeframe, Period: period,
					Value: (sma - prevSMA) / prevSMA * 100,
					SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
				})
			}
		}
	}

	// Fast/slow cross detection when at least two periods are configured.
	if fastPeriod > 0 && slowPeriod > 0 && fastPeriod != slowPeriod && n > slowPeriod {
		fastSMA := calculateSMA(ctx.Klines, fastPeriod)
		fastSMAPrev := calculateSMA(ctx.Klines[:n-1], fastPeriod)
		slowSMA := calculateSMA(ctx.Klines, slowPeriod)
		slowSMAPrev := calculateSMA(ctx.Klines[:n-1], slowPeriod)

		crossUp := 0.0
		if fastSMA > slowSMA && fastSMAPrev <= slowSMAPrev {
			crossUp = 1
		}
		crossDown := 0.0
		if fastSMA < slowSMA && fastSMAPrev >= slowSMAPrev {
			crossDown = 1
		}
		points = append(points,
			IndicatorPoint{Name: "sma_cross_up_fast_slow", Timeframe: ctx.Timeframe, Value: crossUp, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "sma_cross_down_fast_slow", Timeframe: ctx.Timeframe, Value: crossDown, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}

	return points, nil
}
