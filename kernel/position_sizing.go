package kernel

import "math"

const (
	defaultRiskPerTradePct       = 1.0
	defaultMinPositionSizeUSD    = 12.0
	defaultBTCETHMinNotionalUSD  = 60.0
	defaultBTCETHMaxPositionRate = 5.0
	defaultAltMaxPositionRate    = 1.0
)

func applyRiskBasedPositionSizing(signals []CandidateSignal, account AccountInfo, sizing *PositionSizingConfig) {
	if sizing == nil {
		return
	}
	equity := account.TotalEquity
	if equity <= 0 {
		equity = account.AvailableBalance
	}
	if equity <= 0 {
		return
	}

	riskPct := sizing.RiskPerTradePct
	if riskPct <= 0 {
		riskPct = defaultRiskPerTradePct
	}
	minSize := sizing.MinPositionSizeUSD
	if minSize <= 0 {
		minSize = defaultMinPositionSizeUSD
	}
	maxMarginUsage := sizing.MaxMarginUsage
	if maxMarginUsage <= 0 || maxMarginUsage > 1 {
		maxMarginUsage = 1
	}

	for i := range signals {
		if signals[i].Action != "open_long" && signals[i].Action != "open_short" {
			continue
		}
		size, ok := riskBasedPositionSize(signals[i], account, equity, riskPct, minSize, maxMarginUsage, sizing)
		if !ok {
			signals[i].PositionSizeUSD = 0
			continue
		}
		signals[i].PositionSizeUSD = size
	}
}

func riskBasedPositionSize(signal CandidateSignal, account AccountInfo, equity, riskPct, minSize, maxMarginUsage float64, sizing *PositionSizingConfig) (float64, bool) {
	entry := signal.EntryPrice
	stop := signal.StopLoss
	if entry <= 0 || stop <= 0 || riskPct <= 0 {
		return 0, false
	}
	stopDistanceRatio := math.Abs(entry-stop) / entry
	if stopDistanceRatio <= 0 {
		return 0, false
	}

	riskBudget := equity * riskPct / 100
	size := riskBudget / stopDistanceRatio

	if isBTCETHSignal(signal.Symbol) && minSize < defaultBTCETHMinNotionalUSD {
		minSize = defaultBTCETHMinNotionalUSD
	}
	if minSize > 0 && size < minSize {
		return 0, false
	}

	maxPositionValue := equity * maxPositionValueRatio(signal.Symbol, sizing)
	if signal.Leverage > 0 && account.AvailableBalance > 0 && maxMarginUsage > 0 {
		marginCap := account.AvailableBalance * maxMarginUsage * float64(signal.Leverage)
		if marginCap > 0 && (maxPositionValue <= 0 || marginCap < maxPositionValue) {
			maxPositionValue = marginCap
		}
	}
	if maxPositionValue > 0 && size > maxPositionValue {
		size = maxPositionValue
	}

	return math.Round(size*100) / 100, size > 0
}

func maxPositionValueRatio(symbol string, sizing *PositionSizingConfig) float64 {
	if isBTCETHSignal(symbol) {
		if sizing != nil && sizing.BTCETHMaxPositionValueRatio > 0 {
			return sizing.BTCETHMaxPositionValueRatio
		}
		return defaultBTCETHMaxPositionRate
	}
	if sizing != nil && sizing.AltcoinMaxPositionValueRatio > 0 {
		return sizing.AltcoinMaxPositionValueRatio
	}
	return defaultAltMaxPositionRate
}

func isBTCETHSignal(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT"
}
