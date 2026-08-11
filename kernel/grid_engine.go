package kernel

import (
	"fmt"
	"nofx/market"
	"nofx/store"
	"time"
)

// ============================================================================
// Grid Trading Context and Types
// ============================================================================

// GridLevelInfo represents a single grid level's current state
type GridLevelInfo struct {
	Index         int     `json:"index"`          // Level index (0 = lowest)
	Price         float64 `json:"price"`          // Target price for this level
	State         string  `json:"state"`          // "empty", "pending", "filled"
	Side          string  `json:"side"`           // "buy" or "sell"
	OrderID       string  `json:"order_id"`       // Current order ID (if pending)
	OrderQuantity float64 `json:"order_quantity"` // Order quantity
	PositionSize  float64 `json:"position_size"`  // Position size (if filled)
	PositionEntry float64 `json:"position_entry"` // Entry price (if filled)
	AllocatedUSD  float64 `json:"allocated_usd"`  // USD allocated to this level
	UnrealizedPnL float64 `json:"unrealized_pnl"` // Unrealized P&L (if filled)
}

// GridContext contains all information needed for AI grid decision making
type GridContext struct {
	// Basic info
	Symbol       string  `json:"symbol"`
	CurrentTime  string  `json:"current_time"`
	CurrentPrice float64 `json:"current_price"`

	// Grid configuration
	GridCount       int     `json:"grid_count"`
	TotalInvestment float64 `json:"total_investment"`
	Leverage        int     `json:"leverage"`
	UpperPrice      float64 `json:"upper_price"`
	LowerPrice      float64 `json:"lower_price"`
	GridSpacing     float64 `json:"grid_spacing"`
	Distribution    string  `json:"distribution"`

	// Grid state
	Levels           []GridLevelInfo `json:"levels"`
	ActiveOrderCount int             `json:"active_order_count"`
	FilledLevelCount int             `json:"filled_level_count"`
	IsPaused         bool            `json:"is_paused"`

	// Market data
	ATR14           float64 `json:"atr14"`
	BollingerUpper  float64 `json:"bollinger_upper"`
	BollingerMiddle float64 `json:"bollinger_middle"`
	BollingerLower  float64 `json:"bollinger_lower"`
	BollingerWidth  float64 `json:"bollinger_width"` // Percentage
	EMA20           float64 `json:"ema20"`
	EMA50           float64 `json:"ema50"`
	EMADistance     float64 `json:"ema_distance"` // Percentage
	RSI14           float64 `json:"rsi14"`
	MACD            float64 `json:"macd"`
	MACDSignal      float64 `json:"macd_signal"`
	MACDHistogram   float64 `json:"macd_histogram"`
	FundingRate     float64 `json:"funding_rate"`
	Volume24h       float64 `json:"volume_24h"`
	PriceChange1h   float64 `json:"price_change_1h"`
	PriceChange4h   float64 `json:"price_change_4h"`

	// Account info
	TotalEquity      float64 `json:"total_equity"`
	AvailableBalance float64 `json:"available_balance"`
	CurrentPosition  float64 `json:"current_position"` // Net position size
	UnrealizedPnL    float64 `json:"unrealized_pnl"`

	// Performance
	TotalProfit   float64 `json:"total_profit"`
	TotalTrades   int     `json:"total_trades"`
	WinningTrades int     `json:"winning_trades"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	DailyPnL      float64 `json:"daily_pnl"`

	// Box indicators (Donchian Channels)
	BoxData *market.BoxData `json:"box_data,omitempty"`

	// Grid direction (neutral, long, short, long_bias, short_bias)
	CurrentDirection string `json:"current_direction,omitempty"`
}

// ============================================================================
// Grid Decision Functions
// ============================================================================

// GetGridRuleDecisions builds deterministic grid actions from the current grid
// state. Live grid trading should use this path so the LLM cannot directly
// create, resize, or cancel orders during each cycle.
func GetGridRuleDecisions(ctx *GridContext, config *store.GridStrategyConfig) (*FullDecision, error) {
	startTime := time.Now()
	if ctx == nil {
		return nil, fmt.Errorf("grid context is required")
	}
	if config == nil {
		return nil, fmt.Errorf("grid config is required")
	}
	if ctx.Symbol == "" || ctx.CurrentPrice <= 0 {
		return nil, fmt.Errorf("invalid grid context")
	}
	if config.GridCount <= 1 || config.TotalInvestment <= 0 || config.Leverage <= 0 {
		return nil, fmt.Errorf("invalid grid config")
	}

	marketContext := BuildGridMarketContext(ctx)
	rawDecisions := make([]Decision, 0, len(ctx.Levels))
	if ctx.IsPaused {
		rawDecisions = append(rawDecisions, Decision{
			Symbol:     ctx.Symbol,
			Action:     "hold",
			Confidence: 100,
			Reasoning:  "grid is paused",
		})
	} else if ctx.LowerPrice > 0 && ctx.UpperPrice > 0 &&
		(ctx.CurrentPrice <= ctx.LowerPrice || ctx.CurrentPrice >= ctx.UpperPrice) {
		rawDecisions = append(rawDecisions, Decision{
			Symbol:     ctx.Symbol,
			Action:     "hold",
			Confidence: 95,
			Reasoning:  "current price is outside grid bounds; breakout guard should handle pause or adjustment",
		})
	} else {
		for _, level := range ctx.Levels {
			if level.State != "empty" || level.Price <= 0 || level.AllocatedUSD <= 0 {
				continue
			}

			action := ""
			switch level.Side {
			case "buy":
				if level.Price < ctx.CurrentPrice {
					action = "place_buy_limit"
				}
			case "sell":
				if level.Price > ctx.CurrentPrice {
					action = "place_sell_limit"
				}
			}
			if action == "" {
				continue
			}

			quantity := level.AllocatedUSD * float64(config.Leverage) / level.Price
			if quantity <= 0 {
				continue
			}

			rawDecisions = append(rawDecisions, Decision{
				Symbol:     ctx.Symbol,
				Action:     action,
				Price:      level.Price,
				Quantity:   quantity,
				LevelIndex: level.Index,
				Confidence: 90,
				Reasoning:  "deterministic grid rule: place maker order on empty level inside grid bounds",
			})
		}
	}

	decisions := ApplyGridRiskGate(ctx, rawDecisions, marketContext)
	if len(decisions) == 0 {
		decisions = append(decisions, Decision{
			Symbol:     ctx.Symbol,
			Action:     "hold",
			Confidence: 90,
			Reasoning:  "grid risk gate rejected all executable levels",
		})
	}

	duration := time.Since(startTime).Milliseconds()
	return &FullDecision{
		SystemPrompt:        "deterministic_grid_rule_engine_v1",
		UserPrompt:          "structured grid context evaluated by deterministic rules; no LLM call",
		Decisions:           decisions,
		RawResponse:         "deterministic_grid_rule_engine_v1",
		AIRequestDurationMs: duration,
		Timestamp:           time.Now(),
		MarketContext:       marketContext,
	}, nil
}

// BuildGridMarketContext converts grid-specific indicators into the shared
// market context shape used by decision records and risk review.
func BuildGridMarketContext(ctx *GridContext) *MarketContext {
	flags := []string{}
	regime := "range"
	fundingState := "neutral"

	if ctx == nil {
		return &MarketContext{
			GeneratedAt:    time.Now().UTC(),
			MarketRegime:   "unknown",
			ContextSummary: "grid context is missing",
		}
	}

	if ctx.BollingerWidth > 4 || ctx.EMADistance > 2 {
		regime = "trend"
		flags = append(flags, "grid_trend_risk")
	}
	if ctx.BollingerWidth > 6 {
		regime = "high_volatility"
		flags = append(flags, "grid_high_volatility")
	}
	if ctx.ATR14 > 0 && ctx.CurrentPrice > 0 && ctx.ATR14/ctx.CurrentPrice*100 > 2 {
		flags = append(flags, "grid_atr_expansion")
	}
	if ctx.FundingRate > 0.001 {
		fundingState = "long_overheated"
		flags = append(flags, "funding_overheated_long")
	} else if ctx.FundingRate < -0.001 {
		fundingState = "short_overheated"
		flags = append(flags, "funding_overheated_short")
	}
	if ctx.LowerPrice > 0 && ctx.UpperPrice > 0 &&
		(ctx.CurrentPrice <= ctx.LowerPrice || ctx.CurrentPrice >= ctx.UpperPrice) {
		regime = "breakout"
		flags = append(flags, "grid_price_outside_bounds")
	}

	return &MarketContext{
		GeneratedAt:    time.Now().UTC(),
		MarketRegime:   regime,
		RiskFlags:      flags,
		ContextSummary: fmt.Sprintf("grid regime=%s boll_width=%.2f ema_distance=%.2f atr=%.4f funding=%.6f", regime, ctx.BollingerWidth, ctx.EMADistance, ctx.ATR14, ctx.FundingRate),
		FundingState:   fundingState,
		Metrics: map[string]interface{}{
			"current_price":      ctx.CurrentPrice,
			"upper_price":        ctx.UpperPrice,
			"lower_price":        ctx.LowerPrice,
			"bollinger_width":    ctx.BollingerWidth,
			"ema_distance":       ctx.EMADistance,
			"atr14":              ctx.ATR14,
			"funding_rate":       ctx.FundingRate,
			"active_order_count": ctx.ActiveOrderCount,
			"filled_level_count": ctx.FilledLevelCount,
		},
	}
}

// ApplyGridRiskGate applies hard deterministic checks after grid rule generation
// and before order execution.
func ApplyGridRiskGate(ctx *GridContext, decisions []Decision, marketContext *MarketContext) []Decision {
	if len(decisions) == 0 {
		return nil
	}

	rejectReason := gridRiskRejectReason(ctx, marketContext)
	out := make([]Decision, 0, len(decisions))
	for _, decision := range decisions {
		if !isGridOrderAction(decision.Action) {
			out = append(out, decision)
			continue
		}
		if rejectReason != "" {
			out = append(out, Decision{
				Symbol:     decision.Symbol,
				Action:     "hold",
				Confidence: 95,
				Reasoning:  rejectReason,
			})
			continue
		}
		out = append(out, decision)
	}
	return dedupeGridHoldDecisions(out)
}

func gridRiskRejectReason(ctx *GridContext, marketContext *MarketContext) string {
	if ctx == nil {
		return "grid risk gate: missing context"
	}
	if ctx.IsPaused {
		return "grid risk gate: grid is paused"
	}
	if ctx.LowerPrice > 0 && ctx.UpperPrice > 0 &&
		(ctx.CurrentPrice <= ctx.LowerPrice || ctx.CurrentPrice >= ctx.UpperPrice) {
		return "grid risk gate: price outside grid bounds"
	}
	if marketContext == nil {
		return ""
	}
	for _, flag := range marketContext.RiskFlags {
		switch flag {
		case "grid_trend_risk", "grid_high_volatility", "grid_price_outside_bounds":
			return "grid risk gate: " + flag
		}
	}
	return ""
}

func isGridOrderAction(action string) bool {
	return action == "place_buy_limit" || action == "place_sell_limit"
}

func dedupeGridHoldDecisions(decisions []Decision) []Decision {
	out := make([]Decision, 0, len(decisions))
	seenHold := false
	for _, decision := range decisions {
		if decision.Action == "hold" {
			if seenHold {
				continue
			}
			seenHold = true
		}
		out = append(out, decision)
	}
	return out
}

// ============================================================================
// Grid Context Builder Helpers
// ============================================================================

// BuildGridContextFromMarketData builds grid context from market data
func BuildGridContextFromMarketData(mktData *market.Data, config *store.GridStrategyConfig) *GridContext {
	ctx := &GridContext{
		Symbol:       config.Symbol,
		CurrentTime:  time.Now().Format("2006-01-02 15:04:05"),
		CurrentPrice: mktData.CurrentPrice,

		// Grid config
		GridCount:       config.GridCount,
		TotalInvestment: config.TotalInvestment,
		Leverage:        config.Leverage,
		Distribution:    config.Distribution,

		// Market data
		PriceChange1h: mktData.PriceChange1h,
		PriceChange4h: mktData.PriceChange4h,
		FundingRate:   mktData.FundingRate,
	}

	// Extract indicators from timeframe data
	if mktData.TimeframeData != nil {
		if tf5m, ok := mktData.TimeframeData["5m"]; ok {
			if len(tf5m.BOLLUpper) > 0 {
				ctx.BollingerUpper = tf5m.BOLLUpper[len(tf5m.BOLLUpper)-1]
				ctx.BollingerMiddle = tf5m.BOLLMiddle[len(tf5m.BOLLMiddle)-1]
				ctx.BollingerLower = tf5m.BOLLLower[len(tf5m.BOLLLower)-1]
				if ctx.BollingerMiddle > 0 {
					ctx.BollingerWidth = (ctx.BollingerUpper - ctx.BollingerLower) / ctx.BollingerMiddle * 100
				}
			}
			ctx.ATR14 = tf5m.ATR14
			if len(tf5m.RSI14Values) > 0 {
				ctx.RSI14 = tf5m.RSI14Values[len(tf5m.RSI14Values)-1]
			}
		}
	}

	// Extract longer term context
	if mktData.LongerTermContext != nil {
		if ctx.ATR14 == 0 {
			ctx.ATR14 = mktData.LongerTermContext.ATR14
		}
		ctx.EMA50 = mktData.LongerTermContext.EMA50
	}

	ctx.EMA20 = mktData.CurrentEMA20
	ctx.MACD = mktData.CurrentMACD

	// Calculate EMA distance
	if ctx.EMA50 > 0 {
		ctx.EMADistance = (ctx.EMA20 - ctx.EMA50) / ctx.EMA50 * 100
	}

	return ctx
}

// Helper function for max
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
