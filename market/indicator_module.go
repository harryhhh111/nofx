package market

import (
	"fmt"
	"time"
)

// ── Module Interface ──────────────────────────────────────────────────────

// Module is the interface that every indicator module must implement.
type Module interface {
	// Name returns the indicator name used as the key in FactorSnapshot.Technical.
	Name() string

	// Calculate computes indicator values for the given context and request.
	// Returns nil, nil when the indicator is not requested.
	Calculate(ctx CalcContext, req IndicatorRequest) ([]IndicatorPoint, error)
}

// ── CalcContext ───────────────────────────────────────────────────────────

// CalcContext holds the input data for a single indicator calculation on one timeframe.
type CalcContext struct {
	Klines     []Kline
	Timeframe  string
	AsOf       time.Time
	SourceTime time.Time
	Symbol     string
}

// ── MarketInput ───────────────────────────────────────────────────────────

// MarketInput is the raw market data passed into calculation engines.
type MarketInput struct {
	Symbol     string
	Timeframes map[string][]Kline
	AsOf       time.Time
}

// ── IndicatorRequest ──────────────────────────────────────────────────────

// IndicatorRequest describes which indicators to calculate and their parameters.
// Nil / zero-value fields mean "not requested".
type IndicatorRequest struct {
	EMAPeriods  []int      `json:"ema_periods,omitempty"`
	SMAPeriods  []int      `json:"sma_periods,omitempty"`
	RSIPeriods  []int      `json:"rsi_periods,omitempty"`
	ATRPeriods  []int      `json:"atr_periods,omitempty"`
	ADX         *ADXSpec   `json:"adx,omitempty"`
	SAR         *SARSpec   `json:"sar,omitempty"`
	BOLLPeriods []BOLLSpec `json:"boll_periods,omitempty"`
	MACD            *MACDSpec `json:"macd,omitempty"`
	VolumePeriods   []int     `json:"volume_periods,omitempty"`
	DonchianPeriods []int     `json:"donchian_periods,omitempty"`
}

type ADXSpec struct {
	Period int `json:"period"`
}

type SARSpec struct {
	Enabled bool `json:"enabled"`
}

type BOLLSpec struct {
	Period     int     `json:"period"`
	Multiplier float64 `json:"multiplier"`
}

type MACDSpec struct {
	Fast   int `json:"fast"`
	Slow   int `json:"slow"`
	Signal int `json:"signal"`
}

// ── Helpers ───────────────────────────────────────────────────────────────

// sanitizePeriods deduplicates and filters out non-positive periods.
func sanitizePeriods(periods []int) []int {
	seen := make(map[int]bool)
	out := make([]int, 0, len(periods))
	for _, p := range periods {
		if p > 0 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// requireBars returns an error if the kline count is insufficient.
func requireBars(symbol, name string, have, need int) error {
	if have < need {
		return fmt.Errorf("%s %s: need %d bars, have %d", symbol, name, need, have)
	}
	return nil
}

// klineSourceTime extracts the best available timestamp from a Kline.
func klineSourceTime(k Kline) time.Time {
	if k.CloseTime > 0 {
		return time.Unix(k.CloseTime/1000, 0).UTC()
	}
	return time.Unix(k.OpenTime/1000, 0).UTC()
}
