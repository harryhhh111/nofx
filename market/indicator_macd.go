package market

import "fmt"

// MACDModule calculates MACD, Signal Line, and Histogram.
type MACDModule struct{}

func (m *MACDModule) Name() string { return "macd" }

func (m *MACDModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.MACD == nil {
		return nil, nil
	}
	fast := req.MACD.Fast
	slow := req.MACD.Slow
	signal := req.MACD.Signal
	if fast <= 0 {
		return nil, fmt.Errorf("%s macd missing fast period", ctx.Symbol)
	}
	if slow <= 0 {
		return nil, fmt.Errorf("%s macd missing slow period", ctx.Symbol)
	}
	if signal <= 0 {
		return nil, fmt.Errorf("%s macd missing signal period", ctx.Symbol)
	}
	if fast >= slow {
		return nil, fmt.Errorf("%s macd requires fast < slow", ctx.Symbol)
	}
	need := slow + signal
	if err := requireBars(ctx.Symbol, indicatorKey("macd", ctx.Timeframe), len(ctx.Klines), need); err != nil {
		return nil, err
	}
	macd, signalLine, histogram := calculateMACDFull(ctx.Klines, fast, slow, signal)
	params := map[string]float64{"fast": float64(fast), "slow": float64(slow), "signal": float64(signal)}
	return []IndicatorPoint{
		{Name: "macd", Timeframe: ctx.Timeframe, Params: params, Value: macd, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "macd_signal", Timeframe: ctx.Timeframe, Params: params, Value: signalLine, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "macd_histogram", Timeframe: ctx.Timeframe, Params: params, Value: histogram, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	}, nil
}
