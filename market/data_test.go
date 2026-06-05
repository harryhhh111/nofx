package market

import (
	"testing"
	"time"
)

func TestClosedKlinesOnlyDropsTrailingOpenBar(t *testing.T) {
	now := time.Unix(1_000, 0)
	klines := []Kline{
		{OpenTime: now.Add(-10 * time.Minute).UnixMilli(), CloseTime: now.Add(-5 * time.Minute).UnixMilli(), Close: 100},
		{OpenTime: now.Add(-5 * time.Minute).UnixMilli(), CloseTime: now.Add(5 * time.Minute).UnixMilli(), Close: 101},
	}

	closed := closedKlinesOnly(klines, "5m", now)

	if len(closed) != 1 {
		t.Fatalf("expected only closed bars, got %d", len(closed))
	}
	if closed[0].Close != 100 {
		t.Fatalf("unexpected remaining bar: %+v", closed[0])
	}
}

func TestClosedKlinesOnlyUsesOpenTimeWhenCloseTimeMissing(t *testing.T) {
	now := time.Unix(1_000, 0)
	klines := []Kline{
		{OpenTime: now.Add(-10 * time.Minute).UnixMilli(), Close: 100},
		{OpenTime: now.Add(-2 * time.Minute).UnixMilli(), Close: 101},
	}

	closed := closedKlinesOnly(klines, "5m", now)

	if len(closed) != 1 {
		t.Fatalf("expected inferred open bar to be dropped, got %d", len(closed))
	}
}
