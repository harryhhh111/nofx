package store

import "testing"

func TestFormatExchangeOrderIDZeroIsEmpty(t *testing.T) {
	cases := []interface{}{
		0,
		int32(0),
		int64(0),
		float32(0),
		float64(0),
		"0",
		"0.0",
		"",
		nil,
	}

	for _, tc := range cases {
		if got := FormatExchangeOrderID(tc); got != "" {
			t.Fatalf("FormatExchangeOrderID(%v) = %q, want empty", tc, got)
		}
	}
}

func TestFormatExchangeOrderIDFromMapPrefersRealOrderID(t *testing.T) {
	if got := FormatExchangeOrderIDFromMap(map[string]interface{}{"orderId": 0}); got != "" {
		t.Fatalf("FormatExchangeOrderIDFromMap(orderId=0) = %q, want empty", got)
	}

	if got := FormatExchangeOrderIDFromMap(map[string]interface{}{"orderId": int64(12345)}); got != "12345" {
		t.Fatalf("FormatExchangeOrderIDFromMap(orderId=12345) = %q, want 12345", got)
	}
}
