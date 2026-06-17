package market

import "math"

// MTSIModule calculates MTSI (log-ratio of close to VWAP) and derived signals.
// It recalculates VWAP internally; it does not depend on VWAPModule output
// because CalcContext does not carry cross-module results.
type MTSIModule struct{}

func (m *MTSIModule) Name() string { return "mtsi" }

func (m *MTSIModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if !req.EnableMTSI || len(req.VWAPPeriods) == 0 {
		return nil, nil
	}

	var points []IndicatorPoint
	closePrice := ctx.Klines[len(ctx.Klines)-1].Close

	for _, period := range sanitizePeriods(req.VWAPPeriods) {
		if !hasEnoughBars(ctx.Klines, period) {
			continue
		}

		vwap := calculateVWAP(ctx.Klines, period)
		if vwap <= 0 {
			continue
		}

		mtsiVal := math.Log(closePrice / vwap)
		absVal := math.Abs(mtsiVal)
		distPct := (closePrice/vwap - 1) * 100
		aboveVal := 0.0
		if closePrice > vwap {
			aboveVal = 1
		}
		belowVal := 0.0
		if closePrice < vwap {
			belowVal = 1
		}

		pts := []IndicatorPoint{
			{Name: "mtsi", Timeframe: ctx.Timeframe, Period: period, Value: mtsiVal, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "mtsi_abs", Timeframe: ctx.Timeframe, Period: period, Value: absVal, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "close_vwap_distance_pct", Timeframe: ctx.Timeframe, Period: period, Value: distPct, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "close_above_vwap", Timeframe: ctx.Timeframe, Period: period, Value: aboveVal, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "close_below_vwap", Timeframe: ctx.Timeframe, Period: period, Value: belowVal, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		}
		points = append(points, pts...)
	}
	return points, nil
}
