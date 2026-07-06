package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"nofx/market"
	"nofx/mcp"
	"strings"
	"time"
)

// LLMTradingEngine is the AI review implementation for the new trading flow.
// It deliberately depends only on structured snapshots and candidate signals.
// It must not calculate indicators or inspect raw K-line implementation details.
type LLMTradingEngine struct {
	client mcp.AIClient
}

func NewLLMTradingEngine(client mcp.AIClient) *LLMTradingEngine {
	return &LLMTradingEngine{client: client}
}

func (e *LLMTradingEngine) Review(ctx context.Context, req AIReviewRequest) ([]AIReviewDecision, error) {
	if e == nil || e.client == nil {
		return nil, fmt.Errorf("LLM trading engine requires an AI client")
	}

	systemPrompt := buildLLMReviewSystemPrompt()
	userPrompt, err := buildLLMReviewUserPrompt(req)
	if err != nil {
		return nil, err
	}

	type response struct {
		text string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		text, err := e.client.CallWithMessages(systemPrompt, userPrompt)
		done <- response{text: text, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-done:
		if resp.err != nil {
			return nil, fmt.Errorf("LLM review call failed: %w", resp.err)
		}
		if strings.Contains(resp.text, "<reviews>") && !strings.Contains(resp.text, "</reviews>") {
			return nil, fmt.Errorf("AI review response appears truncated: missing </reviews> tag")
		}
		reviews, err := parseLLMReviewResponse(resp.text)
		if err != nil {
			return nil, err
		}
		return reviews, nil
	}
}

func buildLLMReviewSystemPrompt() string {
	return strings.TrimSpace(`
You are the AI review layer of a deterministic trading system.

Your role:
- Review candidate trading signals produced by the program.
- Explain risks, conflicts, and missing evidence.
- Return pass, warn, or reject for each signal.

Strict boundaries:
- Do not calculate indicators.
- Do not infer from raw K-lines.
- Do not choose Fibonacci anchors or support/resistance manually.
- Only use the provided structured factor snapshots, candidate signals, current positions, and memory.
- Market context can warn or reject a signal, but must not create new trades or rewrite strategy parameters.
- If a required factor is unavailable, treat it as unavailable, not zero.
- For open signals, use evidence.protective_levels.risk_reward as the authoritative structural risk/reward.
- candidate.stop_loss is the execution stop with ATR buffer; do not use it to reduce structural risk/reward.
- evidence.protective_levels.target_risk_reward is the configured minimum RR, not an execution percentage placeholder.
- For close signals that are profit-taking or elective exits, use current_positions.net_pnl as the profitability source after fees. Reject profit-taking closes when net_pnl <= 0, but do not block stop-loss, liquidation-risk, or thesis-invalidated risk exits only because net_pnl is negative.

Output only JSON inside <reviews> tags:
<reviews>
[
  {
    "signal_id": "string",
    "status": "pass|warn|reject",
    "reasons": ["short reason"],
    "summary": "brief review summary"
  }
]
</reviews>
`)
}

func buildLLMReviewUserPrompt(req AIReviewRequest) (string, error) {
	payload := struct {
		GeneratedAt       time.Time                        `json:"generated_at"`
		Signals           []CandidateSignal                `json:"signals"`
		MarketContext     *MarketContext                   `json:"market_context,omitempty"`
		FactorSummary     map[string]compactFactorSnapshot `json:"factor_summary"`
		RelevantMemory    []TradeLesson                    `json:"relevant_memory,omitempty"`
		CurrentPositions  []PositionInfo                   `json:"current_positions,omitempty"`
		ReviewInstruction string                           `json:"review_instruction"`
	}{
		GeneratedAt:       time.Now().UTC(),
		Signals:           req.Signals,
		MarketContext:     req.MarketContext,
		FactorSummary:     compactReviewFactorSnapshots(req.FactorSnapshot, req.Signals, req.CurrentPositions),
		RelevantMemory:    req.RelevantMemory,
		CurrentPositions:  req.CurrentPositions,
		ReviewInstruction: "Review each candidate signal. Return one review per signal. Do not create new trades. For open signals, use evidence.protective_levels.risk_reward as the structural RR; candidate.stop_loss includes ATR execution buffer. For profit-taking close signals, use current_positions.net_pnl after accumulated and estimated closing fees; do not treat gross unrealized_pnl as profit if net_pnl <= 0.",
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal LLM review payload: %w", err)
	}
	return string(data), nil
}

type compactFactorSnapshot struct {
	Symbol     string                                `json:"symbol"`
	RiskFlags  []string                              `json:"risk_flags,omitempty"`
	Notes      []string                              `json:"notes,omitempty"`
	Technical  map[string][]compactIndicatorPoint    `json:"technical,omitempty"`
	Structures map[string][]compactStructureSnapshot `json:"structures,omitempty"`
	External   map[string]compactExternalFactor      `json:"external,omitempty"`
}

type compactIndicatorPoint struct {
	Name      string             `json:"name"`
	Timeframe string             `json:"timeframe"`
	Period    int                `json:"period,omitempty"`
	Params    map[string]float64 `json:"params,omitempty"`
	Value     float64            `json:"value"`
}

type compactStructureSnapshot struct {
	Name         string             `json:"name"`
	Timeframe    string             `json:"timeframe"`
	Valid        bool               `json:"valid"`
	Reason       string             `json:"reason,omitempty"`
	Direction    string             `json:"direction,omitempty"`
	Phase        string             `json:"phase,omitempty"`
	Setup        string             `json:"setup,omitempty"`
	Signals      []string           `json:"signals,omitempty"`
	InvalidPrice float64            `json:"invalid_price,omitempty"`
	KeyLevels    map[string]float64 `json:"key_levels,omitempty"`
	Confirmed    bool               `json:"confirmed"`
}

type compactExternalFactor struct {
	Name      string  `json:"name"`
	Source    string  `json:"source"`
	Timeframe string  `json:"timeframe,omitempty"`
	Value     float64 `json:"value,omitempty"`
	State     string  `json:"state,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Available bool    `json:"available"`
	CostClass string  `json:"cost_class,omitempty"`
}

func compactReviewFactorSnapshots(snapshots map[string]*market.FactorSnapshot, signals []CandidateSignal, positions []PositionInfo) map[string]compactFactorSnapshot {
	out := map[string]compactFactorSnapshot{}
	if len(snapshots) == 0 {
		return out
	}
	symbols := reviewPayloadSymbols(signals, positions)
	if len(symbols) == 0 {
		for symbol := range snapshots {
			symbols = append(symbols, symbol)
		}
	}
	for _, symbol := range symbols {
		snapshot := snapshots[symbol]
		if snapshot == nil {
			continue
		}
		out[symbol] = compactFactorSnapshot{
			Symbol:     snapshot.Symbol,
			RiskFlags:  snapshot.RiskFlags,
			Notes:      snapshot.Notes,
			Technical:  compactTechnical(snapshot.Technical),
			Structures: compactStructures(snapshot.Structures),
			External:   compactExternal(snapshot.External),
		}
	}
	return out
}

func reviewPayloadSymbols(signals []CandidateSignal, positions []PositionInfo) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, signal := range signals {
		if signal.Symbol != "" && !seen[signal.Symbol] {
			seen[signal.Symbol] = true
			out = append(out, signal.Symbol)
		}
	}
	for _, position := range positions {
		if position.Symbol != "" && !seen[position.Symbol] {
			seen[position.Symbol] = true
			out = append(out, position.Symbol)
		}
	}
	return out
}

func compactTechnical(input map[string][]market.IndicatorPoint) map[string][]compactIndicatorPoint {
	if len(input) == 0 {
		return nil
	}
	out := map[string][]compactIndicatorPoint{}
	for group, points := range input {
		for _, point := range points {
			out[group] = append(out[group], compactIndicatorPoint{
				Name:      point.Name,
				Timeframe: point.Timeframe,
				Period:    point.Period,
				Params:    point.Params,
				Value:     point.Value,
			})
		}
	}
	return out
}

func compactStructures(input map[string][]market.StructureSnapshot) map[string][]compactStructureSnapshot {
	if len(input) == 0 {
		return nil
	}
	out := map[string][]compactStructureSnapshot{}
	for group, snapshots := range input {
		for _, snapshot := range snapshots {
			out[group] = append(out[group], compactStructureSnapshot{
				Name:         snapshot.Name,
				Timeframe:    snapshot.Timeframe,
				Valid:        snapshot.Valid,
				Reason:       snapshot.Reason,
				Direction:    snapshot.Direction,
				Phase:        snapshot.Phase,
				Setup:        snapshot.Setup,
				Signals:      snapshot.Signals,
				InvalidPrice: snapshot.InvalidPrice,
				KeyLevels:    snapshot.KeyLevels,
				Confirmed:    snapshot.Confirmed,
			})
		}
	}
	return out
}

func compactExternal(input map[string]market.ExternalFactor) map[string]compactExternalFactor {
	if len(input) == 0 {
		return nil
	}
	out := map[string]compactExternalFactor{}
	for name, factor := range input {
		out[name] = compactExternalFactor{
			Name:      factor.Name,
			Source:    factor.Source,
			Timeframe: factor.Timeframe,
			Value:     factor.Value,
			State:     factor.State,
			Score:     factor.Score,
			Available: factor.Available,
			CostClass: factor.CostClass,
		}
	}
	return out
}

func parseLLMReviewResponse(text string) ([]AIReviewDecision, error) {
	body := strings.TrimSpace(text)
	if start := strings.Index(body, "<reviews>"); start >= 0 {
		body = body[start+len("<reviews>"):]
	}
	if end := strings.Index(body, "</reviews>"); end >= 0 {
		body = body[:end]
	}
	body = strings.TrimSpace(body)

	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```json")
		body = strings.TrimPrefix(body, "```")
		body = strings.TrimSuffix(body, "```")
		body = strings.TrimSpace(body)
	}

	var reviews []AIReviewDecision
	if err := json.Unmarshal([]byte(body), &reviews); err != nil {
		return nil, fmt.Errorf("parse LLM review response: %w", err)
	}
	for i := range reviews {
		reviews[i].Status = strings.ToLower(strings.TrimSpace(reviews[i].Status))
		if reviews[i].Status == "" {
			reviews[i].Status = "warn"
		}
		switch reviews[i].Status {
		case "pass", "warn", "reject":
		default:
			reviews[i].Status = "warn"
			reviews[i].Reasons = append(reviews[i].Reasons, "invalid_review_status_normalized_to_warn")
		}
	}
	return reviews, nil
}
