package store

import (
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

const bbMACDEffectiveMovePct = 0.3

// BBMACDSignal stores one observed BB MACD signal snapshot for later evaluation.
type BBMACDSignal struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID       string    `gorm:"column:trader_id;not null;index:idx_bbmacd_trader_time" json:"trader_id"`
	CycleNumber    int       `gorm:"column:cycle_number;not null;default:0" json:"cycle_number"`
	Symbol         string    `gorm:"column:symbol;not null;index:idx_bbmacd_symbol_time" json:"symbol"`
	Timeframe      string    `gorm:"column:timeframe;not null" json:"timeframe"`
	SignalTime     time.Time `gorm:"column:signal_time;not null;index:idx_bbmacd_trader_time,sort:desc;index:idx_bbmacd_symbol_time,sort:desc" json:"signal_time"`
	Price          float64   `gorm:"column:price;not null" json:"price"`
	Regime         string    `gorm:"column:regime;not null;default:''" json:"regime"`
	State          string    `gorm:"column:state;not null;default:'';index:idx_bbmacd_state" json:"state"`
	Strength       float64   `gorm:"column:strength;not null;default:0" json:"strength"`
	FastPeriod     int       `gorm:"column:fast_period;not null;default:0" json:"fast_period"`
	SlowPeriod     int       `gorm:"column:slow_period;not null;default:0" json:"slow_period"`
	SignalPeriod   int       `gorm:"column:signal_period;not null;default:0" json:"signal_period"`
	BOLLPeriod     int       `gorm:"column:boll_period;not null;default:0" json:"boll_period"`
	BOLLMultiplier float64   `gorm:"column:boll_multiplier;not null;default:0" json:"boll_multiplier"`
	MACD           float64   `gorm:"column:macd;not null;default:0" json:"macd"`
	MACDSignal     float64   `gorm:"column:macd_signal;not null;default:0" json:"macd_signal"`
	Histogram      float64   `gorm:"column:histogram;not null;default:0" json:"histogram"`
	UpperBand      float64   `gorm:"column:upper_band;not null;default:0" json:"upper_band"`
	MiddleBand     float64   `gorm:"column:middle_band;not null;default:0" json:"middle_band"`
	LowerBand      float64   `gorm:"column:lower_band;not null;default:0" json:"lower_band"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
}

type BBMACDAccuracyBucket struct {
	Total       int     `json:"total"`
	Resolved3   int     `json:"resolved_3"`
	Correct3    int     `json:"correct_3"`
	Accuracy3   float64 `json:"accuracy_3"`
	AvgReturn3  float64 `json:"avg_return_3"`
	Resolved5   int     `json:"resolved_5"`
	Correct5    int     `json:"correct_5"`
	Accuracy5   float64 `json:"accuracy_5"`
	AvgReturn5  float64 `json:"avg_return_5"`
	Resolved10  int     `json:"resolved_10"`
	Correct10   int     `json:"correct_10"`
	Accuracy10  float64 `json:"accuracy_10"`
	AvgReturn10 float64 `json:"avg_return_10"`
}

type BBMACDAccuracySummary struct {
	Resolved  int     `json:"resolved"`
	Correct   int     `json:"correct"`
	Accuracy  float64 `json:"accuracy"`
	AvgReturn float64 `json:"avg_return"`
}

type BBMACDStateStat struct {
	State string `json:"state"`
	BBMACDAccuracyBucket
}

type BBMACDTimeframeStat struct {
	Timeframe string                `json:"timeframe"`
	Signals   int                   `json:"signals"`
	Overall   BBMACDAccuracySummary `json:"overall"`
	Effective BBMACDAccuracySummary `json:"effective"`
}

type BBMACDAccuracyStats struct {
	TraderID              string                `json:"trader_id"`
	Days                  int                   `json:"days"`
	Total                 int                   `json:"total"`
	EffectiveThresholdPct float64               `json:"effective_threshold_pct"`
	Overall               BBMACDAccuracySummary `json:"overall"`
	Effective             BBMACDAccuracySummary `json:"effective"`
	BreakoutOverall       BBMACDAccuracySummary `json:"breakout_overall"`
	BreakoutEffective     BBMACDAccuracySummary `json:"breakout_effective"`
	Directional           BBMACDAccuracyBucket  `json:"directional"`
	Breakout              BBMACDAccuracyBucket  `json:"breakout"`
	ByState               []BBMACDStateStat     `json:"by_state"`
	ByTimeframe           []BBMACDTimeframeStat `json:"by_timeframe"`
}

func (BBMACDSignal) TableName() string {
	return "bb_macd_signals"
}

// BBMACDSignalStore stores BB MACD signal snapshots.
type BBMACDSignalStore struct {
	db *gorm.DB
}

func NewBBMACDSignalStore(db *gorm.DB) *BBMACDSignalStore {
	return &BBMACDSignalStore{db: db}
}

func (s *BBMACDSignalStore) InitTables() error {
	return s.db.AutoMigrate(&BBMACDSignal{})
}

func (s *BBMACDSignalStore) CreateMany(signals []*BBMACDSignal) error {
	if len(signals) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, signal := range signals {
		if signal.SignalTime.IsZero() {
			signal.SignalTime = now
		}
		signal.SignalTime = signal.SignalTime.UTC()
		if signal.CreatedAt.IsZero() {
			signal.CreatedAt = now
		}
	}
	if err := s.db.Create(&signals).Error; err != nil {
		return fmt.Errorf("failed to insert BB MACD signals: %w", err)
	}
	return nil
}

func (s *BBMACDSignalStore) DeleteByTrader(traderID string) error {
	if traderID == "" {
		return nil
	}
	if err := s.db.Where("trader_id = ?", traderID).Delete(&BBMACDSignal{}).Error; err != nil {
		return fmt.Errorf("failed to delete BB MACD signals: %w", err)
	}
	return nil
}

func (s *BBMACDSignalStore) AccuracyStats(traderID string, days int) (*BBMACDAccuracyStats, error) {
	var signals []BBMACDSignal
	query := s.db.Where("trader_id = ?", traderID)
	if days > 0 {
		since := time.Now().UTC().AddDate(0, 0, -days)
		query = query.Where("signal_time >= ?", since)
	}
	if err := query.Order("symbol ASC, timeframe ASC, signal_time ASC").Find(&signals).Error; err != nil {
		return nil, fmt.Errorf("failed to query BB MACD signals: %w", err)
	}

	stats := &BBMACDAccuracyStats{
		TraderID:              traderID,
		Days:                  days,
		Total:                 len(signals),
		EffectiveThresholdPct: bbMACDEffectiveMovePct,
	}
	byState := make(map[string]*BBMACDStateStat)
	byTimeframe := make(map[string]*BBMACDTimeframeStat)
	grouped := make(map[string][]BBMACDSignal)
	for _, signal := range signals {
		key := signal.Symbol + "|" + signal.Timeframe
		grouped[key] = append(grouped[key], signal)
		if _, ok := byState[signal.State]; !ok {
			byState[signal.State] = &BBMACDStateStat{State: signal.State}
		}
		byState[signal.State].Total++
	}

	for _, group := range grouped {
		for i, signal := range group {
			direction := bbmacdSignalDirection(signal.State)
			if direction != 0 {
				stats.Directional.Total++
			}
			if signal.State == "bullish_breakout" || signal.State == "bearish_breakout" {
				stats.Breakout.Total++
			}
			preferredHorizon := bbmacdValidationHorizon(signal.Timeframe)
			for _, horizon := range []int{3, 5, 10} {
				if i+horizon >= len(group) {
					continue
				}
				ret := (group[i+horizon].Price - signal.Price) / signal.Price * 100
				correct := direction != 0 && ret*float64(direction) > 0
				applyBBMACDResult(&byState[signal.State].BBMACDAccuracyBucket, horizon, correct, ret)
				if direction != 0 {
					applyBBMACDResult(&stats.Directional, horizon, correct, ret)
					applyBBMACDEffectiveResult(&stats.Effective, ret*float64(direction), bbMACDEffectiveMovePct)
				}
				if signal.State == "bullish_breakout" || signal.State == "bearish_breakout" {
					applyBBMACDResult(&stats.Breakout, horizon, correct, ret)
					if horizon == preferredHorizon {
						directionalReturn := ret * float64(direction)
						applyBBMACDSummaryResult(&stats.BreakoutOverall, directionalReturn, 0)
						applyBBMACDEffectiveResult(&stats.BreakoutEffective, directionalReturn, bbMACDEffectiveMovePct)

						tfStat := byTimeframe[signal.Timeframe]
						if tfStat == nil {
							tfStat = &BBMACDTimeframeStat{Timeframe: signal.Timeframe}
							byTimeframe[signal.Timeframe] = tfStat
						}
						tfStat.Signals++
						applyBBMACDSummaryResult(&tfStat.Overall, directionalReturn, 0)
						applyBBMACDEffectiveResult(&tfStat.Effective, directionalReturn, bbMACDEffectiveMovePct)
					}
				}
			}
		}
	}

	stats.ByState = make([]BBMACDStateStat, 0, len(byState))
	for _, stat := range byState {
		finalizeBBMACDBucket(&stat.BBMACDAccuracyBucket)
		stats.ByState = append(stats.ByState, *stat)
	}
	stats.Overall = summarizeBBMACDBucket(&stats.Directional)
	finalizeBBMACDSummary(&stats.Effective)
	finalizeBBMACDSummary(&stats.BreakoutOverall)
	finalizeBBMACDSummary(&stats.BreakoutEffective)
	finalizeBBMACDBucket(&stats.Directional)
	finalizeBBMACDBucket(&stats.Breakout)
	stats.ByTimeframe = make([]BBMACDTimeframeStat, 0, len(byTimeframe))
	for _, stat := range byTimeframe {
		finalizeBBMACDSummary(&stat.Overall)
		finalizeBBMACDSummary(&stat.Effective)
		stats.ByTimeframe = append(stats.ByTimeframe, *stat)
	}
	sort.Slice(stats.ByTimeframe, func(i, j int) bool {
		return bbmacdTimeframeRank(stats.ByTimeframe[i].Timeframe) < bbmacdTimeframeRank(stats.ByTimeframe[j].Timeframe)
	})
	return stats, nil
}

func bbmacdSignalDirection(state string) int {
	switch state {
	case "bullish_breakout", "bullish_momentum":
		return 1
	case "bearish_breakout", "bearish_momentum":
		return -1
	default:
		return 0
	}
}

func applyBBMACDResult(bucket *BBMACDAccuracyBucket, horizon int, correct bool, ret float64) {
	switch horizon {
	case 3:
		bucket.Resolved3++
		if correct {
			bucket.Correct3++
		}
		bucket.AvgReturn3 += ret
	case 5:
		bucket.Resolved5++
		if correct {
			bucket.Correct5++
		}
		bucket.AvgReturn5 += ret
	case 10:
		bucket.Resolved10++
		if correct {
			bucket.Correct10++
		}
		bucket.AvgReturn10 += ret
	}
}

func finalizeBBMACDBucket(bucket *BBMACDAccuracyBucket) {
	if bucket.Resolved3 > 0 {
		bucket.Accuracy3 = float64(bucket.Correct3) / float64(bucket.Resolved3) * 100
		bucket.AvgReturn3 = bucket.AvgReturn3 / float64(bucket.Resolved3)
	}
	if bucket.Resolved5 > 0 {
		bucket.Accuracy5 = float64(bucket.Correct5) / float64(bucket.Resolved5) * 100
		bucket.AvgReturn5 = bucket.AvgReturn5 / float64(bucket.Resolved5)
	}
	if bucket.Resolved10 > 0 {
		bucket.Accuracy10 = float64(bucket.Correct10) / float64(bucket.Resolved10) * 100
		bucket.AvgReturn10 = bucket.AvgReturn10 / float64(bucket.Resolved10)
	}
}

func summarizeBBMACDBucket(bucket *BBMACDAccuracyBucket) BBMACDAccuracySummary {
	resolved := bucket.Resolved3 + bucket.Resolved5 + bucket.Resolved10
	correct := bucket.Correct3 + bucket.Correct5 + bucket.Correct10
	totalReturn := bucket.AvgReturn3 + bucket.AvgReturn5 + bucket.AvgReturn10
	summary := BBMACDAccuracySummary{
		Resolved: resolved,
		Correct:  correct,
	}
	if resolved > 0 {
		summary.Accuracy = float64(correct) / float64(resolved) * 100
		summary.AvgReturn = totalReturn / float64(resolved)
	}
	return summary
}

func applyBBMACDEffectiveResult(summary *BBMACDAccuracySummary, directionalReturn, threshold float64) {
	if directionalReturn > threshold {
		summary.Resolved++
		summary.Correct++
		summary.AvgReturn += directionalReturn
		return
	}
	if directionalReturn < -threshold {
		summary.Resolved++
		summary.AvgReturn += directionalReturn
	}
}

func applyBBMACDSummaryResult(summary *BBMACDAccuracySummary, directionalReturn, threshold float64) {
	if directionalReturn > threshold {
		summary.Resolved++
		summary.Correct++
		summary.AvgReturn += directionalReturn
		return
	}
	if directionalReturn < -threshold || threshold == 0 {
		summary.Resolved++
		summary.AvgReturn += directionalReturn
	}
}

func finalizeBBMACDSummary(summary *BBMACDAccuracySummary) {
	if summary.Resolved == 0 {
		return
	}
	summary.Accuracy = float64(summary.Correct) / float64(summary.Resolved) * 100
	summary.AvgReturn = summary.AvgReturn / float64(summary.Resolved)
}

func bbmacdValidationHorizon(timeframe string) int {
	switch timeframe {
	case "1m", "3m", "5m":
		return 3
	case "15m", "30m":
		return 5
	default:
		return 10
	}
}

func bbmacdTimeframeRank(timeframe string) int {
	switch timeframe {
	case "1m":
		return 1
	case "3m":
		return 3
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "4h":
		return 240
	case "1d":
		return 1440
	default:
		return 100000
	}
}
