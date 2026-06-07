// Command research is an offline, read-only analysis tool over the trading
// system's own database. It never places orders or mutates strategy config.
//
// Usage:
//
//	go run ./cmd/research diagnose [--strategy <id>] [--trader <id>] [--limit N]
//
// It reuses the same database configuration as the main app (.env / config),
// so it points at the exact data the live system produced.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"nofx/config"
	"nofx/research"
	"nofx/store"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]

	switch sub {
	case "config":
		runConfig(os.Args[2:])
	case "diagnose":
		runDiagnose(os.Args[2:])
	case "rule":
		runRule(os.Args[2:])
	case "learn":
		runLearn(os.Args[2:])
	case "export":
		runExport(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `NoFx offline research tool (read-only)

Subcommands:
  config     Read-only snapshot of traders / exchanges / active strategy
  diagnose   Cost-aware closed-trade expectancy diagnostic (real closed trades)
  rule       Backtest a preset regime-aware rule playbook (SL/TP simulated)
  learn      Walk-forward per-rule attribution -> KEEP/REDUCE/DISABLE advice
  export     Print a preset playbook as a ready-to-load strategy config JSON

Presets (for rule/export --name):
  trend_ema_rsi    Trend-follow, NO regime gate (baseline)
  trend_adx        Trend-follow gated by ADX>25 (stand aside in chop)
  range_fade       Mean-reversion only (ADX<20 + RSI extreme)
  regime_playbook  Trend when ADX>25 + mean-revert when ADX<20
  regime_grid      Like playbook but trend direction = multi-indicator consensus
                   (ADX strength + EMA20/EMA50 structure + MACD momentum)

Run "go run ./cmd/research rule -h" for flags.
`)
}

func runConfig(args []string) {
	_ = args
	st := openStore()
	defer st.Close()

	report, err := research.InspectConfig(st.GormDB())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config inspect failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report.Render())
}

func runDiagnose(args []string) {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	strategyID := fs.String("strategy", "", "restrict to one strategy_id")
	traderID := fs.String("trader", "", "restrict to one trader_id")
	limit := fs.Int("limit", 0, "max closed trades to load (0 = all)")
	_ = fs.Parse(args)

	st := openStore()
	defer st.Close()

	report, err := research.Diagnose(st.GormDB(), research.DiagnoseOptions{
		StrategyID: *strategyID,
		TraderID:   *traderID,
		Limit:      *limit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "diagnose failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report.Render())
}

func runRule(args []string) {
	fs := flag.NewFlagSet("rule", flag.ExitOnError)
	symbols := fs.String("symbols", "BTCUSDT,ETHUSDT", "comma-separated symbols")
	tf := fs.String("tf", "15m", "timeframe")
	days := fs.Int("days", 90, "days of history")
	rule := fs.String("name", "regime_grid", "preset: trend_ema_rsi|trend_adx|range_fade|regime_playbook|regime_grid")
	sl := fs.Float64("sl", 2, "trend stop-loss percent (fade rules carry their own)")
	tp := fs.Float64("tp", 4, "trend take-profit percent (fade rules carry their own)")
	maxHold := fs.Int("maxhold", 96, "max bars to hold before time-stop")
	cost := fs.Float64("cost", 10, "round-trip cost in bps")
	_ = fs.Parse(args)

	var syms []string
	for _, s := range strings.Split(*symbols, ",") {
		if t := strings.TrimSpace(s); t != "" {
			syms = append(syms, t)
		}
	}

	report, err := research.RunRuleBacktest(research.RuleBacktestConfig{
		Symbols:   syms,
		Timeframe: *tf,
		Days:      *days,
		RuleName:  *rule,
		StopPct:   *sl,
		TakePct:   *tp,
		MaxHold:   *maxHold,
		CostBps:   *cost,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "rule backtest failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report.Render())
}

// runLearn replays a preset across adjacent walk-forward windows and emits a
// per-rule KEEP/REDUCE/DISABLE advice sheet. Advisory only: it changes nothing.
func runLearn(args []string) {
	fs := flag.NewFlagSet("learn", flag.ExitOnError)
	symbols := fs.String("symbols", "BTCUSDT,ETHUSDT", "comma-separated symbols")
	tf := fs.String("tf", "15m", "timeframe")
	rule := fs.String("name", "regime_grid", "preset: trend_ema_rsi|trend_adx|range_fade|regime_playbook|regime_grid")
	windows := fs.Int("windows", 3, "number of adjacent walk-forward windows (recent first)")
	windowDays := fs.Int("windowdays", 60, "days per window")
	minTrades := fs.Int("mintrades", 20, "min trades for a window to count as effective")
	sl := fs.Float64("sl", 2, "trend stop-loss percent")
	tp := fs.Float64("tp", 4, "trend take-profit percent")
	maxHold := fs.Int("maxhold", 96, "max bars to hold before time-stop")
	cost := fs.Float64("cost", 10, "round-trip cost in bps")
	_ = fs.Parse(args)

	var syms []string
	for _, s := range strings.Split(*symbols, ",") {
		if t := strings.TrimSpace(s); t != "" {
			syms = append(syms, t)
		}
	}

	report, err := research.RunLearningJob(research.LearnConfig{
		Symbols:    syms,
		Timeframe:  *tf,
		RuleName:   *rule,
		Windows:    *windows,
		WindowDays: *windowDays,
		MinTrades:  *minTrades,
		StopPct:    *sl,
		TakePct:    *tp,
		MaxHold:    *maxHold,
		CostBps:    *cost,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "learning job failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report.Render())
}

// runExport prints a preset playbook as a strategy-config JSON that can be sent
// to POST /api/strategies (config field) to create a live, runnable strategy.
// It mutates nothing — it just emits the exact config the backtest validated.
func runExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	name := fs.String("name", "regime_grid", "preset: trend_ema_rsi|trend_adx|range_fade|regime_playbook|regime_grid")
	tf := fs.String("tf", "15m", "timeframe")
	sl := fs.Float64("sl", 2, "trend stop-loss percent")
	tp := fs.Float64("tp", 4, "trend take-profit percent")
	symbols := fs.String("symbols", "BTCUSDT,ETHUSDT", "static candidate coins")
	_ = fs.Parse(args)

	out, err := research.ExportPlaybookJSON(*name, *tf, *sl, *tp, *symbols)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

// openStore opens the store using the same configuration as the main binary.
func openStore() *store.Store {
	config.Init()
	cfg := config.Get()

	dbType := store.DBTypeSQLite
	if cfg.DBType == "postgres" {
		dbType = store.DBTypePostgres
	}
	st, err := store.NewWithConfig(store.DBConfig{
		Type:     dbType,
		Path:     cfg.DBPath,
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		SSLMode:  cfg.DBSSLMode,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %v\n", err)
		os.Exit(1)
	}
	return st
}
