package market

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	structureBoolTrue                  = 1.0
	structureBoolFalse                 = 0.0
	trendContinuationTriggerATRRatio   = 0.5
	trendContinuationMinTriggerATR     = 0.25
	trendContinuationRecentCloseBars   = 4
	trendContinuationRecentCloseNeeded = 2
)

func calculateMarketStructure(input MarketInput, req MarketStructureRequest) ([]StructureSnapshot, error) {
	if err := validateMarketStructureRequest(req); err != nil {
		return nil, err
	}
	klines, err := structureKlines(input, req.Timeframe, req.Lookback)
	if err != nil {
		return nil, err
	}
	sourceTime := klineSourceTime(klines[len(klines)-1])
	parameterHash := marketStructureParameterHash(req)
	atr := calculateATR(klines, 14)
	if atr <= 0 {
		return nil, fmt.Errorf("%s market_structure[%s] requires positive ATR14", input.Symbol, req.Timeframe)
	}

	startIndex := len(input.Timeframes[req.Timeframe]) - len(klines)
	rawSwings := findSwingPoints(klines, startIndex, req.SwingWindow)
	swings := confirmedZigZagSwings(rawSwings, atr, req)
	if len(swings) < 2 {
		invalid := invalidStructure("market_structure", req.Timeframe, "not_enough_confirmed_swings", sourceTime, input.AsOf, parameterHash)
		return []StructureSnapshot{invalid}, nil
	}

	current := klines[len(klines)-1].Close
	trend, sequence := swingTrend(swings)
	latestHigh, hasHigh := latestSwing(swings, "high")
	prevHigh, hasPrevHigh := previousSwing(swings, "high")
	latestLow, hasLow := latestSwing(swings, "low")
	prevLow, hasPrevLow := previousSwing(swings, "low")
	currentLeg := buildCurrentLeg(swings[len(swings)-1], current, len(input.Timeframes[req.Timeframe])-1, sourceTime, atr)
	phase, evidence := structurePhase(trend, current, currentLeg, latestHigh, hasHigh, latestLow, hasLow, klines, atr, req)
	invalidPrice := invalidationPrice(trend, latestHigh, hasHigh, latestLow, hasLow)
	levels := structureLevels(latestHigh, hasHigh, prevHigh, hasPrevHigh, latestLow, hasLow, prevLow, hasPrevLow, currentLeg, evidence)
	levels["confirmed_swing_count"] = float64(len(swings))

	snapshot := StructureSnapshot{
		Name:          "market_structure",
		Timeframe:     req.Timeframe,
		Valid:         true,
		Reason:        sequence,
		Direction:     trendDirection(trend),
		Phase:         phase,
		AnchorLow:     optionalAnchor(latestLow, hasLow),
		AnchorHigh:    optionalAnchor(latestHigh, hasHigh),
		CurrentLeg:    &currentLeg,
		InvalidPrice:  invalidPrice,
		KeyLevels:     levels,
		Evidence:      evidence,
		Confirmed:     true,
		SourceTime:    sourceTime,
		AvailableAt:   input.AsOf,
		ParameterHash: parameterHash,
	}
	setup := detectSetup(snapshot, current, klines, atr, req)
	return []StructureSnapshot{snapshot, setup}, nil
}

func validateMarketStructureRequest(req MarketStructureRequest) error {
	if req.Timeframe == "" {
		return fmt.Errorf("market_structure timeframe is required")
	}
	if req.Lookback <= 0 {
		return fmt.Errorf("market_structure lookback is required")
	}
	if req.SwingWindow <= 0 {
		return fmt.Errorf("market_structure swing_window is required")
	}
	if req.MinLegBars <= 0 {
		return fmt.Errorf("market_structure min_leg_bars is required")
	}
	if req.MinLegATRMultiple <= 0 {
		return fmt.Errorf("market_structure min_leg_atr_multiple is required")
	}
	if req.ZigZagThresholdPct <= 0 {
		return fmt.Errorf("market_structure zigzag_threshold_pct is required")
	}
	if req.BreakoutBufferATR <= 0 {
		return fmt.Errorf("market_structure breakout_buffer_atr is required")
	}
	if req.RetestToleranceATR <= 0 {
		return fmt.Errorf("market_structure retest_tolerance_atr is required")
	}
	if req.ExhaustionRSIPeriod <= 0 {
		return fmt.Errorf("market_structure exhaustion_rsi_period is required")
	}
	return nil
}

func confirmedZigZagSwings(swings []swingPoint, atr float64, req MarketStructureRequest) []swingPoint {
	out := []swingPoint{}
	for _, point := range swings {
		if len(out) == 0 {
			out = append(out, point)
			continue
		}
		last := out[len(out)-1]
		if point.Kind == last.Kind {
			if moreExtremeSwing(point, last) {
				out[len(out)-1] = point
			}
			continue
		}
		bars := point.Index - last.Index
		if bars < req.MinLegBars {
			continue
		}
		move := math.Abs(point.Price - last.Price)
		if move < atr*req.MinLegATRMultiple {
			continue
		}
		base := last.Price
		if base <= 0 || move/base*100 < req.ZigZagThresholdPct {
			continue
		}
		out = append(out, point)
	}
	return out
}

func moreExtremeSwing(next, current swingPoint) bool {
	if next.Kind == "high" {
		return next.Price > current.Price
	}
	return next.Price < current.Price
}

func swingTrend(swings []swingPoint) (string, string) {
	latestHigh, hasLatestHigh := latestSwing(swings, "high")
	prevHigh, hasPrevHigh := previousSwing(swings, "high")
	latestLow, hasLatestLow := latestSwing(swings, "low")
	prevLow, hasPrevLow := previousSwing(swings, "low")
	if hasLatestHigh && hasPrevHigh && hasLatestLow && hasPrevLow {
		switch {
		case latestHigh.Price > prevHigh.Price && latestLow.Price > prevLow.Price:
			return "uptrend", "HH/HL"
		case latestHigh.Price < prevHigh.Price && latestLow.Price < prevLow.Price:
			return "downtrend", "LL/LH"
		}
	}
	return "range", "mixed_high_low_sequence"
}

func latestSwing(swings []swingPoint, kind string) (swingPoint, bool) {
	for i := len(swings) - 1; i >= 0; i-- {
		if swings[i].Kind == kind {
			return swings[i], true
		}
	}
	return swingPoint{}, false
}

func previousSwing(swings []swingPoint, kind string) (swingPoint, bool) {
	seenLatest := false
	for i := len(swings) - 1; i >= 0; i-- {
		if swings[i].Kind != kind {
			continue
		}
		if !seenLatest {
			seenLatest = true
			continue
		}
		return swings[i], true
	}
	return swingPoint{}, false
}

func buildCurrentLeg(from swingPoint, current float64, currentIndex int, currentTime time.Time, atr float64) StructureLeg {
	to := StructureAnchor{Time: currentTime, Index: currentIndex, Price: current, Kind: "current"}
	direction := "flat"
	if current > from.Price {
		direction = "up"
	}
	if current < from.Price {
		direction = "down"
	}
	move := math.Abs(current - from.Price)
	movePct := 0.0
	if from.Price > 0 {
		movePct = move / from.Price * 100
	}
	atrMultiple := 0.0
	if atr > 0 {
		atrMultiple = move / atr
	}
	return StructureLeg{
		Direction:   direction,
		From:        anchorFromSwing(from),
		To:          to,
		Bars:        currentIndex - from.Index,
		MovePct:     movePct,
		ATRMultiple: atrMultiple,
		Confirmed:   false,
	}
}

func structurePhase(trend string, current float64, leg StructureLeg, high swingPoint, hasHigh bool, low swingPoint, hasLow bool, klines []Kline, atr float64, req MarketStructureRequest) (string, map[string]float64) {
	evidence := map[string]float64{}
	if hasHigh {
		evidence["latest_high"] = high.Price
	}
	if hasLow {
		evidence["latest_low"] = low.Price
	}
	evidence["current_leg_atr_multiple"] = leg.ATRMultiple
	evidence["current_leg_move_pct"] = leg.MovePct
	evidence["rsi"] = calculateRSI(klines, req.ExhaustionRSIPeriod)

	breakout := breakoutDirection(current, high, hasHigh, low, hasLow, atr, req.BreakoutBufferATR)
	evidence["breakout_up"] = boolFloat(breakout == "up")
	evidence["breakout_down"] = boolFloat(breakout == "down")
	evidence["retest_confirmed"] = boolFloat(retestConfirmed(breakout, high, hasHigh, low, hasLow, klines, atr, req.RetestToleranceATR))
	evidence["weakening"] = boolFloat(legWeakening(leg, evidence["rsi"]))
	evidence["invalidated"] = boolFloat(structureInvalidated(trend, current, high, hasHigh, low, hasLow))

	if evidence["invalidated"] == structureBoolTrue {
		return "invalidated", evidence
	}
	if breakout != "" || evidence["retest_confirmed"] == structureBoolTrue {
		return "early", evidence
	}
	if evidence["weakening"] == structureBoolTrue {
		return "late", evidence
	}
	if trend == "range" {
		return "middle", evidence
	}
	return "middle", evidence
}

func structureLevels(high swingPoint, hasHigh bool, prevHigh swingPoint, hasPrevHigh bool, low swingPoint, hasLow bool, prevLow swingPoint, hasPrevLow bool, leg StructureLeg, evidence map[string]float64) map[string]float64 {
	levels := map[string]float64{
		"current_leg_move_pct":     leg.MovePct,
		"current_leg_atr_multiple": leg.ATRMultiple,
	}
	for k, v := range evidence {
		levels[k] = v
	}
	if hasHigh {
		levels["resistance"] = high.Price
	}
	if hasPrevHigh {
		levels["previous_resistance"] = prevHigh.Price
	}
	if hasLow {
		levels["support"] = low.Price
	}
	if hasPrevLow {
		levels["previous_support"] = prevLow.Price
	}
	return levels
}

func detectSetup(structure StructureSnapshot, current float64, klines []Kline, atr float64, req MarketStructureRequest) StructureSnapshot {
	setup := StructureSnapshot{
		Name:          "setup",
		Timeframe:     structure.Timeframe,
		Valid:         false,
		Direction:     "neutral",
		Phase:         structure.Phase,
		KeyLevels:     copyFloatValues(structure.KeyLevels),
		Evidence:      copyFloatValues(structure.Evidence),
		Confirmed:     structure.Confirmed,
		SourceTime:    structure.SourceTime,
		AvailableAt:   structure.AvailableAt,
		ParameterHash: structure.ParameterHash,
	}
	if structure.Phase == "invalidated" {
		setup.Setup = "no_trade_structure_invalidated"
		setup.Reason = "confirmed structure was invalidated by price"
		return setup
	}
	support := structure.KeyLevels["support"]
	resistance := structure.KeyLevels["resistance"]
	failedSupport := support
	if previous := structure.KeyLevels["previous_support"]; previous > 0 {
		failedSupport = previous
	}
	failedResistance := resistance
	if previous := structure.KeyLevels["previous_resistance"]; previous > 0 {
		failedResistance = previous
	}
	rsi := structure.Evidence["rsi"]
	breakout := breakoutDirection(current, swingFromLevel(resistance, "high"), resistance > 0, swingFromLevel(support, "low"), support > 0, atr, req.BreakoutBufferATR)
	failed := failedBreakoutDirection(current, failedSupport, failedResistance, klines, atr, req.BreakoutBufferATR)
	nearSupport := nearLevel(current, support, atr, req.RetestToleranceATR)
	nearResistance := nearLevel(current, resistance, atr, req.RetestToleranceATR)

	switch {
	case failed == "up":
		return setup.withSetup("failed_breakout_short", "short", true, resistance, []string{"failed upside breakout", "back below resistance"})
	case failed == "down":
		return setup.withSetup("failed_breakout_long", "long", true, support, []string{"failed downside breakout", "back above support"})
	case breakout == "up":
		return setup.withSetup("breakout_long", "long", true, resistance, []string{"confirmed range break", "price above resistance"})
	case breakout == "down":
		return setup.withSetup("breakout_short", "short", true, support, []string{"confirmed range break", "price below support"})
	case structure.Direction == "up" && nearSupport:
		return setup.withSetup("trend_pullback_long", "long", true, support, []string{"HH/HL trend", "pullback near latest support"})
	case structure.Direction == "down" && nearResistance:
		return setup.withSetup("trend_pullback_short", "short", true, resistance, []string{"LL/LH trend", "pullback near latest resistance"})
	case structure.Direction == "up":
		if signals, ok := trendContinuationSignals(structure, "up", klines, req); ok {
			return setup.withSetup("trend_continuation_long", "long", true, support, append([]string{"HH/HL trend"}, signals...))
		}
		return setup.withSetup("no_trade_wait_trigger", "long", false, support, []string{"HH/HL trend", "waiting for entry trigger"})
	case structure.Direction == "down":
		if signals, ok := trendContinuationSignals(structure, "down", klines, req); ok {
			return setup.withSetup("trend_continuation_short", "short", true, resistance, append([]string{"LL/LH trend"}, signals...))
		}
		return setup.withSetup("no_trade_wait_trigger", "short", false, resistance, []string{"LL/LH trend", "waiting for entry trigger"})
	case nearSupport && rsi <= 35:
		return setup.withSetup("range_reversal_long", "long", true, support, []string{"range support", "momentum exhaustion"})
	case nearResistance && rsi >= 65:
		return setup.withSetup("range_reversal_short", "short", true, resistance, []string{"range resistance", "momentum exhaustion"})
	case rsi <= 30 && support > 0:
		return setup.withSetup("momentum_exhaustion_long", "long", true, support, []string{"oversold momentum", "defined support"})
	case rsi >= 70 && resistance > 0:
		return setup.withSetup("momentum_exhaustion_short", "short", true, resistance, []string{"overbought momentum", "defined resistance"})
	case nearSupport:
		return setup.withSetup("support_resistance_bounce_long", "long", true, support, []string{"price near support"})
	case nearResistance:
		return setup.withSetup("support_resistance_bounce_short", "short", true, resistance, []string{"price near resistance"})
	default:
		setup.Setup = "no_trade_chop"
		setup.Reason = "no reproducible setup from confirmed swings"
		return setup
	}
}

func trendContinuationSignals(structure StructureSnapshot, direction string, klines []Kline, req MarketStructureRequest) ([]string, bool) {
	if structure.CurrentLeg != nil && structure.CurrentLeg.Direction == direction {
		minATR := req.MinLegATRMultiple * trendContinuationTriggerATRRatio
		if minATR < trendContinuationMinTriggerATR {
			minATR = trendContinuationMinTriggerATR
		}
		if structure.CurrentLeg.ATRMultiple >= minATR {
			return []string{"current leg advancing with trend"}, true
		}
	}
	if recentClosesAdvance(direction, klines) {
		return []string{"recent closes re-accelerating with trend"}, true
	}
	return nil, false
}

func recentClosesAdvance(direction string, klines []Kline) bool {
	if len(klines) < 3 {
		return false
	}
	start := len(klines) - trendContinuationRecentCloseBars
	if start < 0 {
		start = 0
	}
	advances := 0
	for i := start + 1; i < len(klines); i++ {
		switch direction {
		case "up":
			if klines[i].Close > klines[i-1].Close {
				advances++
			}
		case "down":
			if klines[i].Close < klines[i-1].Close {
				advances++
			}
		}
	}
	return advances >= trendContinuationRecentCloseNeeded
}

func (s StructureSnapshot) withSetup(name, direction string, valid bool, invalidPrice float64, signals []string) StructureSnapshot {
	s.Setup = name
	s.Direction = direction
	s.Valid = valid
	s.InvalidPrice = invalidPrice
	s.Signals = signals
	s.Reason = strings.Join(signals, "; ")
	return s
}

func trendDirection(trend string) string {
	switch trend {
	case "uptrend":
		return "up"
	case "downtrend":
		return "down"
	default:
		return "range"
	}
}

func optionalAnchor(point swingPoint, ok bool) *StructureAnchor {
	if !ok {
		return nil
	}
	anchor := anchorFromSwing(point)
	return &anchor
}

func invalidationPrice(trend string, high swingPoint, hasHigh bool, low swingPoint, hasLow bool) float64 {
	if trend == "uptrend" && hasLow {
		return low.Price
	}
	if trend == "downtrend" && hasHigh {
		return high.Price
	}
	return 0
}

func structureInvalidated(trend string, current float64, high swingPoint, hasHigh bool, low swingPoint, hasLow bool) bool {
	switch trend {
	case "uptrend":
		return hasLow && current < low.Price
	case "downtrend":
		return hasHigh && current > high.Price
	default:
		return false
	}
}

func breakoutDirection(current float64, high swingPoint, hasHigh bool, low swingPoint, hasLow bool, atr, bufferATR float64) string {
	buffer := atr * bufferATR
	if hasHigh && current > high.Price+buffer {
		return "up"
	}
	if hasLow && current < low.Price-buffer {
		return "down"
	}
	return ""
}

func failedBreakoutDirection(current, support, resistance float64, klines []Kline, atr, bufferATR float64) string {
	if len(klines) == 0 {
		return ""
	}
	buffer := atr * bufferATR
	start := len(klines) - 5
	if start < 0 {
		start = 0
	}
	upFailed := resistance > 0 && current < resistance
	downFailed := support > 0 && current > support
	for _, bar := range klines[start:] {
		if upFailed && bar.High > resistance+buffer {
			return "up"
		}
		if downFailed && bar.Low < support-buffer {
			return "down"
		}
	}
	return ""
}

func retestConfirmed(direction string, high swingPoint, hasHigh bool, low swingPoint, hasLow bool, klines []Kline, atr, toleranceATR float64) bool {
	if len(klines) == 0 || direction == "" {
		return false
	}
	tolerance := atr * toleranceATR
	recent := klines[len(klines)-1]
	switch direction {
	case "up":
		return hasHigh && recent.Low <= high.Price+tolerance && recent.Close > high.Price
	case "down":
		return hasLow && recent.High >= low.Price-tolerance && recent.Close < low.Price
	default:
		return false
	}
}

func legWeakening(leg StructureLeg, rsi float64) bool {
	if leg.ATRMultiple >= 4 {
		return true
	}
	if leg.Direction == "up" && rsi >= 72 {
		return true
	}
	if leg.Direction == "down" && rsi <= 28 {
		return true
	}
	return false
}

func nearLevel(current, level, atr, toleranceATR float64) bool {
	if current <= 0 || level <= 0 || atr <= 0 {
		return false
	}
	return math.Abs(current-level) <= atr*toleranceATR
}

func swingFromLevel(level float64, kind string) swingPoint {
	return swingPoint{Price: level, Kind: kind}
}

func boolFloat(ok bool) float64 {
	if ok {
		return structureBoolTrue
	}
	return structureBoolFalse
}

func copyFloatValues(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func marketStructureParameterHash(req MarketStructureRequest) string {
	return fmt.Sprintf("tf=%s|lookback=%d|swing=%d|minbars=%d|minatr=%.3f|zigzag=%.3f|breakatr=%.3f|retestatr=%.3f|rsi=%d",
		req.Timeframe, req.Lookback, req.SwingWindow, req.MinLegBars, req.MinLegATRMultiple, req.ZigZagThresholdPct, req.BreakoutBufferATR, req.RetestToleranceATR, req.ExhaustionRSIPeriod)
}
