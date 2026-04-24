package kernel

import (
	"strings"
	"testing"

	"nofx/store"
)

func TestBuildPromptsIncludeConfiguredTimeframes(t *testing.T) {
	config := store.GetDefaultStrategyConfig("en")
	config.Indicators.Klines.SelectedTimeframes = []string{"5m", "15m", "1h", "4h", "1d"}
	config.Indicators.Klines.PrimaryTimeframe = "5m"

	engine := NewStrategyEngine(&config)

	systemPrompt := engine.BuildSystemPrompt(1000, "")
	if !strings.Contains(systemPrompt, "Configured K-line timeframes for this run: 5m, 15m, 1h, 4h, 1d") {
		t.Fatalf("system prompt missing configured timeframe list:\n%s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "You MUST analyze every configured timeframe") {
		t.Fatalf("system prompt missing mandatory multi-timeframe instruction:\n%s", systemPrompt)
	}

	ctx := &Context{
		CurrentTime:    "2026-04-24 12:00:00",
		RuntimeMinutes: 10,
		CallCount:      1,
		Account: AccountInfo{
			TotalEquity:      1000,
			AvailableBalance: 800,
			TotalPnLPct:      0,
			MarginUsedPct:    20,
			PositionCount:    0,
		},
		Timeframes: []string{"5m", "15m", "1h", "4h", "1d"},
	}

	userPrompt := engine.BuildUserPrompt(ctx)
	if !strings.Contains(userPrompt, "Analysis timeframes: 5m, 15m, 1h, 4h, 1d") {
		t.Fatalf("user prompt missing configured timeframe list:\n%s", userPrompt)
	}
}

func TestOrderedTimeframesForPrompt(t *testing.T) {
	got := orderedTimeframesForPrompt([]string{"1d", "5m", "4h", "15m", "1h", "5m"})
	want := []string{"5m", "15m", "1h", "4h", "1d"}

	if len(got) != len(want) {
		t.Fatalf("len(got)=%d len(want)=%d got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orderedTimeframesForPrompt mismatch at %d: got=%v want=%v", i, got, want)
		}
	}
}
