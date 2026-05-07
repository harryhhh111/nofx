package market

import "math"

const (
	bbMACDRegimeTrend    = "trend"
	bbMACDRegimeRange    = "range"
	bbMACDRegimeVolatile = "volatile"

	bbMACDStateBullishBreakout = "bullish_breakout"
	bbMACDStateBearishBreakout = "bearish_breakout"
	bbMACDStateBullishMomentum = "bullish_momentum"
	bbMACDStateBearishMomentum = "bearish_momentum"
	bbMACDStateNeutral         = "neutral"
)

var (
	bbMACDTrendParams = BBMACDParams{
		Fast:           8,
		Slow:           21,
		Signal:         5,
		BOLLPeriod:     20,
		BOLLMultiplier: 2.0,
	}
	bbMACDRangeParams = BBMACDParams{
		Fast:           12,
		Slow:           26,
		Signal:         9,
		BOLLPeriod:     20,
		BOLLMultiplier: 2.0,
	}
	bbMACDVolatileParams = BBMACDParams{
		Fast:           16,
		Slow:           34,
		Signal:         9,
		BOLLPeriod:     24,
		BOLLMultiplier: 2.5,
	}
)

func calculateBBMACD(klines []Kline) *BBMACDData {
	regime, params := selectBBMACDParams(klines)
	if len(klines) < params.Slow+maxInt(params.Signal, params.BOLLPeriod) {
		return nil
	}

	macdValues := calculateMACDSeries(klines, params.Fast, params.Slow)
	if len(macdValues) < maxInt(params.Signal, params.BOLLPeriod) {
		return nil
	}

	signalValues := calculateEMAFloatSeries(macdValues, params.Signal)
	if len(signalValues) == 0 {
		return nil
	}

	macd := macdValues[len(macdValues)-1]
	signal := signalValues[len(signalValues)-1]
	histogram := macd - signal
	upper, middle, lower := calculateFloatBOLL(macdValues, params.BOLLPeriod, params.BOLLMultiplier)
	if upper == 0 && middle == 0 && lower == 0 {
		return nil
	}

	state := bbMACDStateNeutral
	switch {
	case macd > upper:
		state = bbMACDStateBullishBreakout
	case macd < lower:
		state = bbMACDStateBearishBreakout
	case macd > signal && histogram > 0:
		state = bbMACDStateBullishMomentum
	case macd < signal && histogram < 0:
		state = bbMACDStateBearishMomentum
	}

	bandWidth := math.Abs(upper - lower)
	strength := 0.0
	if bandWidth > 0 {
		strength = math.Abs(macd-middle) / bandWidth
	}

	return &BBMACDData{
		Regime:    regime,
		Params:    params,
		State:     state,
		Strength:  strength,
		MACD:      macd,
		Signal:    signal,
		Histogram: histogram,
		Upper:     upper,
		Middle:    middle,
		Lower:     lower,
	}
}

func selectBBMACDParams(klines []Kline) (string, BBMACDParams) {
	if len(klines) < 50 {
		return bbMACDRegimeRange, bbMACDRangeParams
	}

	last := klines[len(klines)-1]
	price := last.Close
	if price <= 0 {
		return bbMACDRegimeRange, bbMACDRangeParams
	}

	atrPct := calculateATR(klines, 14) / price * 100
	upper, middle, lower := calculateBOLL(klines, 20, 2.0)
	bollWidthPct := 0.0
	if middle > 0 {
		bollWidthPct = (upper - lower) / middle * 100
	}

	ema20 := calculateEMA(klines, 20)
	ema50 := calculateEMA(klines, 50)
	emaGapPct := 0.0
	if ema20 > 0 && ema50 > 0 {
		emaGapPct = math.Abs(ema20-ema50) / price * 100
	}

	lookback := 20
	if len(klines) <= lookback {
		lookback = len(klines) - 1
	}
	slopePct := 0.0
	if lookback > 0 {
		prev := klines[len(klines)-1-lookback].Close
		if prev > 0 {
			slopePct = math.Abs(price-prev) / prev * 100
		}
	}

	noiseScore := directionChangeRatio(klines, 20)
	if atrPct >= 4.5 || bollWidthPct >= 8.0 || noiseScore >= 0.65 {
		return bbMACDRegimeVolatile, bbMACDVolatileParams
	}
	if (emaGapPct >= 0.8 || slopePct >= 2.0) && noiseScore < 0.55 {
		return bbMACDRegimeTrend, bbMACDTrendParams
	}
	return bbMACDRegimeRange, bbMACDRangeParams
}

func calculateMACDSeries(klines []Kline, fast, slow int) []float64 {
	if fast <= 0 || slow <= 0 || fast >= slow || len(klines) < slow {
		return nil
	}
	values := make([]float64, 0, len(klines)-slow+1)
	for i := slow; i <= len(klines); i++ {
		window := klines[:i]
		values = append(values, calculateEMA(window, fast)-calculateEMA(window, slow))
	}
	return values
}

func calculateEMAFloatSeries(values []float64, period int) []float64 {
	if period <= 0 || len(values) < period {
		return nil
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	ema := sum / float64(period)
	result := []float64{ema}

	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(values); i++ {
		ema = (values[i]-ema)*multiplier + ema
		result = append(result, ema)
	}
	return result
}

func calculateFloatBOLL(values []float64, period int, multiplier float64) (upper, middle, lower float64) {
	if period <= 0 || len(values) < period {
		return 0, 0, 0
	}

	start := len(values) - period
	sum := 0.0
	for i := start; i < len(values); i++ {
		sum += values[i]
	}
	middle = sum / float64(period)

	variance := 0.0
	for i := start; i < len(values); i++ {
		diff := values[i] - middle
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))
	upper = middle + multiplier*stdDev
	lower = middle - multiplier*stdDev
	return upper, middle, lower
}

func directionChangeRatio(klines []Kline, lookback int) float64 {
	if lookback < 3 || len(klines) < 3 {
		return 0
	}
	if len(klines) < lookback {
		lookback = len(klines)
	}

	start := len(klines) - lookback
	changes := 0
	comparisons := 0
	lastDirection := 0
	for i := start + 1; i < len(klines); i++ {
		direction := 0
		switch {
		case klines[i].Close > klines[i-1].Close:
			direction = 1
		case klines[i].Close < klines[i-1].Close:
			direction = -1
		}
		if direction == 0 {
			continue
		}
		if lastDirection != 0 {
			comparisons++
			if direction != lastDirection {
				changes++
			}
		}
		lastDirection = direction
	}
	if comparisons == 0 {
		return 0
	}
	return float64(changes) / float64(comparisons)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
