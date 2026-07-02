package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"nofx/mcp"
	"nofx/store"
	"sort"
	"strings"
	"time"
)

const strategyEvolutionMaxTokens = 6000

type LLMStrategyEvolver struct {
	client mcp.AIClient
}

func NewLLMStrategyEvolver(client mcp.AIClient) *LLMStrategyEvolver {
	return &LLMStrategyEvolver{client: client}
}

type StrategyEvolutionEvidence struct {
	StrategyID           string                           `json:"strategy_id"`
	StrategyVersion      string                           `json:"strategy_version,omitempty"`
	GeneratedAt          time.Time                        `json:"generated_at"`
	CurrentConfig        StrategyEvolutionConfigSummary   `json:"current_config"`
	Calibration          *store.SignalCalibrationReport   `json:"calibration"`
	Replay               *StrategyReplayReport            `json:"replay,omitempty"`
	RecentClosedTrades   []StrategyEvolutionClosedTrade   `json:"recent_closed_trades,omitempty"`
	RecentFailureSamples []StrategyEvolutionFailureSample `json:"recent_failure_samples,omitempty"`
	DataQualityNotes     []string                         `json:"data_quality_notes,omitempty"`
}

type StrategyEvolutionConfigSummary struct {
	StrategyArchetype string                 `json:"strategy_archetype,omitempty"`
	RiskProfile       string                 `json:"risk_profile,omitempty"`
	StrategyMode      string                 `json:"strategy_mode,omitempty"`
	Timeframes        map[string]any         `json:"timeframes,omitempty"`
	EvidenceFilters   map[string]any         `json:"evidence_filters,omitempty"`
	MarketStructure   map[string]any         `json:"market_structure,omitempty"`
	RiskControl       map[string]any         `json:"risk_control,omitempty"`
	EnabledData       map[string]bool        `json:"enabled_data,omitempty"`
	EnabledIndicators []string               `json:"enabled_indicators,omitempty"`
	CoinSource        store.CoinSourceConfig `json:"coin_source"`
}

type StrategyEvolutionClosedTrade struct {
	Symbol                 string  `json:"symbol"`
	Side                   string  `json:"side"`
	Setup                  string  `json:"setup,omitempty"`
	StrategyVersion        string  `json:"strategy_version,omitempty"`
	RealizedPnL            float64 `json:"realized_pnl"`
	Fee                    float64 `json:"fee"`
	HoldDurationMinutes    int64   `json:"hold_duration_minutes,omitempty"`
	CloseReason            string  `json:"close_reason,omitempty"`
	EntryPrice             float64 `json:"entry_price"`
	ExitPrice              float64 `json:"exit_price"`
	Leverage               int     `json:"leverage"`
	StopLossSource         string  `json:"stop_loss_source,omitempty"`
	StopLossTimeframe      string  `json:"stop_loss_timeframe,omitempty"`
	StopLossAnchor         float64 `json:"stop_loss_anchor,omitempty"`
	TakeProfitSource       string  `json:"take_profit_source,omitempty"`
	TakeProfitTimeframe    string  `json:"take_profit_timeframe,omitempty"`
	TakeProfitAnchor       float64 `json:"take_profit_anchor,omitempty"`
	ProtectiveATRTimeframe string  `json:"protective_atr_timeframe,omitempty"`
	ProtectiveATRBuffer    float64 `json:"protective_atr_buffer,omitempty"`
	ProtectiveRiskReward   float64 `json:"protective_risk_reward,omitempty"`
	MaxFavorablePnL        float64 `json:"max_favorable_pnl,omitempty"`
	MaxFavorablePnLPct     float64 `json:"max_favorable_pnl_pct,omitempty"`
	MaxFavorablePrice      float64 `json:"max_favorable_price,omitempty"`
	MaxAdversePnL          float64 `json:"max_adverse_pnl,omitempty"`
	MaxAdversePnLPct       float64 `json:"max_adverse_pnl_pct,omitempty"`
	MaxAdversePrice        float64 `json:"max_adverse_price,omitempty"`
}

type StrategyEvolutionFailureSample struct {
	Symbol           string  `json:"symbol"`
	Setup            string  `json:"setup,omitempty"`
	Action           string  `json:"action,omitempty"`
	RiskStatus       string  `json:"risk_status,omitempty"`
	RiskReason       string  `json:"risk_reason,omitempty"`
	ReviewStatus     string  `json:"review_status,omitempty"`
	PrimaryTimeframe string  `json:"primary_timeframe,omitempty"`
	EntryTimeframe   string  `json:"entry_timeframe,omitempty"`
	PrimaryScore     float64 `json:"primary_score,omitempty"`
	EntryScore       float64 `json:"entry_score,omitempty"`
	AsOf             string  `json:"as_of,omitempty"`
}

type StrategyEvolutionRequest struct {
	StrategyID      string
	StrategyVersion string
	CurrentConfig   *store.StrategyConfig
	Calibration     *store.SignalCalibrationReport
	Replay          *StrategyReplayReport
	RecentSamples   []store.SignalCalibrationSample
	RecentClosed    []store.TraderPosition
	Language        string
	Trigger         string
	UserInstruction string
}

type StrategyEvolutionResult struct {
	Proposal       StrategyEvolutionProposal `json:"proposal"`
	Evidence       StrategyEvolutionEvidence `json:"evidence"`
	ProposedConfig *store.StrategyConfig     `json:"proposed_config,omitempty"`
	Warnings       []string                  `json:"warnings,omitempty"`
}

type StrategyEvolutionProposal struct {
	Summary                 string                       `json:"summary"`
	EvidenceQuality         string                       `json:"evidence_quality"`
	DataUsed                StrategyEvolutionDataUsed    `json:"data_used"`
	Diagnosis               []StrategyEvolutionDiagnosis `json:"diagnosis"`
	RecommendedChanges      []StrategyEvolutionChange    `json:"recommended_changes"`
	ConfigPatch             StrategyEvolutionConfigPatch `json:"config_patch"`
	Warnings                []string                     `json:"warnings,omitempty"`
	RequiresPaperValidation bool                         `json:"requires_paper_validation"`
}

type StrategyEvolutionDataUsed struct {
	Samples      int `json:"samples"`
	ClosedTrades int `json:"closed_trades"`
	Setups       int `json:"setups"`
}

type StrategyEvolutionDiagnosis struct {
	Area     string `json:"area"`
	Finding  string `json:"finding"`
	Evidence string `json:"evidence"`
}

func (d *StrategyEvolutionDiagnosis) UnmarshalJSON(data []byte) error {
	type diagnosis StrategyEvolutionDiagnosis
	var item diagnosis
	if err := json.Unmarshal(data, &item); err == nil {
		*d = StrategyEvolutionDiagnosis(item)
		return nil
	}

	var finding string
	if err := json.Unmarshal(data, &finding); err != nil {
		return err
	}
	*d = StrategyEvolutionDiagnosis{
		Area:    "general",
		Finding: strings.TrimSpace(finding),
	}
	return nil
}

type StrategyEvolutionChange struct {
	Field     string `json:"field"`
	From      string `json:"from"`
	To        string `json:"to"`
	Rationale string `json:"rationale"`
}

func (c *StrategyEvolutionChange) UnmarshalJSON(data []byte) error {
	type change StrategyEvolutionChange
	var item change
	if err := json.Unmarshal(data, &item); err == nil {
		*c = StrategyEvolutionChange(item)
		return nil
	}

	var rationale string
	if err := json.Unmarshal(data, &rationale); err != nil {
		return err
	}
	*c = StrategyEvolutionChange{
		Field:     "strategy",
		Rationale: strings.TrimSpace(rationale),
	}
	return nil
}

type StrategyEvolutionConfigPatch struct {
	StrategyArchetype string                         `json:"strategy_archetype,omitempty"`
	RiskProfile       string                         `json:"risk_profile,omitempty"`
	EvidenceFilters   *EvidenceFilterEvolutionPatch  `json:"evidence_filters,omitempty"`
	MarketStructure   *MarketStructureEvolutionPatch `json:"market_structure,omitempty"`
	RiskControl       *RiskControlEvolutionPatch     `json:"risk_control,omitempty"`
	Klines            *KlineEvolutionPatch           `json:"klines,omitempty"`
}

type EvidenceFilterEvolutionPatch struct {
	FactorWeights           map[string]float64 `json:"factor_weights,omitempty"`
	MinAvailableWeightRatio *float64           `json:"min_available_weight_ratio,omitempty"`
	MinConfidence           *int               `json:"min_confidence,omitempty"`
}

type MarketStructureEvolutionPatch struct {
	EnableMarketStructure *bool          `json:"enable_market_structure,omitempty"`
	Timeframe             string         `json:"timeframe,omitempty"`
	Lookback              *int           `json:"lookback,omitempty"`
	LookbackByTimeframe   map[string]int `json:"lookback_by_timeframe,omitempty"`
	SwingWindow           *int           `json:"swing_window,omitempty"`
	MinLegBars            *int           `json:"min_leg_bars,omitempty"`
	MinLegATRMultiple     *float64       `json:"min_leg_atr_multiple,omitempty"`
	ZigZagThresholdPct    *float64       `json:"zigzag_threshold_pct,omitempty"`
	BreakoutBufferATR     *float64       `json:"breakout_buffer_atr,omitempty"`
	RetestToleranceATR    *float64       `json:"retest_tolerance_atr,omitempty"`
	ExhaustionRSIPeriod   *int           `json:"exhaustion_rsi_period,omitempty"`
}

type RiskControlEvolutionPatch struct {
	MaxPositions          *int     `json:"max_positions,omitempty"`
	BTCETHMaxLeverage     *int     `json:"btc_eth_max_leverage,omitempty"`
	AltcoinMaxLeverage    *int     `json:"altcoin_max_leverage,omitempty"`
	RiskPerTradePct       *float64 `json:"risk_per_trade_pct,omitempty"`
	MinRiskRewardRatio    *float64 `json:"min_risk_reward_ratio,omitempty"`
	MinConfidence         *int     `json:"min_confidence,omitempty"`
	StopLossATRBuffer     *float64 `json:"stop_loss_atr_buffer,omitempty"`
	StopLossTimeframeMode string   `json:"stop_loss_timeframe_mode,omitempty"`
	StopLossTimeframe     string   `json:"stop_loss_timeframe,omitempty"`
}

type KlineEvolutionPatch struct {
	PrimaryTimeframe       string   `json:"primary_timeframe,omitempty"`
	EntryTimeframe         string   `json:"entry_timeframe,omitempty"`
	ConfirmationTimeframes []string `json:"confirmation_timeframes,omitempty"`
	ComputeLookback        *int     `json:"compute_lookback,omitempty"`
	PromptDisplayCount     *int     `json:"prompt_display_count,omitempty"`
}

func (e *LLMStrategyEvolver) Evolve(ctx context.Context, req StrategyEvolutionRequest) (*StrategyEvolutionResult, error) {
	if e == nil || e.client == nil {
		return nil, fmt.Errorf("strategy evolver requires an AI client")
	}
	if req.CurrentConfig == nil {
		return nil, fmt.Errorf("current strategy config is required")
	}
	evidence := BuildStrategyEvolutionEvidence(req)
	systemPrompt := buildStrategyEvolutionSystemPrompt(req.Language)
	userPrompt, err := buildStrategyEvolutionUserPrompt(evidence, req.Trigger, req.UserInstruction)
	if err != nil {
		return nil, err
	}
	requestBody := &mcp.Request{
		Messages: []mcp.Message{
			mcp.NewSystemMessage(systemPrompt),
			mcp.NewUserMessage(userPrompt),
		},
	}
	if err := ensureStructuredOutputsSupported(ctx, e.client); err != nil {
		return nil, err
	}
	if strategyCompileUsesResponseFormat(e.client) {
		requestBody.ResponseFormat = strategyEvolutionResponseFormat()
	}
	maxTokens := strategyEvolutionMaxTokens
	temperature := 0.0
	requestBody.MaxTokens = &maxTokens
	requestBody.Temperature = &temperature
	prepareOpenRouterStructuredRequest(e.client, requestBody)

	type response struct {
		text string
		err  error
	}
	done := make(chan response, 1)
	go func() {
		text, err := e.client.CallWithRequest(requestBody)
		done <- response{text: text, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-done:
		if resp.err != nil {
			return nil, fmt.Errorf("strategy evolution call failed: %w", resp.err)
		}
		proposal, err := parseStrategyEvolutionResponse(resp.text)
		if err != nil {
			return nil, fmt.Errorf("parse strategy evolution response: %w", err)
		}
		proposed, warnings, err := ApplyStrategyEvolutionPatch(req.CurrentConfig, proposal.ConfigPatch)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, proposal.Warnings...)
		return &StrategyEvolutionResult{
			Proposal:       *proposal,
			Evidence:       evidence,
			ProposedConfig: proposed,
			Warnings:       dedupeStrings(warnings),
		}, nil
	}
}

func BuildStrategyEvolutionEvidence(req StrategyEvolutionRequest) StrategyEvolutionEvidence {
	config := req.CurrentConfig
	evidence := StrategyEvolutionEvidence{
		StrategyID:       req.StrategyID,
		StrategyVersion:  strings.TrimSpace(req.StrategyVersion),
		GeneratedAt:      time.Now().UTC(),
		CurrentConfig:    summarizeEvolutionConfig(config),
		Calibration:      req.Calibration,
		Replay:           req.Replay,
		DataQualityNotes: buildEvolutionDataQualityNotes(req.Calibration),
	}
	for _, pos := range req.RecentClosed {
		evidence.RecentClosedTrades = append(evidence.RecentClosedTrades, summarizeClosedTrade(pos))
	}
	for _, sample := range selectFailureSamples(req.RecentSamples, 24) {
		evidence.RecentFailureSamples = append(evidence.RecentFailureSamples, StrategyEvolutionFailureSample{
			Symbol:           sample.Symbol,
			Setup:            sample.Setup,
			Action:           sample.Action,
			RiskStatus:       sample.RiskStatus,
			RiskReason:       sample.RiskReason,
			ReviewStatus:     sample.ReviewStatus,
			PrimaryTimeframe: sample.PrimaryTimeframe,
			EntryTimeframe:   sample.EntryTimeframe,
			PrimaryScore:     round2(sample.PrimaryScore),
			EntryScore:       round2(sample.EntryScore),
			AsOf:             sample.AsOf.UTC().Format(time.RFC3339),
		})
	}
	return evidence
}

func ApplyStrategyEvolutionPatch(base *store.StrategyConfig, patch StrategyEvolutionConfigPatch) (*store.StrategyConfig, []string, error) {
	if base == nil {
		return nil, nil, fmt.Errorf("base strategy config is required")
	}
	var next store.StrategyConfig
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, nil, fmt.Errorf("clone strategy config: %w", err)
	}
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, nil, fmt.Errorf("clone strategy config: %w", err)
	}
	warnings := []string{}
	if strings.TrimSpace(patch.StrategyArchetype) != "" {
		next.StrategyArchetype = strings.TrimSpace(patch.StrategyArchetype)
	}
	if strings.TrimSpace(patch.RiskProfile) != "" {
		next.RiskProfile = strings.TrimSpace(patch.RiskProfile)
	}
	if patch.EvidenceFilters != nil {
		if next.ScoringConfig == nil {
			next.ScoringConfig = &store.ScoringStrategyConfig{Enabled: true}
		}
		applyEvidenceFilterEvolutionPatch(next.ScoringConfig, patch.EvidenceFilters, &warnings)
		next.ResolvedParameters.Scoring = next.ScoringConfig
	}
	if patch.MarketStructure != nil {
		applyMarketStructureEvolutionPatch(&next.Structure, patch.MarketStructure, &warnings)
	}
	if patch.RiskControl != nil {
		applyRiskControlEvolutionPatch(&next.RiskControl, patch.RiskControl, &warnings)
		if next.ScoringConfig != nil {
			next.ScoringConfig.MinConfidence = next.RiskControl.MinConfidence
			next.ScoringConfig.Execution.Confidence = next.ScoringConfig.MinConfidence
			next.ScoringConfig.Execution.Leverage = next.RiskControl.BTCETHMaxLeverage
		}
	}
	if patch.Klines != nil {
		applyKlineEvolutionPatch(&next.Indicators.Klines, patch.Klines, &warnings)
		if next.ScoringConfig != nil && next.Indicators.Klines.PrimaryTimeframe != "" {
			next.ScoringConfig.Timeframe = next.Indicators.Klines.PrimaryTimeframe
		}
	}
	next.ClampLimits()
	if next.ScoringConfig != nil {
		next.ResolvedParameters.Scoring = next.ScoringConfig
	}
	return &next, dedupeStrings(warnings), nil
}

func buildStrategyEvolutionSystemPrompt(lang string) string {
	language := "English"
	if lang == "zh" {
		language = "Chinese"
	}
	return strings.TrimSpace(fmt.Sprintf(`
You are a strategy evolution analyst for a deterministic crypto trading system.

Your task:
- Review historical structured evidence, closed paper outcomes, and current strategy parameters.
- Use replay.parameter_scans to judge whether market_structure parameter changes are stable across persisted K-line windows before proposing them.
- Produce a conservative manual strategy improvement proposal.
- The program calculates K-lines, indicators, market_structure, setup detection, evidence filters, risk gate decisions, and paper outcomes. Do not recalculate raw indicators.
- Do not invent unavailable market data.
- Do not suggest live trading activation.
- Do not rewrite the whole strategy unless evidence clearly supports it.
- Prefer small parameter changes backed by the provided evidence.
- If evidence is insufficient, say so and keep config_patch minimal.
- Do not materially reduce trading opportunity frequency unless the evidence is high-confidence and shows that the filtered opportunities are consistently low quality.
- Do not "optimize" by blindly tightening min_confidence, min_risk_reward_ratio, max_positions, market_structure filters, or timeframe roles just to reduce losses.
- If a change may reduce trade frequency, explain the specific evidence: affected setup(s), sample/outcome counts, loss pattern, and why a less restrictive adjustment is insufficient.
- Prefer targeted improvements that preserve useful opportunities: tune market_structure swing/leg filters, adjust evidence factor weights, fix mismatched timeframe roles, improve stop/target derivation, or isolate a clearly losing setup before globally tightening the strategy.
- Do not recommend a market_structure parameter change only because one variant changes fewer signals. Prefer variants that preserve approved setups, reduce unstable/noisy classifications, and have enough replayable samples.
- In current_config.risk_control, stop_loss_atr_buffer is an explicit ATR multiple; missing values have already been normalized to the product default.

Allowed config_patch fields only:
- strategy_archetype, risk_profile
- evidence_filters.factor_weights, min_available_weight_ratio, min_confidence
- market_structure.enable_market_structure, timeframe, lookback, lookback_by_timeframe, swing_window, min_leg_bars, min_leg_atr_multiple, zigzag_threshold_pct, breakout_buffer_atr, retest_tolerance_atr, exhaustion_rsi_period
- risk_control.max_positions, btc_eth_max_leverage, altcoin_max_leverage, risk_per_trade_pct, min_risk_reward_ratio, min_confidence, stop_loss_atr_buffer, stop_loss_timeframe_mode, stop_loss_timeframe
- klines.primary_timeframe, entry_timeframe, confirmation_timeframes, compute_lookback, prompt_display_count

Return user-facing text in %s.
Output only JSON.
`, language))
}

func buildStrategyEvolutionUserPrompt(evidence StrategyEvolutionEvidence, trigger, instruction string) (string, error) {
	payload := map[string]any{
		"trigger":          strings.TrimSpace(trigger),
		"user_instruction": strings.TrimSpace(instruction),
		"evidence":         evidence,
		"output_contract": map[string]any{
			"summary":                   "short user-facing summary",
			"evidence_quality":          "sufficient|limited|insufficient",
			"data_used":                 map[string]string{"samples": "int", "closed_trades": "int", "setups": "int"},
			"diagnosis":                 []map[string]string{{"area": "string", "finding": "string", "evidence": "string"}},
			"recommended_changes":       []map[string]string{{"field": "string", "from": "string", "to": "string", "rationale": "string"}},
			"config_patch":              "allowed fields only; omit fields that should not change",
			"warnings":                  []string{"short warnings"},
			"requires_paper_validation": true,
		},
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal strategy evolution prompt: %w", err)
	}
	return string(raw), nil
}

func strategyEvolutionResponseFormat() map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "strategy_evolution_proposal",
			"strict": false,
			"schema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"summary":                   map[string]any{"type": "string"},
					"evidence_quality":          map[string]any{"type": "string", "enum": []string{"sufficient", "limited", "insufficient"}},
					"data_used":                 strategyEvolutionDataUsedSchema(),
					"diagnosis":                 map[string]any{"type": "array", "items": strategyEvolutionDiagnosisSchema()},
					"recommended_changes":       map[string]any{"type": "array", "items": strategyEvolutionChangeSchema()},
					"config_patch":              map[string]any{"type": "object"},
					"warnings":                  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"requires_paper_validation": map[string]any{"type": "boolean"},
				},
				"required": []string{"summary", "evidence_quality", "data_used", "diagnosis", "recommended_changes", "config_patch", "requires_paper_validation"},
			},
		},
	}
}

func strategyEvolutionDataUsedSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"samples":       map[string]any{"type": "integer"},
			"closed_trades": map[string]any{"type": "integer"},
			"setups":        map[string]any{"type": "integer"},
		},
		"required": []string{"samples", "closed_trades", "setups"},
	}
}

func strategyEvolutionDiagnosisSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"area":     map[string]any{"type": "string"},
			"finding":  map[string]any{"type": "string"},
			"evidence": map[string]any{"type": "string"},
		},
		"required": []string{"area", "finding", "evidence"},
	}
}

func strategyEvolutionChangeSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"field":     map[string]any{"type": "string"},
			"from":      map[string]any{"type": "string"},
			"to":        map[string]any{"type": "string"},
			"rationale": map[string]any{"type": "string"},
		},
		"required": []string{"field", "from", "to", "rationale"},
	}
}

func parseStrategyEvolutionResponse(text string) (*StrategyEvolutionProposal, error) {
	body, err := extractStrategyCompileJSON(text)
	if err != nil {
		return nil, err
	}
	var proposal StrategyEvolutionProposal
	if err := json.Unmarshal([]byte(body), &proposal); err != nil {
		return nil, err
	}
	if strings.TrimSpace(proposal.Summary) == "" {
		return nil, fmt.Errorf("proposal summary is required")
	}
	if proposal.EvidenceQuality == "" {
		proposal.EvidenceQuality = "limited"
	}
	if proposal.DataUsed.Samples < 0 || proposal.DataUsed.ClosedTrades < 0 {
		return nil, fmt.Errorf("proposal data_used contains negative counts")
	}
	proposal.RequiresPaperValidation = true
	return &proposal, nil
}

func summarizeEvolutionConfig(config *store.StrategyConfig) StrategyEvolutionConfigSummary {
	if config == nil {
		return StrategyEvolutionConfigSummary{}
	}
	evidenceFilters := map[string]any{}
	if config.ScoringConfig != nil {
		evidenceFilters["selected_factors"] = config.ScoringConfig.SelectedFactors
		evidenceFilters["factor_weights"] = config.ScoringConfig.FactorWeights
		evidenceFilters["min_available_weight_ratio"] = config.ScoringConfig.MinAvailableWeightRatio
		evidenceFilters["min_confidence"] = config.ScoringConfig.MinConfidence
	}
	marketStructure := map[string]any{
		"enabled":               config.Structure.EnableMarketStructure,
		"timeframe":             config.Structure.MarketStructure.Timeframe,
		"lookback":              config.Structure.MarketStructure.Lookback,
		"lookback_by_timeframe": config.Structure.MarketStructure.LookbackByTimeframe,
		"swing_window":          config.Structure.MarketStructure.SwingWindow,
		"min_leg_bars":          config.Structure.MarketStructure.MinLegBars,
		"min_leg_atr_multiple":  config.Structure.MarketStructure.MinLegATRMultiple,
		"zigzag_threshold_pct":  config.Structure.MarketStructure.ZigZagThresholdPct,
		"breakout_buffer_atr":   config.Structure.MarketStructure.BreakoutBufferATR,
		"retest_tolerance_atr":  config.Structure.MarketStructure.RetestToleranceATR,
		"exhaustion_rsi_period": config.Structure.MarketStructure.ExhaustionRSIPeriod,
	}
	risk := map[string]any{
		"max_positions":            config.RiskControl.MaxPositions,
		"btc_eth_max_leverage":     config.RiskControl.BTCETHMaxLeverage,
		"altcoin_max_leverage":     config.RiskControl.AltcoinMaxLeverage,
		"risk_per_trade_pct":       config.RiskControl.RiskPerTradePct,
		"min_risk_reward_ratio":    config.RiskControl.MinRiskRewardRatio,
		"min_confidence":           config.RiskControl.MinConfidence,
		"stop_loss_atr_buffer":     normalizedEvolutionStopLossATRBuffer(config.RiskControl.StopLossATRBuffer),
		"stop_loss_timeframe_mode": config.RiskControl.StopLossTimeframeMode,
		"stop_loss_timeframe":      config.RiskControl.StopLossTimeframe,
	}
	klines := config.Indicators.Klines
	return StrategyEvolutionConfigSummary{
		StrategyArchetype: config.StrategyArchetype,
		RiskProfile:       config.RiskProfile,
		StrategyMode:      config.StrategyMode,
		Timeframes: map[string]any{
			"primary":            klines.PrimaryTimeframe,
			"entry":              klines.EntryTimeframe,
			"confirmations":      klines.ConfirmationTimeframes,
			"selected":           klines.SelectedTimeframes,
			"compute_lookback":   klines.ComputeLookback,
			"display_count":      klines.PromptDisplayCount,
			"market_data_source": klines.MarketDataSource,
		},
		EvidenceFilters:   evidenceFilters,
		MarketStructure:   marketStructure,
		RiskControl:       risk,
		EnabledData:       enabledEvolutionData(config),
		EnabledIndicators: enabledEvolutionIndicators(config),
		CoinSource:        config.CoinSource,
	}
}

func normalizedEvolutionStopLossATRBuffer(configured float64) float64 {
	if configured <= 0 {
		return store.DefaultStopLossATRBuffer
	}
	if configured > store.MaxStopLossATRBuffer {
		return store.MaxStopLossATRBuffer
	}
	return configured
}

func enabledEvolutionData(config *store.StrategyConfig) map[string]bool {
	return map[string]bool{
		"oi":            config.Indicators.EnableOI,
		"funding_rate":  config.Indicators.EnableFundingRate,
		"quant_data":    config.Indicators.EnableQuantData,
		"oi_ranking":    config.Indicators.EnableOIRanking,
		"netflow":       config.Indicators.EnableNetFlowRanking,
		"price_ranking": config.Indicators.EnablePriceRanking,
	}
}

func enabledEvolutionIndicators(config *store.StrategyConfig) []string {
	indicators := []string{}
	add := func(enabled bool, name string) {
		if enabled {
			indicators = append(indicators, name)
		}
	}
	add(config.Indicators.EnableEMA, "EMA")
	add(config.Indicators.EnableSMA, "SMA")
	add(config.Indicators.EnableMACD, "MACD")
	add(config.Indicators.EnableRSI, "RSI")
	add(config.Indicators.EnableATR, "ATR")
	add(config.Indicators.EnableADX, "ADX")
	add(config.Indicators.EnableSAR, "SAR")
	add(config.Indicators.EnableBOLL, "BOLL")
	add(config.Indicators.EnableVolume, "Volume")
	add(config.Indicators.EnableDonchian, "Donchian")
	add(config.Indicators.EnableVWAP, "VWAP")
	sort.Strings(indicators)
	return indicators
}

func buildEvolutionDataQualityNotes(report *store.SignalCalibrationReport) []string {
	if report == nil {
		return []string{"no calibration report is available"}
	}
	notes := []string{}
	if !report.EnoughSamples {
		notes = append(notes, fmt.Sprintf("sample count %d is below recommended %d", report.SampleCount, report.MinRequiredSamples))
	}
	if !report.EnoughOutcomes {
		notes = append(notes, fmt.Sprintf("closed outcomes %d is below recommended %d", report.ClosedTradeCount, report.MinRequiredOutcomes))
	}
	if report.ApprovedCount == 0 {
		notes = append(notes, "no approved signals were observed")
	}
	if report.TotalPnL < 0 {
		notes = append(notes, fmt.Sprintf("linked paper outcome PnL is negative: %.2f", report.TotalPnL))
	}
	return notes
}

func summarizeClosedTrade(pos store.TraderPosition) StrategyEvolutionClosedTrade {
	duration := int64(0)
	if pos.EntryTime > 0 && pos.ExitTime > pos.EntryTime {
		duration = (pos.ExitTime - pos.EntryTime) / int64(time.Minute/time.Millisecond)
	}
	return StrategyEvolutionClosedTrade{
		Symbol:                 pos.Symbol,
		Side:                   pos.Side,
		Setup:                  pos.OpeningSetup,
		StrategyVersion:        pos.StrategyVersion,
		RealizedPnL:            round2(pos.RealizedPnL),
		Fee:                    round2(pos.Fee),
		HoldDurationMinutes:    duration,
		CloseReason:            pos.CloseReason,
		EntryPrice:             pos.EntryPrice,
		ExitPrice:              pos.ExitPrice,
		Leverage:               pos.Leverage,
		StopLossSource:         pos.StopLossSource,
		StopLossTimeframe:      pos.StopLossTimeframe,
		StopLossAnchor:         pos.StopLossAnchor,
		TakeProfitSource:       pos.TakeProfitSource,
		TakeProfitTimeframe:    pos.TakeProfitTimeframe,
		TakeProfitAnchor:       pos.TakeProfitAnchor,
		ProtectiveATRTimeframe: pos.ProtectiveATRTimeframe,
		ProtectiveATRBuffer:    pos.ProtectiveATRBuffer,
		ProtectiveRiskReward:   pos.ProtectiveRiskReward,
		MaxFavorablePnL:        round2(pos.MaxFavorablePnL),
		MaxFavorablePnLPct:     round2(pos.MaxFavorablePnLPct),
		MaxFavorablePrice:      pos.MaxFavorablePrice,
		MaxAdversePnL:          round2(pos.MaxAdversePnL),
		MaxAdversePnLPct:       round2(pos.MaxAdversePnLPct),
		MaxAdversePrice:        pos.MaxAdversePrice,
	}
}

func selectFailureSamples(samples []store.SignalCalibrationSample, limit int) []store.SignalCalibrationSample {
	out := []store.SignalCalibrationSample{}
	for _, sample := range samples {
		if sample.RiskStatus == "approved" {
			continue
		}
		out = append(out, sample)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func applyEvidenceFilterEvolutionPatch(scoring *store.ScoringStrategyConfig, patch *EvidenceFilterEvolutionPatch, warnings *[]string) {
	if patch.FactorWeights != nil {
		factors := scoring.SelectedFactors
		if len(factors) == 0 {
			for factor := range patch.FactorWeights {
				factors = append(factors, factor)
			}
			sort.Strings(factors)
			scoring.SelectedFactors = factors
		}
		scoring.FactorWeights = normalizeEvolutionWeights(patch.FactorWeights, factors, warnings)
	}
	if patch.MinAvailableWeightRatio != nil {
		scoring.MinAvailableWeightRatio = clampFloat(*patch.MinAvailableWeightRatio, 0.1, 1)
	}
	if patch.MinConfidence != nil {
		scoring.MinConfidence = clampInt(*patch.MinConfidence, 1, 100)
		scoring.Execution.Confidence = scoring.MinConfidence
	}
}

func applyMarketStructureEvolutionPatch(structure *store.StructureFactorConfig, patch *MarketStructureEvolutionPatch, warnings *[]string) {
	if patch.EnableMarketStructure != nil {
		structure.EnableMarketStructure = *patch.EnableMarketStructure
	}
	if patch.Timeframe != "" {
		structure.MarketStructure.Timeframe = strings.TrimSpace(patch.Timeframe)
	}
	if patch.Lookback != nil {
		structure.MarketStructure.Lookback = clampInt(*patch.Lookback, 20, store.MaxComputeLookback)
	}
	if patch.LookbackByTimeframe != nil {
		next := map[string]int{}
		for timeframe, lookback := range patch.LookbackByTimeframe {
			timeframe = strings.TrimSpace(timeframe)
			if timeframe == "" {
				continue
			}
			next[timeframe] = clampInt(lookback, 20, store.MaxComputeLookback)
		}
		structure.MarketStructure.LookbackByTimeframe = next
	}
	if patch.SwingWindow != nil {
		structure.MarketStructure.SwingWindow = clampInt(*patch.SwingWindow, 1, 20)
	}
	if patch.MinLegBars != nil {
		structure.MarketStructure.MinLegBars = clampInt(*patch.MinLegBars, 1, store.MaxComputeLookback)
	}
	if patch.MinLegATRMultiple != nil {
		structure.MarketStructure.MinLegATRMultiple = clampFloat(*patch.MinLegATRMultiple, 0.1, 20)
	}
	if patch.ZigZagThresholdPct != nil {
		structure.MarketStructure.ZigZagThresholdPct = clampFloat(*patch.ZigZagThresholdPct, 0.1, 100)
	}
	if patch.BreakoutBufferATR != nil {
		structure.MarketStructure.BreakoutBufferATR = clampFloat(*patch.BreakoutBufferATR, 0.05, 10)
	}
	if patch.RetestToleranceATR != nil {
		structure.MarketStructure.RetestToleranceATR = clampFloat(*patch.RetestToleranceATR, 0.05, 10)
	}
	if patch.ExhaustionRSIPeriod != nil {
		structure.MarketStructure.ExhaustionRSIPeriod = clampInt(*patch.ExhaustionRSIPeriod, 2, 100)
	}
	if !structure.EnableMarketStructure {
		*warnings = append(*warnings, "market_structure disabled by evolution patch; setup detection may fall back to evidence-only classification")
	}
}

func applyRiskControlEvolutionPatch(risk *store.RiskControlConfig, patch *RiskControlEvolutionPatch, warnings *[]string) {
	if patch.MaxPositions != nil {
		risk.MaxPositions = clampInt(*patch.MaxPositions, 1, 20)
	}
	if patch.BTCETHMaxLeverage != nil {
		risk.BTCETHMaxLeverage = clampInt(*patch.BTCETHMaxLeverage, 1, 20)
	}
	if patch.AltcoinMaxLeverage != nil {
		risk.AltcoinMaxLeverage = clampInt(*patch.AltcoinMaxLeverage, 1, 20)
	}
	if patch.RiskPerTradePct != nil {
		risk.RiskPerTradePct = clampFloat(*patch.RiskPerTradePct, 0.1, 5)
	}
	if patch.MinRiskRewardRatio != nil {
		risk.MinRiskRewardRatio = clampFloat(*patch.MinRiskRewardRatio, 1, 10)
	}
	if patch.MinConfidence != nil {
		risk.MinConfidence = clampInt(*patch.MinConfidence, 1, 100)
	}
	if patch.StopLossATRBuffer != nil {
		risk.StopLossATRBuffer = clampFloat(*patch.StopLossATRBuffer, 0.5, store.MaxStopLossATRBuffer)
	}
	if patch.StopLossTimeframeMode != "" {
		mode := strings.TrimSpace(patch.StopLossTimeframeMode)
		switch mode {
		case store.StopLossTimeframeModeAuto, store.StopLossTimeframeModePrimary, store.StopLossTimeframeModeEntry, store.StopLossTimeframeModeCustom:
			risk.StopLossTimeframeMode = mode
		default:
			*warnings = append(*warnings, fmt.Sprintf("ignored unsupported stop_loss_timeframe_mode %q", mode))
		}
	}
	if patch.StopLossTimeframe != "" {
		risk.StopLossTimeframe = strings.TrimSpace(patch.StopLossTimeframe)
	}
}

func applyKlineEvolutionPatch(klines *store.KlineConfig, patch *KlineEvolutionPatch, warnings *[]string) {
	if patch.PrimaryTimeframe != "" {
		klines.PrimaryTimeframe = strings.TrimSpace(patch.PrimaryTimeframe)
	}
	if patch.EntryTimeframe != "" {
		klines.EntryTimeframe = strings.TrimSpace(patch.EntryTimeframe)
	}
	if patch.ConfirmationTimeframes != nil {
		klines.ConfirmationTimeframes = append([]string(nil), patch.ConfirmationTimeframes...)
	}
	if patch.ComputeLookback != nil {
		klines.ComputeLookback = clampInt(*patch.ComputeLookback, store.MinKlineCount, store.MaxComputeLookback)
	}
	if patch.PromptDisplayCount != nil {
		klines.PromptDisplayCount = clampInt(*patch.PromptDisplayCount, store.MinKlineCount, store.MaxKlineCount)
	}
	if klines.PrimaryTimeframe == "" {
		*warnings = append(*warnings, "primary timeframe was empty after evolution patch")
	}
}

func normalizeEvolutionWeights(input map[string]float64, factors []string, warnings *[]string) map[string]float64 {
	out := map[string]float64{}
	total := 0.0
	for _, factor := range factors {
		value, ok := input[factor]
		if !ok {
			value = 0
		}
		value = clampFloat(value, 0, 1)
		out[factor] = value
		total += value
	}
	if total <= 0 {
		equal := 1.0 / math.Max(1, float64(len(factors)))
		for _, factor := range factors {
			out[factor] = round4(equal)
		}
		*warnings = append(*warnings, "factor weights were empty; normalized to equal weights")
		return out
	}
	for _, factor := range factors {
		out[factor] = round4(out[factor] / total)
	}
	return out
}

func clampFloat(value, min, max float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return min
	}
	return math.Min(max, math.Max(min, value))
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
