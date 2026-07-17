package store

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultSetupEpisodeHorizonBars = 12

// SetupEpisode is one independent, contiguous market opportunity. Repeated
// scans of the same closed candle update this row instead of creating new
// training samples. Outcome tracking is independent of order execution so
// rejected and skipped opportunities remain useful counterfactual evidence.
type SetupEpisode struct {
	ID               string `gorm:"primaryKey;size:36" json:"id"`
	TraderID         string `gorm:"column:trader_id;not null;index:idx_setup_episode_owner" json:"trader_id"`
	StrategyID       string `gorm:"column:strategy_id;not null;default:'';index:idx_setup_episode_strategy" json:"strategy_id"`
	StrategyVersion  string `gorm:"column:strategy_version;not null;default:'';index:idx_setup_episode_strategy" json:"strategy_version"`
	Symbol           string `gorm:"column:symbol;not null;index:idx_setup_episode_symbol" json:"symbol"`
	Setup            string `gorm:"column:setup;not null;index" json:"setup"`
	Family           string `gorm:"column:family;not null;default:'';index" json:"family"`
	Action           string `gorm:"column:action;not null;default:'';index" json:"action"`
	Regime           string `gorm:"column:regime;not null;default:'';index" json:"regime"`
	PrimaryTimeframe string `gorm:"column:primary_timeframe;not null;default:''" json:"primary_timeframe"`
	EntryTimeframe   string `gorm:"column:entry_timeframe;not null;default:''" json:"entry_timeframe"`
	MarketDataSource string `gorm:"column:market_data_source;not null;default:'default';index" json:"market_data_source"`

	State          string `gorm:"column:state;not null;default:'observed';index" json:"state"`
	EndReason      string `gorm:"column:end_reason;not null;default:''" json:"end_reason,omitempty"`
	EvidenceStatus string `gorm:"column:evidence_status;not null;default:'';index" json:"evidence_status,omitempty"`
	EverEligible   bool   `gorm:"column:ever_eligible;not null;default:false;index" json:"ever_eligible"`
	EverApproved   bool   `gorm:"column:ever_approved;not null;default:false;index" json:"ever_approved"`
	Executed       bool   `gorm:"column:executed;not null;default:false;index" json:"executed"`
	SignalID       string `gorm:"column:signal_id;not null;default:'';index" json:"signal_id,omitempty"`
	DecisionID     int64  `gorm:"column:decision_id;not null;default:0;index" json:"decision_id,omitempty"`

	EntryPrice       float64 `gorm:"column:entry_price;not null;default:0" json:"entry_price"`
	StructuralStop   float64 `gorm:"column:structural_stop;not null;default:0" json:"structural_stop,omitempty"`
	ExecutionStop    float64 `gorm:"column:execution_stop;not null;default:0" json:"execution_stop,omitempty"`
	TakeProfit       float64 `gorm:"column:take_profit;not null;default:0" json:"take_profit,omitempty"`
	StructuralRiskRR float64 `gorm:"column:structural_risk_reward;not null;default:0" json:"structural_risk_reward,omitempty"`
	ExecutionRiskRR  float64 `gorm:"column:execution_risk_reward;not null;default:0" json:"execution_risk_reward,omitempty"`

	ObservationCount   int   `gorm:"column:observation_count;not null;default:1" json:"observation_count"`
	UniqueBarCount     int   `gorm:"column:unique_bar_count;not null;default:1" json:"unique_bar_count"`
	LastBarTime        int64 `gorm:"column:last_bar_time;not null;default:0" json:"last_bar_time,omitempty"`
	ForwardBars        int   `gorm:"column:forward_bars;not null;default:0" json:"forward_bars"`
	LastOutcomeBarTime int64 `gorm:"column:last_outcome_bar_time;not null;default:0" json:"last_outcome_bar_time,omitempty"`
	HorizonBars        int   `gorm:"column:horizon_bars;not null;default:12" json:"horizon_bars"`

	MaxFavorablePct  float64 `gorm:"column:max_favorable_pct;not null;default:0" json:"max_favorable_pct"`
	MaxAdversePct    float64 `gorm:"column:max_adverse_pct;not null;default:0" json:"max_adverse_pct"`
	MaxFavorableR    float64 `gorm:"column:max_favorable_r;not null;default:0" json:"max_favorable_r"`
	MaxAdverseR      float64 `gorm:"column:max_adverse_r;not null;default:0" json:"max_adverse_r"`
	ForwardReturnPct float64 `gorm:"column:forward_return_pct;not null;default:0" json:"forward_return_pct"`

	OutcomeStatus string     `gorm:"column:outcome_status;not null;default:'pending';index" json:"outcome_status"`
	OutcomeLabel  string     `gorm:"column:outcome_label;not null;default:'';index" json:"outcome_label,omitempty"`
	OutcomePrice  float64    `gorm:"column:outcome_price;not null;default:0" json:"outcome_price,omitempty"`
	OutcomeR      float64    `gorm:"column:outcome_r;not null;default:0" json:"outcome_r,omitempty"`
	OutcomeRValid bool       `gorm:"column:outcome_r_valid;not null;default:false" json:"outcome_r_valid"`
	OutcomeAt     *time.Time `gorm:"column:outcome_at" json:"outcome_at,omitempty"`

	FirstSetupTraceJSON     string `gorm:"column:first_setup_trace_json;type:text" json:"first_setup_trace_json,omitempty"`
	LastSetupTraceJSON      string `gorm:"column:last_setup_trace_json;type:text" json:"last_setup_trace_json,omitempty"`
	FirstFactorSnapshotJSON string `gorm:"column:first_factor_snapshot_json;type:text" json:"first_factor_snapshot_json,omitempty"`
	LastFactorSnapshotJSON  string `gorm:"column:last_factor_snapshot_json;type:text" json:"last_factor_snapshot_json,omitempty"`
	MarketContextJSON       string `gorm:"column:market_context_json;type:text" json:"market_context_json,omitempty"`

	StartedAt  time.Time  `gorm:"column:started_at;not null;index:idx_setup_episode_strategy,sort:desc" json:"started_at"`
	LastSeenAt time.Time  `gorm:"column:last_seen_at;not null" json:"last_seen_at"`
	EndedAt    *time.Time `gorm:"column:ended_at" json:"ended_at,omitempty"`
	CreatedAt  time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SetupEpisode) TableName() string { return "setup_episodes" }

type SetupEpisodeStat struct {
	Regime      string  `json:"regime"`
	Setup       string  `json:"setup"`
	Action      string  `json:"action"`
	Episodes    int     `json:"episodes"`
	Eligible    int     `json:"eligible"`
	Approved    int     `json:"approved"`
	Executed    int     `json:"executed"`
	Labeled     int     `json:"labeled"`
	RCount      int     `json:"r_count"`
	Positive    int     `json:"positive"`
	Negative    int     `json:"negative"`
	Flat        int     `json:"flat"`
	HitRate     float64 `json:"hit_rate"`
	AverageR    float64 `json:"average_r"`
	AverageMFER float64 `json:"average_mfe_r"`
	AverageMAER float64 `json:"average_mae_r"`
}

// SetupFactorStat measures whether a factor's directional score agreed with
// later setup outcomes. It is offline calibration evidence, not an online
// trading rule.
type SetupFactorStat struct {
	Regime                  string  `json:"regime"`
	Setup                   string  `json:"setup"`
	Action                  string  `json:"action"`
	Factor                  string  `json:"factor"`
	Labeled                 int     `json:"labeled"`
	Supporting              int     `json:"supporting"`
	Conflicting             int     `json:"conflicting"`
	Neutral                 int     `json:"neutral"`
	PositiveWhenSupporting  int     `json:"positive_when_supporting"`
	NegativeWhenSupporting  int     `json:"negative_when_supporting"`
	PositiveWhenConflicting int     `json:"positive_when_conflicting"`
	NegativeWhenConflicting int     `json:"negative_when_conflicting"`
	SupportHitRate          float64 `json:"support_hit_rate"`
	ConflictHitRate         float64 `json:"conflict_hit_rate"`
	AverageDirectionalScore float64 `json:"average_directional_score"`
	PredictiveAlignment     float64 `json:"predictive_alignment"`
}

type setupEpisodeTrace struct {
	DetectedSetup  string `json:"detected_setup"`
	DetectedAction string `json:"detected_action"`
	Setup          string `json:"setup"`
	Action         string `json:"action"`
	Route          struct {
		Regime string `json:"regime"`
		Family string `json:"family"`
	} `json:"route"`
	EvidenceDecision struct {
		Status string `json:"status"`
	} `json:"evidence_decision"`
	Primary struct {
		Components map[string]float64 `json:"components"`
	} `json:"primary"`
	Entry struct {
		Components map[string]float64 `json:"components"`
	} `json:"entry"`
}

type setupEpisodeSignal struct {
	EntryPrice float64                    `json:"entry_price"`
	StopLoss   float64                    `json:"stop_loss"`
	TakeProfit float64                    `json:"take_profit"`
	Evidence   map[string]json.RawMessage `json:"evidence"`
}

type setupEpisodeProtectiveLevels struct {
	StopAnchor          float64 `json:"stop_anchor"`
	RiskReward          float64 `json:"risk_reward"`
	ExecutionRiskReward float64 `json:"execution_risk_reward"`
}

type setupEpisodeKline struct {
	OpenTime  int64   `json:"openTime"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	CloseTime int64   `json:"closeTime"`
}

type setupEpisodeObservation struct {
	Setup          string
	Action         string
	Family         string
	Regime         string
	EvidenceStatus string
	Price          float64
	High           float64
	Low            float64
	BarTime        int64
	Signal         setupEpisodeSignal
}

func (s *SignalCalibrationStore) observeSetupEpisodes(tx *gorm.DB, samples []*SignalCalibrationSample) error {
	seen := map[string]bool{}
	for _, sample := range samples {
		if sample == nil || sample.SampleKind != "setup" {
			continue
		}
		key := sample.TraderID + "|" + sample.StrategyID + "|" + sample.StrategyVersion + "|" + sample.Symbol
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := observeSetupEpisode(tx, sample); err != nil {
			return err
		}
	}
	return nil
}

func observeSetupEpisode(tx *gorm.DB, sample *SignalCalibrationSample) error {
	observation, err := parseSetupEpisodeObservation(sample)
	if err != nil {
		return err
	}
	observedAt := sample.AsOf.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	if observation.BarTime > 0 {
		if err := updatePendingSetupEpisodeOutcomes(tx, sample, observation, observedAt); err != nil {
			return err
		}
	}

	var active SetupEpisode
	activeErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("trader_id = ? AND strategy_id = ? AND strategy_version = ? AND symbol = ? AND state IN ?",
			sample.TraderID, sample.StrategyID, sample.StrategyVersion, sample.Symbol,
			[]string{"observed", "triggered", "approved", "executed"}).
		Order("started_at DESC").First(&active).Error
	if activeErr != nil && activeErr != gorm.ErrRecordNotFound {
		return fmt.Errorf("load active setup episode: %w", activeErr)
	}

	actionable := (observation.Action == "open_long" || observation.Action == "open_short") && observation.Setup != ""
	if activeErr == nil && active.OutcomeStatus != "pending" && !active.EverEligible && sample.Eligible && active.Setup == observation.Setup && active.Action == observation.Action {
		if err := tx.Model(&SetupEpisode{}).Where("id = ?", active.ID).Updates(map[string]any{
			"state": "ended", "end_reason": "triggered_after_observation_horizon", "ended_at": observedAt, "last_seen_at": observedAt,
		}).Error; err != nil {
			return fmt.Errorf("end observed setup episode before late trigger: %w", err)
		}
		activeErr = gorm.ErrRecordNotFound
	}
	if activeErr == nil && (!actionable || active.Setup != observation.Setup || active.Action != observation.Action) {
		reason := "setup_no_longer_actionable"
		if actionable {
			reason = "setup_changed:" + observation.Setup
		}
		if err := tx.Model(&SetupEpisode{}).Where("id = ?", active.ID).Updates(map[string]any{
			"state": "ended", "end_reason": reason, "ended_at": observedAt, "last_seen_at": observedAt,
		}).Error; err != nil {
			return fmt.Errorf("end setup episode: %w", err)
		}
		activeErr = gorm.ErrRecordNotFound
	}
	if !actionable {
		return nil
	}
	if activeErr == nil {
		return updateActiveSetupEpisode(tx, &active, sample, observation, observedAt)
	}
	return createSetupEpisode(tx, sample, observation, observedAt)
}

func createSetupEpisode(tx *gorm.DB, sample *SignalCalibrationSample, observation setupEpisodeObservation, observedAt time.Time) error {
	state := setupEpisodeState(sample)
	entryPrice := observation.Price
	if observation.Signal.EntryPrice > 0 {
		entryPrice = observation.Signal.EntryPrice
	}
	episode := &SetupEpisode{
		ID:                      uuid.New().String(),
		TraderID:                sample.TraderID,
		StrategyID:              sample.StrategyID,
		StrategyVersion:         sample.StrategyVersion,
		Symbol:                  sample.Symbol,
		Setup:                   observation.Setup,
		Family:                  observation.Family,
		Action:                  observation.Action,
		Regime:                  observation.Regime,
		PrimaryTimeframe:        sample.PrimaryTimeframe,
		EntryTimeframe:          sample.EntryTimeframe,
		MarketDataSource:        sample.MarketDataSource,
		State:                   state,
		EvidenceStatus:          observation.EvidenceStatus,
		EverEligible:            sample.Eligible,
		EverApproved:            sample.RiskStatus == "approved",
		Executed:                sample.ExecutionStatus == "executed",
		SignalID:                sample.SignalID,
		DecisionID:              sample.DecisionID,
		EntryPrice:              entryPrice,
		ExecutionStop:           observation.Signal.StopLoss,
		TakeProfit:              observation.Signal.TakeProfit,
		ObservationCount:        1,
		UniqueBarCount:          1,
		LastBarTime:             observation.BarTime,
		HorizonBars:             defaultSetupEpisodeHorizonBars,
		OutcomeStatus:           "pending",
		FirstSetupTraceJSON:     sample.SetupTraceJSON,
		LastSetupTraceJSON:      sample.SetupTraceJSON,
		FirstFactorSnapshotJSON: sample.FactorSnapshotJSON,
		LastFactorSnapshotJSON:  sample.FactorSnapshotJSON,
		MarketContextJSON:       sample.MarketContextJSON,
		StartedAt:               observedAt,
		LastSeenAt:              observedAt,
	}
	applySignalProtection(episode, observation.Signal)
	if episode.EntryPrice <= 0 {
		return nil
	}
	if err := tx.Create(episode).Error; err != nil {
		return fmt.Errorf("create setup episode: %w", err)
	}
	return nil
}

func updateActiveSetupEpisode(tx *gorm.DB, episode *SetupEpisode, sample *SignalCalibrationSample, observation setupEpisodeObservation, observedAt time.Time) error {
	firstTrigger := !episode.EverEligible && sample.Eligible && episode.OutcomeStatus == "pending"
	episode.ObservationCount++
	if observation.BarTime > episode.LastBarTime {
		episode.UniqueBarCount++
		episode.LastBarTime = observation.BarTime
	}
	episode.LastSeenAt = observedAt
	episode.State = laterSetupEpisodeState(episode.State, setupEpisodeState(sample))
	episode.EverEligible = episode.EverEligible || sample.Eligible
	episode.EverApproved = episode.EverApproved || sample.RiskStatus == "approved"
	episode.Executed = episode.Executed || sample.ExecutionStatus == "executed"
	if sample.SignalID != "" {
		episode.SignalID = sample.SignalID
	}
	if sample.DecisionID > 0 {
		episode.DecisionID = sample.DecisionID
	}
	if observation.EvidenceStatus != "" {
		episode.EvidenceStatus = observation.EvidenceStatus
	}
	episode.LastSetupTraceJSON = sample.SetupTraceJSON
	episode.LastFactorSnapshotJSON = sample.FactorSnapshotJSON
	episode.MarketContextJSON = sample.MarketContextJSON
	if firstTrigger && observation.Signal.EntryPrice > 0 {
		episode.EntryPrice = observation.Signal.EntryPrice
		episode.StructuralStop = 0
		episode.ExecutionStop = 0
		episode.TakeProfit = 0
		episode.StructuralRiskRR = 0
		episode.ExecutionRiskRR = 0
		episode.ForwardBars = 0
		episode.LastOutcomeBarTime = observation.BarTime
		episode.MaxFavorablePct = 0
		episode.MaxAdversePct = 0
		episode.MaxFavorableR = 0
		episode.MaxAdverseR = 0
		episode.ForwardReturnPct = 0
		applySignalProtection(episode, observation.Signal)
	}
	if err := tx.Save(episode).Error; err != nil {
		return fmt.Errorf("update setup episode: %w", err)
	}
	return nil
}

func updatePendingSetupEpisodeOutcomes(tx *gorm.DB, sample *SignalCalibrationSample, observation setupEpisodeObservation, observedAt time.Time) error {
	bars := setupEpisodeBars(sample.KlineWindowsJSON, sample.PrimaryTimeframe)
	if len(bars) == 0 {
		return nil
	}
	var episodes []SetupEpisode
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("trader_id = ? AND strategy_id = ? AND strategy_version = ? AND symbol = ? AND outcome_status = ?",
			sample.TraderID, sample.StrategyID, sample.StrategyVersion, sample.Symbol, "pending").
		Find(&episodes).Error; err != nil {
		return fmt.Errorf("load pending setup episode outcomes: %w", err)
	}
	for i := range episodes {
		episode := &episodes[i]
		if !episode.EverEligible && sample.Eligible && episode.Setup == observation.Setup && episode.Action == observation.Action {
			continue
		}
		lastOutcomeBar := episode.LastOutcomeBarTime
		if lastOutcomeBar <= 0 {
			lastOutcomeBar = episode.LastBarTime
		}
		changed := false
		for _, bar := range bars {
			if bar.CloseTime <= lastOutcomeBar {
				continue
			}
			barObservation := observation
			barObservation.Price = bar.Close
			barObservation.High = bar.High
			barObservation.Low = bar.Low
			barObservation.BarTime = bar.CloseTime
			barObservedAt := observedAt
			if bar.CloseTime > 0 {
				barObservedAt = time.UnixMilli(bar.CloseTime).UTC()
			}
			episode.LastOutcomeBarTime = bar.CloseTime
			episode.ForwardBars++
			updateSetupEpisodeExcursion(episode, barObservation)
			labelSetupEpisodeOutcome(episode, barObservation, barObservedAt)
			lastOutcomeBar = bar.CloseTime
			changed = true
			if episode.OutcomeStatus != "pending" {
				break
			}
		}
		if !changed {
			continue
		}
		if err := tx.Save(episode).Error; err != nil {
			return fmt.Errorf("update setup episode outcome: %w", err)
		}
	}
	return nil
}

func updateSetupEpisodeExcursion(episode *SetupEpisode, observation setupEpisodeObservation) {
	if episode.EntryPrice <= 0 {
		return
	}
	direction := 1.0
	if episode.Action == "open_short" {
		direction = -1
	}
	favorable := (observation.High - episode.EntryPrice) / episode.EntryPrice * 100
	adverse := (episode.EntryPrice - observation.Low) / episode.EntryPrice * 100
	if direction < 0 {
		favorable = (episode.EntryPrice - observation.Low) / episode.EntryPrice * 100
		adverse = (observation.High - episode.EntryPrice) / episode.EntryPrice * 100
	}
	if favorable > episode.MaxFavorablePct {
		episode.MaxFavorablePct = favorable
	}
	if adverse > episode.MaxAdversePct {
		episode.MaxAdversePct = adverse
	}
	episode.ForwardReturnPct = direction * (observation.Price - episode.EntryPrice) / episode.EntryPrice * 100
	risk := setupEpisodeStructuralRisk(episode)
	if risk > 0 {
		episode.MaxFavorableR = episode.MaxFavorablePct / (risk / episode.EntryPrice * 100)
		episode.MaxAdverseR = episode.MaxAdversePct / (risk / episode.EntryPrice * 100)
	}
}

func labelSetupEpisodeOutcome(episode *SetupEpisode, observation setupEpisodeObservation, observedAt time.Time) {
	stopHit := false
	targetHit := false
	if episode.Action == "open_long" {
		stopHit = episode.StructuralStop > 0 && observation.Low <= episode.StructuralStop
		targetHit = episode.TakeProfit > 0 && observation.High >= episode.TakeProfit
	} else {
		stopHit = episode.StructuralStop > 0 && observation.High >= episode.StructuralStop
		targetHit = episode.TakeProfit > 0 && observation.Low <= episode.TakeProfit
	}
	if stopHit && targetHit {
		episode.OutcomeStatus = "ambiguous"
		episode.OutcomeLabel = "same_bar_stop_and_target"
		episode.OutcomeAt = &observedAt
		return
	}
	if targetHit {
		setSetupEpisodeOutcome(episode, "target_hit", episode.TakeProfit, observedAt)
		return
	}
	if stopHit {
		setSetupEpisodeOutcome(episode, "structure_invalidated", episode.StructuralStop, observedAt)
		return
	}
	if episode.HorizonBars <= 0 {
		episode.HorizonBars = defaultSetupEpisodeHorizonBars
	}
	if episode.ForwardBars < episode.HorizonBars {
		return
	}
	label := "horizon_flat"
	if episode.ForwardReturnPct > 0.10 {
		label = "horizon_positive"
	} else if episode.ForwardReturnPct < -0.10 {
		label = "horizon_negative"
	}
	setSetupEpisodeOutcome(episode, label, observation.Price, observedAt)
}

func setSetupEpisodeOutcome(episode *SetupEpisode, label string, price float64, observedAt time.Time) {
	episode.OutcomeStatus = "labeled"
	episode.OutcomeLabel = label
	episode.OutcomePrice = price
	episode.OutcomeAt = &observedAt
	risk := setupEpisodeStructuralRisk(episode)
	if risk <= 0 {
		episode.OutcomeR = 0
		episode.OutcomeRValid = false
		return
	}
	direction := 1.0
	if episode.Action == "open_short" {
		direction = -1
	}
	episode.OutcomeR = direction * (price - episode.EntryPrice) / risk
	episode.OutcomeRValid = true
}

func parseSetupEpisodeObservation(sample *SignalCalibrationSample) (setupEpisodeObservation, error) {
	observation := setupEpisodeObservation{}
	var trace setupEpisodeTrace
	if sample.SetupTraceJSON != "" {
		if err := json.Unmarshal([]byte(sample.SetupTraceJSON), &trace); err != nil {
			return observation, fmt.Errorf("parse setup episode trace: %w", err)
		}
	}
	observation.Setup = strings.TrimSpace(trace.DetectedSetup)
	observation.Action = strings.TrimSpace(trace.DetectedAction)
	if observation.Setup == "" && sample.Eligible {
		observation.Setup = strings.TrimSpace(sample.Setup)
	}
	if observation.Action == "" && sample.Eligible {
		observation.Action = strings.TrimSpace(sample.Action)
	}
	observation.Family = strings.TrimSpace(trace.Route.Family)
	if observation.Family == "" {
		observation.Family = setupEpisodeFamily(observation.Setup)
	}
	observation.Regime = strings.TrimSpace(sample.SymbolRegime)
	if observation.Regime == "" {
		observation.Regime = strings.TrimSpace(trace.Route.Regime)
	}
	observation.EvidenceStatus = strings.TrimSpace(trace.EvidenceDecision.Status)
	if sample.SignalJSON != "" && sample.SignalJSON != "null" {
		_ = json.Unmarshal([]byte(sample.SignalJSON), &observation.Signal)
	}
	bar, ok := latestSetupEpisodeBar(sample.KlineWindowsJSON, sample.PrimaryTimeframe)
	if ok {
		observation.Price = bar.Close
		observation.High = bar.High
		observation.Low = bar.Low
		observation.BarTime = bar.CloseTime
	}
	if observation.High <= 0 {
		observation.High = observation.Price
	}
	if observation.Low <= 0 {
		observation.Low = observation.Price
	}
	return observation, nil
}

func latestSetupEpisodeBar(raw, timeframe string) (setupEpisodeKline, bool) {
	bars := setupEpisodeBars(raw, timeframe)
	if len(bars) == 0 {
		return setupEpisodeKline{}, false
	}
	return bars[len(bars)-1], true
}

func setupEpisodeBars(raw, timeframe string) []setupEpisodeKline {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(timeframe) == "" {
		return nil
	}
	var windows map[string][]setupEpisodeKline
	if err := json.Unmarshal([]byte(raw), &windows); err != nil {
		return nil
	}
	bars := make([]setupEpisodeKline, 0, len(windows[timeframe]))
	for _, bar := range windows[timeframe] {
		if bar.Close > 0 && bar.CloseTime > 0 {
			bars = append(bars, bar)
		}
	}
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].CloseTime < bars[j].CloseTime })
	return bars
}

func applySignalProtection(episode *SetupEpisode, signal setupEpisodeSignal) {
	if signal.StopLoss > 0 {
		episode.ExecutionStop = signal.StopLoss
	}
	if signal.TakeProfit > 0 {
		episode.TakeProfit = signal.TakeProfit
	}
	if raw := signal.Evidence["protective_levels"]; len(raw) > 0 {
		var levels setupEpisodeProtectiveLevels
		if json.Unmarshal(raw, &levels) == nil {
			if levels.StopAnchor > 0 {
				episode.StructuralStop = levels.StopAnchor
			}
			episode.StructuralRiskRR = levels.RiskReward
			episode.ExecutionRiskRR = levels.ExecutionRiskReward
		}
	}
	if episode.StructuralStop <= 0 {
		episode.StructuralStop = episode.ExecutionStop
	}
}

func setupEpisodeState(sample *SignalCalibrationSample) string {
	if sample.ExecutionStatus == "executed" {
		return "executed"
	}
	if sample.RiskStatus == "approved" {
		return "approved"
	}
	if sample.Eligible {
		return "triggered"
	}
	return "observed"
}

func laterSetupEpisodeState(current, next string) string {
	rank := map[string]int{"observed": 0, "triggered": 1, "approved": 2, "executed": 3}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func setupEpisodeFamily(setup string) string {
	name := strings.ToLower(strings.TrimSpace(setup))
	switch {
	case strings.Contains(name, "trend_continuation"), strings.Contains(name, "trend_pullback"):
		return "trend"
	case strings.HasPrefix(name, "breakout"):
		return "breakout"
	case strings.Contains(name, "failed_breakout"), strings.Contains(name, "range_reversal"), strings.Contains(name, "support_resistance_bounce"):
		return "reversal"
	case strings.Contains(name, "momentum_exhaustion"):
		return "exhaustion"
	default:
		return "none"
	}
}

func setupEpisodeStructuralRisk(episode *SetupEpisode) float64 {
	if episode == nil || episode.EntryPrice <= 0 || episode.StructuralStop <= 0 {
		return 0
	}
	return math.Abs(episode.EntryPrice - episode.StructuralStop)
}

func (s *SignalCalibrationStore) EpisodeBySignal(traderID, signalID string) (*SetupEpisode, error) {
	traderID = strings.TrimSpace(traderID)
	signalID = strings.TrimSpace(signalID)
	if traderID == "" || signalID == "" {
		return nil, fmt.Errorf("trader_id and signal_id are required")
	}
	var episode SetupEpisode
	if err := s.db.Where("trader_id = ? AND signal_id = ?", traderID, signalID).
		Order("started_at DESC").First(&episode).Error; err != nil {
		return nil, err
	}
	return &episode, nil
}

func (s *SignalCalibrationStore) applySetupEpisodeStats(report *SignalCalibrationReport, strategyID, strategyVersion string, limit int) error {
	if report == nil {
		return nil
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	var episodes []SetupEpisode
	query := s.db.Where("strategy_id = ?", strings.TrimSpace(strategyID))
	if strings.TrimSpace(strategyVersion) != "" {
		query = query.Where("strategy_version = ?", strings.TrimSpace(strategyVersion))
	}
	if err := query.Order("started_at DESC").Limit(limit).Find(&episodes).Error; err != nil {
		return fmt.Errorf("query setup episode calibration stats: %w", err)
	}
	stats := map[string]*SetupEpisodeStat{}
	factorStats := map[string]*SetupFactorStat{}
	totalR := 0.0
	for _, episode := range episodes {
		report.EpisodeCount++
		switch episode.OutcomeStatus {
		case "pending":
			report.PendingEpisodeCount++
		case "ambiguous":
			report.AmbiguousEpisodeCount++
		case "labeled":
			report.LabeledEpisodeCount++
			if episode.OutcomeRValid {
				report.EpisodeRCount++
				totalR += episode.OutcomeR
			}
		}
		key := nonEmptyCalibrationValue(episode.Regime, "unknown") + "|" + episode.Setup + "|" + episode.Action
		stat := stats[key]
		if stat == nil {
			stat = &SetupEpisodeStat{Regime: nonEmptyCalibrationValue(episode.Regime, "unknown"), Setup: episode.Setup, Action: episode.Action}
			stats[key] = stat
		}
		stat.Episodes++
		if episode.EverEligible {
			stat.Eligible++
		}
		if episode.EverApproved {
			stat.Approved++
		}
		if episode.Executed {
			stat.Executed++
		}
		if episode.OutcomeStatus != "labeled" {
			continue
		}
		applySetupFactorStats(factorStats, episode)
		stat.Labeled++
		if episode.OutcomeRValid {
			stat.RCount++
			stat.AverageR += episode.OutcomeR
			stat.AverageMFER += episode.MaxFavorableR
			stat.AverageMAER += episode.MaxAdverseR
		}
		switch episode.OutcomeLabel {
		case "target_hit", "horizon_positive":
			report.PositiveEpisodeCount++
			stat.Positive++
		case "structure_invalidated", "horizon_negative":
			report.NegativeEpisodeCount++
			stat.Negative++
		default:
			stat.Flat++
		}
	}
	if report.EpisodeRCount > 0 {
		report.AverageEpisodeR = totalR / float64(report.EpisodeRCount)
	}
	directional := report.PositiveEpisodeCount + report.NegativeEpisodeCount
	if directional > 0 {
		report.EpisodeHitRate = float64(report.PositiveEpisodeCount) / float64(directional)
	}
	for _, stat := range stats {
		if stat.RCount > 0 {
			stat.AverageR /= float64(stat.RCount)
			stat.AverageMFER /= float64(stat.RCount)
			stat.AverageMAER /= float64(stat.RCount)
		}
		directional := stat.Positive + stat.Negative
		if directional > 0 {
			stat.HitRate = float64(stat.Positive) / float64(directional)
		}
		report.EpisodeStats = append(report.EpisodeStats, *stat)
	}
	sort.Slice(report.EpisodeStats, func(i, j int) bool {
		return report.EpisodeStats[i].Episodes > report.EpisodeStats[j].Episodes
	})
	for _, stat := range factorStats {
		if stat.Labeled > 0 {
			stat.AverageDirectionalScore /= float64(stat.Labeled)
			stat.PredictiveAlignment /= float64(stat.Labeled)
		}
		supportDirectional := stat.PositiveWhenSupporting + stat.NegativeWhenSupporting
		if supportDirectional > 0 {
			stat.SupportHitRate = float64(stat.PositiveWhenSupporting) / float64(supportDirectional)
		}
		conflictDirectional := stat.PositiveWhenConflicting + stat.NegativeWhenConflicting
		if conflictDirectional > 0 {
			stat.ConflictHitRate = float64(stat.PositiveWhenConflicting) / float64(conflictDirectional)
		}
		report.FactorStats = append(report.FactorStats, *stat)
	}
	sort.Slice(report.FactorStats, func(i, j int) bool {
		if report.FactorStats[i].Labeled != report.FactorStats[j].Labeled {
			return report.FactorStats[i].Labeled > report.FactorStats[j].Labeled
		}
		return math.Abs(report.FactorStats[i].PredictiveAlignment) > math.Abs(report.FactorStats[j].PredictiveAlignment)
	})
	return nil
}

func applySetupFactorStats(stats map[string]*SetupFactorStat, episode SetupEpisode) {
	var trace setupEpisodeTrace
	if strings.TrimSpace(episode.FirstSetupTraceJSON) == "" || json.Unmarshal([]byte(episode.FirstSetupTraceJSON), &trace) != nil {
		return
	}
	factors := map[string]bool{}
	for factor := range trace.Primary.Components {
		factors[factor] = true
	}
	for factor := range trace.Entry.Components {
		factors[factor] = true
	}
	direction := 1.0
	if episode.Action == "open_short" {
		direction = -1
	}
	outcomeSign := setupEpisodeOutcomeSign(episode.OutcomeLabel)
	for factor := range factors {
		primary, primaryOK := trace.Primary.Components[factor]
		entry, entryOK := trace.Entry.Components[factor]
		score := 0.0
		switch {
		case primaryOK && entryOK:
			score = (primary + entry) / 2
		case primaryOK:
			score = primary
		case entryOK:
			score = entry
		default:
			continue
		}
		directionalScore := score * direction
		key := nonEmptyCalibrationValue(episode.Regime, "unknown") + "|" + episode.Setup + "|" + episode.Action + "|" + factor
		stat := stats[key]
		if stat == nil {
			stat = &SetupFactorStat{
				Regime: nonEmptyCalibrationValue(episode.Regime, "unknown"),
				Setup:  episode.Setup,
				Action: episode.Action,
				Factor: factor,
			}
			stats[key] = stat
		}
		stat.Labeled++
		stat.AverageDirectionalScore += directionalScore
		stat.PredictiveAlignment += directionalScore / 100 * outcomeSign
		switch {
		case directionalScore >= 10:
			stat.Supporting++
			if outcomeSign > 0 {
				stat.PositiveWhenSupporting++
			} else if outcomeSign < 0 {
				stat.NegativeWhenSupporting++
			}
		case directionalScore <= -10:
			stat.Conflicting++
			if outcomeSign > 0 {
				stat.PositiveWhenConflicting++
			} else if outcomeSign < 0 {
				stat.NegativeWhenConflicting++
			}
		default:
			stat.Neutral++
		}
	}
}

func setupEpisodeOutcomeSign(label string) float64 {
	switch label {
	case "target_hit", "horizon_positive":
		return 1
	case "structure_invalidated", "horizon_negative":
		return -1
	default:
		return 0
	}
}
