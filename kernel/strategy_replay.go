package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"nofx/market"
	"nofx/store"
	"sort"
	"strings"
	"time"
)

type StrategyReplayRequest struct {
	StrategyID      string
	StrategyVersion string
	CurrentConfig   *store.StrategyConfig
	Samples         []store.SignalCalibrationSample
	KlineLoader     func(store.SignalCalibrationSample) (map[string][]market.Kline, error)
	Limit           int
}

type StrategyReplayReport struct {
	StrategyID              string                     `json:"strategy_id"`
	StrategyVersion         string                     `json:"strategy_version,omitempty"`
	RawSampleCount          int                        `json:"raw_sample_count"`
	DuplicateScanCount      int                        `json:"duplicate_scan_count"`
	SampleCount             int                        `json:"sample_count"`
	ReplayableSampleCount   int                        `json:"replayable_sample_count"`
	MissingKlineWindowCount int                        `json:"missing_kline_window_count"`
	BaselineMatchCount      int                        `json:"baseline_match_count"`
	BaselineMatchRate       float64                    `json:"baseline_match_rate"`
	ExecutedSampleCount     int                        `json:"executed_sample_count"`
	ParameterScans          []StrategyReplayScanResult `json:"parameter_scans"`
	QualityNotes            []string                   `json:"quality_notes,omitempty"`
	GeneratedAt             time.Time                  `json:"generated_at"`
}

type StrategyReplayScanResult struct {
	VariantID                string         `json:"variant_id"`
	Label                    string         `json:"label"`
	Parameters               map[string]any `json:"parameters"`
	ReplayedCount            int            `json:"replayed_count"`
	TradableCount            int            `json:"tradable_count"`
	NoTradeCount             int            `json:"no_trade_count"`
	MatchRecordedCount       int            `json:"match_recorded_count"`
	ChangedFromBaselineCount int            `json:"changed_from_baseline_count"`
	ExecutedPreservedCount   int            `json:"executed_preserved_count"`
	ExecutedChangedCount     int            `json:"executed_changed_count"`
	SetupCounts              map[string]int `json:"setup_counts"`
	ErrorCount               int            `json:"error_count,omitempty"`
}

type strategyReplayVariant struct {
	ID         string
	Label      string
	Config     *store.StrategyConfig
	Parameters map[string]any
}

type replayedSetup struct {
	Setup    string
	Action   string
	Tradable bool
}

func BuildStrategyReplayReport(req StrategyReplayRequest) (*StrategyReplayReport, error) {
	if req.CurrentConfig == nil {
		return nil, fmt.Errorf("current strategy config is required")
	}
	limit := req.Limit
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rawSampleCount := replaySetupSampleCount(req.Samples)
	independentSamples := independentReplaySamples(req.Samples)
	duplicateScanCount := rawSampleCount - len(independentSamples)
	samples := independentSamples
	if len(samples) > limit {
		samples = samples[:limit]
	}
	variants, err := replayParameterVariants(req.CurrentConfig)
	if err != nil {
		return nil, err
	}
	report := &StrategyReplayReport{
		StrategyID:         strings.TrimSpace(req.StrategyID),
		StrategyVersion:    strings.TrimSpace(req.StrategyVersion),
		RawSampleCount:     rawSampleCount,
		DuplicateScanCount: duplicateScanCount,
		SampleCount:        len(samples),
		ParameterScans:     make([]StrategyReplayScanResult, len(variants)),
		GeneratedAt:        time.Now().UTC(),
	}
	for i, variant := range variants {
		report.ParameterScans[i] = StrategyReplayScanResult{
			VariantID:   variant.ID,
			Label:       variant.Label,
			Parameters:  variant.Parameters,
			SetupCounts: map[string]int{},
		}
	}

	for _, sample := range samples {
		if sample.ExecutionStatus == "executed" {
			report.ExecutedSampleCount++
		}
		windows, ok := parseReplayKlineWindows(sample.KlineWindowsJSON)
		if !ok && req.KlineLoader != nil {
			loaded, loadErr := req.KlineLoader(sample)
			if loadErr == nil && len(loaded) > 0 {
				windows = copyReplayKlineWindows(loaded)
				ok = len(windows) > 0
			}
		}
		if !ok {
			report.MissingKlineWindowCount++
			continue
		}
		report.ReplayableSampleCount++
		baseline := replayedSetup{Setup: "replay_error"}
		for i, variant := range variants {
			replayed, err := replaySampleSetup(variant.Config, sample.Symbol, sample.AsOf, windows, sample.FactorSnapshotJSON)
			scan := &report.ParameterScans[i]
			scan.ReplayedCount++
			if err != nil {
				scan.ErrorCount++
				replayed = replayedSetup{Setup: "replay_error"}
			}
			if i == 0 {
				baseline = replayed
			}
			setupName := nonEmptyReplaySetup(replayed.Setup)
			scan.SetupCounts[setupName]++
			if replayed.Tradable {
				scan.TradableCount++
			} else {
				scan.NoTradeCount++
			}
			if normalizedReplaySetup(recordedReplaySetup(sample)) == normalizedReplaySetup(setupName) {
				scan.MatchRecordedCount++
				if i == 0 {
					report.BaselineMatchCount++
				}
			}
			if i > 0 && normalizedReplaySetup(setupName) != normalizedReplaySetup(baseline.Setup) {
				scan.ChangedFromBaselineCount++
				if sample.ExecutionStatus == "executed" {
					scan.ExecutedChangedCount++
				}
			} else if sample.ExecutionStatus == "executed" {
				scan.ExecutedPreservedCount++
			}
		}
	}
	if report.ReplayableSampleCount > 0 {
		report.BaselineMatchRate = round2(float64(report.BaselineMatchCount) / float64(report.ReplayableSampleCount))
	}
	report.QualityNotes = buildReplayQualityNotes(report)
	sortReplayScans(report.ParameterScans)
	return report, nil
}

func independentReplaySamples(samples []store.SignalCalibrationSample) []store.SignalCalibrationSample {
	out := make([]store.SignalCalibrationSample, 0, len(samples))
	seen := map[string]bool{}
	for index, sample := range samples {
		if sample.SampleKind != "" && sample.SampleKind != "setup" {
			continue
		}
		key := fmt.Sprintf("fallback:%d|%s|%s|%d", index, sample.Symbol, sample.PrimaryTimeframe, sample.AsOf.UnixMilli())
		if sample.PrimaryBarTime > 0 {
			key = fmt.Sprintf("%s|%s|%d", sample.Symbol, sample.PrimaryTimeframe, sample.PrimaryBarTime)
		}
		if windows, ok := parseReplayKlineWindows(sample.KlineWindowsJSON); ok {
			if bars := windows[sample.PrimaryTimeframe]; len(bars) > 0 {
				key = fmt.Sprintf("%s|%s|%d", sample.Symbol, sample.PrimaryTimeframe, bars[len(bars)-1].CloseTime)
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, sample)
	}
	return out
}

func replaySetupSampleCount(samples []store.SignalCalibrationSample) int {
	count := 0
	for _, sample := range samples {
		if sample.SampleKind == "" || sample.SampleKind == "setup" {
			repeats := sample.RepeatCount
			if repeats <= 0 {
				repeats = 1
			}
			count += repeats
		}
	}
	return count
}

func recordedReplaySetup(sample store.SignalCalibrationSample) string {
	if strings.TrimSpace(sample.SetupTraceJSON) == "" {
		return sample.Setup
	}
	var trace struct {
		DetectedSetup string `json:"detected_setup"`
		Setup         string `json:"setup"`
	}
	if json.Unmarshal([]byte(sample.SetupTraceJSON), &trace) != nil {
		return sample.Setup
	}
	if strings.TrimSpace(trace.DetectedSetup) != "" {
		return trace.DetectedSetup
	}
	if strings.TrimSpace(trace.Setup) != "" {
		return trace.Setup
	}
	return sample.Setup
}

func parseReplayKlineWindows(raw string) (map[string][]market.Kline, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	var windows map[string][]market.Kline
	if err := json.Unmarshal([]byte(raw), &windows); err != nil {
		return nil, false
	}
	copied := copyReplayKlineWindows(windows)
	return copied, len(copied) > 0
}

func replaySampleSetup(config *store.StrategyConfig, symbol string, asOf time.Time, windows map[string][]market.Kline, factorSnapshotJSON string) (replayedSetup, error) {
	if config == nil {
		return replayedSetup{}, fmt.Errorf("strategy config is required")
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	input := market.MarketInput{
		Symbol:     strings.TrimSpace(strings.ToUpper(symbol)),
		Timeframes: copyReplayKlineWindows(windows),
		AsOf:       asOf.UTC(),
	}
	if input.Symbol == "" || len(input.Timeframes) == 0 {
		return replayedSetup{}, fmt.Errorf("sample has no replayable market input")
	}

	snapshot, err := market.NewDefaultIndicatorEngine().Calculate(context.Background(), input, IndicatorRequestFromStrategyConfig(config))
	if err != nil {
		snapshot = &market.FactorSnapshot{
			Symbol:     input.Symbol,
			AsOf:       input.AsOf,
			Structures: map[string][]market.StructureSnapshot{},
		}
	}
	if snapshot.Structures == nil {
		snapshot.Structures = map[string][]market.StructureSnapshot{}
	}
	if strings.TrimSpace(factorSnapshotJSON) != "" {
		var recorded market.FactorSnapshot
		if json.Unmarshal([]byte(factorSnapshotJSON), &recorded) == nil {
			snapshot.External = recorded.External
			snapshot.RiskFlags = append([]string(nil), recorded.RiskFlags...)
		}
	}
	structures, structureErr := market.NewDefaultStructureEngine().Calculate(context.Background(), input, StructureRequestFromStrategyConfig(config))
	if structureErr != nil {
		return replayedSetup{}, structureErr
	}
	for _, structure := range structures {
		snapshot.Structures[structure.Name] = append(snapshot.Structures[structure.Name], structure)
	}

	if scoring := scoringFromStrategyConfig(config); scoring != nil {
		trace := evaluateSetupSnapshot(scoring, input.Symbol, snapshot)
		return replayedSetup{
			Setup:    nonEmptyReplaySetup(trace.Setup),
			Action:   trace.Action,
			Tradable: trace.Eligible && trace.Action != "",
		}, nil
	}
	if setup, ok := preferredStructureSetup(snapshot, TimeframeRoleTrace{}); ok {
		action := actionForStructureSetup(setup)
		return replayedSetup{
			Setup:    nonEmptyReplaySetup(setup.Setup),
			Action:   action,
			Tradable: setup.Valid && action != "" && !strings.HasPrefix(setup.Setup, "no_trade"),
		}, nil
	}
	return replayedSetup{Setup: "no_trade_no_structure", Tradable: false}, nil
}

func replayParameterVariants(config *store.StrategyConfig) ([]strategyReplayVariant, error) {
	baseline, err := cloneReplayStrategyConfig(config)
	if err != nil {
		return nil, err
	}
	variants := []strategyReplayVariant{{
		ID:         "baseline",
		Label:      "Current parameters",
		Config:     baseline,
		Parameters: replayMarketStructureParameters(baseline),
	}}
	base := activeReplayMarketStructure(baseline)
	add := func(id string, label string, mutate func(*store.StructureMarketConfig)) {
		next, err := cloneReplayStrategyConfig(config)
		if err != nil {
			return
		}
		structure := activeReplayMarketStructure(next)
		mutate(&structure)
		setReplayMarketStructure(next, structure)
		variants = append(variants, strategyReplayVariant{
			ID:         id,
			Label:      label,
			Config:     next,
			Parameters: replayMarketStructureParameters(next),
		})
	}
	if base.SwingWindow > 1 {
		add("swing_window_down", "Swing window -1", func(s *store.StructureMarketConfig) {
			s.SwingWindow--
		})
	}
	add("swing_window_up", "Swing window +1", func(s *store.StructureMarketConfig) {
		s.SwingWindow++
	})
	if base.MinLegBars > 1 {
		add("min_leg_bars_down", "Min leg bars -1", func(s *store.StructureMarketConfig) {
			s.MinLegBars--
		})
	}
	add("min_leg_bars_up", "Min leg bars +1", func(s *store.StructureMarketConfig) {
		s.MinLegBars++
	})
	add("zigzag_threshold_down", "ZigZag threshold x0.75", func(s *store.StructureMarketConfig) {
		s.ZigZagThresholdPct = replayScaledPositive(s.ZigZagThresholdPct, 0.75, 0.1)
	})
	add("zigzag_threshold_up", "ZigZag threshold x1.25", func(s *store.StructureMarketConfig) {
		s.ZigZagThresholdPct = replayScaledPositive(s.ZigZagThresholdPct, 1.25, 0.1)
	})
	add("min_leg_atr_down", "Min leg ATR x0.8", func(s *store.StructureMarketConfig) {
		s.MinLegATRMultiple = replayScaledPositive(s.MinLegATRMultiple, 0.8, 0.1)
	})
	add("min_leg_atr_up", "Min leg ATR x1.2", func(s *store.StructureMarketConfig) {
		s.MinLegATRMultiple = replayScaledPositive(s.MinLegATRMultiple, 1.2, 0.1)
	})
	add("breakout_buffer_down", "Breakout buffer x0.75", func(s *store.StructureMarketConfig) {
		s.BreakoutBufferATR = replayScaledPositive(s.BreakoutBufferATR, 0.75, 0.05)
	})
	add("breakout_buffer_up", "Breakout buffer x1.25", func(s *store.StructureMarketConfig) {
		s.BreakoutBufferATR = replayScaledPositive(s.BreakoutBufferATR, 1.25, 0.05)
	})
	add("retest_tolerance_down", "Retest tolerance x0.75", func(s *store.StructureMarketConfig) {
		s.RetestToleranceATR = replayScaledPositive(s.RetestToleranceATR, 0.75, 0.05)
	})
	add("retest_tolerance_up", "Retest tolerance x1.25", func(s *store.StructureMarketConfig) {
		s.RetestToleranceATR = replayScaledPositive(s.RetestToleranceATR, 1.25, 0.05)
	})
	return variants, nil
}

func cloneReplayStrategyConfig(config *store.StrategyConfig) (*store.StrategyConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("strategy config is required")
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var out store.StrategyConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.NormalizeForExecution()
	return &out, nil
}

func activeReplayMarketStructure(config *store.StrategyConfig) store.StructureMarketConfig {
	if config == nil {
		return store.StructureMarketConfig{}
	}
	if config.ResolvedParameters.Structure.MarketStructure != nil {
		return *config.ResolvedParameters.Structure.MarketStructure
	}
	return config.Structure.MarketStructure
}

func setReplayMarketStructure(config *store.StrategyConfig, structure store.StructureMarketConfig) {
	if config == nil {
		return
	}
	config.Structure.EnableMarketStructure = true
	config.Structure.MarketStructure = structure
	config.ResolvedParameters.Structure.MarketStructure = &config.Structure.MarketStructure
	config.NormalizeForExecution()
}

func replayMarketStructureParameters(config *store.StrategyConfig) map[string]any {
	structure := activeReplayMarketStructure(config)
	return map[string]any{
		"lookback":              structure.Lookback,
		"swing_window":          structure.SwingWindow,
		"min_leg_bars":          structure.MinLegBars,
		"min_leg_atr_multiple":  round2(structure.MinLegATRMultiple),
		"zigzag_threshold_pct":  round2(structure.ZigZagThresholdPct),
		"breakout_buffer_atr":   round2(structure.BreakoutBufferATR),
		"retest_tolerance_atr":  round2(structure.RetestToleranceATR),
		"exhaustion_rsi_period": structure.ExhaustionRSIPeriod,
		"lookback_by_timeframe": structure.LookbackByTimeframe,
	}
}

func replayScaledPositive(value float64, factor float64, minimum float64) float64 {
	if value <= 0 {
		value = minimum
	}
	next := value * factor
	if next < minimum {
		next = minimum
	}
	return round2(next)
}

func copyReplayKlineWindows(windows map[string][]market.Kline) map[string][]market.Kline {
	out := make(map[string][]market.Kline, len(windows))
	for timeframe, klines := range windows {
		if strings.TrimSpace(timeframe) == "" || len(klines) == 0 {
			continue
		}
		out[timeframe] = append([]market.Kline(nil), klines...)
	}
	return out
}

func nonEmptyReplaySetup(setup string) string {
	setup = strings.TrimSpace(setup)
	if setup == "" {
		return "unclassified"
	}
	return setup
}

func normalizedReplaySetup(setup string) string {
	return strings.ToLower(nonEmptyReplaySetup(setup))
}

func buildReplayQualityNotes(report *StrategyReplayReport) []string {
	if report == nil {
		return []string{"replay report is unavailable"}
	}
	notes := []string{}
	if report.SampleCount == 0 {
		notes = append(notes, "no independent closed-bar samples are available for this strategy version")
	}
	if report.DuplicateScanCount > 0 {
		notes = append(notes, fmt.Sprintf("%d repeated scans of already-counted closed bars were removed", report.DuplicateScanCount))
	}
	if report.MissingKlineWindowCount > 0 {
		notes = append(notes, fmt.Sprintf("%d sample(s) were collected before raw K-line windows were persisted", report.MissingKlineWindowCount))
	}
	if report.ReplayableSampleCount > 0 && report.ReplayableSampleCount < 30 {
		notes = append(notes, fmt.Sprintf("only %d replayable sample(s); parameter scan is diagnostic, not statistically reliable", report.ReplayableSampleCount))
	}
	if report.ReplayableSampleCount == 0 && report.SampleCount > 0 {
		notes = append(notes, "samples exist, but none include replayable K-line windows")
	}
	return notes
}

func sortReplayScans(scans []StrategyReplayScanResult) {
	if len(scans) <= 1 {
		return
	}
	baseline := scans[0]
	rest := append([]StrategyReplayScanResult(nil), scans[1:]...)
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].ExecutedChangedCount != rest[j].ExecutedChangedCount {
			return rest[i].ExecutedChangedCount < rest[j].ExecutedChangedCount
		}
		if rest[i].ChangedFromBaselineCount != rest[j].ChangedFromBaselineCount {
			return rest[i].ChangedFromBaselineCount < rest[j].ChangedFromBaselineCount
		}
		return rest[i].VariantID < rest[j].VariantID
	})
	scans[0] = baseline
	copy(scans[1:], rest)
}
