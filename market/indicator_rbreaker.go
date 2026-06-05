package market

import (
	"fmt"
	"time"
)

// RBreakerModule calculates R-Breaker pivot levels using previous session's
// high/low/close. It also tracks intraday state (whether setup levels have
// been touched in the current session) by re-scanning the current session's
// klines on every Calculate call.
type RBreakerModule struct{}

func (m *RBreakerModule) Name() string { return "rbreaker" }

func (m *RBreakerModule) Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error) {
	if !req.EnableRBreaker {
		return nil, nil
	}

	if len(ctx.Klines) == 0 {
		return nil, nil
	}

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
			return nil, fmt.Errorf("invalid rbreaker timezone %q: %w", spec.Timezone, err)
		}

		pts, err := m.calculateRBreaker(ctx, spec, loc)
		if err != nil {
			return nil, err
		}
		points = append(points, pts...)
	}
	return points, nil
}

func (m *RBreakerModule) calculateRBreaker(ctx CalcContext, spec SessionSpec, loc *time.Location) ([]IndicatorPoint, error) {
	// Determine session key for each kline using CloseTime (same as SessionModule)
	sessionKeys := make([]string, len(ctx.Klines))
	for i, k := range ctx.Klines {
		closeTime := time.UnixMilli(k.CloseTime).In(loc)
		sessionKeys[i] = sessionKeyFromTime(closeTime, spec)
	}

	if len(sessionKeys) < 2 {
		// Need at least 2 sessions (previous + current) to calculate R-Breaker
		return nil, nil
	}

	// Find session boundaries
	sessions := make(map[string]*sessionAgg)
	var keys []string
	for i, k := range ctx.Klines {
		key := sessionKeys[i]
		agg, ok := sessions[key]
		if !ok {
			agg = &sessionAgg{Open: k.Open, High: k.High, Low: k.Low, OpenTime: k.OpenTime}
			sessions[key] = agg
			keys = append(keys, key)
		}
		if k.High > agg.High {
			agg.High = k.High
		}
		if k.Low < agg.Low {
			agg.Low = k.Low
		}
		agg.Close = k.Close
		agg.CloseTime = k.CloseTime
	}

	if len(keys) < 2 {
		return nil, nil
	}

	// Previous session data
	prevKey := keys[len(keys)-2]
	prev := sessions[prevKey]

	// Current session data
	currentKey := keys[len(keys)-1]
	current := sessions[currentKey]
	currentPrice := ctx.Klines[len(ctx.Klines)-1].Close

	// Calculate pivot and six levels
	p := (prev.High + prev.Low + prev.Close) / 3

	breakBuy := prev.High + 2*p - 2*prev.Low
	setupSell := p + prev.High - prev.Low
	reverseSell := 2*p - prev.Low

	reverseBuy := 2*p - prev.High
	setupBuy := p - (prev.High - prev.Low)
	breakSell := prev.Low - 2*(prev.High-p)

	// Breakout signals (current price vs levels)
	breakoutLong := boolToFloat(currentPrice > breakBuy)
	breakoutShort := boolToFloat(currentPrice < breakSell)

	// Reversal signals: need to check if current session has touched setup levels
	setupSellHit := current.High > setupSell
	setupBuyHit := current.Low < setupBuy

	reverseToShort := boolToFloat(setupSellHit && currentPrice < reverseSell)
	reverseToLong := boolToFloat(setupBuyHit && currentPrice > reverseBuy)

	return []IndicatorPoint{
		{Name: "rbreaker_pivot", Timeframe: ctx.Timeframe, Value: p, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_break_buy", Timeframe: ctx.Timeframe, Value: breakBuy, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_setup_sell", Timeframe: ctx.Timeframe, Value: setupSell, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_reverse_sell", Timeframe: ctx.Timeframe, Value: reverseSell, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_reverse_buy", Timeframe: ctx.Timeframe, Value: reverseBuy, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_setup_buy", Timeframe: ctx.Timeframe, Value: setupBuy, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_break_sell", Timeframe: ctx.Timeframe, Value: breakSell, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_breakout_long", Timeframe: ctx.Timeframe, Value: breakoutLong, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_breakout_short", Timeframe: ctx.Timeframe, Value: breakoutShort, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_reverse_to_long", Timeframe: ctx.Timeframe, Value: reverseToLong, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_reverse_to_short", Timeframe: ctx.Timeframe, Value: reverseToShort, SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_setup_sell_hit", Timeframe: ctx.Timeframe, Value: boolToFloat(setupSellHit), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
		{Name: "rbreaker_setup_buy_hit", Timeframe: ctx.Timeframe, Value: boolToFloat(setupBuyHit), SourceTime: ctx.SourceTime, AvailableAt: ctx.AsOf},
	}, nil
}
