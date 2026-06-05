package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"nofx/mcp"
	"nofx/store"
	"strings"
	"time"
)

const minTradeMemoryQuality = 0.3
const minTradeMemoryConfidence = 0.3

type StoreTradeMemory struct {
	store    *store.Store
	traderID string
}

func NewStoreTradeMemory(st *store.Store, traderID string) *StoreTradeMemory {
	return &StoreTradeMemory{store: st, traderID: strings.TrimSpace(traderID)}
}

func (m *StoreTradeMemory) FindRelevant(ctx context.Context, symbols []string, limit int) ([]TradeLesson, error) {
	if m == nil || m.store == nil {
		return nil, nil
	}
	records, err := m.store.TradeMemory().FindRelevant(m.traderID, symbols, limit)
	if err != nil {
		return nil, err
	}
	lessons := make([]TradeLesson, 0, len(records))
	for _, record := range records {
		lessons = append(lessons, TradeLesson{
			ID:         fmt.Sprintf("%d", record.ID),
			Scope:      record.Scope,
			Summary:    record.Summary,
			Evidence:   record.Evidence,
			Confidence: record.Confidence,
			UpdatedAt:  record.UpdatedAt,
		})
	}
	return lessons, nil
}

func (m *StoreTradeMemory) Record(ctx context.Context, record TradeMemoryRecord) error {
	if m == nil || m.store == nil {
		return nil
	}
	if strings.TrimSpace(record.Summary) == "" {
		return fmt.Errorf("trade memory summary is required")
	}
	if record.QualityScore < minTradeMemoryQuality {
		return fmt.Errorf("trade memory quality_score %.2f below minimum %.2f", record.QualityScore, minTradeMemoryQuality)
	}
	if record.Confidence < minTradeMemoryConfidence {
		return fmt.Errorf("trade memory confidence %.2f below minimum %.2f", record.Confidence, minTradeMemoryConfidence)
	}
	traderID := strings.TrimSpace(record.TraderID)
	if traderID == "" {
		traderID = m.traderID
	}
	lessonsJSON, _ := json.Marshal(record.Lessons)
	tagsJSON, _ := json.Marshal(record.Tags)
	return m.store.TradeMemory().Create(&store.TradeMemory{
		TraderID:        traderID,
		StrategyID:      record.StrategyID,
		StrategyVersion: record.StrategyVersion,
		Symbol:          record.Signal.Symbol,
		Side:            sideFromAction(record.Signal.Action),
		Action:          record.Signal.Action,
		Scope:           record.Scope,
		SourceType:      record.SourceType,
		SignalID:        record.Signal.ID,
		PositionID:      record.PositionID,
		Result:          record.Result,
		OutcomePnL:      record.OutcomePnL,
		OutcomePnLPct:   record.OutcomePnLPct,
		Summary:         record.Summary,
		Evidence:        record.Evidence,
		LessonsJSON:     string(lessonsJSON),
		TagsJSON:        string(tagsJSON),
		QualityScore:    record.QualityScore,
		Confidence:      record.Confidence,
		ExpiresAt:       record.ExpiresAt,
	})
}

type ClosedTradeOutcome struct {
	TraderID          string  `json:"trader_id"`
	StrategyID        string  `json:"strategy_id,omitempty"`
	StrategyVersion   string  `json:"strategy_version,omitempty"`
	SignalID          string  `json:"signal_id,omitempty"`
	RuleID            string  `json:"rule_id,omitempty"`
	Setup             string  `json:"setup,omitempty"`
	PositionID        int64   `json:"position_id"`
	Symbol            string  `json:"symbol"`
	Side              string  `json:"side"`
	EntryPrice        float64 `json:"entry_price"`
	ExitPrice         float64 `json:"exit_price"`
	Quantity          float64 `json:"quantity"`
	Leverage          int     `json:"leverage"`
	RealizedPnL       float64 `json:"realized_pnl"`
	RealizedPnLPct    float64 `json:"realized_pnl_pct"`
	Fee               float64 `json:"fee"`
	EntryTimeMs       int64   `json:"entry_time_ms"`
	ExitTimeMs        int64   `json:"exit_time_ms"`
	HoldDuration      string  `json:"hold_duration,omitempty"`
	CloseReason       string  `json:"close_reason,omitempty"`
	OpeningReasoning  string  `json:"opening_reasoning,omitempty"`
	LastReviewSummary string  `json:"last_review_summary,omitempty"`
}

type TradeMemorySummary struct {
	Result       string   `json:"result"`
	Summary      string   `json:"summary"`
	Evidence     string   `json:"evidence"`
	Lessons      []string `json:"lessons,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	QualityScore float64  `json:"quality_score"`
	Confidence   float64  `json:"confidence"`
	ExpiresDays  int      `json:"expires_days,omitempty"`
}

type TradeMemorySummarizer interface {
	SummarizeClosedTrade(ctx context.Context, outcome ClosedTradeOutcome) (*TradeMemorySummary, error)
}

type LLMTradeMemorySummarizer struct {
	client mcp.AIClient
}

func NewLLMTradeMemorySummarizer(client mcp.AIClient) *LLMTradeMemorySummarizer {
	return &LLMTradeMemorySummarizer{client: client}
}

func (s *LLMTradeMemorySummarizer) SummarizeClosedTrade(ctx context.Context, outcome ClosedTradeOutcome) (*TradeMemorySummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("trade memory summarizer requires an AI client")
	}
	payload, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal trade outcome: %w", err)
	}
	systemPrompt := strings.TrimSpace(`
You write compact post-trade memory for a structured crypto trading system.

Rules:
- Summarize only the provided closed trade outcome.
- Do not suggest live parameter changes.
- Do not invent market facts that are not present.
- Extract reusable lessons that can help future AI risk review.
- Keep the summary short and operational.

Output only JSON inside <trade_memory> tags:
<trade_memory>
{
  "result": "win|loss|breakeven",
  "summary": "one short paragraph",
  "evidence": "specific evidence from the outcome",
  "lessons": ["short reusable lesson"],
  "tags": ["trend|chop|risk|execution|entry|exit"],
  "quality_score": 0.0,
  "confidence": 0.0,
  "expires_days": 90
}
</trade_memory>
`)
	userPrompt := string(payload)

	type response struct {
		text string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		text, err := s.client.CallWithMessages(systemPrompt, userPrompt)
		done <- response{text: text, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-done:
		if resp.err != nil {
			return nil, fmt.Errorf("trade memory LLM call failed: %w", resp.err)
		}
		return parseTradeMemorySummary(resp.text)
	}
}

func parseTradeMemorySummary(text string) (*TradeMemorySummary, error) {
	body := strings.TrimSpace(text)
	if start := strings.Index(body, "<trade_memory>"); start >= 0 {
		body = body[start+len("<trade_memory>"):]
	}
	if end := strings.Index(body, "</trade_memory>"); end >= 0 {
		body = body[:end]
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```json")
		body = strings.TrimPrefix(body, "```")
		body = strings.TrimSuffix(body, "```")
		body = strings.TrimSpace(body)
	}

	var summary TradeMemorySummary
	if err := json.Unmarshal([]byte(body), &summary); err != nil {
		return nil, fmt.Errorf("parse trade memory summary: %w", err)
	}
	summary.Result = strings.ToLower(strings.TrimSpace(summary.Result))
	switch summary.Result {
	case "win", "loss", "breakeven":
	default:
		return nil, fmt.Errorf("invalid trade memory result %q", summary.Result)
	}
	if strings.TrimSpace(summary.Summary) == "" {
		return nil, fmt.Errorf("trade memory summary is required")
	}
	if summary.QualityScore < 0 || summary.QualityScore > 1 {
		return nil, fmt.Errorf("trade memory quality_score must be between 0 and 1")
	}
	if summary.Confidence < 0 || summary.Confidence > 1 {
		return nil, fmt.Errorf("trade memory confidence must be between 0 and 1")
	}
	return &summary, nil
}

func BuildTradeMemoryRecord(outcome ClosedTradeOutcome, summary TradeMemorySummary) TradeMemoryRecord {
	expiresAt := (*time.Time)(nil)
	if summary.ExpiresDays > 0 {
		t := time.Now().UTC().AddDate(0, 0, summary.ExpiresDays)
		expiresAt = &t
	}
	action := "close_long"
	if strings.EqualFold(outcome.Side, "SHORT") || strings.EqualFold(outcome.Side, "short") {
		action = "close_short"
	}
	return TradeMemoryRecord{
		TraderID:        outcome.TraderID,
		StrategyID:      outcome.StrategyID,
		StrategyVersion: outcome.StrategyVersion,
		PositionID:      outcome.PositionID,
		Signal: CandidateSignal{
			ID:              outcome.SignalID,
			RuleID:          outcome.RuleID,
			Setup:           outcome.Setup,
			StrategyVersion: outcome.StrategyVersion,
			Symbol:          outcome.Symbol,
			Action:          action,
		},
		Result:        summary.Result,
		OutcomePnL:    outcome.RealizedPnL,
		OutcomePnLPct: outcome.RealizedPnLPct,
		Summary:       summary.Summary,
		Evidence:      summary.Evidence,
		Lessons:       summary.Lessons,
		Tags:          summary.Tags,
		QualityScore:  summary.QualityScore,
		Confidence:    summary.Confidence,
		Scope:         "symbol",
		SourceType:    "trade_outcome",
		ExpiresAt:     expiresAt,
		CreatedAt:     time.Now().UTC(),
	}
}

func sideFromAction(action string) string {
	switch action {
	case "open_long", "close_long":
		return "LONG"
	case "open_short", "close_short":
		return "SHORT"
	default:
		return ""
	}
}
