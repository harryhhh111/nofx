package kernel

import (
	"context"
	"fmt"
)

// DefaultRiskGate reuses the existing decision validator as the hard
// enforcement layer for candidate signals.
type DefaultRiskGate struct {
	BTCETHLeverage       int
	AltcoinLeverage      int
	BTCETHPositionRatio  float64
	AltcoinPositionRatio float64
	MinRiskRewardRatio   float64
	MarketPrices         map[string]float64
	MinSLDistances       map[string]float64
}

func NewDefaultRiskGate(btcEthLeverage, altcoinLeverage int, btcEthRatio, altcoinRatio, minRR float64, marketPrices, minSLDistances map[string]float64) *DefaultRiskGate {
	return &DefaultRiskGate{
		BTCETHLeverage:       btcEthLeverage,
		AltcoinLeverage:      altcoinLeverage,
		BTCETHPositionRatio:  btcEthRatio,
		AltcoinPositionRatio: altcoinRatio,
		MinRiskRewardRatio:   minRR,
		MarketPrices:         marketPrices,
		MinSLDistances:       minSLDistances,
	}
}

func (g *DefaultRiskGate) Validate(ctx context.Context, req RiskGateRequest) (*RiskGateResult, error) {
	reviewBySignal := map[string]AIReviewDecision{}
	for _, review := range req.Reviews {
		reviewBySignal[review.SignalID] = review
	}

	result := &RiskGateResult{}
	for _, signal := range req.Signals {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		review := reviewBySignal[signal.ID]
		if review.Status == "reject" {
			result.Rejected = append(result.Rejected, RiskRejectedSignal{SignalID: signal.ID, Reason: "llm_review_rejected"})
			continue
		}
		contextRisk := marketContextRiskAssessment(req.MarketContext, signal)
		if contextRisk.HardRejectReason != "" {
			result.Rejected = append(result.Rejected, RiskRejectedSignal{SignalID: signal.ID, Reason: "market_context_rejected:" + contextRisk.HardRejectReason})
			continue
		}
		if len(contextRisk.SoftWarnings) > 0 {
			signal.RiskFlags = append(signal.RiskFlags, contextRisk.SoftWarnings...)
			for _, warning := range contextRisk.SoftWarnings {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s:%s", signal.ID, warning))
			}
		}

		decision := signal.ToDecision(review)
		decisions := []Decision{decision}
		rejected := validateDecisions(
			decisions,
			req.Account.TotalEquity,
			g.BTCETHLeverage,
			g.AltcoinLeverage,
			g.BTCETHPositionRatio,
			g.AltcoinPositionRatio,
			g.MinRiskRewardRatio,
			g.MarketPrices,
			g.MinSLDistances,
		)
		if rejected > 0 || decisions[0].Action == "wait" {
			result.Rejected = append(result.Rejected, RiskRejectedSignal{
				SignalID: signal.ID,
				Reason:   fmt.Sprintf("risk_gate_rejected:%s", decisions[0].Reasoning),
			})
			continue
		}
		result.Approved = append(result.Approved, signal)
	}
	return result, nil
}

type marketContextRisk struct {
	HardRejectReason string
	SoftWarnings     []string
}

func marketContextRiskAssessment(context *MarketContext, signal CandidateSignal) marketContextRisk {
	risk := marketContextRisk{}
	if context == nil {
		return risk
	}
	if signal.Action != "open_long" && signal.Action != "open_short" {
		return risk
	}
	switch context.DirectionBias {
	case "bearish":
		if signal.Action == "open_long" {
			risk.HardRejectReason = "direction_bias_bearish"
			return risk
		}
	case "bullish":
		if signal.Action == "open_short" {
			risk.HardRejectReason = "direction_bias_bullish"
			return risk
		}
	}
	switch context.MarketRegime {
	case "risk_off":
		if signal.Action == "open_long" {
			risk.HardRejectReason = "risk_off"
			return risk
		}
	case "high_volatility", "overheated":
		risk.SoftWarnings = append(risk.SoftWarnings, "market_regime_"+context.MarketRegime)
	}
	for _, flag := range context.RiskFlags {
		switch flag {
		case "high_volatility", "funding_overheated":
			risk.SoftWarnings = append(risk.SoftWarnings, "market_context_"+flag)
		case "major_assets_bearish", "external_factors_bearish":
			if signal.Action == "open_long" {
				risk.SoftWarnings = append(risk.SoftWarnings, "market_context_"+flag)
			}
		}
	}
	return risk
}

func (s CandidateSignal) ToDecision(review AIReviewDecision) Decision {
	reasoning := s.TriggerReason
	if review.Summary != "" {
		if reasoning != "" {
			reasoning += " | "
		}
		reasoning += review.Summary
	}
	decision := Decision{
		Symbol:            s.Symbol,
		Action:            s.Action,
		PositionSizeUSD:   s.PositionSizeUSD,
		Leverage:          s.Leverage,
		StopLoss:          s.StopLoss,
		TakeProfit:        s.TakeProfit,
		Confidence:        s.Confidence,
		SignalGeneratedAt: s.GeneratedAt.UnixMilli(),
		SignalID:          s.ID,
		RuleID:            s.RuleID,
		Setup:             s.Setup,
		StrategyVersion:   s.StrategyVersion,
		Reasoning:         reasoning,
	}
	if levels, ok := s.Evidence["protective_levels"].(ProtectiveLevelTrace); ok {
		decision.StopLossSource = levels.StopSource
		decision.StopLossTF = levels.StopTimeframe
		decision.StopLossAnchor = levels.StopAnchor
		decision.TakeProfitSource = levels.TargetSource
		decision.TakeProfitTF = levels.TargetTimeframe
		decision.TakeProfitAnchor = levels.TargetAnchor
		decision.ProtectiveATR = levels.ATR
		decision.ProtectiveATRTF = levels.ATRTimeframe
		decision.ProtectiveATRBuffer = levels.ATRBuffer
		decision.ProtectiveRiskReward = levels.RiskReward
	}
	return decision
}
