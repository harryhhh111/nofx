package market

import "time"

// KlineWindowSpec separates how much history the program may use for
// calculations from how much raw history is exposed to the AI prompt.
type KlineWindowSpec struct {
	ComputeLookback    int  `json:"compute_lookback"`
	PromptDisplayCount int  `json:"prompt_display_count"`
	IncludeOpenBar     bool `json:"include_open_bar"`
}

// IndicatorRequest describes the technical indicators the program should
// calculate from OHLCV data. It intentionally contains parameters, not values.
type IndicatorRequest struct {
	EMAPeriods         []int      `json:"ema_periods,omitempty"`
	RSIPeriods         []int      `json:"rsi_periods,omitempty"`
	ATRPeriods         []int      `json:"atr_periods,omitempty"`
	BOLLPeriods        []BOLLSpec `json:"boll_periods,omitempty"`
	MACD               *MACDSpec  `json:"macd,omitempty"`
	VWAPPeriods        []int      `json:"vwap_periods,omitempty"`
	VolumePeriods      []int      `json:"volume_periods,omitempty"`
	DonchianPeriods    []int      `json:"donchian_periods,omitempty"`
	RealizedVolPeriods []int      `json:"realized_vol_periods,omitempty"`
	PriceChangeWindows []int      `json:"price_change_windows,omitempty"`
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

// MarketInput is the raw market data passed into calculation engines.
// Fetching data is deliberately kept outside this layer.
type MarketInput struct {
	Symbol     string             `json:"symbol"`
	Timeframes map[string][]Kline `json:"timeframes"`
	AsOf       time.Time          `json:"as_of"`
	Window     KlineWindowSpec    `json:"window"`
}

// IndicatorPoint is a single calculated indicator value with enough metadata
// to keep prompt, replay, and live trading aligned.
type IndicatorPoint struct {
	Name        string             `json:"name"`
	Timeframe   string             `json:"timeframe"`
	Period      int                `json:"period,omitempty"`
	Params      map[string]float64 `json:"params,omitempty"`
	Value       float64            `json:"value"`
	SourceTime  time.Time          `json:"source_time,omitempty"`
	AvailableAt time.Time          `json:"available_at,omitempty"`
}

// StructureSnapshot is produced by the StructureEngine. In the first
// architecture pass the engine may return Valid=false until specific structure
// algorithms are implemented.
type StructureSnapshot struct {
	Name          string             `json:"name"`
	Timeframe     string             `json:"timeframe"`
	Valid         bool               `json:"valid"`
	Reason        string             `json:"reason,omitempty"`
	Direction     string             `json:"direction,omitempty"`
	AnchorLow     *StructureAnchor   `json:"anchor_low,omitempty"`
	AnchorHigh    *StructureAnchor   `json:"anchor_high,omitempty"`
	InvalidPrice  float64            `json:"invalid_price,omitempty"`
	KeyLevels     map[string]float64 `json:"key_levels,omitempty"`
	Confirmed     bool               `json:"confirmed"`
	SourceTime    time.Time          `json:"source_time,omitempty"`
	AvailableAt   time.Time          `json:"available_at,omitempty"`
	ParameterHash string             `json:"parameter_hash,omitempty"`
}

type StructureAnchor struct {
	Time  time.Time `json:"time"`
	Index int       `json:"index"`
	Price float64   `json:"price"`
	Kind  string    `json:"kind"`
}

// ExternalFactor is the normalized form of data that cannot be calculated from
// K-lines alone, such as OI, funding, rankings, orderbook, or netflow.
type ExternalFactor struct {
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Timeframe   string    `json:"timeframe,omitempty"`
	Value       float64   `json:"value,omitempty"`
	State       string    `json:"state,omitempty"`
	Score       float64   `json:"score,omitempty"`
	Available   bool      `json:"available"`
	SourceTime  time.Time `json:"source_time,omitempty"`
	AvailableAt time.Time `json:"available_at,omitempty"`
	CostClass   string    `json:"cost_class,omitempty"` // free, paid, unknown
}

// FactorSnapshot is the structured market context the AI should read instead
// of raw, long, and inconsistent prompt data.
type FactorSnapshot struct {
	Symbol     string                         `json:"symbol"`
	AsOf       time.Time                      `json:"as_of"`
	Technical  map[string][]IndicatorPoint    `json:"technical,omitempty"`
	Structures map[string][]StructureSnapshot `json:"structures,omitempty"`
	External   map[string]ExternalFactor      `json:"external,omitempty"`
	RiskFlags  []string                       `json:"risk_flags,omitempty"`
	Notes      []string                       `json:"notes,omitempty"`
}
