package market

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// StructureRequest describes structure indicators that need anchors, not just
// periods. Specific algorithms can be implemented behind this stable contract.
type StructureRequest struct {
	Fibonacci *FibonacciRequest `json:"fibonacci,omitempty"`
	Support   *SupportRequest   `json:"support,omitempty"`
}

type FibonacciRequest struct {
	Timeframe             string    `json:"timeframe"`
	Lookback              int       `json:"lookback"`
	SwingWindow           int       `json:"swing_window"`
	MinLegBars            int       `json:"min_leg_bars"`
	MinLegATRMultiple     float64   `json:"min_leg_atr_multiple"`
	ZigZagThresholdPct    float64   `json:"zigzag_threshold_pct"`
	Levels                []float64 `json:"levels"`
	InvalidateOnBreakBase bool      `json:"invalidate_on_break_base"`
}

type SupportRequest struct {
	Timeframe       string  `json:"timeframe"`
	Lookback        int     `json:"lookback"`
	SwingWindow     int     `json:"swing_window"`
	ZoneWidthATR    float64 `json:"zone_width_atr"`
	MinTouches      int     `json:"min_touches"`
	MinDistanceBars int     `json:"min_distance_bars"`
}

// StructureEngine identifies market structure from OHLCV data. It should
// return Valid=false when no reproducible structure exists.
type StructureEngine interface {
	Calculate(ctx context.Context, input MarketInput, req StructureRequest) ([]StructureSnapshot, error)
}

type DefaultStructureEngine struct{}

func NewDefaultStructureEngine() *DefaultStructureEngine {
	return &DefaultStructureEngine{}
}

func (e *DefaultStructureEngine) Calculate(ctx context.Context, input MarketInput, req StructureRequest) ([]StructureSnapshot, error) {
	if input.Symbol == "" {
		return nil, fmt.Errorf("market input symbol is required")
	}
	if input.AsOf.IsZero() {
		input.AsOf = time.Now().UTC()
	}

	out := []StructureSnapshot{}
	if req.Fibonacci != nil {
		snapshot, err := calculateFibonacciStructure(input, *req.Fibonacci)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	if req.Support != nil {
		snapshot, err := calculateSupportResistanceStructure(input, *req.Support)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, nil
}

type swingPoint struct {
	Index int
	Time  time.Time
	Price float64
	Kind  string
}

func calculateFibonacciStructure(input MarketInput, req FibonacciRequest) (StructureSnapshot, error) {
	if err := validateFibonacciRequest(req); err != nil {
		return StructureSnapshot{}, err
	}
	klines, err := structureKlines(input, req.Timeframe, req.Lookback)
	if err != nil {
		return StructureSnapshot{}, err
	}
	startIndex := len(input.Timeframes[req.Timeframe]) - len(klines)
	swings := findSwingPoints(klines, startIndex, req.SwingWindow)
	sourceTime := klineSourceTime(klines[len(klines)-1])
	atr := calculateATR(klines, 14)
	if atr <= 0 {
		return StructureSnapshot{}, fmt.Errorf("%s fibonacci[%s] requires positive ATR14", input.Symbol, req.Timeframe)
	}
	if len(swings) < 2 {
		return invalidStructure("fibonacci", req.Timeframe, "not_enough_swings", sourceTime, input.AsOf, fibonacciParameterHash(req)), nil
	}

	current := klines[len(klines)-1].Close
	for i := len(swings) - 1; i > 0; i-- {
		a := swings[i-1]
		b := swings[i]
		if a.Kind == b.Kind {
			continue
		}
		bars := b.Index - a.Index
		if bars < req.MinLegBars {
			continue
		}
		leg := math.Abs(b.Price - a.Price)
		if leg < atr*req.MinLegATRMultiple {
			continue
		}
		base := a.Price
		if base <= 0 {
			continue
		}
		if math.Abs(b.Price-a.Price)/base*100 < req.ZigZagThresholdPct {
			continue
		}

		direction := "down"
		anchorLow := anchorFromSwing(b)
		anchorHigh := anchorFromSwing(a)
		invalidPrice := anchorHigh.Price
		invalidated := req.InvalidateOnBreakBase && current > invalidPrice
		if a.Kind == "low" && b.Kind == "high" {
			direction = "up"
			anchorLow = anchorFromSwing(a)
			anchorHigh = anchorFromSwing(b)
			invalidPrice = anchorLow.Price
			invalidated = req.InvalidateOnBreakBase && current < invalidPrice
		}
		if invalidated {
			continue
		}

		keyLevels := fibonacciLevels(direction, anchorLow.Price, anchorHigh.Price, req.Levels)
		return StructureSnapshot{
			Name:          "fibonacci",
			Timeframe:     req.Timeframe,
			Valid:         true,
			Direction:     direction,
			AnchorLow:     &anchorLow,
			AnchorHigh:    &anchorHigh,
			InvalidPrice:  invalidPrice,
			KeyLevels:     keyLevels,
			Confirmed:     true,
			SourceTime:    sourceTime,
			AvailableAt:   input.AsOf,
			ParameterHash: fibonacciParameterHash(req),
		}, nil
	}

	return invalidStructure("fibonacci", req.Timeframe, "no_valid_unexpired_leg", sourceTime, input.AsOf, fibonacciParameterHash(req)), nil
}

func calculateSupportResistanceStructure(input MarketInput, req SupportRequest) (StructureSnapshot, error) {
	if err := validateSupportRequest(req); err != nil {
		return StructureSnapshot{}, err
	}
	klines, err := structureKlines(input, req.Timeframe, req.Lookback)
	if err != nil {
		return StructureSnapshot{}, err
	}
	startIndex := len(input.Timeframes[req.Timeframe]) - len(klines)
	swings := findSwingPoints(klines, startIndex, req.SwingWindow)
	sourceTime := klineSourceTime(klines[len(klines)-1])
	atr := calculateATR(klines, 14)
	if atr <= 0 {
		return StructureSnapshot{}, fmt.Errorf("%s support_resistance[%s] requires positive ATR14", input.Symbol, req.Timeframe)
	}
	zoneWidth := atr * req.ZoneWidthATR
	current := klines[len(klines)-1].Close
	support, supportTouches := nearestCluster(swings, "low", current, zoneWidth, req.MinTouches, req.MinDistanceBars, true)
	resistance, resistanceTouches := nearestCluster(swings, "high", current, zoneWidth, req.MinTouches, req.MinDistanceBars, false)
	if support <= 0 && resistance <= 0 {
		return invalidStructure("support_resistance", req.Timeframe, "no_level_with_required_touches", sourceTime, input.AsOf, supportParameterHash(req)), nil
	}

	levels := map[string]float64{"zone_width": zoneWidth}
	if support > 0 {
		levels["support"] = support
		levels["support_touches"] = float64(supportTouches)
	}
	if resistance > 0 {
		levels["resistance"] = resistance
		levels["resistance_touches"] = float64(resistanceTouches)
	}
	return StructureSnapshot{
		Name:          "support_resistance",
		Timeframe:     req.Timeframe,
		Valid:         true,
		KeyLevels:     levels,
		Confirmed:     true,
		SourceTime:    sourceTime,
		AvailableAt:   input.AsOf,
		ParameterHash: supportParameterHash(req),
	}, nil
}

func validateFibonacciRequest(req FibonacciRequest) error {
	if req.Timeframe == "" {
		return fmt.Errorf("fibonacci timeframe is required")
	}
	if req.Lookback <= 0 {
		return fmt.Errorf("fibonacci lookback is required")
	}
	if req.SwingWindow <= 0 {
		return fmt.Errorf("fibonacci swing_window is required")
	}
	if req.MinLegBars <= 0 {
		return fmt.Errorf("fibonacci min_leg_bars is required")
	}
	if req.MinLegATRMultiple <= 0 {
		return fmt.Errorf("fibonacci min_leg_atr_multiple is required")
	}
	if req.ZigZagThresholdPct <= 0 {
		return fmt.Errorf("fibonacci zigzag_threshold_pct is required")
	}
	if len(req.Levels) == 0 {
		return fmt.Errorf("fibonacci levels are required")
	}
	return nil
}

func validateSupportRequest(req SupportRequest) error {
	if req.Timeframe == "" {
		return fmt.Errorf("support timeframe is required")
	}
	if req.Lookback <= 0 {
		return fmt.Errorf("support lookback is required")
	}
	if req.SwingWindow <= 0 {
		return fmt.Errorf("support swing_window is required")
	}
	if req.ZoneWidthATR <= 0 {
		return fmt.Errorf("support zone_width_atr is required")
	}
	if req.MinTouches <= 0 {
		return fmt.Errorf("support min_touches is required")
	}
	if req.MinDistanceBars <= 0 {
		return fmt.Errorf("support min_distance_bars is required")
	}
	return nil
}

func structureKlines(input MarketInput, timeframe string, lookback int) ([]Kline, error) {
	klines, ok := input.Timeframes[timeframe]
	if !ok || len(klines) == 0 {
		return nil, fmt.Errorf("%s timeframe %s is unavailable", input.Symbol, timeframe)
	}
	if len(klines) < lookback {
		return nil, fmt.Errorf("%s %s requires %d bars, got %d", input.Symbol, timeframe, lookback, len(klines))
	}
	return klines[len(klines)-lookback:], nil
}

func findSwingPoints(klines []Kline, startIndex, window int) []swingPoint {
	out := []swingPoint{}
	for i := window; i < len(klines)-window; i++ {
		isHigh := true
		isLow := true
		for j := i - window; j <= i+window; j++ {
			if j == i {
				continue
			}
			if klines[j].High >= klines[i].High {
				isHigh = false
			}
			if klines[j].Low <= klines[i].Low {
				isLow = false
			}
		}
		if isHigh {
			out = append(out, swingPoint{Index: startIndex + i, Time: klineSourceTime(klines[i]), Price: klines[i].High, Kind: "high"})
		}
		if isLow {
			out = append(out, swingPoint{Index: startIndex + i, Time: klineSourceTime(klines[i]), Price: klines[i].Low, Kind: "low"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})
	return out
}

func nearestCluster(swings []swingPoint, kind string, current, width float64, minTouches, minDistanceBars int, below bool) (float64, int) {
	bestLevel := 0.0
	bestDistance := math.MaxFloat64
	bestTouches := 0
	for _, pivot := range swings {
		if pivot.Kind != kind {
			continue
		}
		if below && pivot.Price > current {
			continue
		}
		if !below && pivot.Price < current {
			continue
		}
		touches := 0
		sum := 0.0
		lastIndex := -minDistanceBars * 2
		for _, candidate := range swings {
			if candidate.Kind != kind {
				continue
			}
			if math.Abs(candidate.Price-pivot.Price) > width {
				continue
			}
			if candidate.Index-lastIndex < minDistanceBars {
				continue
			}
			touches++
			sum += candidate.Price
			lastIndex = candidate.Index
		}
		if touches < minTouches {
			continue
		}
		level := sum / float64(touches)
		distance := math.Abs(current - level)
		if distance < bestDistance {
			bestDistance = distance
			bestLevel = level
			bestTouches = touches
		}
	}
	return bestLevel, bestTouches
}

func fibonacciLevels(direction string, low, high float64, levels []float64) map[string]float64 {
	out := map[string]float64{}
	leg := high - low
	for _, level := range levels {
		key := fmt.Sprintf("fib_%s", strings.ReplaceAll(fmt.Sprintf("%.3f", level), ".", "_"))
		if direction == "up" {
			out[key] = high - leg*level
		} else {
			out[key] = low + leg*level
		}
	}
	return out
}

func anchorFromSwing(s swingPoint) StructureAnchor {
	return StructureAnchor{
		Time:  s.Time,
		Index: s.Index,
		Price: s.Price,
		Kind:  s.Kind,
	}
}

func invalidStructure(name, timeframe, reason string, sourceTime, availableAt time.Time, parameterHash string) StructureSnapshot {
	return StructureSnapshot{
		Name:          name,
		Timeframe:     timeframe,
		Valid:         false,
		Reason:        reason,
		Confirmed:     false,
		SourceTime:    sourceTime,
		AvailableAt:   availableAt,
		ParameterHash: parameterHash,
	}
}

func fibonacciParameterHash(req FibonacciRequest) string {
	return fmt.Sprintf("tf=%s|lookback=%d|swing=%d|minbars=%d|minatr=%.3f|zigzag=%.3f|levels=%v|breakbase=%t",
		req.Timeframe, req.Lookback, req.SwingWindow, req.MinLegBars, req.MinLegATRMultiple, req.ZigZagThresholdPct, req.Levels, req.InvalidateOnBreakBase)
}

func supportParameterHash(req SupportRequest) string {
	return fmt.Sprintf("tf=%s|lookback=%d|swing=%d|zoneatr=%.3f|touches=%d|distance=%d",
		req.Timeframe, req.Lookback, req.SwingWindow, req.ZoneWidthATR, req.MinTouches, req.MinDistanceBars)
}
