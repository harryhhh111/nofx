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

	// Pre-compute current and previous SMA for every configured period so we
	// don't call calculateSMA multiple times for the same period.
	smaCurrent := make(map[int]float64, len(periods))
	smaPrevious := make(map[int]float64, len(periods))
	for _, period := range periods {
		if err := requireBars(ctx.Symbol, indicatorKey("sma", ctx.Timeframe), n, period); err != nil {
			return nil, err
		}
		smaCurrent[period] = calculateSMA(ctx.Klines, period)
		if n > period {
			smaPrevious[period] = calculateSMA(ctx.Klines[:n-1], period)
		}
	}

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

	crossUp, crossDown := 0.0, 0.0
	if fastPeriod > 0 && slowPeriod > 0 && fastPeriod != slowPeriod {
		fastCur, fastOk := smaCurrent[fastPeriod]
		fastPrev, fastPrevOk := smaPrevious[fastPeriod]
		slowCur, slowOk := smaCurrent[slowPeriod]
		slowPrev, slowPrevOk := smaPrevious[slowPeriod]
		if fastOk && fastPrevOk && slowOk && slowPrevOk {
			if fastCur > slowCur && fastPrev <= slowPrev {
				crossUp = 1
			}
			if fastCur < slowCur && fastPrev >= slowPrev {
				crossDown = 1
			}
		}
	}

	for _, period := range periods {
		sma := smaCurrent[period]
		points = append(points, IndicatorPoint{
			Name:        "sma", Timeframe: ctx.Timeframe, Period: period,
			Value:       sma, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})

		// Price relative to SMA. Always emit the data points; when SMA is 0 the
		// derived values are 0, matching the base SMA value.
		above := 0.0
		distance := 0.0
		if sma > 0 {
			if currentClose > sma {
				above = 1
			}
			distance = (currentClose - sma) / sma * 100
		}
		points = append(points,
			IndicatorPoint{Name: "price_above_sma", Timeframe: ctx.Timeframe, Period: period, Value: above, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "price_distance_pct", Timeframe: ctx.Timeframe, Period: period, Value: distance, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)

		// SMA slope (percentage change from previous bar's SMA).
		prevSMA, hasPrev := smaPrevious[period]
		slope := 0.0
		if hasPrev && prevSMA != 0 {
			slope = (sma - prevSMA) / prevSMA * 100
		}
		points = append(points, IndicatorPoint{
			Name:        "sma_slope", Timeframe: ctx.Timeframe, Period: period,
			Value:       slope, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})
	}

	if fastPeriod > 0 && slowPeriod > 0 && fastPeriod != slowPeriod {
		points = append(points,
			IndicatorPoint{Name: "sma_cross_up_fast_slow", Timeframe: ctx.Timeframe, Value: crossUp, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "sma_cross_down_fast_slow", Timeframe: ctx.Timeframe, Value: crossDown, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}

	return points, nil
}
