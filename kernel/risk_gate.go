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

func (s CandidateSignal) ToDecision(review AIReviewDecision) Decision {
	reasoning := s.TriggerReason
	if review.Summary != "" {
		if reasoning != "" {
			reasoning += " | "
		}
		reasoning += review.Summary
	}
	return Decision{
		Symbol:          s.Symbol,
		Action:          s.Action,
		PositionSizeUSD: s.PositionSizeUSD,
		Leverage:        s.Leverage,
		StopLoss:        s.StopLoss,
		TakeProfit:      s.TakeProfit,
		Confidence:      s.Confidence,
		Reasoning:       reasoning,
	}
}
