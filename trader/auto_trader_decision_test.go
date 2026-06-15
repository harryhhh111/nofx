package trader

import (
	"testing"

	"nofx/store"
)

// TestSaveGuardEvents_DoesNotBlockOnDBError verifies that a failure to
// bulk-insert guard events is logged but never propagated or panicked.
// Guard-event writes are telemetry only and must not fail the decision path.
func TestSaveGuardEvents_DoesNotBlockOnDBError(t *testing.T) {
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	// Force lazy initialization so the store holds a reference to the
	// (soon-to-be-closed) GORM DB.
	_ = s.GuardEvent()

	sqlDB, err := s.GormDB().DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	// Simulate a database outage: the GORM handle is still alive, but the
	// underlying connection is closed.
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("failed to close sql.DB: %v", err)
	}

	at := &AutoTrader{
		id:          "test-trader",
		strategyID:  "test-strategy",
		cycleNumber: 7,
		store:       s,
	}

	events := []*store.GuardEvent{
		{
			GuardType: "entry_risk_guard",
			Action:    "block",
			Reason:    "simulated block",
		},
	}

	// This must not panic or return an error. If it does, the test fails.
	at.saveGuardEvents(42, events)
}
