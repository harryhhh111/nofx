package kernel

import (
	"strings"
	"testing"
)

func TestValidateJSONFormat_AllowsThousandsSeparatorInsideStrings(t *testing.T) {
	jsonStr := `[{"symbol":"BTCUSDT","action":"hold","reasoning":"Price rejected $76,400 and remains below $76,150."}]`

	if err := validateJSONFormat(jsonStr); err != nil {
		t.Fatalf("validateJSONFormat() error = %v, want nil", err)
	}
}

func TestValidateJSONFormat_RejectsThousandsSeparatorOutsideStrings(t *testing.T) {
	jsonStr := `[{"symbol":"ETHUSDT","action":"open_short","position_size_usd":6,400,"reasoning":"test"}]`

	err := validateJSONFormat(jsonStr)
	if err == nil {
		t.Fatal("validateJSONFormat() error = nil, want thousand separator error")
	}
	if !strings.Contains(err.Error(), "thousand separator") {
		t.Fatalf("validateJSONFormat() error = %v, want thousand separator error", err)
	}
}
