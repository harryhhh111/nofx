package trader

import (
	"strings"
	"testing"
	"time"

	"nofx/kernel"
	"nofx/store"
)

func TestCheckTrailingStop_Disabled(t *testing.T) {
	pos := kernel.PositionInfo{Symbol: "BTCUSDT", Side: "LONG", UnrealizedPnLPct: 5}
	trigger, peak, reason := CheckTrailingStop(pos, 0, nil)
	if trigger || reason != "" || peak != 0 {
		t.Fatalf("disabled trailing stop should not trigger")
	}
}

func TestCheckTrailingStop_ArmsAndFiresOnRetracement(t *testing.T) {
	cfg := &store.TrailingStopConfig{Enabled: true, TriggerPct: 2.0, RetractPct: 0.5}
	pos := kernel.PositionInfo{Symbol: "BTCUSDT", Side: "LONG", UnrealizedPnLPct: 3.0}
	trigger, peak, reason := CheckTrailingStop(pos, 0, cfg)
	if trigger {
		t.Fatalf("should not trigger while still below trigger pct relative to peak? peak=%v reason=%q", peak, reason)
	}
	if peak != 3.0 {
		t.Fatalf("peak should update to 3.0, got %v", peak)
	}

	// Now current PnL retreats 0.6% from peak of 3.0 -> floor 2.5, current 2.4 -> trigger.
	pos.UnrealizedPnLPct = 2.4
	trigger, peak, reason = CheckTrailingStop(pos, peak, cfg)
	if !trigger {
		t.Fatalf("expected trigger after retracement, got peak=%v reason=%q", peak, reason)
	}
	if !strings.Contains(reason, "retracted") {
		t.Fatalf("reason should mention retraction, got %q", reason)
	}
}

func TestCheckTrailingStop_MinProfitLock(t *testing.T) {
	cfg := &store.TrailingStopConfig{Enabled: true, TriggerPct: 2.0, RetractPct: 0.5, MinProfitLock: 1.0}
	pos := kernel.PositionInfo{Symbol: "BTCUSDT", Side: "LONG", UnrealizedPnLPct: 1.2}
	trigger, peak, reason := CheckTrailingStop(pos, 3.0, cfg)
	if !trigger {
		t.Fatalf("expected trigger because current 1.2%% is below minProfitLock 1.0%% floor after peak 3.0%%, peak=%v reason=%q", peak, reason)
	}
}

func TestCheckTimeStop_Disabled(t *testing.T) {
	pos := kernel.PositionInfo{Symbol: "BTCUSDT", Side: "LONG", UpdateTime: time.Now().Add(-2 * time.Hour).UnixMilli()}
	trigger, reason := CheckTimeStop(pos, nil, time.Now())
	if trigger || reason != "" {
		t.Fatalf("disabled time stop should not trigger")
	}
}

func TestCheckTimeStop_FiresAfterThreshold(t *testing.T) {
	cfg := &store.TimeStopConfig{Enabled: true, MaxBars: 2, BarInterval: "1h"}
	pos := kernel.PositionInfo{Symbol: "BTCUSDT", Side: "LONG", UpdateTime: time.Now().Add(-3 * time.Hour).UnixMilli()}
	trigger, reason := CheckTimeStop(pos, cfg, time.Now())
	if !trigger {
		t.Fatalf("expected time stop after 3h with threshold 2h")
	}
	if !strings.Contains(reason, "time_stop") {
		t.Fatalf("reason should mention time_stop, got %q", reason)
	}
}

func TestBuildLifecycleCloseDecision(t *testing.T) {
	pos := kernel.PositionInfo{Symbol: "BTCUSDT", Side: "LONG", Leverage: 5}
	d := BuildLifecycleCloseDecision(pos, store.GuardEventTypeLifecycleExit, "test reason", nil)
	if d.Action != "close_long" {
		t.Fatalf("expected close_long, got %s", d.Action)
	}
	if !strings.Contains(d.Reasoning, "test reason") {
		t.Fatalf("reasoning should include original reason, got %q", d.Reasoning)
	}

	pos2 := kernel.PositionInfo{Symbol: "ETHUSDT", Side: "SHORT", Leverage: 3}
	d2 := BuildLifecycleCloseDecision(pos2, store.GuardEventTypeLifecycleExit, "test", &store.TimeStopConfig{CloseImmediately: true})
	if d2.Action != "close_short" {
		t.Fatalf("expected close_short, got %s", d2.Action)
	}
	if !strings.Contains(d2.Reasoning, "FORCE_CLOSE") {
		t.Fatalf("expected FORCE_CLOSE marker, got %q", d2.Reasoning)
	}
}

func TestIntervalDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"15m", 15 * time.Minute},
		{"1h", time.Hour},
		{"4h", 4 * time.Hour},
		{"1d", 24 * time.Hour},
		{"", time.Hour},
	}
	for _, c := range cases {
		if got := intervalDuration(c.in); got != c.want {
			t.Fatalf("intervalDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
