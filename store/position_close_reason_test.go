package store

import (
	"testing"
	"time"
)

func TestEffectiveCloseReasonSurvivesTradeIDOrderIDMismatch(t *testing.T) {
	nowMs := time.Now().UTC().UnixMilli()
	pos := TraderPosition{
		PendingCloseReason:  "ai",
		PendingCloseOrderID: "close-order-123",
		UpdatedAt:           nowMs,
	}

	got := effectiveCloseReasonFromPending(pos, "fill-trade-456", "sync", nowMs+1000)
	if got != "ai" {
		t.Fatalf("effective close reason = %q, want ai", got)
	}
}

func TestEffectiveCloseReasonExpiresOnMismatch(t *testing.T) {
	nowMs := time.Now().UTC().UnixMilli()
	pos := TraderPosition{
		PendingCloseReason:  "ai",
		PendingCloseOrderID: "close-order-123",
		UpdatedAt:           nowMs - int64(pendingCloseReasonTTL/time.Millisecond) - 1000,
	}

	got := effectiveCloseReasonFromPending(pos, "fill-trade-456", "sync", nowMs)
	if got != "sync" {
		t.Fatalf("effective close reason = %q, want sync", got)
	}
}

func TestEffectiveCloseReasonKeepsExactOrderMatchEvenWhenOld(t *testing.T) {
	nowMs := time.Now().UTC().UnixMilli()
	pos := TraderPosition{
		PendingCloseReason:  "manual",
		PendingCloseOrderID: "close-order-123",
		UpdatedAt:           nowMs - int64(pendingCloseReasonTTL/time.Millisecond) - 1000,
	}

	got := effectiveCloseReasonFromPending(pos, "close-order-123", "sync", nowMs)
	if got != "manual" {
		t.Fatalf("effective close reason = %q, want manual", got)
	}
}
