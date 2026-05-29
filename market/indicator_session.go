package market

import (
	"fmt"
	"time"
)

// SessionModule aggregates OHLCV data by trading session.
// In crypto (24/7), a session is defined by timezone + offset + duration.
// Phase 1 supports UTC day (00:00–23:59 UTC) only.
type SessionModule struct{}

func (m *SessionModule) Name() string { return "session" }

func (m *SessionModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if len(req.Sessions) == 0 {
		return nil, nil
	}
	var points []IndicatorPoint
	for _, spec := range req.Sessions {
		if spec.Timezone == "" {
			spec.Timezone = "UTC"
		}
		if spec.Duration <= 0 {
			spec.Duration = 1440 // 24 hours
		}

		// Phase 1: UTC only
		loc, err := time.LoadLocation(spec.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid session timezone %q: %w", spec.Timezone, err)
		}

		pts, err := m.calculateSession(ctx, spec, loc)
		if err != nil {
			return nil, err
		}
		points = append(points, pts...)
	}
	return points, nil
}

func (m *SessionModule) calculateSession(ctx CalcContext, spec SessionSpec, loc *time.Location) ([]IndicatorPoint, error) {
	// Group klines by session date in the given timezone.
	// Session boundary is determined by CloseTime.
	sessions := make(map[string]*sessionAgg)
	var keys []string // ordered session keys

	for _, k := range ctx.Klines {
		closeTime := time.UnixMilli(k.CloseTime).In(loc)
		sessionKey := sessionKeyFromTime(closeTime, spec)

		agg, ok := sessions[sessionKey]
		if !ok {
			agg = &sessionAgg{Open: k.Open, High: k.High, Low: k.Low, Volume: k.Volume, OpenTime: k.OpenTime}
			sessions[sessionKey] = agg
			keys = append(keys, sessionKey)
		}
		if k.High > agg.High {
			agg.High = k.High
		}
		if k.Low < agg.Low {
			agg.Low = k.Low
		}
		agg.Close = k.Close
		agg.CloseTime = k.CloseTime
		agg.Volume += k.Volume
		agg.Bars++
	}

	if len(keys) == 0 {
		return nil, nil
	}

	lastKey := keys[len(keys)-1]
	lastAgg := sessions[lastKey]
	currentPrice := ctx.Klines[len(ctx.Klines)-1].Close

	// Count bars since current session opened
	barsSinceOpen := 0
	for i := len(ctx.Klines) - 1; i >= 0; i-- {
		k := ctx.Klines[i]
		closeTime := time.UnixMilli(k.CloseTime).In(loc)
		key := sessionKeyFromTime(closeTime, spec)
		if key != lastKey {
			break
		}
		barsSinceOpen++
	}

	var points []IndicatorPoint
	base := IndicatorPoint{
		Timeframe:   ctx.Timeframe,
		SourceTime:  ctx.SourceTime,
		AvailableAt: ctx.AsOf,
		Params: map[string]float64{
			"timezone": float64(len(spec.Timezone)), // placeholder; real params should be strings
		},
	}

	// Current session
	points = append(points,
		IndicatorPoint{Name: "session_id", Timeframe: ctx.Timeframe, Value: 0, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf, Params: map[string]float64{"session": sessionToFloat(lastKey)}},
		IndicatorPoint{Name: "session_open", Timeframe: ctx.Timeframe, Value: lastAgg.Open, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		IndicatorPoint{Name: "session_high", Timeframe: ctx.Timeframe, Value: lastAgg.High, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		IndicatorPoint{Name: "session_low", Timeframe: ctx.Timeframe, Value: lastAgg.Low, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		IndicatorPoint{Name: "session_close", Timeframe: ctx.Timeframe, Value: lastAgg.Close, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		IndicatorPoint{Name: "session_volume", Timeframe: ctx.Timeframe, Value: lastAgg.Volume, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		IndicatorPoint{Name: "bars_since_session_open", Timeframe: ctx.Timeframe, Value: float64(barsSinceOpen), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	)
	_ = base

	// Previous session
	if len(keys) >= 2 {
		prevKey := keys[len(keys)-2]
		prevAgg := sessions[prevKey]
		points = append(points,
			IndicatorPoint{Name: "prev_session_high", Timeframe: ctx.Timeframe, Value: prevAgg.High, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "prev_session_low", Timeframe: ctx.Timeframe, Value: prevAgg.Low, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "prev_session_close", Timeframe: ctx.Timeframe, Value: prevAgg.Close, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "prev_session_volume", Timeframe: ctx.Timeframe, Value: prevAgg.Volume, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}

	// Break signals vs previous session
	if len(keys) >= 2 {
		prevKey := keys[len(keys)-2]
		prevAgg := sessions[prevKey]
		breakAbove := currentPrice > prevAgg.High
		breakBelow := currentPrice < prevAgg.Low
		points = append(points,
			IndicatorPoint{Name: "break_above_prev_session_high", Timeframe: ctx.Timeframe, Value: boolToFloat(breakAbove), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
			IndicatorPoint{Name: "break_below_prev_session_low", Timeframe: ctx.Timeframe, Value: boolToFloat(breakBelow), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		)
	}

	return points, nil
}

// sessionAgg holds aggregated OHLCV for a single session.
type sessionAgg struct {
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	OpenTime  int64
	CloseTime int64
	Bars      int
}

// sessionKeyFromTime returns a stable session identifier string.
// For UTC day sessions this is "2006-01-02".
func sessionKeyFromTime(t time.Time, spec SessionSpec) string {
	if spec.Duration == 1440 && spec.Offset == "" {
		return t.Format("2006-01-02")
	}
	// General case: compute minutes since epoch aligned by offset
	// Phase 1: only UTC day is supported, so this fallback is sufficient.
	return t.Format("2006-01-02")
}

// sessionToFloat encodes a date string "2006-01-02" into a numeric ID
// suitable for IndicatorPoint.Params (map[string]float64).
// Example: "2026-05-09" → 20260509.0
func sessionToFloat(key string) float64 {
	t, err := time.Parse("2006-01-02", key)
	if err != nil {
		return 0
	}
	return float64(t.Year())*10000 + float64(t.Month())*100 + float64(t.Day())
}
