package data

import "time"

const (
	DefaultAbnormalJumpPct = 0.20
	DefaultFundingInterval = 8 * time.Hour
)

// KlineRecord is the stable JSONL shape used by Phase 0 exports.
type KlineRecord struct {
	OpenTime            time.Time `json:"open_time"`
	OpenTimeUnixMs      int64     `json:"open_time_unix_ms"`
	Open                float64   `json:"open"`
	High                float64   `json:"high"`
	Low                 float64   `json:"low"`
	Close               float64   `json:"close"`
	Volume              float64   `json:"volume"`
	CloseTime           time.Time `json:"close_time"`
	CloseTimeUnixMs     int64     `json:"close_time_unix_ms"`
	QuoteVolume         float64   `json:"quote_volume,omitempty"`
	Trades              int       `json:"trades,omitempty"`
	TakerBuyBaseVolume  float64   `json:"taker_buy_base_volume,omitempty"`
	TakerBuyQuoteVolume float64   `json:"taker_buy_quote_volume,omitempty"`
}

// FeeSchedule records the static fee assumptions used by Phase 0 and later backtests.
type FeeSchedule struct {
	Exchange      string    `json:"exchange"`
	Symbol        string    `json:"symbol,omitempty"`
	MakerFeeRate  float64   `json:"maker_fee_rate"`
	TakerFeeRate  float64   `json:"taker_fee_rate"`
	Source        string    `json:"source"`
	EffectiveTime time.Time `json:"effective_time"`
}

// FundingRateRecord is the stable JSONL shape for funding rate exports.
type FundingRateRecord struct {
	Symbol            string    `json:"symbol"`
	FundingTime       time.Time `json:"funding_time"`
	FundingTimeUnixMs int64     `json:"funding_time_unix_ms"`
	FundingRate       float64   `json:"funding_rate"`
	MarkPrice         float64   `json:"mark_price,omitempty"`
}

type QualitySeverity string

const (
	SeverityInfo    QualitySeverity = "info"
	SeverityWarning QualitySeverity = "warning"
	SeverityError   QualitySeverity = "error"
)

// QualityIssue captures a single data-quality finding with enough context to debug the source rows.
type QualityIssue struct {
	Severity  QualitySeverity `json:"severity"`
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Symbol    string          `json:"symbol,omitempty"`
	Timeframe string          `json:"timeframe,omitempty"`
	Time      time.Time       `json:"time,omitempty"`
	Index     int             `json:"index,omitempty"`
}

type KlineQualityReport struct {
	Symbol             string         `json:"symbol"`
	Timeframe          string         `json:"timeframe"`
	ExpectedIntervalMs int64          `json:"expected_interval_ms"`
	BarCount           int            `json:"bar_count"`
	ExpectedBarCount   int            `json:"expected_bar_count,omitempty"`
	FirstOpenTime      time.Time      `json:"first_open_time,omitempty"`
	LastOpenTime       time.Time      `json:"last_open_time,omitempty"`
	MissingBars        int            `json:"missing_bars"`
	InvalidOHLCBars    int            `json:"invalid_ohlc_bars"`
	NonIncreasingBars  int            `json:"non_increasing_bars"`
	AbnormalJumps      int            `json:"abnormal_jumps"`
	Issues             []QualityIssue `json:"issues,omitempty"`
	OK                 bool           `json:"ok"`
}

type FundingQualityReport struct {
	Symbol            string         `json:"symbol"`
	RecordCount       int            `json:"record_count"`
	ExpectedInterval  string         `json:"expected_interval"`
	FirstFundingTime  time.Time      `json:"first_funding_time,omitempty"`
	LastFundingTime   time.Time      `json:"last_funding_time,omitempty"`
	CoverageStartTime time.Time      `json:"coverage_start_time"`
	CoverageEndTime   time.Time      `json:"coverage_end_time"`
	MissingIntervals  int            `json:"missing_intervals"`
	Issues            []QualityIssue `json:"issues,omitempty"`
	OK                bool           `json:"ok"`
}

type QualitySummary struct {
	KlineSeriesChecked   int `json:"kline_series_checked"`
	FundingSeriesChecked int `json:"funding_series_checked"`
	ErrorCount           int `json:"error_count"`
	WarningCount         int `json:"warning_count"`
	InfoCount            int `json:"info_count"`
}

type QualityReport struct {
	GeneratedAt   time.Time              `json:"generated_at"`
	Exchange      string                 `json:"exchange"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Klines        []KlineQualityReport   `json:"klines"`
	Funding       []FundingQualityReport `json:"funding"`
	FeeSchedules  []FeeSchedule          `json:"fee_schedules"`
	ExportedFiles map[string]string      `json:"exported_files,omitempty"`
	Summary       QualitySummary         `json:"summary"`
	OK            bool                   `json:"ok"`
}
