package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SignalCalibrationSample stores deterministic signal evidence for later
// replay, calibration, and strategy-version quality gates. It deliberately
// records structured snapshots instead of raw LLM prompt text.
type SignalCalibrationSample struct {
	ID                         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID                   string    `gorm:"column:trader_id;not null;index:idx_signal_calib_trader_time" json:"trader_id"`
	StrategyID                 string    `gorm:"column:strategy_id;default:'';index" json:"strategy_id,omitempty"`
	StrategyVersion            string    `gorm:"column:strategy_version;default:'';index" json:"strategy_version,omitempty"`
	DecisionID                 int64     `gorm:"column:decision_id;default:0;index" json:"decision_id,omitempty"`
	CycleNumber                int       `gorm:"column:cycle_number;default:0" json:"cycle_number,omitempty"`
	Symbol                     string    `gorm:"column:symbol;not null;index:idx_signal_calib_symbol_time" json:"symbol"`
	SampleKind                 string    `gorm:"column:sample_kind;not null;default:'setup';index" json:"sample_kind"`
	SignalID                   string    `gorm:"column:signal_id;default:'';index" json:"signal_id,omitempty"`
	RuleID                     string    `gorm:"column:rule_id;default:'';index" json:"rule_id,omitempty"`
	Setup                      string    `gorm:"column:setup;default:'';index" json:"setup,omitempty"`
	Action                     string    `gorm:"column:action;default:'';index" json:"action,omitempty"`
	Eligible                   bool      `gorm:"column:eligible;default:false;index" json:"eligible"`
	Timeframe                  string    `gorm:"column:timeframe;default:''" json:"timeframe,omitempty"`
	PrimaryTimeframe           string    `gorm:"column:primary_timeframe;default:''" json:"primary_timeframe,omitempty"`
	EntryTimeframe             string    `gorm:"column:entry_timeframe;default:''" json:"entry_timeframe,omitempty"`
	ConfirmationTimeframesJSON string    `gorm:"column:confirmation_timeframes_json;default:'[]'" json:"confirmation_timeframes_json,omitempty"`
	EntryPrice                 float64   `gorm:"column:entry_price;default:0" json:"entry_price,omitempty"`
	Confidence                 int       `gorm:"column:confidence;default:0" json:"confidence,omitempty"`
	Score                      float64   `gorm:"column:score;default:0" json:"score,omitempty"`
	PrimaryScore               float64   `gorm:"column:primary_score;default:0" json:"primary_score,omitempty"`
	EntryScore                 float64   `gorm:"column:entry_score;default:0" json:"entry_score,omitempty"`
	ReviewStatus               string    `gorm:"column:review_status;default:'';index" json:"review_status,omitempty"`
	ReviewReasonsJSON          string    `gorm:"column:review_reasons_json;default:'[]'" json:"review_reasons_json,omitempty"`
	RiskStatus                 string    `gorm:"column:risk_status;default:'';index" json:"risk_status,omitempty"`
	RiskReason                 string    `gorm:"column:risk_reason;default:''" json:"risk_reason,omitempty"`
	FactorSnapshotJSON         string    `gorm:"column:factor_snapshot_json;type:text" json:"factor_snapshot_json,omitempty"`
	SetupTraceJSON             string    `gorm:"column:setup_trace_json;type:text" json:"setup_trace_json,omitempty"`
	ScoringTraceJSON           string    `gorm:"column:scoring_trace_json;type:text" json:"scoring_trace_json,omitempty"`
	SignalJSON                 string    `gorm:"column:signal_json;type:text" json:"signal_json,omitempty"`
	MarketContextJSON          string    `gorm:"column:market_context_json;type:text" json:"market_context_json,omitempty"`
	AsOf                       time.Time `gorm:"column:as_of;not null;index:idx_signal_calib_trader_time,sort:desc;index:idx_signal_calib_symbol_time,sort:desc" json:"as_of"`
	CreatedAt                  time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (SignalCalibrationSample) TableName() string { return "signal_calibration_samples" }

type SignalCalibrationStore struct {
	db *gorm.DB
}

type SignalCalibrationReport struct {
	StrategyID          string                           `json:"strategy_id"`
	StrategyVersion     string                           `json:"strategy_version,omitempty"`
	SampleCount         int                              `json:"sample_count"`
	SignalCount         int                              `json:"signal_count"`
	SetupCount          int                              `json:"setup_count"`
	EligibleCount       int                              `json:"eligible_count"`
	ApprovedCount       int                              `json:"approved_count"`
	RiskRejectedCount   int                              `json:"risk_rejected_count"`
	ReviewRejectedCount int                              `json:"review_rejected_count"`
	NoSignalCount       int                              `json:"no_signal_count"`
	ClosedTradeCount    int                              `json:"closed_trade_count"`
	WinningTradeCount   int                              `json:"winning_trade_count"`
	LosingTradeCount    int                              `json:"losing_trade_count"`
	WinRate             float64                          `json:"win_rate"`
	TotalPnL            float64                          `json:"total_pnl"`
	AveragePnL          float64                          `json:"average_pnl"`
	MinRequiredSamples  int                              `json:"min_required_samples"`
	MinRequiredOutcomes int                              `json:"min_required_outcomes"`
	EnoughOutcomes      bool                             `json:"enough_outcomes"`
	EnoughSamples       bool                             `json:"enough_samples"`
	QualityGate         string                           `json:"quality_gate"`
	Recommendation      string                           `json:"recommendation"`
	RiskStatusCounts    map[string]int                   `json:"risk_status_counts"`
	ReviewStatusCounts  map[string]int                   `json:"review_status_counts"`
	SetupStats          []SignalCalibrationSetupStat     `json:"setup_stats"`
	TimeframeStats      []SignalCalibrationTimeframeStat `json:"timeframe_stats"`
	LatestSampleAt      *time.Time                       `json:"latest_sample_at,omitempty"`
	GeneratedAt         time.Time                        `json:"generated_at"`
}

type SignalCalibrationSetupStat struct {
	Setup          string  `json:"setup"`
	Samples        int     `json:"samples"`
	Eligible       int     `json:"eligible"`
	Approved       int     `json:"approved"`
	RiskRejected   int     `json:"risk_rejected"`
	ReviewRejected int     `json:"review_rejected"`
	NoSignal       int     `json:"no_signal"`
	ClosedTrades   int     `json:"closed_trades"`
	Wins           int     `json:"wins"`
	Losses         int     `json:"losses"`
	WinRate        float64 `json:"win_rate"`
	TotalPnL       float64 `json:"total_pnl"`
	AveragePnL     float64 `json:"average_pnl"`
}

type SignalCalibrationTimeframeStat struct {
	PrimaryTimeframe string `json:"primary_timeframe"`
	EntryTimeframe   string `json:"entry_timeframe"`
	Samples          int    `json:"samples"`
	Approved         int    `json:"approved"`
}

func NewSignalCalibrationStore(db *gorm.DB) *SignalCalibrationStore {
	return &SignalCalibrationStore{db: db}
}

func (s *SignalCalibrationStore) initTables() error {
	if s.db.Dialector.Name() == "postgres" {
		var tableExists int64
		s.db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'signal_calibration_samples'`).Scan(&tableExists)
		if tableExists > 0 {
			return nil
		}
	}
	return s.db.AutoMigrate(&SignalCalibrationSample{})
}

func (s *SignalCalibrationStore) CreateMany(samples []*SignalCalibrationSample) error {
	if s == nil || s.db == nil || len(samples) == 0 {
		return nil
	}
	now := time.Now().UTC()
	valid := make([]*SignalCalibrationSample, 0, len(samples))
	for _, sample := range samples {
		if sample == nil {
			continue
		}
		sample.TraderID = strings.TrimSpace(sample.TraderID)
		sample.StrategyID = strings.TrimSpace(sample.StrategyID)
		sample.StrategyVersion = strings.TrimSpace(sample.StrategyVersion)
		sample.Symbol = strings.TrimSpace(strings.ToUpper(sample.Symbol))
		sample.SampleKind = strings.TrimSpace(sample.SampleKind)
		if sample.SampleKind == "" {
			sample.SampleKind = "setup"
		}
		if sample.TraderID == "" || sample.Symbol == "" {
			return fmt.Errorf("signal calibration sample requires trader_id and symbol")
		}
		if sample.ConfirmationTimeframesJSON == "" {
			sample.ConfirmationTimeframesJSON = "[]"
		}
		if sample.ReviewReasonsJSON == "" {
			sample.ReviewReasonsJSON = "[]"
		}
		if sample.AsOf.IsZero() {
			sample.AsOf = now
		} else {
			sample.AsOf = sample.AsOf.UTC()
		}
		valid = append(valid, sample)
	}
	if len(valid) == 0 {
		return nil
	}
	return s.db.Create(&valid).Error
}

func (s *SignalCalibrationStore) BuildReport(strategyID string, limit int) (*SignalCalibrationReport, error) {
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	var samples []SignalCalibrationSample
	err := s.db.Where("strategy_id = ?", strategyID).
		Order("as_of DESC").
		Limit(limit).
		Find(&samples).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query signal calibration samples: %w", err)
	}

	report := &SignalCalibrationReport{
		StrategyID:          strategyID,
		MinRequiredSamples:  100,
		MinRequiredOutcomes: 30,
		RiskStatusCounts:    map[string]int{},
		ReviewStatusCounts:  map[string]int{},
		GeneratedAt:         time.Now().UTC(),
	}
	setupStats := map[string]*SignalCalibrationSetupStat{}
	timeframeStats := map[string]*SignalCalibrationTimeframeStat{}
	latest := time.Time{}
	versionCounts := map[string]int{}
	for _, sample := range samples {
		report.SampleCount++
		if sample.SampleKind == "signal" {
			report.SignalCount++
		} else {
			report.SetupCount++
		}
		if sample.Eligible {
			report.EligibleCount++
		}
		if sample.StrategyVersion != "" {
			versionCounts[sample.StrategyVersion]++
		}
		if !sample.AsOf.IsZero() && (latest.IsZero() || sample.AsOf.After(latest)) {
			latest = sample.AsOf.UTC()
		}

		riskStatus := strings.TrimSpace(sample.RiskStatus)
		if riskStatus == "" {
			riskStatus = "unknown"
		}
		reviewStatus := strings.TrimSpace(sample.ReviewStatus)
		if reviewStatus == "" {
			reviewStatus = "none"
		}
		report.RiskStatusCounts[riskStatus]++
		report.ReviewStatusCounts[reviewStatus]++
		switch riskStatus {
		case "approved":
			report.ApprovedCount++
		case "risk_rejected":
			report.RiskRejectedCount++
		case "no_signal":
			report.NoSignalCount++
		}
		if reviewStatus == "reject" {
			report.ReviewRejectedCount++
		}

		setupName := strings.TrimSpace(sample.Setup)
		if setupName == "" {
			setupName = "unclassified"
		}
		stat := setupStats[setupName]
		if stat == nil {
			stat = &SignalCalibrationSetupStat{Setup: setupName}
			setupStats[setupName] = stat
		}
		stat.Samples++
		if sample.Eligible {
			stat.Eligible++
		}
		switch riskStatus {
		case "approved":
			stat.Approved++
		case "risk_rejected":
			stat.RiskRejected++
		case "no_signal":
			stat.NoSignal++
		}
		if reviewStatus == "reject" {
			stat.ReviewRejected++
		}

		tfKey := sample.PrimaryTimeframe + "|" + sample.EntryTimeframe
		tfStat := timeframeStats[tfKey]
		if tfStat == nil {
			tfStat = &SignalCalibrationTimeframeStat{
				PrimaryTimeframe: sample.PrimaryTimeframe,
				EntryTimeframe:   sample.EntryTimeframe,
			}
			timeframeStats[tfKey] = tfStat
		}
		tfStat.Samples++
		if riskStatus == "approved" {
			tfStat.Approved++
		}
	}
	if !latest.IsZero() {
		report.LatestSampleAt = &latest
	}
	report.StrategyVersion = mostCommonString(versionCounts)
	report.EnoughSamples = report.SampleCount >= report.MinRequiredSamples
	if err := s.applyClosedPositionOutcomes(report, setupStats, strategyID, limit); err != nil {
		return nil, err
	}
	report.QualityGate, report.Recommendation = calibrationGate(report)

	for _, stat := range setupStats {
		report.SetupStats = append(report.SetupStats, *stat)
	}
	sort.Slice(report.SetupStats, func(i, j int) bool {
		left := report.SetupStats[i].Samples + report.SetupStats[i].ClosedTrades
		right := report.SetupStats[j].Samples + report.SetupStats[j].ClosedTrades
		return left > right
	})
	for _, stat := range timeframeStats {
		report.TimeframeStats = append(report.TimeframeStats, *stat)
	}
	sort.Slice(report.TimeframeStats, func(i, j int) bool {
		return report.TimeframeStats[i].Samples > report.TimeframeStats[j].Samples
	})
	return report, nil
}

func (s *SignalCalibrationStore) RecentSamples(strategyID string, limit int) ([]SignalCalibrationSample, error) {
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var samples []SignalCalibrationSample
	err := s.db.Where("strategy_id = ?", strategyID).
		Order("as_of DESC").
		Limit(limit).
		Find(&samples).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query recent calibration samples: %w", err)
	}
	return samples, nil
}

func (s *SignalCalibrationStore) RecentClosedPositions(strategyID string, limit int) ([]TraderPosition, error) {
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var positions []TraderPosition
	err := s.db.Where("strategy_id = ? AND status = ?", strategyID, "CLOSED").
		Order("exit_time DESC").
		Limit(limit).
		Find(&positions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query recent closed positions: %w", err)
	}
	return positions, nil
}

func (s *SignalCalibrationStore) applyClosedPositionOutcomes(report *SignalCalibrationReport, setupStats map[string]*SignalCalibrationSetupStat, strategyID string, limit int) error {
	var positions []TraderPosition
	err := s.db.Where("strategy_id = ? AND status = ?", strategyID, "CLOSED").
		Order("exit_time DESC").
		Limit(limit).
		Find(&positions).Error
	if err != nil {
		return fmt.Errorf("failed to query calibration closed positions: %w", err)
	}
	for _, pos := range positions {
		report.ClosedTradeCount++
		report.TotalPnL += pos.RealizedPnL
		if pos.RealizedPnL > 0 {
			report.WinningTradeCount++
		} else if pos.RealizedPnL < 0 {
			report.LosingTradeCount++
		}
		setupName := strings.TrimSpace(pos.OpeningSetup)
		if setupName == "" {
			setupName = strings.TrimSpace(pos.OpeningRuleID)
		}
		if setupName == "" {
			setupName = "unclassified"
		}
		stat := setupStats[setupName]
		if stat == nil {
			stat = &SignalCalibrationSetupStat{Setup: setupName}
			setupStats[setupName] = stat
		}
		stat.ClosedTrades++
		stat.TotalPnL += pos.RealizedPnL
		if pos.RealizedPnL > 0 {
			stat.Wins++
		} else if pos.RealizedPnL < 0 {
			stat.Losses++
		}
	}
	if report.ClosedTradeCount > 0 {
		report.WinRate = float64(report.WinningTradeCount) / float64(report.ClosedTradeCount)
		report.AveragePnL = report.TotalPnL / float64(report.ClosedTradeCount)
	}
	for _, stat := range setupStats {
		if stat.ClosedTrades > 0 {
			stat.WinRate = float64(stat.Wins) / float64(stat.ClosedTrades)
			stat.AveragePnL = stat.TotalPnL / float64(stat.ClosedTrades)
		}
	}
	report.EnoughOutcomes = report.ClosedTradeCount >= report.MinRequiredOutcomes
	return nil
}

func calibrationGate(report *SignalCalibrationReport) (string, string) {
	if report == nil || report.SampleCount == 0 {
		return "no_data", "No calibration samples yet. Run the strategy in paper mode before calibration."
	}
	if !report.EnoughSamples {
		return "collecting", "Calibration samples are still insufficient. Keep collecting deterministic setup/signal evidence before changing parameters."
	}
	if report.SignalCount == 0 || report.ApprovedCount == 0 {
		return "blocked", "Samples exist, but no approved candidate signals were observed. Review setup thresholds and data availability before paper validation."
	}
	if report.ClosedTradeCount == 0 {
		return "paper_collecting", "Candidate signal coverage is sufficient, but no closed paper trades are linked to this strategy yet. Keep paper mode running until outcomes are available."
	}
	if !report.EnoughOutcomes {
		return "outcome_collecting", "Closed trade outcomes are linked, but still below the calibration threshold. Review manually; do not auto-evolve parameters yet."
	}
	if report.WinRate < 0.4 || report.TotalPnL < 0 {
		return "needs_review", "Outcome sample size is sufficient but performance is weak. Use manual strategy review before any live deployment."
	}
	return "paper_ready", "Sample coverage is sufficient for the first paper-mode validation report. Do not enable live trading until linked paper outcomes and calibration evidence are reviewed."
}

func mostCommonString(counts map[string]int) string {
	best := ""
	bestCount := 0
	for value, count := range counts {
		if count > bestCount {
			best = value
			bestCount = count
		}
	}
	return best
}

func MarshalCalibrationJSON(value interface{}) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
