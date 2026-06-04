package market

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OpeningRangeModule calculates the opening range (high/low) for the first N minutes
// of each trading session. It shares the same session definition as SessionModule.
type OpeningRangeModule struct{}

func (m *OpeningRangeModule) Name() string { return "opening_range" }

func (m *OpeningRangeModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if req.OpeningRange == nil || req.OpeningRange.RangeMinutes <= 0 {
		return nil, nil
	}
	rangeMinutes := req.OpeningRange.RangeMinutes

	// Default to UTC day session if none configured
	sessions := req.Sessions
	if len(sessions) == 0 {
		sessions = []SessionSpec{{Timezone: "UTC", Offset: "00:00", Duration: 1440}}
	}

	var points []IndicatorPoint
	for _, spec := range sessions {
		if spec.Timezone == "" {
			spec.Timezone = "UTC"
		}
		if spec.Duration <= 0 {
			spec.Duration = 1440
		}

		loc, err := time.LoadLocation(spec.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid opening range timezone %q: %w", spec.Timezone, err)
		}

		pts, err := m.calculateOpeningRange(ctx, spec, loc, rangeMinutes)
		if err != nil {
			return nil, err
		}
		points = append(points, pts...)
	}
	return points, nil
}

func (m *OpeningRangeModule) calculateOpeningRange(ctx CalcContext, spec SessionSpec, loc *time.Location, rangeMinutes int) ([]IndicatorPoint, error) {
	if len(ctx.Klines) == 0 {
		return nil, nil
	}

	// Determine session key for each kline using CloseTime (same as SessionModule)
	sessionKeys := make([]string, len(ctx.Klines))
	for i, k := range ctx.Klines {
		closeTime := time.UnixMilli(k.CloseTime).In(loc)
		sessionKeys[i] = sessionKeyFromTime(closeTime, spec)
	}

	// Current session is the last one in the slice
	currentKey := sessionKeys[len(sessionKeys)-1]
	currentPrice := ctx.Klines[len(ctx.Klines)-1].Close

	// Find the first bar of the current session
	firstIdx := len(ctx.Klines) - 1
	for i := len(ctx.Klines) - 1; i >= 0; i-- {
		if sessionKeys[i] != currentKey {
			break
		}
		firstIdx = i
	}

	barMinutes := timeframeToMinutes(ctx.Timeframe)
	if barMinutes <= 0 {
		barMinutes = 1
	}
	barsNeeded := rangeMinutes / barMinutes
	if barsNeeded < 2 {
		barsNeeded = 2 // need at least 2 bars to form a range
	}

	barsAvailable := len(ctx.Klines) - firstIdx
	ready := barsAvailable >= barsNeeded
	if !ready {
		return []IndicatorPoint{
			{Name: "opening_range_high", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "opening_range_low", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "opening_range_mid", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "opening_range_width_pct", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "opening_range_ready", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "break_opening_range_high", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			{Name: "break_opening_range_low", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		}, nil
	}

	endIdx := firstIdx + barsNeeded
	if endIdx > len(ctx.Klines) {
		endIdx = len(ctx.Klines)
	}

	high := ctx.Klines[firstIdx].High
	low := ctx.Klines[firstIdx].Low
	for i := firstIdx; i < endIdx; i++ {
		k := ctx.Klines[i]
		if k.High > high {
			high = k.High
		}
		if k.Low < low {
			low = k.Low
		}
	}

	mid := (high + low) / 2
	widthPct := 0.0
	if low > 0 {
		widthPct = (high - low) / low * 100
	}

	breakHigh := boolToFloat(currentPrice > high)
	breakLow := boolToFloat(currentPrice < low)

	return []IndicatorPoint{
		{Name: "opening_range_high", Timeframe: ctx.Timeframe, Value: high, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "opening_range_low", Timeframe: ctx.Timeframe, Value: low, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "opening_range_mid", Timeframe: ctx.Timeframe, Value: mid, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "opening_range_width_pct", Timeframe: ctx.Timeframe, Value: widthPct, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "opening_range_ready", Timeframe: ctx.Timeframe, Value: 1, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "break_opening_range_high", Timeframe: ctx.Timeframe, Value: breakHigh, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "break_opening_range_low", Timeframe: ctx.Timeframe, Value: breakLow, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	}, nil
}

func timeframeToMinutes(tf string) int {
	tf = strings.ToLower(strings.TrimSpace(tf))
	switch tf {
	case "1m":
		return 1
	case "3m":
		return 3
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "6h":
		return 360
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "3d":
		return 4320
	case "1w":
		return 10080
	}
	if strings.HasSuffix(tf, "m") {
		if n, err := strconv.Atoi(strings.TrimSuffix(tf, "m")); err == nil {
			return n
		}
	}
	if strings.HasSuffix(tf, "h") {
		if n, err := strconv.Atoi(strings.TrimSuffix(tf, "h")); err == nil {
			return n * 60
		}
	}
	if strings.HasSuffix(tf, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(tf, "d")); err == nil {
			return n * 1440
		}
	}
	return 0
}
