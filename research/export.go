package research

import (
	"encoding/json"
	"fmt"
	"strings"

	"nofx/store"
)

// ExportPlaybookJSON builds a preset regime playbook and returns it as a
// strategy-config JSON. Send it as the "config" field to POST /api/strategies
// to create a live, runnable rule-mode strategy — what you backtest with
// `research rule --name <preset>` is exactly what you ship.
//
// The output is a PARTIAL config on purpose: the create endpoint merges it with
// system defaults (risk management, intervals, etc.), so we only specify the
// parts that define the strategy's behavior (mode, indicators, rules, coins).
func ExportPlaybookJSON(name, tf string, stopPct, takePct float64, symbolsCSV string) (string, error) {
	cfg, desc, _, err := BuildPlaybookConfig(name, tf, stopPct, takePct)
	if err != nil {
		return "", err
	}

	var coins []string
	for _, s := range strings.Split(symbolsCSV, ",") {
		if t := strings.ToUpper(strings.TrimSpace(s)); t != "" {
			coins = append(coins, t)
		}
	}
	if len(coins) == 0 {
		coins = []string{"BTCUSDT", "ETHUSDT"}
	}

	cfg.StrategyPrompt = desc
	cfg.CoinSource = store.CoinSourceConfig{
		SourceType:  "static",
		StaticCoins: coins,
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(out), nil
}
