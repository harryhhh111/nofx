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
	Kind      string  `json:"kind"` // indicator, external_factor, structure, literal
	Name      string  `json:"name,omitempty"`
	Timeframe string  `json:"timeframe,omitempty"`
	Period    int     `json:"period,omitempty"`
	Field     string  `json:"field,omitempty"`
	Value     float64 `json:"value,omitempty"`
}

type StrategyCompileRequest struct {
	StrategyID      string `json:"strategy_id"`
	StrategyVersion string `json:"strategy_version"`
	Prompt          string `json:"prompt,omitempty"`
}

type StrategyCompileResult struct {
	Rules    []StrategyRule `json:"rules"`
	Warnings []string       `json:"warnings,omitempty"`
	Errors   []string       `json:"errors,omitempty"`
}

type StrategyCompiler interface {
	Compile(ctx context.Context, req StrategyCompileRequest) (*StrategyCompileResult, error)
}

// SignalRequest is the deterministic input to the signal engine.
type SignalRequest struct {
	Account        AccountInfo                       `json:"account"`
	Positions      []PositionInfo                    `json:"positions"`
	Candidates     []CandidateCoin                   `json:"candidates"`
	Rules          []StrategyRule                    `json:"rules"`
	FactorSnapshot map[string]*market.FactorSnapshot `json:"factor_snapshot"`
	Now            time.Time                         `json:"now"`
}

type CandidateSignal struct {
	ID              string                 `json:"id"`
	RuleID          string                 `json:"rule_id"`
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
	RelevantMemory   []TradeLesson                     `json:"relevant_memory,omitempty"`
	CurrentPositions []PositionInfo                    `json:"current_positions,omitempty"`
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
	Account   AccountInfo        `json:"account"`
	Positions []PositionInfo     `json:"positions"`
	Signals   []CandidateSignal  `json:"signals"`
	Reviews   []AIReviewDecision `json:"reviews"`
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
	Signal      CandidateSignal   `json:"signal"`
	Review      AIReviewDecision  `json:"review"`
	Result      string            `json:"result,omitempty"`
	OutcomePnL  float64           `json:"outcome_pnl,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	FactorTrace map[string]string `json:"factor_trace,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
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

type TradingEngine struct {
	SignalEngine SignalEngine
	AIReviewer   AIReviewer
	RiskGate     RiskGate
	Memory       TradeMemoryStore
}

type TradingEngineRequest struct {
	SignalRequest SignalRequest `json:"signal_request"`
}

type TradingEngineResult struct {
	Signals []CandidateSignal  `json:"signals"`
	Reviews []AIReviewDecision `json:"reviews"`
	Risk    *RiskGateResult    `json:"risk"`
	Memory  []TradeLesson      `json:"memory,omitempty"`
}

func NewTradingEngine(signalEngine SignalEngine, reviewer AIReviewer, riskGate RiskGate, memory TradeMemoryStore) *TradingEngine {
	return &TradingEngine{
		SignalEngine: signalEngine,
		AIReviewer:   reviewer,
		RiskGate:     riskGate,
		Memory:       memory,
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
	if len(signals) == 0 {
		return &TradingEngineResult{Signals: signals, Reviews: []AIReviewDecision{}, Risk: &RiskGateResult{}}, nil
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
		RelevantMemory:   lessons,
		CurrentPositions: req.SignalRequest.Positions,
	})
	if err != nil {
		return nil, fmt.Errorf("AI review: %w", err)
	}

	risk, err := e.RiskGate.Validate(ctx, RiskGateRequest{
		Account:   req.SignalRequest.Account,
		Positions: req.SignalRequest.Positions,
		Signals:   signals,
		Reviews:   reviews,
	})
	if err != nil {
		return nil, fmt.Errorf("risk gate: %w", err)
	}

	return &TradingEngineResult{
		Signals: signals,
		Reviews: reviews,
		Risk:    risk,
		Memory:  lessons,
	}, nil
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
