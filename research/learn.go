package research

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// LearnConfig drives the offline "learning job": it replays a preset playbook
// over several adjacent walk-forward windows and, per rule, decides whether the
// evidence supports keeping, reducing, or disabling that rule. It is ADVISORY
// ONLY — it never mutates strategy config. This is stage-1 of continuous
// learning: a deterministic, anti-overfit statistical feedback loop with
// guardrails (minimum sample size per window + cross-window sign consistency).
type LearnConfig struct {
	Symbols    []string
	Timeframe  string
	RuleName   string
	Windows    int     // number of adjacent walk-forward windows (most-recent first)
	WindowDays int     // length of each window in days
	MinTrades  int     // a window counts as "effective" only at/above this trade count
	StopPct    float64 // trend SL (fade rules carry their own)
	TakePct    float64 // trend TP
	MaxHold    int
	CostBps    float64
}

func (c *LearnConfig) applyDefaults() {
	if c.Timeframe == "" {
		c.Timeframe = "15m"
	}
	if c.RuleName == "" {
		c.RuleName = "regime_grid"
	}
	if c.Windows <= 0 {
		c.Windows = 3
	}
	if c.WindowDays <= 0 {
		c.WindowDays = 60
	}
	if c.MinTrades <= 0 {
		c.MinTrades = 20
	}
	if c.StopPct <= 0 {
		c.StopPct = 2
	}
	if c.TakePct <= 0 {
		c.TakePct = 4
	}
	if c.MaxHold <= 0 {
		c.MaxHold = 96
	}
	if c.CostBps <= 0 {
		c.CostBps = 10
	}
}

// Verdict thresholds. An effective window's edge "counts" as positive/negative
// only beyond these bands; inside the band it is treated as flat/noise.
const (
	learnExpRBand = 0.02 // |expectancy R| below this = flat
	learnPFKeep   = 1.05 // PF floor for a window to count as a genuine win
)

// RuleWindowStat is one rule's behavior in one walk-forward window.
type RuleWindowStat struct {
	Window    int
	Label     string
	Trades    int
	ExpR      float64
	PF        float64
	WinRate   float64
	Effective bool // Trades >= MinTrades
}

// RuleLearnRow aggregates a rule across all windows plus the advisory verdict.
type RuleLearnRow struct {
	RuleID  string
	Windows []RuleWindowStat
	Verdict string // KEEP | DISABLE | REDUCE | OBSERVE
	Detail  string
}

// LearnReport is the advisory output.
type LearnReport struct {
	Preset     string
	Timeframe  string
	Symbols    []string
	Windows    int
	WindowDays int
	MinTrades  int
	CostBps    float64
	Rows       []RuleLearnRow
	Warnings   []string
}

// RunLearningJob runs the walk-forward attribution and produces per-rule advice.
// Read-only (only fetches klines via the rule backtest); applies nothing.
func RunLearningJob(cfg LearnConfig) (*LearnReport, error) {
	cfg.applyDefaults()
	if len(cfg.Symbols) == 0 {
		return nil, fmt.Errorf("no symbols provided")
	}

	rep := &LearnReport{
		Preset:     cfg.RuleName,
		Timeframe:  cfg.Timeframe,
		Symbols:    cfg.Symbols,
		Windows:    cfg.Windows,
		WindowDays: cfg.WindowDays,
		MinTrades:  cfg.MinTrades,
		CostBps:    cfg.CostBps,
	}

	now := time.Now().UTC()
	// ruleID -> window index -> stat
	collected := map[string]map[int]RuleWindowStat{}
	var ruleOrder []string

	for w := 0; w < cfg.Windows; w++ {
		end := now.Add(-time.Duration(w*cfg.WindowDays) * 24 * time.Hour)
		label := fmt.Sprintf("%d-%dd", w*cfg.WindowDays, (w+1)*cfg.WindowDays)

		br, err := RunRuleBacktest(RuleBacktestConfig{
			Symbols:   cfg.Symbols,
			Timeframe: cfg.Timeframe,
			Days:      cfg.WindowDays,
			RuleName:  cfg.RuleName,
			StopPct:   cfg.StopPct,
			TakePct:   cfg.TakePct,
			MaxHold:   cfg.MaxHold,
			CostBps:   cfg.CostBps,
			EndTime:   end,
		})
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("window %s: %v", label, err))
			continue
		}
		for _, w2 := range br.Warnings {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("[%s] %s", label, w2))
		}
		for _, rs := range br.PerRule {
			id := rs.Symbol // PerRule stores the rule id in Symbol
			if _, ok := collected[id]; !ok {
				collected[id] = map[int]RuleWindowStat{}
				ruleOrder = append(ruleOrder, id)
			}
			collected[id][w] = RuleWindowStat{
				Window:    w,
				Label:     label,
				Trades:    rs.Trades,
				ExpR:      rs.ExpectancyR,
				PF:        rs.ProfitFactor,
				WinRate:   rs.WinRate,
				Effective: rs.Trades >= cfg.MinTrades,
			}
		}
	}

	sort.Strings(ruleOrder)
	for _, id := range ruleOrder {
		row := RuleLearnRow{RuleID: id}
		for w := 0; w < cfg.Windows; w++ {
			if st, ok := collected[id][w]; ok {
				row.Windows = append(row.Windows, st)
			} else {
				row.Windows = append(row.Windows, RuleWindowStat{
					Window: w,
					Label:  fmt.Sprintf("%d-%dd", w*cfg.WindowDays, (w+1)*cfg.WindowDays),
				})
			}
		}
		row.Verdict, row.Detail = ruleLearnVerdict(row.Windows)
		rep.Rows = append(rep.Rows, row)
	}
	return rep, nil
}

// ruleLearnVerdict applies the guardrails: require >=2 effective windows, then
// judge cross-window sign consistency of the edge.
func ruleLearnVerdict(windows []RuleWindowStat) (verdict, detail string) {
	var eff []RuleWindowStat
	for _, w := range windows {
		if w.Effective {
			eff = append(eff, w)
		}
	}
	if len(eff) < 2 {
		return "OBSERVE", fmt.Sprintf("insufficient data: only %d window(s) with >= min trades", len(eff))
	}
	pos, neg, flat := 0, 0, 0
	for _, w := range eff {
		switch {
		case w.ExpR > learnExpRBand && w.PF >= learnPFKeep:
			pos++
		case w.ExpR < -learnExpRBand:
			neg++
		default:
			flat++
		}
	}
	n := len(eff)
	switch {
	case neg == n:
		return "DISABLE", fmt.Sprintf("negative edge in all %d effective windows", n)
	case pos == n:
		return "KEEP", fmt.Sprintf("positive edge (expR>%.2f, PF>%.2f) in all %d windows", learnExpRBand, learnPFKeep, n)
	case neg == 0 && pos >= 1:
		return "KEEP", fmt.Sprintf("positive in %d/%d, flat in %d, never negative", pos, n, flat)
	case pos > 0 && neg > 0:
		return "REDUCE", fmt.Sprintf("sign flips across windows (pos %d / neg %d / flat %d): not robust", pos, neg, flat)
	default:
		return "REDUCE", fmt.Sprintf("mostly flat/weak (pos %d / neg %d / flat %d)", pos, neg, flat)
	}
}

// Render returns the advisory report.
func (r *LearnReport) Render() string {
	var b strings.Builder
	b.WriteString("==================================================================\n")
	b.WriteString(" NoFx Learning Job — walk-forward rule attribution (ADVISORY ONLY)\n")
	b.WriteString("==================================================================\n")
	fmt.Fprintf(&b, " preset=%s  symbols=%s  tf=%s\n", r.Preset, strings.Join(r.Symbols, ","), r.Timeframe)
	fmt.Fprintf(&b, " windows=%d x %dd (most-recent first)  cost=%.1fbps  minTrades/window=%d\n",
		r.Windows, r.WindowDays, r.CostBps, r.MinTrades)
	fmt.Fprintf(&b, " edge bands: |expR|>%.2f counts; PF>%.2f to count a window as a win\n\n", learnExpRBand, learnPFKeep)

	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "   ! %s\n", w)
	}
	if len(r.Warnings) > 0 {
		b.WriteString("\n")
	}
	if len(r.Rows) == 0 {
		b.WriteString(" No rules fired in any window. Nothing to advise.\n")
		return b.String()
	}

	for _, row := range r.Rows {
		fmt.Fprintf(&b, " %-18s => %-8s %s\n", row.RuleID, row.Verdict, row.Detail)
		for _, w := range row.Windows {
			eff := " "
			if w.Effective {
				eff = "*"
			}
			if w.Trades == 0 {
				fmt.Fprintf(&b, "     %s W%d [%-8s] (no trades)\n", eff, w.Window, w.Label)
				continue
			}
			fmt.Fprintf(&b, "     %s W%d [%-8s] n=%-4d expR=%+.3f PF=%-5s win=%.1f%%\n",
				eff, w.Window, w.Label, w.Trades, w.ExpR, pf(w.PF), w.WinRate)
		}
	}
	b.WriteString("\n")
	b.WriteString(" LEGEND: '*' = effective window (>= minTrades). KEEP/REDUCE/DISABLE/OBSERVE\n")
	b.WriteString(" are ADVICE ONLY — nothing is changed. Apply via human review + audit.\n")
	b.WriteString(" CAVEAT: adjacent windows can each be regime-biased (e.g. all-bear);\n")
	b.WriteString(" a KEEP that is really 'short worked in a downtrend' must be confirmed\n")
	b.WriteString(" across a bull window before trusting it live.\n")
	return b.String()
}
