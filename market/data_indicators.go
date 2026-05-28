package market

import "math"

// calculateSMA calculates Simple Moving Average
func calculateSMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}
	sum := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		sum += klines[i].Close
	}
	return sum / float64(period)
}

// calculateEMA calculates EMA
func calculateEMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	// Calculate SMA as initial EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	// Calculate EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

// calculateMACD calculates MACD
func calculateMACD(klines []Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	// Calculate 12-period and 26-period EMA
	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	// MACD = EMA12 - EMA26
	return ema12 - ema26
}

func calculateMACDFull(klines []Kline, fast, slow, signal int) (macd, signalLine, histogram float64) {
	if fast <= 0 || slow <= 0 || signal <= 0 || fast >= slow || len(klines) < slow+signal {
		return 0, 0, 0
	}
	macdSeries := make([]float64, 0, len(klines)-slow+1)
	for i := slow; i <= len(klines); i++ {
		macdSeries = append(macdSeries, calculateEMA(klines[:i], fast)-calculateEMA(klines[:i], slow))
	}
	signalSeries := calculateEMAFloatSeries(macdSeries, signal)
	if len(signalSeries) == 0 {
		return 0, 0, 0
	}
	macd = macdSeries[len(macdSeries)-1]
	signalLine = signalSeries[len(signalSeries)-1]
	histogram = macd - signalLine
	return macd, signalLine, histogram
}

// calculateRSI calculates RSI
func calculateRSI(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	// Calculate initial average gain/loss
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Use Wilder smoothing method to calculate subsequent RSI
	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateATR calculates ATR
func calculateATR(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// Calculate initial ATR
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder smoothing
	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateBOLL calculates Bollinger Bands (upper, middle, lower)
// period: typically 20, multiplier: typically 2
func calculateBOLL(klines []Kline, period int, multiplier float64) (upper, middle, lower float64) {
	if len(klines) < period {
		return 0, 0, 0
	}

	// Calculate SMA (middle band)
	sum := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		sum += klines[i].Close
	}
	sma := sum / float64(period)

	// Calculate standard deviation
	variance := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		diff := klines[i].Close - sma
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	// Calculate bands
	middle = sma
	upper = sma + multiplier*stdDev
	lower = sma - multiplier*stdDev

	return upper, middle, lower
}

// calculateDonchian calculates Donchian channel (highest high, lowest low) for given period
func calculateDonchian(klines []Kline, period int) (upper, lower float64) {
	if len(klines) == 0 || period <= 0 {
		return 0, 0
	}

	// Use all available klines if period > len(klines)
	start := len(klines) - period
	if start < 0 {
		start = 0
	}

	upper = klines[start].High
	lower = klines[start].Low

	for i := start + 1; i < len(klines); i++ {
		if klines[i].High > upper {
			upper = klines[i].High
		}
		if klines[i].Low < lower {
			lower = klines[i].Low
		}
	}

	return upper, lower
}

func calculateVWAP(klines []Kline, period int) float64 {
	if len(klines) < period || period <= 0 {
		return 0
	}
	start := len(klines) - period
	priceVolume := 0.0
	volume := 0.0
	for i := start; i < len(klines); i++ {
		typical := (klines[i].High + klines[i].Low + klines[i].Close) / 3
		priceVolume += typical * klines[i].Volume
		volume += klines[i].Volume
	}
	if volume <= 0 {
		return 0
	}
	return priceVolume / volume
}

func calculateAverageVolume(klines []Kline, period int) float64 {
	if len(klines) < period || period <= 0 {
		return 0
	}
	start := len(klines) - period
	sum := 0.0
	for i := start; i < len(klines); i++ {
		sum += klines[i].Volume
	}
	return sum / float64(period)
}

func calculatePriceChangeWindow(klines []Kline, bars int) float64 {
	if bars <= 0 || len(klines) <= bars {
		return 0
	}
	current := klines[len(klines)-1].Close
	base := klines[len(klines)-1-bars].Close
	if base <= 0 {
		return 0
	}
	return (current - base) / base * 100
}

func calculateRealizedVol(klines []Kline, period int) float64 {
	if len(klines) <= period || period <= 1 {
		return 0
	}
	start := len(klines) - period
	returns := make([]float64, 0, period)
	for i := start + 1; i < len(klines); i++ {
		prev := klines[i-1].Close
		if prev <= 0 || klines[i].Close <= 0 {
			continue
		}
		returns = append(returns, math.Log(klines[i].Close/prev))
	}
	if len(returns) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	return math.Sqrt(variance/float64(len(returns)-1)) * 100
}

// Box period constants (in 1h candles)
const (
	ShortBoxPeriod = 72  // 3 days of 1h candles
	MidBoxPeriod   = 240 // 10 days of 1h candles
	LongBoxPeriod  = 500 // ~21 days of 1h candles
)

// calculateBoxData calculates multi-period box data from klines
func calculateBoxData(klines []Kline, currentPrice float64) *BoxData {
	box := &BoxData{
		CurrentPrice: currentPrice,
	}

	if len(klines) == 0 {
		return box
	}

	box.ShortUpper, box.ShortLower = calculateDonchian(klines, ShortBoxPeriod)
	box.MidUpper, box.MidLower = calculateDonchian(klines, MidBoxPeriod)
	box.LongUpper, box.LongLower = calculateDonchian(klines, LongBoxPeriod)

	return box
}

// ========== Exported indicator calculation functions (for testing) ==========

// ExportCalculateSMA exports calculateSMA for testing
func ExportCalculateSMA(klines []Kline, period int) float64 {
	return calculateSMA(klines, period)
}

// ExportCalculateEMA exports calculateEMA for testing
func ExportCalculateEMA(klines []Kline, period int) float64 {
	return calculateEMA(klines, period)
}

// ExportCalculateMACD exports calculateMACD for testing
func ExportCalculateMACD(klines []Kline) float64 {
	return calculateMACD(klines)
}

// ExportCalculateRSI exports calculateRSI for testing
func ExportCalculateRSI(klines []Kline, period int) float64 {
	return calculateRSI(klines, period)
}

// ExportCalculateATR exports calculateATR for testing
func ExportCalculateATR(klines []Kline, period int) float64 {
	return calculateATR(klines, period)
}

// ExportCalculateBOLL exports calculateBOLL for testing
func ExportCalculateBOLL(klines []Kline, period int, multiplier float64) (upper, middle, lower float64) {
	return calculateBOLL(klines, period, multiplier)
}

// ExportCalculateDonchian exports calculateDonchian for testing
func ExportCalculateDonchian(klines []Kline, period int) (float64, float64) {
	return calculateDonchian(klines, period)
}

// ExportCalculateBoxData exports calculateBoxData for testing
func ExportCalculateBoxData(klines []Kline, currentPrice float64) *BoxData {
	return calculateBoxData(klines, currentPrice)
}

// calculateParabolicSAR calculates Parabolic SAR (Stop and Reverse).
// Returns the current SAR value, whether trend is up, and flip signals.
// AF starts at 0.02, increments by 0.02 on new extremes, max 0.2.
func calculateParabolicSAR(klines []Kline) (sar float64, isUptrend, flipUp, flipDown bool) {
	if len(klines) < 2 {
		return 0, false, false, false
	}

	// Initial trend: up if close[1] >= close[0]
	isUptrend = klines[1].Close >= klines[0].Close
	af := 0.02
	var ep float64

	if isUptrend {
		sar = klines[0].Low
		ep = klines[1].High
	} else {
		sar = klines[0].High
		ep = klines[1].Low
	}

	// Iterate from the 3rd bar
	for i := 2; i < len(klines); i++ {
		curr := klines[i]

		// Calculate SAR
		sar = sar + af*(ep-sar)

		if isUptrend {
			// Limit SAR to not exceed previous two lows
			if i >= 2 {
				sar = math.Min(sar, klines[i-1].Low)
				sar = math.Min(sar, klines[i-2].Low)
			}

			// Check for trend reversal
			if curr.Low < sar {
				// Flip to downtrend
				flipDown = true
				flipUp = false
				isUptrend = false
				sar = ep
				if sar < curr.High {
					sar = curr.High
				}
				ep = curr.Low
				af = 0.02
			} else {
				flipDown = false
				flipUp = false
				// Continue uptrend
				if curr.High > ep {
					ep = curr.High
					af = math.Min(af+0.02, 0.2)
				}
			}
		} else {
			// Downtrend: limit SAR to not be below previous two highs
			if i >= 2 {
				sar = math.Max(sar, klines[i-1].High)
				sar = math.Max(sar, klines[i-2].High)
			}

			// Check for trend reversal
			if curr.High > sar {
				// Flip to uptrend
				flipUp = true
				flipDown = false
				isUptrend = true
				sar = ep
				if sar > curr.Low {
					sar = curr.Low
				}
				ep = curr.High
				af = 0.02
			} else {
				flipUp = false
				flipDown = false
				// Continue downtrend
				if curr.Low < ep {
					ep = curr.Low
					af = math.Min(af+0.02, 0.2)
				}
			}
		}
	}

	return sar, isUptrend, flipUp, flipDown
}

// ExportCalculateParabolicSAR exports calculateParabolicSAR for testing
func ExportCalculateParabolicSAR(klines []Kline) (sar float64, isUptrend, flipUp, flipDown bool) {
	return calculateParabolicSAR(klines)
}

// wilderSmooth applies Wilder's smoothing to a series of values.
// The first smoothed value is the SMA of the first 'period' values.
// Returns a slice where result[i] corresponds to the smoothed value
// covering the input range [i, i+period-1].
func wilderSmooth(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}
	result := make([]float64, len(values)-period+1)
	var sum float64
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[0] = sum / float64(period)
	for i := period; i < len(values); i++ {
		result[i-period+1] = (result[i-period]*float64(period-1) + values[i]) / float64(period)
	}
	return result
}

// calculateADX calculates ADX, +DI, and -DI using Wilder's smoothing method.
// Returns the current (latest) values. Requires at least 2*period+1 klines.
func calculateADX(klines []Kline, period int) (adx, plusDI, minusDI float64) {
	if len(klines) < 2*period+1 {
		return 0, 0, 0
	}

	// Step 1: Calculate TR, +DM, -DM for each bar (starting from index 1)
	n := len(klines) - 1
	trs := make([]float64, n)
	plusDMs := make([]float64, n)
	minusDMs := make([]float64, n)

	for i := 1; i < len(klines); i++ {
		curr, prev := klines[i], klines[i-1]
		trs[i-1] = math.Max(curr.High-curr.Low, math.Max(math.Abs(curr.High-prev.Close), math.Abs(curr.Low-prev.Close)))

		upMove := curr.High - prev.High
		downMove := prev.Low - curr.Low

		if upMove > downMove && upMove > 0 {
			plusDMs[i-1] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDMs[i-1] = downMove
		}
	}

	// Step 2: Wilder smoothing for TR, +DM, -DM
	atrSeries := wilderSmooth(trs, period)
	smoothPlusDM := wilderSmooth(plusDMs, period)
	smoothMinusDM := wilderSmooth(minusDMs, period)

	if len(atrSeries) == 0 || len(smoothPlusDM) == 0 || len(smoothMinusDM) == 0 {
		return 0, 0, 0
	}

	// Step 3: Calculate +DI, -DI, DX
	dxSeries := make([]float64, len(atrSeries))
	for i := range atrSeries {
		if atrSeries[i] != 0 {
			pdi := 100 * smoothPlusDM[i] / atrSeries[i]
			mdi := 100 * smoothMinusDM[i] / atrSeries[i]
			diSum := pdi + mdi
			if diSum != 0 {
				dxSeries[i] = 100 * math.Abs(pdi-mdi) / diSum
			}
		}
	}

	// Step 4: ADX = Wilder smooth of DX
	adxSeries := wilderSmooth(dxSeries, period)

	last := len(adxSeries) - 1
	atrLast := len(atrSeries) - 1
	if last >= 0 && atrLast >= 0 {
		adx = adxSeries[last]
		if atrSeries[atrLast] != 0 {
			plusDI = 100 * smoothPlusDM[atrLast] / atrSeries[atrLast]
			minusDI = 100 * smoothMinusDM[atrLast] / atrSeries[atrLast]
		}
	}
	return
}

// ExportCalculateADX exports calculateADX for testing
func ExportCalculateADX(klines []Kline, period int) (adx, plusDI, minusDI float64) {
	return calculateADX(klines, period)
}
