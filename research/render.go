package research

import (
	"fmt"
	"math"
	"strings"
)

// Render returns a human-readable text report.
func (r *DiagnoseReport) Render() string {
	var b strings.Builder
	b.WriteString("==================================================================\n")
	b.WriteString(" NoFx Offline Diagnostic — Closed-Trade Expectancy (cost-aware)\n")
	b.WriteString("==================================================================\n")
	scope := "all strategies / all traders"
	if r.Options.StrategyID != "" {
		scope = "strategy=" + r.Options.StrategyID
	}
	if r.Options.TraderID != "" {
		if scope != "" {
			scope += ", "
		}
		scope += "trader=" + r.Options.TraderID
	}
	fmt.Fprintf(&b, " Scope: %s\n", scope)
	fmt.Fprintf(&b, " Convention: Net P&L = realized_pnl - fee (fee assumed separate)\n\n")

	if r.Overall.Trades == 0 {
		b.WriteString(" No CLOSED positions found for this scope.\n")
		b.WriteString(" => Run the strategy (paper or live) to accumulate trade history first.\n")
		return b.String()
	}

	b.WriteString(" OVERALL\n")
	b.WriteString(renderStat(r.Overall))
	b.WriteString("\n")
	b.WriteString(verdict(r.Overall))
	b.WriteString("\n")

	b.WriteString(" BY SETUP\n")
	b.WriteString(renderTable(r.BySetup))
	b.WriteString("\n BY SIDE\n")
	b.WriteString(renderTable(r.BySide))
	b.WriteString("\n BY CLOSE REASON (where P&L actually comes from / exit mechanics)\n")
	b.WriteString(renderTable(r.ByCloseReason))
	return b.String()
}

func renderStat(s TradeStat) string {
	var b strings.Builder
	fmt.Fprintf(&b, "   Trades: %d  (wins %d / losses %d / scratch %d)\n", s.Trades, s.Wins, s.Losses, s.Scratches)
	fmt.Fprintf(&b, "   Win rate:        %s\n", pct(s.WinRate))
	fmt.Fprintf(&b, "   Net P&L:         %s   (realized %s − fee %s)\n", money(s.NetPnL), money(s.RealizedPnL), money(s.TotalFee))
	fmt.Fprintf(&b, "   Expectancy/trade:%s   <-- the number that decides profitability\n", money(s.AvgNetPnL))
	fmt.Fprintf(&b, "   Avg win / loss:  %s / %s   (realized R:R %s)\n", money(s.AvgWin), money(s.AvgLoss), ratio(s.RealizedRR))
	fmt.Fprintf(&b, "   Profit factor:   %s\n", ratio(s.ProfitFactor))
	fmt.Fprintf(&b, "   Fee drag:        %.1f bps of notional\n", s.FeeDragBps)
	return b.String()
}

func renderTable(stats []TradeStat) string {
	if len(stats) == 0 {
		return "   (none)\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "   %-22s %6s %7s %10s %10s %8s %7s\n",
		"group", "trades", "winrate", "net_pnl", "exp/trade", "R:R", "PF")
	for _, s := range stats {
		fmt.Fprintf(&b, "   %-22s %6d %7s %10s %10s %8s %7s\n",
			truncate(s.Group, 22), s.Trades, pct(s.WinRate), money(s.NetPnL), money(s.AvgNetPnL), ratio(s.RealizedRR), ratio(s.ProfitFactor))
	}
	return b.String()
}

// verdict gives an evidence-based, deliberately conservative read.
func verdict(s TradeStat) string {
	const minMeaningful = 30
	if s.Trades < minMeaningful {
		return fmt.Sprintf("   VERDICT: sample too small (%d < %d) — treat all numbers as indicative only.\n", s.Trades, minMeaningful)
	}
	switch {
	case s.NetPnL <= 0:
		return "   VERDICT: NEGATIVE expectancy after fees. Scoring tweaks are premature; the\n" +
			"            system is structurally unprofitable as-is. Investigate costs/exits first.\n"
	case s.ProfitFactor < 1.1:
		return "   VERDICT: barely positive (PF < 1.1) — likely within noise. No robust edge proven yet.\n"
	default:
		return "   VERDICT: positive expectancy after fees. Proceed to test whether SCORE\n" +
			"            discriminates outcomes (forward-labeling step) before changing weights.\n"
	}
}

func pct(v float64) string {
	if math.IsNaN(v) {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", v*100)
}

func money(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

func ratio(v float64) string {
	if math.IsInf(v, 1) {
		return "inf"
	}
	if v == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
