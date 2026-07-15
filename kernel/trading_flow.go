package kernel

import (
	"context"
	"fmt"
	"nofx/market"
	"time"
)

// StrategyRule is the executable form of a user strategy prompt. Natural
// language must be compiled into these rules before it can affect trading.
type StrategyRule struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	Symbols     []string        `json:"symbols,omitempty"`
	Timeframe   string          `json:"timeframe,omitempty"`
	Conditions  []RuleCondition `json:"conditions"`
	Action      string          `json:"action"`
	Execution   RuleExecution   `json:"execution"`
	Enabled     bool            `json:"enabled"`
}

type RuleExecution struct {
	Leverage        int     `json:"leverage,omitempty"`
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"`
	StopLossPct     float64 `json:"stop_loss_pct,omitempty"`
	TakeProfitPct   float64 `json:"take_profit_pct,omitempty"`
	Confidence      int     `json:"confidence,omitempty"`
}

type RuleCondition struct {
	Left     RuleOperand `json:"left"`
	Operator string      `json:"operator"`
	Right    RuleOperand `json:"right"`
}

type RuleOperand struct {
	Kind      string  `json:"kind"` // indicator, external_factor, structure, literal/value
	Name      string  `json:"name,omitempty"`
	Timeframe string  `json:"timeframe,omitempty"`
	Period    int     `json:"period,omitempty"`
	Field     string  `json:"field,omitempty"`
	Value     float64 `json:"value,omitempty"`
}

type StrategyCompileRequest struct {
	StrategyID      string        `json:"strategy_id"`
	StrategyVersion string        `json:"strategy_version"`
	Prompt          string        `json:"prompt,omitempty"`
	Context         string        `json:"context,omitempty"`
	Execution       RuleExecution `json:"execution,omitempty"`
}

type StrategyCompileResult struct {
	StrategyMode  string           `json:"strategy_mode,omitempty"`
	Rules         []StrategyRule   `json:"rules"`
	ScoringConfig *ScoringStrategy `json:"scoring_config,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	Errors        []string         `json:"errors,omitempty"`
}

type StrategyCompiler interface {
	Compile(ctx context.Context, req StrategyCompileRequest) (*StrategyCompileResult, error)
}

// SignalRequest is the deterministic input to the signal engine.
type SignalRequest struct {
	Account        AccountInfo           `json:"account"`
	Positions      []PositionInfo        `json:"positions"`
	DrawdownAlerts []DrawdownAlert       `json:"drawdown_alerts,omitempty"`
	TradingStats   *TradingStats         `json:"trading_stats,omitempty"`
	RecentOrders   []RecentOrder         `json:"recent_orders,omitempty"`
	Candidates     []CandidateCoin       `json:"candidates"`
	Rules          []StrategyRule        `json:"rules"`
	Scoring        *ScoringStrategy      `json:"scoring,omitempty"`
	PositionSizing *PositionSizingConfig `json:"position_sizing,omitempty"`
	// MinRiskRewardRatio is the configured structural RR floor. It is separate
	// from execution SL/TP percentage placeholders used by compiled rules.
	MinRiskRewardRatio float64 `json:"min_risk_reward_ratio,omitempty"`
	// ProtectiveATRBuffer controls stop-loss distance as ATR14 multiples.
	// Strategy configs should provide the explicit default; the signal engine still guards unset values.
	ProtectiveATRBuffer  float64                              `json:"protective_atr_buffer,omitempty"`
	ProtectiveTimeframes ProtectiveTimeframeConfig            `json:"protective_timeframes,omitempty"`
	FactorSnapshot       map[string]*market.FactorSnapshot    `json:"factor_snapshot"`
	KlineWindows         map[string]map[string][]market.Kline `json:"-"`
	Now                  time.Time                            `json:"now"`
}

type ProtectiveTimeframeConfig struct {
	StopLossMode      string `json:"stop_loss_timeframe_mode,omitempty"`
	StopLossTimeframe string `json:"stop_loss_timeframe,omitempty"`
}

type PositionSizingConfig struct {
	RiskPerTradePct              float64 `json:"risk_per_trade_pct,omitempty"`
	MinPositionSizeUSD           float64 `json:"min_position_size_usd,omitempty"`
	MaxMarginUsage               float64 `json:"max_margin_usage,omitempty"`
	BTCETHMaxPositionValueRatio  float64 `json:"btc_eth_max_position_value_ratio,omitempty"`
	AltcoinMaxPositionValueRatio float64 `json:"altcoin_max_position_value_ratio,omitempty"`
}

type ScoringStrategy struct {
	Enabled                 bool               `json:"enabled"`
	Version                 string             `json:"version,omitempty"`
	SelectedFactors         []string           `json:"selected_factors,omitempty"`
	FactorWeights           map[string]float64 `json:"factor_weights,omitempty"`
	LongThreshold           float64            `json:"long_threshold,omitempty"`
	ShortThreshold          float64            `json:"short_threshold,omitempty"`
	MinAvailableWeightRatio float64            `json:"min_available_weight_ratio,omitempty"`
	MinConfidence           int                `json:"min_confidence,omitempty"`
	Timeframe               string             `json:"timeframe,omitempty"`
	EntryTimeframe          string             `json:"entry_timeframe,omitempty"`
	ConfirmationTimeframes  []string           `json:"confirmation_timeframes,omitempty"`
	Symbols                 []string           `json:"symbols,omitempty"`
	Execution               RuleExecution      `json:"execution"`
}

type CandidateSignal struct {
	ID              string                 `json:"id"`
	RuleID          string                 `json:"rule_id"`
	Setup           string                 `json:"setup,omitempty"`
	StrategyVersion string                 `json:"strategy_version"`
	Symbol          string                 `json:"symbol"`
	Action          string                 `json:"action"`
	Timeframe       string                 `json:"timeframe,omitempty"`
	EntryPrice      float64                `json:"entry_price,omitempty"`
	Leverage        int                    `json:"leverage,omitempty"`
	PositionSizeUSD float64                `json:"position_size_usd,omitempty"`
	StopLoss        float64                `json:"stop_loss,omitempty"`
	TakeProfit      float64                `json:"take_profit,omitempty"`
	Confidence      int                    `json:"confidence,omitempty"`
	RiskFlags       []string               `json:"risk_flags,omitempty"`
	TriggerReason   string                 `json:"trigger_reason,omitempty"`
	Evidence        map[string]interface{} `json:"evidence,omitempty"`
	GeneratedAt     time.Time              `json:"generated_at"`
}

type SignalEngine interface {
	Generate(ctx context.Context, req SignalRequest) ([]CandidateSignal, error)
}

type AIReviewRequest struct {
	Signals          []CandidateSignal                 `json:"signals"`
	FactorSnapshot   map[string]*market.FactorSnapshot `json:"factor_snapshot"`
	MarketContext    *MarketContext                    `json:"market_context,omitempty"`
	RelevantMemory   []TradeLesson                     `json:"relevant_memory,omitempty"`
	CurrentPositions []PositionInfo                    `json:"current_positions,omitempty"`
	DrawdownAlerts   []DrawdownAlert                   `json:"drawdown_alerts,omitempty"`
	TradingStats     *TradingStats                     `json:"trading_stats,omitempty"`
	RecentOrders     []RecentOrder                     `json:"recent_orders,omitempty"`
}

type AIReviewDecision struct {
	SignalID string   `json:"signal_id"`
	Status   string   `json:"status"` // pass, warn, reject
	Reasons  []string `json:"reasons,omitempty"`
	Summary  string   `json:"summary,omitempty"`
}

type AIReviewer interface {
	Review(ctx context.Context, req AIReviewRequest) ([]AIReviewDecision, error)
}

type RiskGateRequest struct {
	Account       AccountInfo        `json:"account"`
	Positions     []PositionInfo     `json:"positions"`
	Signals       []CandidateSignal  `json:"signals"`
	Reviews       []AIReviewDecision `json:"reviews"`
	MarketContext *MarketContext     `json:"market_context,omitempty"`
}

type RiskGateResult struct {
	Approved []CandidateSignal    `json:"approved"`
	Rejected []RiskRejectedSignal `json:"rejected,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
}

type RiskRejectedSignal struct {
	SignalID string `json:"signal_id"`
	Reason   string `json:"reason"`
}

type RiskGate interface {
	Validate(ctx context.Context, req RiskGateRequest) (*RiskGateResult, error)
}

type TradeMemoryRecord struct {
	TraderID        string            `json:"trader_id,omitempty"`
	StrategyID      string            `json:"strategy_id,omitempty"`
	StrategyVersion string            `json:"strategy_version,omitempty"`
	PositionID      int64             `json:"position_id,omitempty"`
	Signal          CandidateSignal   `json:"signal"`
	Review          AIReviewDecision  `json:"review"`
	Result          string            `json:"result,omitempty"`
	OutcomePnL      float64           `json:"outcome_pnl,omitempty"`
	OutcomePnLPct   float64           `json:"outcome_pnl_pct,omitempty"`
	Summary         string            `json:"summary,omitempty"`
	Evidence        string            `json:"evidence,omitempty"`
	Lessons         []string          `json:"lessons,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	QualityScore    float64           `json:"quality_score,omitempty"`
	Confidence      float64           `json:"confidence,omitempty"`
	Scope           string            `json:"scope,omitempty"`
	SourceType      string            `json:"source_type,omitempty"`
	FactorTrace     map[string]string `json:"factor_trace,omitempty"`
	ExpiresAt       *time.Time        `json:"expires_at,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

type TradeLesson struct {
	ID         string    `json:"id"`
	Scope      string    `json:"scope"`
	Summary    string    `json:"summary"`
	Evidence   string    `json:"evidence,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TradeMemoryStore interface {
	FindRelevant(ctx context.Context, symbols []string, limit int) ([]TradeLesson, error)
	Record(ctx context.Context, record TradeMemoryRecord) error
}

type MarketContext struct {
	GeneratedAt      time.Time              `json:"generated_at"`
	MarketRegime     string                 `json:"market_regime"`
	DirectionBias    string                 `json:"direction_bias,omitempty"`
	VolatilityRegime string                 `json:"volatility_regime,omitempty"`
	RiskFlags        []string               `json:"risk_flags,omitempty"`
	ContextSummary   string                 `json:"context_summary,omitempty"`
	BTCTrend         string                 `json:"btc_trend,omitempty"`
	ETHTrend         string                 `json:"eth_trend,omitempty"`
	FundingState     string                 `json:"funding_state,omitempty"`
	BreadthState     string                 `json:"breadth_state,omitempty"`
	Metrics          map[string]interface{} `json:"metrics,omitempty"`
}

type MarketContextRequest struct {
	FactorSnapshot map[string]*market.FactorSnapshot `json:"factor_snapshot"`
	Signals        []CandidateSignal                 `json:"signals,omitempty"`
	Now            time.Time                         `json:"now"`
}

type MarketContextEngine interface {
	Build(ctx context.Context, req MarketContextRequest) (*MarketContext, error)
}

type TradingEngine struct {
	SignalEngine        SignalEngine
	MarketContextEngine MarketContextEngine
	AIReviewer          AIReviewer
	RiskGate            RiskGate
	Memory              TradeMemoryStore
}

type TradingEngineRequest struct {
	SignalRequest SignalRequest `json:"signal_request"`
}

type TradingEngineResult struct {
	Signals           []CandidateSignal      `json:"signals"`
	SuppressedSignals []SuppressedSignal     `json:"suppressed_signals,omitempty"`
	SetupEvaluations  []SetupEvaluationTrace `json:"setup_evaluations,omitempty"`
	RuleEvaluations   []RuleEvaluationTrace  `json:"rule_evaluations,omitempty"`
	MarketContext     *MarketContext         `json:"market_context,omitempty"`
	Reviews           []AIReviewDecision     `json:"reviews"`
	Risk              *RiskGateResult        `json:"risk"`
	Memory            []TradeLesson          `json:"memory,omitempty"`
}

type SuppressedSignal struct {
	Signal CandidateSignal `json:"signal"`
	Reason string          `json:"reason"`
}

// SignalCalibrationSample is an internal, structured record used by the store
// layer to persist enough evidence for later calibration and paper validation. It records
// deterministic outputs and review/risk outcomes; it does not affect trading.
type SignalCalibrationSample struct {
	SampleKind             string
	Symbol                 string
	StrategyVersion        string
	SignalID               string
	RuleID                 string
	Setup                  string
	SymbolRegime           string
	Action                 string
	Eligible               bool
	Timeframe              string
	PrimaryTimeframe       string
	EntryTimeframe         string
	ConfirmationTimeframes []string
	EntryPrice             float64
	Confidence             int
	Score                  float64
	PrimaryScore           float64
	EntryScore             float64
	ReviewStatus           string
	ReviewReasons          []string
	RiskStatus             string
	RiskReason             string
	ExecutionStatus        string
	ExecutionReason        string
	FactorSnapshot         *market.FactorSnapshot
	KlineWindows           map[string][]market.Kline
	SetupTrace             *SetupEvaluationTrace
	EvidenceTrace          *ScoringEvaluationTrace
	Signal                 *CandidateSignal
	MarketContext          *MarketContext
	AsOf                   time.Time
}

func NewTradingEngine(signalEngine SignalEngine, reviewer AIReviewer, riskGate RiskGate, memory TradeMemoryStore) *TradingEngine {
	return &TradingEngine{
		SignalEngine:        signalEngine,
		MarketContextEngine: NewDefaultMarketContextEngine(),
		AIReviewer:          reviewer,
		RiskGate:            riskGate,
		Memory:              memory,
	}
}

// Evaluate runs the new deterministic-first trading flow. It does not execute
// orders; execution remains a separate adapter after risk approval.
func (e *TradingEngine) Evaluate(ctx context.Context, req TradingEngineRequest) (*TradingEngineResult, error) {
	if e == nil {
		return nil, fmt.Errorf("trading engine is nil")
	}
	if e.SignalEngine == nil {
		return nil, fmt.Errorf("signal engine is required")
	}
	if e.AIReviewer == nil {
		return nil, fmt.Errorf("AI reviewer is required")
	}
	if e.RiskGate == nil {
		return nil, fmt.Errorf("risk gate is required")
	}

	signals, err := e.SignalEngine.Generate(ctx, req.SignalRequest)
	if err != nil {
		return nil, fmt.Errorf("generate signals: %w", err)
	}
	drawdownSignals := GenerateDrawdownAlertSignals(req.SignalRequest, signals)
	signals = mergePositionLifecycleSignals(signals, drawdownSignals)
	lifecycleSignals := GeneratePositionLifecycleSignals(req.SignalRequest, signals)
	signals = mergePositionLifecycleSignals(signals, lifecycleSignals)
	signals, suppressedSignals := suppressOpenSignalsWithPositions(signals, req.SignalRequest.Positions)
	applyRiskBasedPositionSizing(signals, req.SignalRequest.Account, req.SignalRequest.PositionSizing)
	setupEvaluations := TraceSetupEvaluations(req.SignalRequest)
	ruleEvaluations := TraceRuleEvaluations(req.SignalRequest)
	var marketContext *MarketContext
	if e.MarketContextEngine != nil {
		marketContext, err = e.MarketContextEngine.Build(ctx, MarketContextRequest{
			FactorSnapshot: req.SignalRequest.FactorSnapshot,
			Signals:        signals,
			Now:            req.SignalRequest.Now,
		})
		if err != nil {
			return nil, fmt.Errorf("build market context: %w", err)
		}
	}
	if len(signals) == 0 {
		return &TradingEngineResult{Signals: signals, SuppressedSignals: suppressedSignals, SetupEvaluations: setupEvaluations, RuleEvaluations: ruleEvaluations, MarketContext: marketContext, Reviews: []AIReviewDecision{}, Risk: &RiskGateResult{}}, nil
	}

	var lessons []TradeLesson
	if e.Memory != nil {
		lessons, err = e.Memory.FindRelevant(ctx, signalSymbols(signals), 10)
		if err != nil {
			return nil, fmt.Errorf("load trade memory: %w", err)
		}
	}

	reviews, err := e.AIReviewer.Review(ctx, AIReviewRequest{
		Signals:          signals,
		FactorSnapshot:   req.SignalRequest.FactorSnapshot,
		MarketContext:    marketContext,
		RelevantMemory:   lessons,
		CurrentPositions: req.SignalRequest.Positions,
		DrawdownAlerts:   req.SignalRequest.DrawdownAlerts,
		TradingStats:     req.SignalRequest.TradingStats,
		RecentOrders:     req.SignalRequest.RecentOrders,
	})
	if err != nil {
		return nil, fmt.Errorf("AI review: %w", err)
	}
	if err := validateAIReviews(signals, reviews); err != nil {
		return nil, fmt.Errorf("AI review response: %w", err)
	}

	risk, err := e.RiskGate.Validate(ctx, RiskGateRequest{
		Account:       req.SignalRequest.Account,
		Positions:     req.SignalRequest.Positions,
		Signals:       signals,
		Reviews:       reviews,
		MarketContext: marketContext,
	})
	if err != nil {
		return nil, fmt.Errorf("risk gate: %w", err)
	}

	return &TradingEngineResult{
		Signals:           signals,
		SuppressedSignals: suppressedSignals,
		SetupEvaluations:  setupEvaluations,
		RuleEvaluations:   ruleEvaluations,
		MarketContext:     marketContext,
		Reviews:           reviews,
		Risk:              risk,
		Memory:            lessons,
	}, nil
}

func suppressOpenSignalsWithPositions(signals []CandidateSignal, positions []PositionInfo) ([]CandidateSignal, []SuppressedSignal) {
	openSymbols := map[string]bool{}
	for _, position := range positions {
		if normalizedPositionSide(position.Side) == "" {
			continue
		}
		if symbol := market.Normalize(position.Symbol); symbol != "" {
			openSymbols[symbol] = true
		}
	}
	candidates := make([]CandidateSignal, 0, len(signals))
	suppressed := []SuppressedSignal{}
	for _, signal := range signals {
		if (signal.Action == "open_long" || signal.Action == "open_short") && openSymbols[market.Normalize(signal.Symbol)] {
			suppressed = append(suppressed, SuppressedSignal{
				Signal: signal,
				Reason: "position_already_open_close_or_hold_first",
			})
			continue
		}
		candidates = append(candidates, signal)
	}

	openIndexes := map[string][]int{}
	for i, signal := range candidates {
		if signal.Action != "open_long" && signal.Action != "open_short" {
			continue
		}
		symbol := market.Normalize(signal.Symbol)
		openIndexes[symbol] = append(openIndexes[symbol], i)
	}
	suppressionReason := map[int]string{}
	for _, indexes := range openIndexes {
		if len(indexes) < 2 {
			continue
		}
		action := candidates[indexes[0]].Action
		conflict := false
		for _, index := range indexes[1:] {
			if candidates[index].Action != action {
				conflict = true
				break
			}
		}
		if conflict {
			for _, index := range indexes {
				suppressionReason[index] = "conflicting_open_signals_same_cycle"
			}
			continue
		}

		winner := indexes[0]
		for _, index := range indexes[1:] {
			if candidates[index].Confidence > candidates[winner].Confidence {
				winner = index
			}
		}
		for _, index := range indexes {
			if index != winner {
				suppressionReason[index] = "duplicate_open_signal_same_cycle"
			}
		}
	}

	active := make([]CandidateSignal, 0, len(candidates))
	for i, signal := range candidates {
		if reason := suppressionReason[i]; reason != "" {
			suppressed = append(suppressed, SuppressedSignal{Signal: signal, Reason: reason})
			continue
		}
		active = append(active, signal)
	}
	return active, suppressed
}

func validateAIReviews(signals []CandidateSignal, reviews []AIReviewDecision) error {
	expected := make(map[string]bool, len(signals))
	for _, signal := range signals {
		if signal.ID == "" {
			return fmt.Errorf("candidate signal has no id")
		}
		expected[signal.ID] = true
	}
	seen := make(map[string]bool, len(reviews))
	for _, review := range reviews {
		if !expected[review.SignalID] {
			return fmt.Errorf("review references unknown signal_id %q", review.SignalID)
		}
		if review.Status != "pass" && review.Status != "warn" && review.Status != "reject" {
			return fmt.Errorf("review for signal_id %q has invalid status %q", review.SignalID, review.Status)
		}
		if seen[review.SignalID] {
			return fmt.Errorf("duplicate review for signal_id %q", review.SignalID)
		}
		seen[review.SignalID] = true
	}
	for signalID := range expected {
		if !seen[signalID] {
			return fmt.Errorf("missing review for signal_id %q", signalID)
		}
	}
	return nil
}

func signalSymbols(signals []CandidateSignal) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(signals))
	for _, s := range signals {
		if s.Symbol == "" || seen[s.Symbol] {
			continue
		}
		seen[s.Symbol] = true
		out = append(out, s.Symbol)
	}
	return out
}

func BuildSignalCalibrationSamples(req SignalRequest, result *TradingEngineResult) []SignalCalibrationSample {
	if result == nil {
		return nil
	}
	reviewBySignal := map[string]AIReviewDecision{}
	for _, review := range result.Reviews {
		reviewBySignal[review.SignalID] = review
	}
	riskBySignal := map[string]RiskRejectedSignal{}
	approved := map[string]bool{}
	if result.Risk != nil {
		for _, signal := range result.Risk.Approved {
			approved[signal.ID] = true
		}
		for _, rejected := range result.Risk.Rejected {
			riskBySignal[rejected.SignalID] = rejected
		}
	}
	signalsBySetupKey := map[string]CandidateSignal{}
	suppressedBySetupKey := map[string]SuppressedSignal{}
	seenSignalIDs := map[string]bool{}
	for _, signal := range result.Signals {
		key := calibrationSetupKey(signal.Symbol, signal.Action, signal.Evidence)
		if key != "" {
			signalsBySetupKey[key] = signal
		}
	}
	for _, suppressed := range result.SuppressedSignals {
		key := calibrationSetupKey(suppressed.Signal.Symbol, suppressed.Signal.Action, suppressed.Signal.Evidence)
		if key != "" {
			suppressedBySetupKey[key] = suppressed
		}
	}

	samples := make([]SignalCalibrationSample, 0, len(result.SetupEvaluations)+len(result.Signals))
	for _, trace := range result.SetupEvaluations {
		traceCopy := trace
		sample := SignalCalibrationSample{
			SampleKind:             "setup",
			Symbol:                 trace.Symbol,
			Setup:                  trace.Setup,
			SymbolRegime:           trace.Route.Regime,
			Action:                 trace.Action,
			Eligible:               trace.Eligible,
			Timeframe:              trace.Timeframes.Primary,
			PrimaryTimeframe:       trace.Timeframes.Primary,
			EntryTimeframe:         trace.Timeframes.Entry,
			ConfirmationTimeframes: append([]string(nil), trace.Timeframes.Confirmations...),
			PrimaryScore:           trace.Primary.Score,
			EntryScore:             trace.Entry.Score,
			FactorSnapshot:         req.FactorSnapshot[trace.Symbol],
			KlineWindows:           calibrationKlineWindows(req, trace.Symbol),
			SetupTrace:             &traceCopy,
			MarketContext:          result.MarketContext,
			AsOf:                   calibrationAsOf(req, trace.Symbol),
		}
		if trace.Eligible {
			sample.RiskStatus = "candidate_pending"
		} else {
			sample.RiskStatus = "no_signal"
		}
		key := calibrationTraceKey(trace)
		if signal, ok := signalsBySetupKey[key]; ok {
			enrichCalibrationSampleFromSignal(&sample, signal, reviewBySignal, approved, riskBySignal)
			seenSignalIDs[signal.ID] = true
		} else if suppressed, ok := suppressedBySetupKey[key]; ok {
			enrichCalibrationSampleFromSignal(&sample, suppressed.Signal, reviewBySignal, approved, riskBySignal)
			sample.ReviewStatus = "not_requested"
			sample.RiskStatus = "position_open"
			sample.RiskReason = suppressed.Reason
		}
		samples = append(samples, sample)
	}
	for _, signal := range result.Signals {
		if seenSignalIDs[signal.ID] {
			continue
		}
		signalCopy := signal
		sample := SignalCalibrationSample{
			SampleKind:      "signal",
			Symbol:          signal.Symbol,
			Action:          signal.Action,
			Eligible:        true,
			Timeframe:       signal.Timeframe,
			EntryPrice:      signal.EntryPrice,
			Confidence:      signal.Confidence,
			SignalID:        signal.ID,
			RuleID:          signal.RuleID,
			StrategyVersion: signal.StrategyVersion,
			FactorSnapshot:  req.FactorSnapshot[signal.Symbol],
			KlineWindows:    calibrationKlineWindows(req, signal.Symbol),
			Signal:          &signalCopy,
			MarketContext:   result.MarketContext,
			AsOf:            calibrationAsOf(req, signal.Symbol),
		}
		if evidence, ok := signal.Evidence["scoring"].(ScoringEvaluationTrace); ok {
			evidenceCopy := evidence
			sample.Score = evidence.Score
			sample.EvidenceTrace = &evidenceCopy
		}
		enrichCalibrationSampleFromSignal(&sample, signal, reviewBySignal, approved, riskBySignal)
		samples = append(samples, sample)
	}
	return samples
}

func enrichCalibrationSampleFromSignal(sample *SignalCalibrationSample, signal CandidateSignal, reviews map[string]AIReviewDecision, approved map[string]bool, rejected map[string]RiskRejectedSignal) {
	if sample == nil {
		return
	}
	signalCopy := signal
	sample.Signal = &signalCopy
	sample.SignalID = signal.ID
	sample.RuleID = signal.RuleID
	sample.StrategyVersion = signal.StrategyVersion
	sample.EntryPrice = signal.EntryPrice
	sample.Confidence = signal.Confidence
	if sample.Timeframe == "" {
		sample.Timeframe = signal.Timeframe
	}
	if setup, ok := signal.Evidence["setup"].(SetupEvaluationTrace); ok {
		setupCopy := setup
		sample.SetupTrace = &setupCopy
		sample.Setup = setup.Setup
		sample.SymbolRegime = setup.Route.Regime
		sample.PrimaryScore = setup.Primary.Score
		sample.EntryScore = setup.Entry.Score
		sample.PrimaryTimeframe = setup.Timeframes.Primary
		sample.EntryTimeframe = setup.Timeframes.Entry
		sample.ConfirmationTimeframes = append([]string(nil), setup.Timeframes.Confirmations...)
	}
	if evidence, ok := signal.Evidence["scoring"].(ScoringEvaluationTrace); ok {
		evidenceCopy := evidence
		sample.EvidenceTrace = &evidenceCopy
		sample.Score = evidence.Score
	}
	if review, ok := reviews[signal.ID]; ok {
		sample.ReviewStatus = review.Status
		sample.ReviewReasons = append([]string(nil), review.Reasons...)
	}
	switch {
	case approved[signal.ID]:
		sample.RiskStatus = "approved"
	case rejected[signal.ID].SignalID != "":
		sample.RiskStatus = "risk_rejected"
		sample.RiskReason = rejected[signal.ID].Reason
	case sample.ReviewStatus != "":
		sample.RiskStatus = "reviewed_not_approved"
	default:
		sample.RiskStatus = "candidate_pending"
	}
}

func calibrationTraceKey(trace SetupEvaluationTrace) string {
	return trace.Symbol + "|" + trace.Action + "|" + trace.Setup
}

func calibrationSetupKey(symbol, action string, evidence map[string]interface{}) string {
	setup, ok := evidence["setup"].(SetupEvaluationTrace)
	if !ok {
		return ""
	}
	return symbol + "|" + action + "|" + setup.Setup
}

func calibrationAsOf(req SignalRequest, symbol string) time.Time {
	if snapshot := req.FactorSnapshot[symbol]; snapshot != nil && !snapshot.AsOf.IsZero() {
		return snapshot.AsOf
	}
	if !req.Now.IsZero() {
		return req.Now.UTC()
	}
	return time.Now().UTC()
}

func calibrationKlineWindows(req SignalRequest, symbol string) map[string][]market.Kline {
	if req.KlineWindows == nil {
		return nil
	}
	windows := req.KlineWindows[symbol]
	if len(windows) == 0 {
		return nil
	}
	out := make(map[string][]market.Kline, len(windows))
	for timeframe, klines := range windows {
		if timeframe == "" || len(klines) == 0 {
			continue
		}
		out[timeframe] = append([]market.Kline(nil), klines...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
