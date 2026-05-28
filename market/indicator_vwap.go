package market

import "fmt"

// VWAPModule calculates Volume Weighted Average Price.
type VWAPModule struct{}

func (m *VWAPModule) Name() string { return "vwap" }

func (m *VWAPModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.VWAPPeriods) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, period := range sanitizePeriods(req.VWAPPeriods) {
		if err := requireBars(ctx.Symbol, indicatorKey("vwap", ctx.Timeframe), len(ctx.Klines), period); err != nil {
			return nil, err
		}
		value := calculateVWAP(ctx.Klines, period)
		if value <= 0 {
			return nil, fmt.Errorf("%s %s cannot be calculated without positive volume", ctx.Symbol, indicatorKey("vwap", ctx.Timeframe))
		}
		points = append(points, IndicatorPoint{
			Name: "vwap", Timeframe: ctx.Timeframe, Period: period, Value: value,
			SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf,
		})
	}
	return points, nil
}
