package market

import (
	"context"
	"fmt"
	"time"
)

// BuildFactorSnapshotFromData adapts the existing market.Data shape into the
// new structured factor protocol consumed by signal and LLM trading engines.
func BuildFactorSnapshotFromData(data *Data, asOf time.Time) *FactorSnapshot {
	snapshot, err := BuildFactorSnapshotFromDataWithRequests(data, asOf, DefaultIndicatorRequest(), StructureRequest{})
	if err != nil {
		return nil
	}
	return snapshot
}

func BuildFactorSnapshotFromDataWithRequest(data *Data, asOf time.Time, req IndicatorRequest) (*FactorSnapshot, error) {
	return BuildFactorSnapshotFromDataWithRequests(data, asOf, req, StructureRequest{})
}

func BuildFactorSnapshotFromDataWithRequests(data *Data, asOf time.Time, req IndicatorRequest, structureReq StructureRequest) (*FactorSnapshot, error) {
	if data == nil {
		return nil, fmt.Errorf("market data is nil")
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}

	input := MarketInput{
		Symbol:     data.Symbol,
		Timeframes: marketInputTimeframes(data),
		AsOf:       asOf,
	}
	if len(input.Timeframes) == 0 {
		return nil, fmt.Errorf("%s has no timeframe data for indicator calculation", data.Symbol)
	}
	s, err := NewDefaultIndicatorEngine().Calculate(context.Background(), input, req)
	if err != nil {
		return nil, err
	}
	if s.Structures == nil {
		s.Structures = map[string][]StructureSnapshot{}
	}
	structures, err := NewDefaultStructureEngine().Calculate(context.Background(), input, structureReq)
	if err != nil {
		return nil, err
	}
	for _, structure := range structures {
		s.Structures[structure.Name] = append(s.Structures[structure.Name], structure)
	}
	if s.External == nil {
		s.External = map[string]ExternalFactor{}
	}
	s.addLatest("price", "", 0, data.CurrentPrice, nil, asOf)

	if data.OpenInterest != nil {
		s.External["open_interest"] = ExternalFactor{
			Name:        "open_interest",
			Source:      "binance_futures",
			Value:       data.OpenInterest.Latest,
			Available:   data.OpenInterest.Latest > 0,
			SourceTime:  nonZeroTime(data.OpenInterest.Time, asOf),
			AvailableAt: asOf,
			CostClass:   "free",
		}
	}
	s.External["funding_rate"] = ExternalFactor{
		Name:        "funding_rate",
		Source:      "binance_futures",
		Value:       data.FundingRate,
		State:       fundingAvailabilityState(data.FundingRate, data.FundingAvailable),
		Available:   data.FundingAvailable,
		SourceTime:  nonZeroTime(data.FundingRateTime, asOf),
		AvailableAt: asOf,
		CostClass:   "free",
	}

	return s, nil
}

func DefaultIndicatorRequest() IndicatorRequest {
	return IndicatorRequest{
		EMAPeriods:         []int{20, 50},
		SMAPeriods:         []int{5, 20, 50},
		RSIPeriods:         []int{7, 14},
		ATRPeriods:         []int{14},
		ADX:                &ADXSpec{Period: 14},
		SAR:                &SARSpec{Enabled: false},
		BOLLPeriods:        []BOLLSpec{{Period: 20, Multiplier: 2}},
		MACD:               &MACDSpec{Fast: 12, Slow: 26, Signal: 9},
		VWAPPeriods:        []int{20},
		VolumePeriods:      []int{20},
		DonchianPeriods:    []int{20},
		RealizedVolPeriods: []int{20, 60},
		PriceChangeWindows: []int{12, 48},
		Sessions:           []SessionSpec{{Timezone: "UTC", Offset: "00:00", Duration: 1440}},
	}
}

func marketInputTimeframes(data *Data) map[string][]Kline {
	out := map[string][]Kline{}
	for tf, tfData := range data.TimeframeData {
		if tfData == nil {
			continue
		}
		if len(tfData.ComputeBars) > 0 {
			out[tf] = tfData.ComputeBars
			continue
		}
		if len(tfData.Klines) > 0 {
			out[tf] = klineBarsToKlines(tfData.Klines)
		}
	}
	return out
}

func klineBarsToKlines(bars []KlineBar) []Kline {
	out := make([]Kline, 0, len(bars))
	for _, bar := range bars {
		out = append(out, Kline{
			OpenTime:  bar.Time,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
			CloseTime: bar.Time,
		})
	}
	return out
}

func (s *FactorSnapshot) addLatest(name, timeframe string, period int, value float64, params map[string]float64, sourceTime time.Time) {
	if s.Technical == nil {
		s.Technical = map[string][]IndicatorPoint{}
	}
	s.Technical[name] = append(s.Technical[name], IndicatorPoint{
		Name:        name,
		Timeframe:   timeframe,
		Period:      period,
		Params:      params,
		Value:       value,
		SourceTime:  sourceTime,
		AvailableAt: s.AsOf,
	})
}

func latestKlineBarTime(klines []KlineBar) time.Time {
	if len(klines) == 0 {
		return time.Time{}
	}
	return time.UnixMilli(klines[len(klines)-1].Time).UTC()
}

func fundingState(rate float64) string {
	switch {
	case rate >= 0.0005:
		return "overheated_positive"
	case rate <= -0.0005:
		return "overheated_negative"
	case rate > 0:
		return "positive"
	case rate < 0:
		return "negative"
	default:
		return "neutral"
	}
}

func fundingAvailabilityState(rate float64, available bool) string {
	if !available {
		return "unavailable"
	}
	return fundingState(rate)
}

func nonZeroTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func (s *FactorSnapshot) IndicatorValue(name, timeframe string, period int) (float64, bool) {
	if s == nil || s.Technical == nil {
		return 0, false
	}
	points := s.Technical[name]
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		if p.Timeframe == timeframe && p.Period == period {
			return p.Value, true
		}
	}
	return 0, false
}

func (s *FactorSnapshot) OperandValue(op interface{}) (float64, bool) {
	return 0, false
}

func MissingFactorReason(name string) string {
	return fmt.Sprintf("factor_unavailable:%s", name)
}
