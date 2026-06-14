package kernel

import (
	"nofx/market"
	"nofx/store"
	"strings"
	"testing"
)

// TestLeverageFallback tests automatic correction when leverage exceeds limit
func TestLeverageFallback(t *testing.T) {
	tests := []struct {
		name            string
		decision        Decision
		accountEquity   float64
		btcEthLeverage  int
		altcoinLeverage int
		marketPrices    map[string]float64
		wantLeverage    int // Expected leverage after correction
		wantError       bool
	}{
		{
			name: "Altcoin leverage exceeded - auto-correct to limit",
			decision: Decision{
				Symbol:          "SOLUSDT",
				Action:          "open_long",
				Leverage:        20, // Exceeds limit
				PositionSizeUSD: 100,
				StopLoss:        50,
				TakeProfit:      200,
			},
			accountEquity:   100,
			btcEthLeverage:  10,
			altcoinLeverage: 5, // Limit 5x
			marketPrices:    map[string]float64{"SOLUSDT": 80},
			wantLeverage:    5, // Should be corrected to 5
			wantError:       false,
		},
		{
			name: "BTC leverage exceeded - auto-correct to limit",
			decision: Decision{
				Symbol:          "BTCUSDT",
				Action:          "open_long",
				Leverage:        20, // Exceeds limit
				PositionSizeUSD: 1000,
				StopLoss:        90000,
				TakeProfit:      110000,
			},
			accountEquity:   100,
			btcEthLeverage:  10, // Limit 10x
			altcoinLeverage: 5,
			marketPrices:    map[string]float64{"BTCUSDT": 95000},
			wantLeverage:    10, // Should be corrected to 10
			wantError:       false,
		},
		{
			name: "Leverage within limit - no correction",
			decision: Decision{
				Symbol:          "ETHUSDT",
				Action:          "open_short",
				Leverage:        5, // Not exceeded
				PositionSizeUSD: 500,
				StopLoss:        4000,
				TakeProfit:      3000,
			},
			accountEquity:   100,
			btcEthLeverage:  10,
			altcoinLeverage: 5,
			marketPrices:    map[string]float64{"ETHUSDT": 3750},
			wantLeverage:    5, // Stays unchanged
			wantError:       false,
		},
		{
			name: "Leverage is 0 - should error",
			decision: Decision{
				Symbol:          "SOLUSDT",
				Action:          "open_long",
				Leverage:        0, // Invalid
				PositionSizeUSD: 100,
				StopLoss:        50,
				TakeProfit:      200,
			},
			accountEquity:   100,
			btcEthLeverage:  10,
			altcoinLeverage: 5,
			marketPrices:    map[string]float64{"SOLUSDT": 80},
			wantLeverage:    0,
			wantError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use default position value ratios for testing (10x for BTC/ETH, 1.5x for altcoins)
			err := validateDecision(&tt.decision, tt.accountEquity, tt.btcEthLeverage, tt.altcoinLeverage, 10.0, 1.5, 3.0, nil, nil, tt.marketPrices, nil)

			// Check error status
			if (err != nil) != tt.wantError {
				t.Errorf("validateDecision() error = %v, wantError %v", err, tt.wantError)
				return
			}

			// If shouldn't error, check if leverage was correctly corrected
			if !tt.wantError && tt.decision.Leverage != tt.wantLeverage {
				t.Errorf("Leverage not corrected: got %d, want %d", tt.decision.Leverage, tt.wantLeverage)
			}
		})
	}
}

func TestEntryRiskGuardHardBlocksExtremeRSIShort(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_short",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        61900,
		TakeProfit:      60800,
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeHardBlock
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = false

	err := validateDecision(
		&decision,
		1000,
		5,
		5,
		10,
		5,
		0.5, // minRiskRewardRatio lowered so legacy R/R check passes (this test focuses on RSI)
		guard,
		map[string]*market.Data{
			"BTCUSDT": {
				Symbol:       "BTCUSDT",
				CurrentPrice: 61350,
				TimeframeData: map[string]*market.TimeframeSeriesData{
					"1h": {
						Timeframe:   "1h",
						RSI7Values:  []float64{13.62},
						RSI14Values: []float64{25.0},
					},
				},
			},
		},
		map[string]float64{"BTCUSDT": 61350},
		nil,
	)

	if err == nil {
		t.Fatal("validateDecision() error = nil, want entry guard rejection")
	}
	if !contains(err.Error(), "entry risk guard") {
		t.Fatalf("error = %q, want entry risk guard", err.Error())
	}
}

func TestEntryRiskGuardWarnReduceKeepsDecisionAndReducesSize(t *testing.T) {
	decision := Decision{
		Symbol:          "SOLUSDT",
		Action:          "open_short",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        64,
		TakeProfit:      60.5,
		Reasoning:       "trend setup",
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeWarnReduce
	guard.ReducePositionPct = 0.4
	guard.BlockExtremeRSI = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = false

	err := validateDecision(
		&decision,
		1000,
		5,
		5,
		10,
		5,
		1.5,
		guard,
		map[string]*market.Data{
			"SOLUSDT": {
				Symbol:       "SOLUSDT",
				CurrentPrice: 62.65,
				TimeframeData: map[string]*market.TimeframeSeriesData{
					"15m": {
						Timeframe:  "15m",
						ATR14:      0.4,
						BOLLLower:  []float64{62.7},
						BOLLUpper:  []float64{65.0},
						BOLLMiddle: []float64{63.8},
					},
				},
			},
		},
		map[string]float64{"SOLUSDT": 62.65},
		nil,
	)

	if err != nil {
		t.Fatalf("validateDecision() error = %v, want nil", err)
	}
	if decision.PositionSizeUSD != 400 {
		t.Fatalf("position size = %.2f, want 400", decision.PositionSizeUSD)
	}
	if !contains(decision.Reasoning, "[ENTRY_GUARD_WARNING]") {
		t.Fatalf("reasoning = %q, want entry guard warning", decision.Reasoning)
	}
}

// contains checks if string contains substring (helper function)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestEntryRiskGuardTPExtensionDefaultWarnReduce covers the default
// behavior (Mode = warn_reduce and no explicit TakeProfitGuardMode):
// the TP-extension reason reduces size but does not block.
func TestEntryRiskGuardTPExtensionDefaultWarnReduce(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      70000, // far above recent highs to trigger TP extension
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeWarnReduce
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = true
	guard.BlockLowRiskReward = false

	// Recent K-line highs are around 65000 (well below TP=70000).
	klines := makeKlines("BTCUSDT", 65000, 30, 0.005)

	err := applyEntryRiskGuard(&decision, guard, map[string]*market.Data{
		"BTCUSDT": {
			Symbol:       "BTCUSDT",
			CurrentPrice: 65100,
			TimeframeData: map[string]*market.TimeframeSeriesData{
				"15m": {
					Timeframe: "15m",
					ATR14:     100,
					Klines:    klines,
				},
			},
		},
	}, nil, 0.5)
	if err != nil {
		t.Fatalf("applyEntryRiskGuard() error = %v, want nil (warn_reduce mode should not block)", err)
	}
	if decision.PositionSizeUSD != 500 {
		t.Fatalf("position size = %.2f, want 500 (default reduce 0.5x)", decision.PositionSizeUSD)
	}
	if !contains(decision.Reasoning, "[TP_EXTENSION_GUARD]") {
		t.Fatalf("reasoning = %q, want TP_EXTENSION_GUARD prefix", decision.Reasoning)
	}
}

// TestEntryRiskGuardTPExtensionHardBlockMode confirms explicit
// hard_block on TakeProfitGuardMode still converts the decision to wait.
func TestEntryRiskGuardTPExtensionHardBlockMode(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      70000,
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeWarnReduce // global is warn_reduce
	guard.TakeProfitGuardMode = store.TakeProfitGuardModeHardBlock
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = true
	guard.BlockLowRiskReward = false

	klines := makeKlines("BTCUSDT", 65000, 30, 0.005)

	err := applyEntryRiskGuard(&decision, guard, map[string]*market.Data{
		"BTCUSDT": {
			Symbol:       "BTCUSDT",
			CurrentPrice: 65100,
			TimeframeData: map[string]*market.TimeframeSeriesData{
				"15m": {
					Timeframe: "15m",
					ATR14:     100,
					Klines:    klines,
				},
			},
		},
	}, nil, 0.5)
	if err == nil {
		t.Fatalf("applyEntryRiskGuard() error = nil, want TP hard block error")
	}
	if !contains(err.Error(), "TP") {
		t.Fatalf("error = %q, want TP-related reason", err.Error())
	}
	if decision.Action != "open_long" {
		t.Fatalf("decision action mutated: %q (applyEntryRiskGuard should not modify action)", decision.Action)
	}
}

// TestEntryRiskGuardTPExtensionWarnReduceOverride confirms that
// TakeProfitGuardMode = warn_reduce overrides a global hard_block Mode
// ONLY for the TP reason (TP reasons → warn_reduce, others still hard_block).
func TestEntryRiskGuardTPExtensionWarnReduceOverride(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      70000,
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeHardBlock // global hard block
	guard.TakeProfitGuardMode = store.TakeProfitGuardModeWarnReduce
	guard.BlockExtendedTakeProfit = true
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockLowRiskReward = false

	klines := makeKlines("BTCUSDT", 65000, 30, 0.005)

	err := applyEntryRiskGuard(&decision, guard, map[string]*market.Data{
		"BTCUSDT": {
			Symbol:       "BTCUSDT",
			CurrentPrice: 65100,
			TimeframeData: map[string]*market.TimeframeSeriesData{
				"15m": {
					Timeframe: "15m",
					ATR14:     100,
					Klines:    klines,
				},
			},
		},
	}, nil, 0.5)
	// Only TP reason triggered → warn_reduce should keep the decision.
	if err != nil {
		t.Fatalf("applyEntryRiskGuard() error = %v, want nil (TP reason in warn_reduce)", err)
	}
	if decision.PositionSizeUSD != 500 {
		t.Fatalf("position size = %.2f, want 500", decision.PositionSizeUSD)
	}
	if !contains(decision.Reasoning, "[TP_EXTENSION_GUARD]") {
		t.Fatalf("reasoning = %q, want TP_EXTENSION_GUARD prefix", decision.Reasoning)
	}
}

// TestEntryRiskGuardNonTPStillHardBlock confirms that under the same
// override, a non-TP reason still triggers hard_block.
func TestEntryRiskGuardNonTPStillHardBlock(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      60100, // tight TP → no extension
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeHardBlock
	guard.TakeProfitGuardMode = store.TakeProfitGuardModeWarnReduce
	guard.BlockExtendedTakeProfit = true
	guard.BlockExtremeRSI = true
	guard.BlockNearBollBand = false
	guard.BlockLowRiskReward = false
	guard.BlockTransitionMarket = false

	err := applyEntryRiskGuard(&decision, guard, map[string]*market.Data{
		"BTCUSDT": {
			Symbol:       "BTCUSDT",
			CurrentPrice: 60150,
			TimeframeData: map[string]*market.TimeframeSeriesData{
				"15m": {
					Timeframe: "15m",
					ATR14:     50,
					Klines:    makeKlines("BTCUSDT", 60000, 20, 0.001),
				},
				"1h": {
					Timeframe:   "1h",
					RSI7Values:  []float64{85}, // triggers extreme RSI
					RSI14Values: []float64{75},
				},
			},
		},
	}, nil, 0.5)
	if err == nil {
		t.Fatalf("applyEntryRiskGuard() error = nil, want hard block on RSI reason")
	}
	if contains(err.Error(), "TP ") {
		t.Fatalf("expected non-TP reason error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "long chase risk") {
		t.Fatalf("error = %q, want RSI reason", err.Error())
	}
}

// TestEntryRiskGuardRRTierWarnReduce confirms the new R/R tiered check:
// R/R between RiskRewardSoftFloor × MinRR and MinRR triggers warn_reduce.
func TestEntryRiskGuardRRTierWarnReduce(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000, // distance 1500
		TakeProfit:      63600, // distance 1500 → R/R = 1.0:1
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeHardBlock
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = true
	guard.RiskRewardSoftFloor = 0.8

	// minRR=1.5, soft=0.8 → hard floor=1.2.
	// Decision R/R ≈ 1.0 → below hard floor → expect hard block.
	err := applyEntryRiskGuard(&decision, guard, nil, nil, 1.5)
	if err == nil {
		t.Fatalf("expected hard block on R/R below hard floor, got nil")
	}
	if !contains(err.Error(), "risk/reward") {
		t.Fatalf("error = %q, want risk/reward reason", err.Error())
	}
}

// TestEntryRiskGuardRRTierSoftFloorTriggersWarnReduce covers the soft
// tier: R/R between RiskRewardSoftFloor × MinRR and MinRR should trigger
// warn_reduce (reduce position, no hard block).
func TestEntryRiskGuardRRTierSoftFloorTriggersWarnReduce(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000, // distance 1500
		TakeProfit:      63000, // distance 1500 → R/R = 1.0
		Reasoning:       "trade idea",
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Mode = store.EntryRiskGuardModeHardBlock
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = true
	guard.RiskRewardSoftFloor = 0.5 // soft floor = 1.5 × 0.5 = 0.75

	// Decision R/R = 1.0 → above soft floor (0.75) but below MinRR (1.5)
	// → soft tier: warn_reduce, no error.
	err := applyEntryRiskGuard(&decision, guard, nil, nil, 1.5)
	if err != nil {
		t.Fatalf("expected warn_reduce (nil err) for R/R in soft tier, got %v", err)
	}
	if decision.PositionSizeUSD != 500 {
		t.Fatalf("position size = %.2f, want 500", decision.PositionSizeUSD)
	}
	if !contains(decision.Reasoning, "R/R") {
		t.Fatalf("reasoning should mention R/R, got %q", decision.Reasoning)
	}
}

// TestEntryRiskGuardRRDisabledKeepsLegacyHardFloor ensures that turning
// BlockLowRiskReward off restores the legacy hard-floor behavior at
// minRiskRewardRatio × 0.8 (the original magic number).
func TestEntryRiskGuardRRDisabledKeepsLegacyHardFloor(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      63000, // R/R ≈ 1.0
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = false // disabled → legacy behavior

	// minRR=1.5, hardFloor=1.2. R/R=1.0 below hard floor → hard block
	// (legacy behavior preserved).
	err := applyEntryRiskGuard(&decision, guard, nil, nil, 1.5)
	if err == nil {
		t.Fatalf("expected hard block under legacy hard floor, got nil")
	}
	if !contains(err.Error(), "risk/reward") {
		t.Fatalf("error = %q, want legacy risk/reward reason", err.Error())
	}
}

// TestEntryRiskGuardRRMeetsTargetPassesThrough verifies that an R/R
// above MinRR passes the tiered check unchanged.
func TestEntryRiskGuardRRMeetsTargetPassesThrough(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      69000, // distance 9000 vs 1000 → R/R ≈ 9.0
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = true

	// Provide entry price so R/R = 9.0 (well above MinRR=1.5).
	err := applyEntryRiskGuard(&decision, guard, nil, map[string]float64{"BTCUSDT": 61000}, 1.5)
	if err != nil {
		t.Fatalf("expected nil for high R/R, got %v", err)
	}
	if decision.PositionSizeUSD != 1000 {
		t.Fatalf("position size should be unchanged, got %.2f", decision.PositionSizeUSD)
	}
}

// TestEntryRiskGuardRRStillRunsWhenGuardDisabled is the regression test
// for P0-1: turning entry_risk_guard off (or leaving it nil) must NOT
// disable the R/R hard floor check. The R/R floor is a hard contract
// that the gate layer must enforce regardless of the soft guards.
func TestEntryRiskGuardRRStillRunsWhenGuardDisabled(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      63000, // R/R ≈ 1.0
	}

	// nil cfg → guard disabled entirely. R/R check must still run.
	err := applyEntryRiskGuard(&decision, nil, nil, nil, 1.5)
	if err == nil {
		t.Fatalf("expected R/R hard block with nil cfg, got nil")
	}
	if !contains(err.Error(), "risk/reward") {
		t.Fatalf("error = %q, want risk/reward reason", err.Error())
	}

	// cfg with Enabled=false → guard disabled. R/R check must still run.
	guard := store.DefaultEntryRiskGuardConfig()
	guard.Enabled = false
	decision2 := decision
	err = applyEntryRiskGuard(&decision2, guard, nil, nil, 1.5)
	if err == nil {
		t.Fatalf("expected R/R hard block with Enabled=false, got nil")
	}
	if !contains(err.Error(), "risk/reward") {
		t.Fatalf("error = %q, want risk/reward reason", err.Error())
	}
}

// TestEntryRiskGuardTPLongBelowEntryRejected is the regression test for
// P0-2: an open_long decision whose take-profit sits below the current
// price must be rejected outright, not silently allowed through the
// R/R check (which would see rewardPct ≤ 0 and skip).
func TestEntryRiskGuardTPLongBelowEntryRejected(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_long",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        60000,
		TakeProfit:      60500, // TP below current entry (61000)
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = false // even with tiered check disabled

	err := applyEntryRiskGuard(&decision, guard, nil, map[string]float64{"BTCUSDT": 61000}, 1.5)
	if err == nil {
		t.Fatalf("expected rejection for long TP below entry, got nil")
	}
	if !contains(err.Error(), "wrong side") {
		t.Fatalf("error = %q, want 'wrong side' rejection", err.Error())
	}
}

// TestEntryRiskGuardTPShortAboveEntryRejected is the mirror test for
// shorts: TP above entry must also be rejected.
func TestEntryRiskGuardTPShortAboveEntryRejected(t *testing.T) {
	decision := Decision{
		Symbol:          "BTCUSDT",
		Action:          "open_short",
		Leverage:        3,
		PositionSizeUSD: 1000,
		StopLoss:        62000,
		TakeProfit:      61500, // TP above current entry (61000)
	}
	guard := store.DefaultEntryRiskGuardConfig()
	guard.BlockExtremeRSI = false
	guard.BlockNearBollBand = false
	guard.BlockTransitionMarket = false
	guard.BlockExtendedTakeProfit = false
	guard.BlockLowRiskReward = false

	err := applyEntryRiskGuard(&decision, guard, nil, map[string]float64{"BTCUSDT": 61000}, 1.5)
	if err == nil {
		t.Fatalf("expected rejection for short TP above entry, got nil")
	}
	if !contains(err.Error(), "wrong side") {
		t.Fatalf("error = %q, want 'wrong side' rejection", err.Error())
	}
}

// `count` bars and `pct` per-bar volatility (e.g. 0.005 = 0.5%).
func makeKlines(_ string, base float64, count int, pct float64) []market.KlineBar {
	out := make([]market.KlineBar, count)
	delta := base * pct
	for i := 0; i < count; i++ {
		open := base - delta
		close := base + delta
		high := close + delta
		low := open - delta
		out[i] = market.KlineBar{
			Open:   open,
			High:   high,
			Low:    low,
			Close:  close,
			Volume: 1000,
		}
	}
	return out
}
