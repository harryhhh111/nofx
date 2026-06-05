package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"nofx/mcp"
	"strings"
	"time"
)

type StrategyEvolutionRequest struct {
	StrategyID    string                 `json:"strategy_id"`
	BaseVersion   string                 `json:"base_version,omitempty"`
	Trigger       string                 `json:"trigger"`
	CurrentConfig interface{}            `json:"current_config"`
	MarketContext *MarketContext         `json:"market_context,omitempty"`
	Performance   map[string]interface{} `json:"performance,omitempty"`
	Notes         string                 `json:"notes,omitempty"`
	RequestedAt   time.Time              `json:"requested_at"`
}

type StrategyEvolutionProposal struct {
	ProposalID          string                 `json:"proposal_id"`
	StrategyID          string                 `json:"strategy_id"`
	BaseVersion         string                 `json:"base_version,omitempty"`
	ProposedVersion     string                 `json:"proposed_version"`
	Trigger             string                 `json:"trigger"`
	Summary             string                 `json:"summary"`
	ChangeReasons       []string               `json:"change_reasons,omitempty"`
	ParameterChanges    []StrategyParamChange  `json:"parameter_changes,omitempty"`
	ExpectedImpact      string                 `json:"expected_impact,omitempty"`
	Risks               []string               `json:"risks,omitempty"`
	ProposedConfigPatch map[string]interface{} `json:"proposed_config_patch,omitempty"`
	RequiresApproval    bool                   `json:"requires_approval"`
	CreatedAt           time.Time              `json:"created_at"`
}

type StrategyParamChange struct {
	Path     string      `json:"path"`
	OldValue interface{} `json:"old_value,omitempty"`
	NewValue interface{} `json:"new_value,omitempty"`
	Reason   string      `json:"reason,omitempty"`
}

type StrategyEvolver interface {
	Propose(ctx context.Context, req StrategyEvolutionRequest) (*StrategyEvolutionProposal, error)
}

type LLMStrategyEvolver struct {
	client mcp.AIClient
}

func NewLLMStrategyEvolver(client mcp.AIClient) *LLMStrategyEvolver {
	return &LLMStrategyEvolver{client: client}
}

func (e *LLMStrategyEvolver) Propose(ctx context.Context, req StrategyEvolutionRequest) (*StrategyEvolutionProposal, error) {
	if e == nil || e.client == nil {
		return nil, fmt.Errorf("strategy evolver requires an AI client")
	}
	if strings.TrimSpace(req.StrategyID) == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}
	if strings.TrimSpace(req.Trigger) == "" {
		return nil, fmt.Errorf("trigger is required")
	}
	if req.CurrentConfig == nil {
		return nil, fmt.Errorf("current_config is required")
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}

	systemPrompt := buildStrategyEvolverSystemPrompt()
	userPrompt, err := buildStrategyEvolverUserPrompt(req)
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
			return nil, fmt.Errorf("strategy evolution call failed: %w", resp.err)
		}
		proposal, err := parseStrategyEvolutionProposal(resp.text)
		if err != nil {
			return nil, err
		}
		if err := validateStrategyEvolutionProposal(proposal, req); err != nil {
			return nil, err
		}
		return proposal, nil
	}
}

func buildStrategyEvolverSystemPrompt() string {
	return strings.TrimSpace(`
You propose user-requested, versioned improvements for a deterministic trading strategy.

Strict boundaries:
- Generate a proposal only. Do not claim it is saved, active, or applied.
- Do not change live strategy parameters silently.
- Do not suggest that strategy evolution should happen automatically; the user must explicitly request and approve changes.
	- Do not remove hard risk controls.
	- Prefer small, auditable parameter changes.
	- Base optimization on provided performance, calibration setup_stats, recent trades, and market_context. If historical trade evidence is missing or too small, say so and keep proposed_config_patch conservative or empty.
	- Treat performance.calibration.quality_gate and enough_outcomes as evidence gates. Do not recommend threshold or factor-weight changes from weak sample sizes.
	- Use setup_stats to identify which executable setup underperforms; avoid broad global changes when only one setup has weak outcomes.
	- If evidence is insufficient, return conservative or empty proposed_config_patch and explain the risk.
	- proposed_config_patch must use valid top-level StrategyConfig field names.

Output only JSON inside <strategy_evolution_proposal> tags:
<strategy_evolution_proposal>
{
  "proposal_id": "stable-id",
  "strategy_id": "strategy-id",
  "base_version": "old-version",
  "proposed_version": "new-version",
  "trigger": "manual|daily_review|weekly_review|loss_streak|regime_shift",
  "summary": "short explanation",
  "change_reasons": ["reason"],
  "parameter_changes": [
    {"path":"scoring_config.long_threshold","old_value":60,"new_value":65,"reason":"reduce weak entries"}
  ],
  "expected_impact": "expected practical impact",
  "risks": ["risk"],
  "proposed_config_patch": {},
  "requires_approval": true,
  "created_at": "RFC3339 timestamp"
}
</strategy_evolution_proposal>
`)
}

func buildStrategyEvolverUserPrompt(req StrategyEvolutionRequest) (string, error) {
	payload := struct {
		StrategyID    string                 `json:"strategy_id"`
		BaseVersion   string                 `json:"base_version,omitempty"`
		Trigger       string                 `json:"trigger"`
		CurrentConfig interface{}            `json:"current_config"`
		MarketContext *MarketContext         `json:"market_context,omitempty"`
		Performance   map[string]interface{} `json:"performance,omitempty"`
		Notes         string                 `json:"notes,omitempty"`
		RequestedAt   time.Time              `json:"requested_at"`
		Instruction   string                 `json:"instruction"`
	}{
		StrategyID:    req.StrategyID,
		BaseVersion:   req.BaseVersion,
		Trigger:       req.Trigger,
		CurrentConfig: req.CurrentConfig,
		MarketContext: req.MarketContext,
		Performance:   req.Performance,
		Notes:         req.Notes,
		RequestedAt:   req.RequestedAt,
		Instruction:   "Return a proposal only. The user will review and explicitly apply it in a separate workflow. Prefer calibration setup/outcome evidence over intuition; if the quality gate is still collecting, keep proposed_config_patch empty or very conservative.",
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal strategy evolution payload: %w", err)
	}
	return string(data), nil
}

func parseStrategyEvolutionProposal(text string) (*StrategyEvolutionProposal, error) {
	body := strings.TrimSpace(text)
	if start := strings.Index(body, "<strategy_evolution_proposal>"); start >= 0 {
		body = body[start+len("<strategy_evolution_proposal>"):]
	}
	if end := strings.Index(body, "</strategy_evolution_proposal>"); end >= 0 {
		body = body[:end]
	}
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```json")
		body = strings.TrimPrefix(body, "```")
		body = strings.TrimSuffix(body, "```")
		body = strings.TrimSpace(body)
	}

	var proposal StrategyEvolutionProposal
	if err := json.Unmarshal([]byte(body), &proposal); err != nil {
		return nil, fmt.Errorf("parse strategy evolution proposal: %w", err)
	}
	return &proposal, nil
}

func validateStrategyEvolutionProposal(proposal *StrategyEvolutionProposal, req StrategyEvolutionRequest) error {
	if proposal == nil {
		return fmt.Errorf("strategy evolution proposal is nil")
	}
	if strings.TrimSpace(proposal.ProposalID) == "" {
		return fmt.Errorf("strategy evolution proposal missing proposal_id")
	}
	if strings.TrimSpace(proposal.Summary) == "" {
		return fmt.Errorf("strategy evolution proposal missing summary")
	}
	if proposal.StrategyID == "" {
		proposal.StrategyID = req.StrategyID
	}
	if proposal.StrategyID != req.StrategyID {
		return fmt.Errorf("strategy evolution proposal strategy_id mismatch")
	}
	if proposal.Trigger == "" {
		proposal.Trigger = req.Trigger
	}
	if !IsAllowedStrategyEvolutionTrigger(proposal.Trigger) {
		return fmt.Errorf("strategy evolution proposal trigger %q is not supported", proposal.Trigger)
	}
	if proposal.Trigger != req.Trigger {
		return fmt.Errorf("strategy evolution proposal trigger mismatch")
	}
	if proposal.ProposedVersion == "" {
		proposal.ProposedVersion = time.Now().UTC().Format("20060102150405")
	}
	if proposal.BaseVersion == "" {
		proposal.BaseVersion = req.BaseVersion
	}
	if hasConfigPatch(proposal.ProposedConfigPatch) && len(proposal.ParameterChanges) == 0 {
		return fmt.Errorf("strategy evolution proposal with config patch must include parameter_changes")
	}
	if err := validateEvolutionPatch(proposal.ProposedConfigPatch); err != nil {
		return err
	}
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = time.Now().UTC()
	}
	proposal.RequiresApproval = true
	return nil
}

func IsAllowedStrategyEvolutionTrigger(trigger string) bool {
	switch trigger {
	case "manual", "daily_review", "weekly_review", "loss_streak", "regime_shift":
		return true
	default:
		return false
	}
}

func hasConfigPatch(patch map[string]interface{}) bool {
	return len(patch) > 0
}

func validateEvolutionPatch(patch map[string]interface{}) error {
	for key := range patch {
		switch key {
		case "id", "user_id", "created_at", "updated_at", "deleted_at", "is_active", "active", "status":
			return fmt.Errorf("strategy evolution proposal cannot patch runtime field %q", key)
		}
	}
	return nil
}
