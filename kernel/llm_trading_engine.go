package kernel

import (
	"context"
	"encoding/json"
	"fmt"
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
		GeneratedAt       time.Time         `json:"generated_at"`
		Signals           []CandidateSignal `json:"signals"`
		MarketContext     *MarketContext    `json:"market_context,omitempty"`
		FactorSnapshot    interface{}       `json:"factor_snapshot"`
		RelevantMemory    []TradeLesson     `json:"relevant_memory,omitempty"`
		CurrentPositions  []PositionInfo    `json:"current_positions,omitempty"`
		ReviewInstruction string            `json:"review_instruction"`
	}{
		GeneratedAt:       time.Now().UTC(),
		Signals:           req.Signals,
		MarketContext:     req.MarketContext,
		FactorSnapshot:    req.FactorSnapshot,
		RelevantMemory:    req.RelevantMemory,
		CurrentPositions:  req.CurrentPositions,
		ReviewInstruction: "Review each candidate signal. Return one review per signal. Do not create new trades.",
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal LLM review payload: %w", err)
	}
	return string(data), nil
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
