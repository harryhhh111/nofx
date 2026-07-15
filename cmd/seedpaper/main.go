// Command seedpaper inserts (or updates) a self-contained PAPER trading setup
// for forward-validating the regime playbook: one rule-mode strategy + one
// dedicated paper trader bound to it. It NEVER touches existing traders or
// strategies — it copies the user/exchange/AI-model wiring from an existing
// trader so the new paper trader runs in the same account context.
//
// Idempotent: re-running updates the same strategy/trader (matched by name)
// instead of creating duplicates.
//
//	go run ./cmd/seedpaper                 # seed regime_grid on BTC/ETH, 15m, IsRunning=true
//	go run ./cmd/seedpaper --off           # same, but leave the trader stopped
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"nofx/config"
	"nofx/research"
	"nofx/store"
)

const (
	defaultStrategyName = "Playbook RegimeGrid (paper)"
	defaultTraderName   = "playbook-paper"
)

func main() {
	preset := flag.String("name", "regime_grid", "playbook preset")
	tf := flag.String("tf", "15m", "timeframe")
	sl := flag.Float64("sl", 2, "trend stop-loss percent")
	tp := flag.Float64("tp", 4, "trend take-profit percent")
	symbols := flag.String("symbols", "BTCUSDT,ETHUSDT", "static candidate coins")
	scan := flag.Int("scan", 15, "scan interval minutes")
	strategyName := flag.String("strategy-name", defaultStrategyName, "strategy name used for the upsert")
	traderName := flag.String("trader-name", defaultTraderName, "trader name used for the upsert")
	off := flag.Bool("off", false, "create the trader stopped (IsRunning=false)")
	flag.Parse()

	st := openStore()
	db := st.GormDB()

	// 1) Find an existing trader to inherit account wiring from (prefer paper).
	var traders []store.Trader
	if err := db.Order("created_at asc").Find(&traders).Error; err != nil {
		fatal("load traders: %v", err)
	}
	if len(traders) == 0 {
		fatal("no existing trader to copy user/exchange/ai-model from; create one in the UI first")
	}
	src := pickSource(db, traders)
	fmt.Printf("inherit from trader %q  user=%s exchange=%s aiModel=%s balance=%.2f\n",
		src.Name, short(src.UserID), short(src.ExchangeID), short(src.AIModelID), src.InitialBalance)

	// 2) Build the playbook rule config and fill in product defaults.
	cfg, desc, _, err := research.BuildPlaybookConfig(*preset, *tf, *sl, *tp)
	if err != nil {
		fatal("build playbook: %v", err)
	}
	cfg.StrategyPrompt = desc
	cfg.CoinSource = store.CoinSourceConfig{SourceType: "static", StaticCoins: parseSymbols(*symbols)}

	raw, err := json.Marshal(cfg)
	if err != nil {
		fatal("marshal cfg: %v", err)
	}
	defaulted, err := store.ParseStrategyConfigWithDefaults(raw, "zh")
	if err != nil {
		fatal("apply defaults: %v", err)
	}
	if defaulted.StrategyMode != "rule" || len(defaulted.CompiledRules) == 0 {
		fatal("sanity check failed: mode=%q rules=%d (expected rule mode with compiled rules)",
			defaulted.StrategyMode, len(defaulted.CompiledRules))
	}
	cfgJSON, err := json.Marshal(defaulted)
	if err != nil {
		fatal("marshal defaulted: %v", err)
	}

	// 3) Upsert the strategy (matched by name within the same user).
	var strat store.Strategy
	err = db.Where("user_id = ? AND name = ?", src.UserID, *strategyName).First(&strat).Error
	if err != nil {
		strat = store.Strategy{
			ID:          uuid.New().String(),
			UserID:      src.UserID,
			Name:        *strategyName,
			Description: "Forward-validation playbook (auto-seeded). Rule mode, regime-gated.",
			IsActive:    true,
			Config:      string(cfgJSON),
		}
		if err := db.Create(&strat).Error; err != nil {
			fatal("create strategy: %v", err)
		}
		fmt.Printf("created strategy %s (%s) with %d rules\n", strat.Name, short(strat.ID), len(defaulted.CompiledRules))
	} else {
		strat.Config = string(cfgJSON)
		strat.IsActive = true
		if err := db.Save(&strat).Error; err != nil {
			fatal("update strategy: %v", err)
		}
		fmt.Printf("updated strategy %s (%s) with %d rules\n", strat.Name, short(strat.ID), len(defaulted.CompiledRules))
	}

	// 4) Upsert the dedicated paper trader (matched by name within the same user).
	running := !*off
	var tr store.Trader
	err = db.Where("user_id = ? AND name = ?", src.UserID, *traderName).First(&tr).Error
	if err != nil {
		tr = store.Trader{
			ID:                  uuid.New().String(),
			UserID:              src.UserID,
			Name:                *traderName,
			AIModelID:           src.AIModelID,
			ExchangeID:          src.ExchangeID,
			StrategyID:          strat.ID,
			InitialBalance:      src.InitialBalance,
			ScanIntervalMinutes: *scan,
			IsRunning:           running,
			IsCrossMargin:       true,
		}
		if err := db.Create(&tr).Error; err != nil {
			fatal("create trader: %v", err)
		}
		fmt.Printf("created trader %s (%s) -> strategy %s  scan=%dmin running=%v\n",
			tr.Name, short(tr.ID), short(strat.ID), *scan, running)
	} else {
		tr.StrategyID = strat.ID
		tr.ScanIntervalMinutes = *scan
		tr.IsRunning = running
		if err := db.Save(&tr).Error; err != nil {
			fatal("update trader: %v", err)
		}
		fmt.Printf("updated trader %s (%s) -> strategy %s  scan=%dmin running=%v\n",
			tr.Name, short(tr.ID), short(strat.ID), *scan, running)
	}

	fmt.Println("\nDone. Start the app so AutoStartRunningTraders picks this up:")
	fmt.Println("  go run .")
	fmt.Println("Inspect anytime:   go run ./cmd/research config")
	fmt.Println("After trades close: go run ./cmd/research diagnose")
}

// pickSource prefers a trader whose exchange is a paper account so the seeded
// paper trader inherits a paper exchange (never a live one).
func pickSource(db *gorm.DB, traders []store.Trader) store.Trader {
	for _, t := range traders {
		var exType string
		if err := db.Model(&store.Exchange{}).
			Select("exchange_type").
			Where("id = ?", t.ExchangeID).
			Scan(&exType).Error; err == nil && strings.EqualFold(exType, "paper") {
			return t
		}
	}
	return traders[0]
}

func parseSymbols(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if t := strings.ToUpper(strings.TrimSpace(s)); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		out = []string{"BTCUSDT", "ETHUSDT"}
	}
	return out
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "(none)"
	}
	return id
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "seedpaper: "+format+"\n", args...)
	os.Exit(1)
}

func openStore() *store.Store {
	_ = godotenv.Load()
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
		fatal("failed to open database: %v", err)
	}
	return st
}
