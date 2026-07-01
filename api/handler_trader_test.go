package api

import "testing"

func TestSanitizeTraderIDPart(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keeps safe characters",
			in:   "binance_main-1.model_2",
			want: "binance_main-1.model_2",
		},
		{
			name: "replaces slashes and spaces",
			in:   "qwen/qwen3 plus",
			want: "qwen-qwen3-plus",
		},
		{
			name: "replaces non-ascii characters",
			in:   "模型/alpha",
			want: "---alpha",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeTraderIDPart(tc.in); got != tc.want {
				t.Fatalf("sanitizeTraderIDPart(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
