package research

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"nofx/kernel"
	"nofx/market"
	"nofx/store"
)

// RuleBacktestConfig drives a discretionary-rule behavior backtest. Unlike the
// score backtest (which asks "does the score predict returns?"), this asks the
// trader-relevant question: "if I mechanically traded this rule, how does it
// behave?" — trigger frequency, win rate, expectancy in R, profit factor,
// drawdown and fee drag. It does NOT try to prove statistical alpha; the
// directional thesis is the trader's, the harness only quantifies behavior/risk.
type RuleBacktestConfig struct {
	Symbols   []string
	Timeframe string
	Days      int
	Lookback  int       // trailing bars fed to indicators each step (warmup)
	MaxHold   int       // max bars to hold before a time-stop exit
	StopPct   float64   // stop-loss distance in percent
	TakePct   float64   // take-profit distance in percent
	CostBps   float64   // round-trip cost (fee+slippage) in bps
	RuleName  string    // which preset rule to run
	EndTime   time.Time // window end (zero = now); enables walk-forward over past windows
}

func (c *RuleBacktestConfig) applyDefaults() {
	if c.Timeframe == "" {
		c.Timeframe = "15m"
	}
	if c.Days <= 0 {
		c.Days = 90
	}
	if c.Lookback <= 0 {
		c.Lookback = 320
	}
	if c.MaxHold <= 0 {
		c.MaxHold = 96 // ~1 day on 15m
	}
	if c.StopPct <= 0 {
		c.StopPct = 2
	}
	if c.TakePct <= 0 {
		c.TakePct = 4
	}
	if c.CostBps <= 0 {
		c.CostBps = 10
	}
	if c.RuleName == "" {
		c.RuleName = "regime_playbook"
	}
}

// RuleTrade is one simulated round-trip.
type RuleTrade struct {
	Symbol   string
	RuleID   string // which playbook rule fired (for per-regime attribution)
	Side     string
	EntryIdx int
	ExitIdx  int
	Entry    float64
	Exit     float64
	HoldBars int
	Reason   string  // tp | sl | timeout
	DirRet   float64 // directional gross return fraction
	NetRet   float64 // dirRet - cost
	R        float64 // net return in risk units (NetRet / (stop%/100))
}

// SymbolRuleStats summarizes one symbol's trades.
type SymbolRuleStats struct {
	Symbol       string
	Trades       int
	Wins         int
	Losses       int
	TP           int
	SL           int
	Timeout      int
	WinRate      float64
	ExpectancyR  float64
	ProfitFactor float64
	AvgHoldBars  float64
	TotalReturn  float64 // compounded unleveraged
	MaxDrawdown  float64
}

// RuleBacktestReport is the full result.
type RuleBacktestReport struct {
	RuleName  string
	RuleDesc  string
	Timeframe string
	Days      int
	StopPct   float64
	TakePct   float64
	MixedRR   bool // true when rules carry their own per-rule SL/TP (e.g. the playbook)
	MaxHold   int
	CostBps   float64
	SpanHours float64
	PerSymbol []SymbolRuleStats
	PerRule   []SymbolRuleStats // per-firing-rule breakdown (regime区分度反验)
	Pooled    SymbolRuleStats
	Warnings  []string
}

// RunRuleBacktest replays a preset discretionary rule over history and simulates
// SL/TP exits. Read-only (only fetches klines).
func RunRuleBacktest(bcfg RuleBacktestConfig) (*RuleBacktestReport, error) {
	bcfg.applyDefaults()
	if len(bcfg.Symbols) == 0 {
		return nil, fmt.Errorf("no symbols provided")
	}
	tf := bcfg.Timeframe
	tfMin := parseTFMinutes(tf)
	if tfMin <= 0 {
		return nil, fmt.Errorf("unsupported timeframe %q", tf)
	}

	cfg, desc, mixed, err := buildPresetRule(bcfg.RuleName, tf, bcfg.StopPct, bcfg.TakePct)
	if err != nil {
		return nil, err
	}
	indReq := kernel.IndicatorRequestFromStrategyConfig(cfg)
	structReq := kernel.StructureRequestFromStrategyConfig(cfg)

	rep := &RuleBacktestReport{
		RuleName:  bcfg.RuleName,
		RuleDesc:  desc,
		Timeframe: tf,
		Days:      bcfg.Days,
		StopPct:   bcfg.StopPct,
		TakePct:   bcfg.TakePct,
		MixedRR:   mixed,
		MaxHold:   bcfg.MaxHold,
		CostBps:   bcfg.CostBps,
	}

	end := bcfg.EndTime.UTC()
	if end.IsZero() {
		end = time.Now().UTC()
	}
	evalStart := end.Add(-time.Duration(bcfg.Days) * 24 * time.Hour)
	fetchStart := evalStart.Add(-time.Duration(bcfg.Lookback*tfMin) * time.Minute)

	var allTrades []RuleTrade
	var minTime, maxTime time.Time

	for _, sym := range bcfg.Symbols {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym == "" {
			continue
		}
		primary, err := market.GetKlinesRange(sym, tf, fetchStart, end)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: fetch klines failed: %v", sym, err))
			continue
		}
		if len(primary) < bcfg.Lookback+bcfg.MaxHold+5 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: only %d bars; skipped", sym, len(primary)))
			continue
		}

		startI := bcfg.Lookback
		if idx := lastIdxAtOrBefore(primary, evalStart.UnixMilli()); idx+1 > startI {
			startI = idx + 1
		}
		lastEval := len(primary) - 1

		var trades []RuleTrade
		busyUntil := -1
		for i := startI; i < lastEval; i++ {
			if i <= busyUntil {
				continue // still holding a position
			}
			window := primary[i+1-bcfg.Lookback : i+1]
			asOf := time.UnixMilli(primary[i].OpenTime).UTC()
			data := &market.Data{
				Symbol:       sym,
				CurrentPrice: primary[i].Close,
				TimeframeData: map[string]*market.TimeframeSeriesData{
					tf: {Timeframe: tf, ComputeBars: append([]market.Kline(nil), window...)},
				},
			}
			snap, err := market.BuildFactorSnapshotFromDataWithRequests(data, asOf, indReq, structReq)
			if err != nil || snap == nil {
				continue
			}
			preview, err := kernel.PreviewStrategySignals(cfg, []kernel.CandidateCoin{{Symbol: sym}}, map[string]*market.FactorSnapshot{sym: snap}, asOf)
			if err != nil || preview == nil || len(preview.Signals) == 0 {
				continue
			}
			sig, ok := firstOpenSignal(preview.Signals)
			if !ok {
				continue
			}
			side := "long"
			if sig.Action == "open_short" {
				side = "short"
			}
			// Use the FIRED rule's own SL/TP (the engine derives signal.StopLoss/
			// TakeProfit from that rule's Execution pct), so a playbook mixing trend
			// (wide R:R) and fade (tight R:R) rules is simulated faithfully.
			entry := primary[i].Close
			stopPct, takePct := signalExitPct(side, entry, sig.StopLoss, sig.TakeProfit)
			if stopPct <= 0 || takePct <= 0 {
				stopPct, takePct = bcfg.StopPct, bcfg.TakePct
			}
			trade := simulateRuleExit(sym, primary, i, side, stopPct, takePct, bcfg.MaxHold, bcfg.CostBps)
			trade.RuleID = sig.RuleID
			trades = append(trades, trade)
			busyUntil = trade.ExitIdx

			t := asOf
			if minTime.IsZero() || t.Before(minTime) {
				minTime = t
			}
			if t.After(maxTime) {
				maxTime = t
			}
		}

		rep.PerSymbol = append(rep.PerSymbol, summarizeRuleTrades(sym, trades))
		allTrades = append(allTrades, trades...)
	}

	rep.Pooled = summarizeRuleTrades("ALL", allTrades)
	rep.PerRule = breakdownByRule(allTrades)
	if !minTime.IsZero() && maxTime.After(minTime) {
		rep.SpanHours = maxTime.Sub(minTime).Hours()
	}
	return rep, nil
}

// breakdownByRule groups trades by the rule that fired and summarizes each, so
// you can see whether each regime's playbook (trend vs fade, long vs short)
// actually carries its own edge — the empirical "is this regime label
// actionable?" check. Order is stable for readable reports.
func breakdownByRule(trades []RuleTrade) []SymbolRuleStats {
	byRule := map[string][]RuleTrade{}
	var order []string
	for _, t := range trades {
		id := t.RuleID
		if id == "" {
			id = "(unattributed)"
		}
		if _, seen := byRule[id]; !seen {
			order = append(order, id)
		}
		byRule[id] = append(byRule[id], t)
	}
	sort.Strings(order)
	out := make([]SymbolRuleStats, 0, len(order))
	for _, id := range order {
		out = append(out, summarizeRuleTrades(id, byRule[id]))
	}
	return out
}

func simulateRuleExit(sym string, bars []market.Kline, entryIdx int, side string, stopPct, takePct float64, maxHold int, costBps float64) RuleTrade {
	entry := bars[entryIdx].Close
	var sl, tp float64
	if side == "long" {
		sl = entry * (1 - stopPct/100)
		tp = entry * (1 + takePct/100)
	} else {
		sl = entry * (1 + stopPct/100)
		tp = entry * (1 - takePct/100)
	}

	endIdx := entryIdx + maxHold
	if endIdx > len(bars)-1 {
		endIdx = len(bars) - 1
	}
	exitIdx := endIdx
	exit := bars[endIdx].Close
	reason := "timeout"

	for j := entryIdx + 1; j <= endIdx; j++ {
		hi, lo := bars[j].High, bars[j].Low
		if side == "long" {
			if lo <= sl { // stop checked first (conservative on same-bar ambiguity)
				exitIdx, exit, reason = j, sl, "sl"
				break
			}
			if hi >= tp {
				exitIdx, exit, reason = j, tp, "tp"
				break
			}
		} else {
			if hi >= sl {
				exitIdx, exit, reason = j, sl, "sl"
				break
			}
			if lo <= tp {
				exitIdx, exit, reason = j, tp, "tp"
				break
			}
		}
	}

	var dirRet float64
	if side == "long" {
		dirRet = exit/entry - 1
	} else {
		dirRet = (entry - exit) / entry
	}
	net := dirRet - costBps/10000.0
	risk := stopPct / 100.0
	r := 0.0
	if risk > 0 {
		r = net / risk
	}
	return RuleTrade{
		Symbol:   sym,
		Side:     side,
		EntryIdx: entryIdx,
		ExitIdx:  exitIdx,
		Entry:    entry,
		Exit:     exit,
		HoldBars: exitIdx - entryIdx,
		Reason:   reason,
		DirRet:   dirRet,
		NetRet:   net,
		R:        r,
	}
}

func summarizeRuleTrades(label string, trades []RuleTrade) SymbolRuleStats {
	st := SymbolRuleStats{Symbol: label, Trades: len(trades)}
	if len(trades) == 0 {
		return st
	}
	var sumR, sumHold, grossWin, grossLoss float64
	equity := 1.0
	peak := 1.0
	for _, t := range trades {
		sumR += t.R
		sumHold += float64(t.HoldBars)
		if t.NetRet > 0 {
			st.Wins++
			grossWin += t.NetRet
		} else {
			st.Losses++
			grossLoss += -t.NetRet
		}
		switch t.Reason {
		case "tp":
			st.TP++
		case "sl":
			st.SL++
		default:
			st.Timeout++
		}
		equity *= (1 + t.NetRet)
		if equity > peak {
			peak = equity
		}
		if dd := (peak - equity) / peak; dd > st.MaxDrawdown {
			st.MaxDrawdown = dd
		}
	}
	n := float64(len(trades))
	st.WinRate = 100 * float64(st.Wins) / n
	st.ExpectancyR = sumR / n
	st.AvgHoldBars = sumHold / n
	st.TotalReturn = equity - 1
	if grossLoss > 0 {
		st.ProfitFactor = grossWin / grossLoss
	} else if grossWin > 0 {
		st.ProfitFactor = math.Inf(1)
	}
	return st
}

// Regime-gate thresholds. ADX>trend => trending; ADX<range => chop. The 20-25
// band is deliberately "no-man's land" where the playbook stands aside.
const (
	adxTrend = 25.0
	adxRange = 20.0
	rsiLow   = 30.0
	rsiHigh  = 70.0
)

// BuildPlaybookConfig builds the hand-vetted, regime-aware rule-mode strategy
// config for a named preset. It is the single source of truth shared by the
// rule backtest AND the `export` command (so what you validate is exactly what
// you can ship live). Returns the config, a human description, and whether the
// preset mixes per-rule R:R (true for the full playbook).
//
// State-based conditions only (the deterministic rule engine has no crossover
// operator); this is faithful to how the live engine evaluates each bar.
func BuildPlaybookConfig(name, tf string, stopPct, takePct float64) (*store.StrategyConfig, string, bool, error) {
	indicators := store.IndicatorConfig{
		EnableEMA:  true,
		EMAPeriods: []int{20, 50, 200},
		EnableRSI:  true,
		RSIPeriods: []int{14},
		EnableATR:  true,
		ATRPeriods: []int{14},
		EnableADX:  true,
		ADXPeriod:  14,
		// MACD powers the multi-indicator regime direction consensus
		// (direction = EMA20/EMA50 structure + MACD momentum).
		EnableMACD:       true,
		MACDFastPeriod:   12,
		MACDSlowPeriod:   26,
		MACDSignalPeriod: 9,
		Klines: store.KlineConfig{
			PrimaryTimeframe:   tf,
			SelectedTimeframes: []string{tf},
			PrimaryCount:       200,
			ComputeLookback:    320,
		},
	}
	execFor := func(sl, tp float64) store.CompiledRuleExecution {
		return store.CompiledRuleExecution{
			Leverage:        3,
			PositionSizeUSD: 100,
			StopLossPct:     sl,
			TakeProfitPct:   tp,
			Confidence:      70,
		}
	}

	// --- Trend continuation (ride the move); adxGate adds the ADX>25 filter. ---
	trendLong := func(adxGate bool) []store.CompiledRuleCondition {
		c := []store.CompiledRuleCondition{
			{Left: indOp("ema", 50, tf), Operator: ">", Right: indOp("ema", 200, tf)},
			{Left: priceOp(), Operator: ">", Right: indOp("ema", 50, tf)},
			{Left: indOp("rsi", 14, tf), Operator: ">", Right: litOp(50)},
		}
		if adxGate {
			c = append(c, store.CompiledRuleCondition{Left: indOp("adx", 14, tf), Operator: ">", Right: litOp(adxTrend)})
		}
		return c
	}
	trendShort := func(adxGate bool) []store.CompiledRuleCondition {
		c := []store.CompiledRuleCondition{
			{Left: indOp("ema", 50, tf), Operator: "<", Right: indOp("ema", 200, tf)},
			{Left: priceOp(), Operator: "<", Right: indOp("ema", 50, tf)},
			{Left: indOp("rsi", 14, tf), Operator: "<", Right: litOp(50)},
		}
		if adxGate {
			c = append(c, store.CompiledRuleCondition{Left: indOp("adx", 14, tf), Operator: ">", Right: litOp(adxTrend)})
		}
		return c
	}
	// --- Multi-indicator DIRECTION consensus (mirrors MarketContextEngine's
	// assetTrend: price vs EMA20/EMA50 structure + MACD momentum). Expressed as
	// AND of a clean structure, which is stricter and more faithful than a single
	// EMA50/200 + RSI>50 check, so the regime "direction" is less noisy. ---
	structLong := func() []store.CompiledRuleCondition {
		return []store.CompiledRuleCondition{
			{Left: indOp("adx", 14, tf), Operator: ">", Right: litOp(adxTrend)},
			{Left: priceOp(), Operator: ">", Right: indOp("ema", 20, tf)},
			{Left: indOp("ema", 20, tf), Operator: ">", Right: indOp("ema", 50, tf)},
			{Left: indOp("macd_histogram", 0, tf), Operator: ">", Right: litOp(0)},
		}
	}
	structShort := func() []store.CompiledRuleCondition {
		return []store.CompiledRuleCondition{
			{Left: indOp("adx", 14, tf), Operator: ">", Right: litOp(adxTrend)},
			{Left: priceOp(), Operator: "<", Right: indOp("ema", 20, tf)},
			{Left: indOp("ema", 20, tf), Operator: "<", Right: indOp("ema", 50, tf)},
			{Left: indOp("macd_histogram", 0, tf), Operator: "<", Right: litOp(0)},
		}
	}
	// --- Mean reversion (fade extremes) — ONLY in chop (ADX<20). ---
	fadeLong := []store.CompiledRuleCondition{
		{Left: indOp("adx", 14, tf), Operator: "<", Right: litOp(adxRange)},
		{Left: indOp("rsi", 14, tf), Operator: "<", Right: litOp(rsiLow)},
	}
	fadeShort := []store.CompiledRuleCondition{
		{Left: indOp("adx", 14, tf), Operator: "<", Right: litOp(adxRange)},
		{Left: indOp("rsi", 14, tf), Operator: ">", Right: litOp(rsiHigh)},
	}

	mk := func(rules []store.CompiledStrategyRule) *store.StrategyConfig {
		return &store.StrategyConfig{StrategyMode: "rule", Indicators: indicators, CompiledRules: rules}
	}
	trendExec := execFor(stopPct, takePct)
	// Mean-reversion targets are smaller and symmetric (price snaps back to the
	// mean, it does not run): tight ~1:1.5 toward the band middle.
	fadeExec := execFor(1.5, 2.0)

	switch name {
	case "trend_ema_rsi":
		cfg := mk([]store.CompiledStrategyRule{
			{ID: "trend_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: trendLong(false), Execution: trendExec},
			{ID: "trend_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: trendShort(false), Execution: trendExec},
		})
		return cfg, "Trend-follow (NO regime gate): EMA50/EMA200 filter, price beyond EMA50, RSI(14) beyond 50", false, nil

	case "trend_adx":
		cfg := mk([]store.CompiledStrategyRule{
			{ID: "trend_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: trendLong(true), Execution: trendExec},
			{ID: "trend_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: trendShort(true), Execution: trendExec},
		})
		return cfg, "Regime-gated trend-follow: same as trend_ema_rsi PLUS ADX(14)>25 (stand aside in chop)", false, nil

	case "range_fade":
		cfg := mk([]store.CompiledStrategyRule{
			{ID: "fade_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: fadeLong, Execution: fadeExec},
			{ID: "fade_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: fadeShort, Execution: fadeExec},
		})
		return cfg, "Mean-reversion only: ADX(14)<20 (chop) + RSI(14) extreme (<30 / >70), tight SL1.5%/TP2%", false, nil

	case "regime_playbook":
		// The deliverable: ONE strategy that switches behavior by regime.
		// Trend regime (ADX>25): ride continuation, wide R:R.
		// Chop regime (ADX<20): fade RSI extremes, tight R:R.
		// 20-25 ADX band: stand aside. Conditions are mutually exclusive on ADX,
		// so at most one rule fires per bar.
		cfg := mk([]store.CompiledStrategyRule{
			{ID: "trend_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: trendLong(true), Execution: trendExec},
			{ID: "trend_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: trendShort(true), Execution: trendExec},
			{ID: "fade_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: fadeLong, Execution: fadeExec},
			{ID: "fade_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: fadeShort, Execution: fadeExec},
		})
		return cfg, "REGIME PLAYBOOK: trend-follow when ADX>25 (SL2%/TP4%) + mean-revert when ADX<20 & RSI extreme (SL1.5%/TP2%); stand aside in the 20-25 band", true, nil

	case "regime_grid":
		// The upgraded deliverable: same regime grid, but the TREND direction is a
		// multi-indicator consensus (ADX strength + EMA20/EMA50 structure + MACD
		// momentum) instead of the weaker EMA50/200 + RSI>50 check, so the
		// "which way is the trend" decision is more robust.
		cfg := mk([]store.CompiledStrategyRule{
			{ID: "grid_trend_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: structLong(), Execution: trendExec},
			{ID: "grid_trend_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: structShort(), Execution: trendExec},
			{ID: "fade_long", Action: "open_long", Enabled: true, Timeframe: tf, Conditions: fadeLong, Execution: fadeExec},
			{ID: "fade_short", Action: "open_short", Enabled: true, Timeframe: tf, Conditions: fadeShort, Execution: fadeExec},
		})
		return cfg, "REGIME GRID: trend = ADX>25 + EMA20/EMA50 structure + MACD momentum (consensus, SL2%/TP4%) + fade = ADX<20 & RSI extreme (SL1.5%/TP2%)", true, nil

	default:
		return nil, "", false, fmt.Errorf("unknown preset %q (available: trend_ema_rsi, trend_adx, range_fade, regime_playbook, regime_grid)", name)
	}
}

// buildPresetRule is the backtest-facing wrapper around BuildPlaybookConfig.
func buildPresetRule(name, tf string, stopPct, takePct float64) (*store.StrategyConfig, string, bool, error) {
	return BuildPlaybookConfig(name, tf, stopPct, takePct)
}

// firstOpenSignal returns the first open_long/open_short signal in the slice.
func firstOpenSignal(sigs []kernel.CandidateSignal) (kernel.CandidateSignal, bool) {
	for _, s := range sigs {
		if s.Action == "open_long" || s.Action == "open_short" {
			return s, true
		}
	}
	return kernel.CandidateSignal{}, false
}

// signalExitPct recovers the SL/TP distance (percent) the fired rule implies,
// from the signal's absolute SL/TP prices relative to the entry.
func signalExitPct(side string, entry, sl, tp float64) (stopPct, takePct float64) {
	if entry <= 0 || sl <= 0 || tp <= 0 {
		return 0, 0
	}
	if side == "long" {
		return (entry - sl) / entry * 100, (tp - entry) / entry * 100
	}
	return (sl - entry) / entry * 100, (entry - tp) / entry * 100
}

func indOp(name string, period int, tf string) store.CompiledRuleOperand {
	return store.CompiledRuleOperand{Kind: "indicator", Name: name, Period: period, Timeframe: tf}
}

func priceOp() store.CompiledRuleOperand {
	return store.CompiledRuleOperand{Kind: "indicator", Name: "price"}
}

func litOp(v float64) store.CompiledRuleOperand {
	return store.CompiledRuleOperand{Kind: "literal", Value: v}
}

// lastIdxAtOrBefore returns the index of the last kline whose OpenTime <= tMs,
// or -1 if none. Klines are assumed ascending by OpenTime.
func lastIdxAtOrBefore(klines []market.Kline, tMs int64) int {
	idx := -1
	for i := range klines {
		if klines[i].OpenTime <= tMs {
			idx = i
		} else {
			break
		}
	}
	return idx
}

// parseTFMinutes converts a timeframe like "15m"/"1h"/"4h"/"1d" to minutes.
func parseTFMinutes(tf string) int {
	tf = strings.TrimSpace(strings.ToLower(tf))
	if tf == "" {
		return 0
	}
	unit := tf[len(tf)-1]
	numStr := tf[:len(tf)-1]
	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil || n <= 0 {
		return 0
	}
	switch unit {
	case 'm':
		return n
	case 'h':
		return n * 60
	case 'd':
		return n * 60 * 24
	case 'w':
		return n * 60 * 24 * 7
	default:
		return 0
	}
}

// Render returns a human-readable rule-behavior report.
func (r *RuleBacktestReport) Render() string {
	var b strings.Builder
	b.WriteString("==================================================================\n")
	b.WriteString(" NoFx Rule Behavior Backtest — discretionary rule, SL/TP simulated\n")
	b.WriteString("==================================================================\n")
	fmt.Fprintf(&b, " Rule: %s\n", r.RuleName)
	fmt.Fprintf(&b, "   %s\n", r.RuleDesc)
	if r.MixedRR {
		fmt.Fprintf(&b, " tf=%s days=%d  exits=per-rule SL/TP (regime playbook) maxHold=%d bars cost=%.1fbps\n",
			r.Timeframe, r.Days, r.MaxHold, r.CostBps)
	} else {
		fmt.Fprintf(&b, " tf=%s days=%d  SL=%.1f%% TP=%.1f%% (R:R=%.2f) maxHold=%d bars cost=%.1fbps\n",
			r.Timeframe, r.Days, r.StopPct, r.TakePct, r.TakePct/r.StopPct, r.MaxHold, r.CostBps)
	}
	fmt.Fprintf(&b, " Span: %.1f h\n\n", r.SpanHours)

	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "   ! %s\n", w)
	}
	if len(r.Warnings) > 0 {
		b.WriteString("\n")
	}
	if r.Pooled.Trades == 0 {
		b.WriteString(" No trades triggered. The rule's conditions were never simultaneously met.\n")
		return b.String()
	}

	fmt.Fprintf(&b, " %-8s %7s %7s %8s %8s %8s %9s %9s %8s\n",
		"symbol", "trades", "win%", "expR", "PF", "avgHold", "totRet%", "maxDD%", "tp/sl/to")
	for _, s := range r.PerSymbol {
		fmt.Fprintf(&b, " %-8s %7d %6.1f %+8.3f %8s %8.1f %8.2f %8.2f  %d/%d/%d\n",
			s.Symbol, s.Trades, s.WinRate, s.ExpectancyR, pf(s.ProfitFactor), s.AvgHoldBars,
			100*s.TotalReturn, 100*s.MaxDrawdown, s.TP, s.SL, s.Timeout)
	}
	s := r.Pooled
	fmt.Fprintf(&b, " %-8s %7d %6.1f %+8.3f %8s %8.1f %8s %8s  %d/%d/%d\n",
		"POOLED", s.Trades, s.WinRate, s.ExpectancyR, pf(s.ProfitFactor), s.AvgHoldBars, "-", "-", s.TP, s.SL, s.Timeout)

	if len(r.PerRule) > 0 {
		b.WriteString("\n Per-rule (regime区分度: does each regime's playbook carry its own edge?):\n")
		fmt.Fprintf(&b, " %-18s %7s %7s %8s %8s %8s  %s\n",
			"rule", "trades", "win%", "expR", "PF", "avgHold", "tp/sl/to")
		for _, rs := range r.PerRule {
			fmt.Fprintf(&b, " %-18s %7d %6.1f %+8.3f %8s %8.1f  %d/%d/%d\n",
				rs.Symbol, rs.Trades, rs.WinRate, rs.ExpectancyR, pf(rs.ProfitFactor), rs.AvgHoldBars, rs.TP, rs.SL, rs.Timeout)
		}
	}

	b.WriteString("\n")
	b.WriteString(ruleVerdict(r))
	return b.String()
}

func pf(v float64) string {
	if math.IsInf(v, 1) {
		return "inf"
	}
	return fmt.Sprintf("%.2f", v)
}

func ruleVerdict(r *RuleBacktestReport) string {
	var b strings.Builder
	b.WriteString(" VERDICT:\n")
	s := r.Pooled
	if !r.MixedRR {
		be := r.StopPct / (r.StopPct + r.TakePct) * 100 // breakeven win-rate ignoring cost, for this R:R
		fmt.Fprintf(&b, "   Breakeven win-rate for R:R=%.2f is ~%.1f%% (before cost).\n", r.TakePct/r.StopPct, be)
	} else {
		b.WriteString("   Mixed per-rule R:R (trend vs fade) — judge by expectancy/PF, not a single breakeven.\n")
	}
	switch {
	case s.ExpectancyR > 0.02 && s.ProfitFactor > 1.1:
		fmt.Fprintf(&b, "   POSITIVE: expectancy %+.3fR, PF %.2f, win%% %.1f. The rule's behavior is\n", s.ExpectancyR, s.ProfitFactor, s.WinRate)
		b.WriteString("     favorable on this sample. Validate on more --days and out-of-sample before trust.\n")
	case s.ExpectancyR > -0.02:
		fmt.Fprintf(&b, "   ~BREAKEVEN: expectancy %+.3fR, PF %.2f. After cost the rule roughly breaks\n", s.ExpectancyR, s.ProfitFactor)
		b.WriteString("     even — edge (if any) is thin. Tune R:R / filters or accept it as a discipline tool.\n")
	default:
		fmt.Fprintf(&b, "   NEGATIVE: expectancy %+.3fR, PF %.2f. As-is this rule loses after cost on the\n", s.ExpectancyR, s.ProfitFactor)
		b.WriteString("     majors. The value here is the FRAMEWORK (faithful execution + risk); the\n")
		b.WriteString("     directional thesis needs to be the trader's own and better than this preset.\n")
	}
	b.WriteString("   NOTE: returns are unleveraged; leverage scales both return and risk linearly\n")
	b.WriteString("     (liquidation not modeled). R = net return in stop-risk units.\n")
	return b.String()
}
