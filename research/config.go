package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"nofx/store"

	"gorm.io/gorm"
)

// exchangeRow is a projection that deliberately avoids the encrypted API-key
// columns (which would require the crypto service to decrypt on read).
type exchangeRow struct {
	ID           string
	ExchangeType string
	AccountName  string
	Name         string
	Type         string
	Enabled      bool
	Testnet      bool
}

// ConfigReport is a read-only snapshot of traders / exchanges / strategies,
// used to decide exactly what to change for breadth data collection.
type ConfigReport struct {
	traders    []store.Trader
	exchanges  map[string]exchangeRow
	strategies map[string]store.Strategy
	stratOrder []string
}

// InspectConfig loads the current trader/exchange/strategy setup (read-only).
func InspectConfig(db *gorm.DB) (*ConfigReport, error) {
	if db == nil {
		return nil, fmt.Errorf("nil database handle")
	}
	r := &ConfigReport{
		exchanges:  map[string]exchangeRow{},
		strategies: map[string]store.Strategy{},
	}

	if err := db.Order("created_at asc").Find(&r.traders).Error; err != nil {
		return nil, fmt.Errorf("load traders: %w", err)
	}

	var exRows []exchangeRow
	if err := db.Model(&store.Exchange{}).
		Select("id", "exchange_type", "account_name", "name", "type", "enabled", "testnet").
		Scan(&exRows).Error; err != nil {
		return nil, fmt.Errorf("load exchanges: %w", err)
	}
	for _, e := range exRows {
		r.exchanges[e.ID] = e
	}

	var strategies []store.Strategy
	if err := db.Order("created_at asc").Find(&strategies).Error; err != nil {
		return nil, fmt.Errorf("load strategies: %w", err)
	}
	for _, s := range strategies {
		r.strategies[s.ID] = s
		r.stratOrder = append(r.stratOrder, s.ID)
	}
	return r, nil
}

// orDefault returns v, or def when v is empty.
func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// Render returns a human-readable configuration snapshot.
func (r *ConfigReport) Render() string {
	var b strings.Builder
	b.WriteString("==================================================================\n")
	b.WriteString(" NoFx Config Snapshot (read-only)\n")
	b.WriteString("==================================================================\n")

	b.WriteString(fmt.Sprintf(" Traders: %d\n", len(r.traders)))
	for _, t := range r.traders {
		ex := r.exchanges[t.ExchangeID]
		exType := ex.ExchangeType
		if exType == "" {
			exType = "(unknown)"
		}
		running := "stopped"
		if t.IsRunning {
			running = "RUNNING"
		}
		fmt.Fprintf(&b, "   • %-16s [%s] exchange=%s(%s) scan=%dmin strategy=%s\n",
			t.Name, running, exType, shortID(t.ExchangeID), t.ScanIntervalMinutes, shortID(t.StrategyID))
	}

	b.WriteString("\n Exchanges:\n")
	for _, t := range r.traders {
		if ex, ok := r.exchanges[t.ExchangeID]; ok {
			paper := ""
			if strings.EqualFold(ex.ExchangeType, "paper") {
				paper = "  <-- PAPER (safe for data collection)"
			}
			fmt.Fprintf(&b, "   • %s type=%s enabled=%v testnet=%v%s\n",
				orDefault(ex.AccountName, ex.Name), ex.ExchangeType, ex.Enabled, ex.Testnet, paper)
		}
	}

	// Strategy actually used = the one bound to a (running) trader; fall back to first.
	usedID := ""
	for _, t := range r.traders {
		if t.IsRunning && t.StrategyID != "" {
			usedID = t.StrategyID
			break
		}
	}
	if usedID == "" {
		for _, t := range r.traders {
			if t.StrategyID != "" {
				usedID = t.StrategyID
				break
			}
		}
	}
	if usedID == "" && len(r.stratOrder) > 0 {
		usedID = r.stratOrder[0]
	}

	b.WriteString("\n Strategy in use:\n")
	strat, ok := r.strategies[usedID]
	if !ok {
		b.WriteString("   (could not resolve the active strategy)\n")
		return b.String()
	}
	fmt.Fprintf(&b, "   name: %s   id: %s   active=%v\n", strat.Name, shortID(strat.ID), strat.IsActive)

	var cfg store.StrategyConfig
	if err := json.Unmarshal([]byte(strat.Config), &cfg); err != nil {
		fmt.Fprintf(&b, "   (failed to parse config: %v)\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "   strategy_mode: %s\n", orDefault(cfg.StrategyMode, "(unset)"))
	b.WriteString("   coin_source:\n")
	b.WriteString(indent(prettyJSON(cfg.CoinSource), "     "))
	if cfg.ScoringConfig != nil {
		b.WriteString("   scoring_config (key fields):\n")
		b.WriteString(indent(prettyJSON(cfg.ScoringConfig), "     "))
	} else {
		b.WriteString("   scoring_config: (none — strategy may be rule-based)\n")
	}

	b.WriteString("\n")
	b.WriteString(configVerdict(cfg))
	return b.String()
}

func configVerdict(cfg store.StrategyConfig) string {
	cs := cfg.CoinSource
	universe := 0
	switch cs.SourceType {
	case "static":
		universe = len(cs.StaticCoins)
	case "ai500":
		universe = cs.AI500Limit
	}
	var b strings.Builder
	b.WriteString(" VERDICT (breadth for research):\n")
	fmt.Fprintf(&b, "   source_type=%q approx universe per cycle: %d\n", cs.SourceType, universe)
	if universe > 0 && universe < 5 {
		b.WriteString("   >> universe is too small for edge research. Widen it (e.g. ai500 limit 10,\n")
		b.WriteString("      or a static list of ~10 diverse symbols) and run in PAPER mode for weeks.\n")
	} else {
		b.WriteString("   >> universe size is adequate; ensure it runs in PAPER mode long enough (>=1 week).\n")
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "(none)"
	}
	return id
}

func prettyJSON(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("(marshal error: %v)\n", err)
	}
	return string(data) + "\n"
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n") + "\n"
}
