package smallcap

import "testing"

func TestSetSharedProviderBeforeSharedProvider(t *testing.T) {
	mock := &MockProvider{}
	SetSharedProvider(mock)
	t.Cleanup(func() { SetSharedProvider(nil) })

	if got := SharedProvider(); got != mock {
		t.Fatalf("expected preset shared provider, got %T", got)
	}
}

func TestSetSharedProviderAfterSharedProvider(t *testing.T) {
	SetSharedProvider(nil)
	_ = SharedProvider()

	mock := &MockProvider{}
	SetSharedProvider(mock)
	t.Cleanup(func() { SetSharedProvider(nil) })

	if got := SharedProvider(); got != mock {
		t.Fatalf("expected overridden shared provider, got %T", got)
	}
}
