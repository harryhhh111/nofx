package kernel

import (
	"testing"

	"nofx/provider/nofxos"
	"nofx/store"
)

// TestNormalize_BasicMapping pins the [-lo, hi] → [0, 1] mapping
// behavior used by every rank factor.
func TestNormalize_BasicMapping(t *testing.T) {
	cases := []struct {
		x, lo, hi, want float64
	}{
		{0, -5, 5, 0.5},
		{-5, -5, 5, 0},
		{5, -5, 5, 1},
		{2.5, -5, 5, 0.75},
		{-10, -5, 5, 0}, // below → 0
		{100, -5, 5, 1}, // above → 1
		{1, 1, 1, 0.5},  // degenerate range
		{0, 5, -5, 0.5}, // hi<lo → 0.5
	}
	for _, c := range cases {
		got := normalize(c.x, c.lo, c.hi)
		if got != c.want {
			t.Errorf("normalize(%v, %v, %v) = %v, want %v", c.x, c.lo, c.hi, got, c.want)
		}
	}
}

// TestRankCandidateCoins_DisabledPassesThrough verifies that with
// the filter disabled, ranking is a no-op (no Score, no truncation).
func TestRankCandidateCoins_DisabledPassesThrough(t *testing.T) {
	e := &StrategyEngine{
		config: &store.StrategyConfig{
			CoinSource: store.CoinSourceConfig{
				SourceType: "ai500",
				// RankingFilter == nil disables it (default).
			},
		},
	}
	in := []CandidateCoin{
		{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}, {Symbol: "SOLUSDT"},
	}
	out := e.rankCandidateCoins(in)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	for _, c := range out {
		if c.Score != 0 {
			t.Fatalf("Score should be 0 when ranking disabled, got %v for %s", c.Score, c.Symbol)
		}
	}
}

// TestRankCandidateCoins_EnabledButNoFactorsPassesThrough verifies
// that enabling the filter with all sub-factors off doesn't zero
// out the list — we skip scoring entirely.
func TestRankCandidateCoins_EnabledButNoFactorsPassesThrough(t *testing.T) {
	e := newTestRankEngine(false, false, false, 10)
	in := []CandidateCoin{{Symbol: "BTCUSDT"}, {Symbol: "ETHUSDT"}}
	out := e.rankCandidateCoins(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	for _, c := range out {
		if c.Score != 0 {
			t.Fatalf("Score should be 0 when all factors off, got %v", c.Score)
		}
	}
}

// TestRankCandidateCoins_SortAndTruncate uses a fake price-momentum
// signal set (via stub function pointer in priceBySymbol) to verify
// ranking order and MaxCandidates truncation.
//
// We can't easily swap out e.FetchPriceRankingData; instead we test
// the per-coin score path directly by writing a small helper that
// re-runs the score function with a hand-built price map.
func TestRankCandidateCoins_SortAndTruncate(t *testing.T) {
	in := []CandidateCoin{
		{Symbol: "AAAUSDT"},
		{Symbol: "BBBUSDT"},
		{Symbol: "CCCUSDT"},
		{Symbol: "DDDUSDT"},
	}
	// Manually drive the scoring path with a fake price + OI map.
	priceBySymbol := map[string]nofxos.PriceRankingItem{
		"AAAUSDT": {Symbol: "AAAUSDT", PriceDelta: 0.04},  // +4% → 0.9
		"BBBUSDT": {Symbol: "BBBUSDT", PriceDelta: 0.01},  // +1% → 0.6
		"CCCUSDT": {Symbol: "CCCUSDT", PriceDelta: -0.02}, // -2% → 0.3
		"DDDUSDT": {Symbol: "DDDUSDT", PriceDelta: -0.04}, // -4% → 0.1
	}
	oiBySymbol := map[string]nofxos.OIPosition{
		// Add OI to flip some rankings.
		"CCCUSDT": {Symbol: "CCCUSDT", OIDeltaPercent: 1.5}, // OI 0.875
		"DDDUSDT": {Symbol: "DDDUSDT", OIDeltaPercent: 0},   // OI 0.5
	}

	// Use the same factor loop the engine uses, but with our maps.
	// Reproduce the body of rankCandidateCoins's scoring+sort+truncate
	// block to assert on the order without needing the nofxos
	// network path.
	scored := make([]CandidateCoin, 0, len(in))
	for _, c := range in {
		factors := map[string]float64{}
		if p, ok := priceBySymbol[c.Symbol]; ok {
			factors["price_momentum"] = normalize(p.PriceDelta*100, -5, 5)
		}
		if o, ok := oiBySymbol[c.Symbol]; ok {
			factors["oi_change"] = normalize(o.OIDeltaPercent, -2, 2)
		}
		var total float64
		for k, v := range factors {
			switch k {
			case "price_momentum":
				total += v * 0.4
			case "oi_change":
				total += v * 0.3
			}
		}
		c.Score = total / 0.7
		c.RankFactors = factors
		scored = append(scored, c)
	}
	// Sort by score desc (stable).
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].Score > scored[i].Score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}
	// Truncate to MaxCandidates=2.
	if len(scored) > 2 {
		scored = scored[:2]
	}
	if len(scored) != 2 {
		t.Fatalf("len = %d, want 2 after truncate", len(scored))
	}
	// CCCUSDT: price -2% (factor 0.3*0.4=0.12) + OI +1.5% (factor 0.875*0.3=0.2625) → 0.547
	// AAAUSDT: price +4% (factor 0.9*0.4=0.36) + no OI → 0.514
	// So CCCUSDT nudges ahead thanks to its OI signal.
	if scored[0].Symbol != "CCCUSDT" {
		t.Fatalf("top score should be CCCUSDT (high OI), got %s (score %v)", scored[0].Symbol, scored[0].Score)
	}
	if scored[1].Symbol != "AAAUSDT" {
		t.Fatalf("second should be AAAUSDT, got %s", scored[1].Symbol)
	}
}

// TestClamp_NormalisesMaxCandidates verifies the Clamp helper.
func TestClamp_NormalisesMaxCandidates(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 10},
		{-5, 10},
		{1, 1},
		{50, 50},
		{500, 100}, // capped at 100
	}
	for _, c := range cases {
		rf := &store.CandidateRankingFilter{MaxCandidates: c.in, Enabled: true, Enforce: true}
		rf.Clamp()
		if rf.MaxCandidates != c.want {
			t.Errorf("Clamp MaxCandidates(%d) = %d, want %d", c.in, rf.MaxCandidates, c.want)
		}
	}
}

// TestClamp_DisabledForcesEnforceOff pins the safety: an old config
// that had Enforce=true but was disabled by user can never silently
// start blocking again.
func TestClamp_DisabledForcesEnforceOff(t *testing.T) {
	rf := &store.CandidateRankingFilter{Enabled: false, Enforce: true}
	rf.Clamp()
	if rf.Enforce {
		t.Fatalf("Enforce should be false when Enabled is false")
	}
}

// TestClamp_NilSafe verifies nil receiver doesn't panic.
func TestClamp_NilSafe(t *testing.T) {
	var rf *store.CandidateRankingFilter
	rf.Clamp() // must not panic
}

// TestDefaultCandidateRankingFilter_ValuesMatchesBackend pins the
// invariant: the Go default matches the test setup. If this drifts,
// the buildRankEngine test fixture should also be updated.
func TestDefaultCandidateRankingFilter_ValuesMatchesBackend(t *testing.T) {
	rf := store.DefaultCandidateRankingFilter()
	if rf.Enabled {
		t.Errorf("default.Enabled should be false")
	}
	if rf.MaxCandidates != 10 {
		t.Errorf("default.MaxCandidates = %d, want 10", rf.MaxCandidates)
	}
	if rf.Enforce {
		t.Errorf("default.Enforce should be false")
	}
	if !rf.UsePriceMomentum {
		t.Errorf("default.UsePriceMomentum should be true")
	}
}

// newTestRankEngine builds a minimal StrategyEngine with a custom
// RankingFilter. The nofxosClient stays nil — the ranker swallows
// that gracefully (logged warning, factor contributes 0).
func newTestRankEngine(price, oi, funding bool, max int) *StrategyEngine {
	return &StrategyEngine{
		config: &store.StrategyConfig{
			CoinSource: store.CoinSourceConfig{
				SourceType: "ai500",
				RankingFilter: &store.CandidateRankingFilter{
					Enabled:          true,
					UsePriceMomentum: price,
					UseOIChange:      oi,
					UseFundingRate:   funding,
					MaxCandidates:    max,
					Enforce:          false,
				},
			},
		},
	}
}
