package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"nofx/store"
)

// StrategyConfigFingerprint returns a stable audit version for deterministic
// setup/scoring strategies after the same normalization used by live trading.
func StrategyConfigFingerprint(config *store.StrategyConfig) string {
	if config == nil {
		return ""
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return ""
	}
	var normalized store.StrategyConfig
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return ""
	}
	normalized.NormalizeForExecution()
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "cfg_" + hex.EncodeToString(sum[:])[:12]
}
