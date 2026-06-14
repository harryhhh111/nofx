package kernel

import (
	"nofx/market"
	"nofx/store"
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
