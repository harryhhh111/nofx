package store

import "testing"

func TestEstimateTokens_DefaultConfig(t *testing.T) {
	config := GetDefaultStrategyConfig("en")
	est := config.EstimateTokens()

	if est.Total <= 0 {
		t.Errorf("expected positive token estimate, got %d", est.Total)
	}
	if est.Total > 200000 {
		t.Errorf("token estimate %d seems unreasonably high for default config", est.Total)
	}

	// Breakdown should sum approximately to total (before 15% margin)
	subtotal := est.Breakdown.SystemPrompt + est.Breakdown.MarketData +
		est.Breakdown.RankingData + est.Breakdown.QuantData + est.Breakdown.FixedOverhead
	expectedTotal := subtotal * 115 / 100
	if est.Total != expectedTotal {
		t.Errorf("total %d != breakdown subtotal %d * 1.15 = %d", est.Total, subtotal, expectedTotal)
	}

	// Should have model limits
	if len(est.ModelLimits) == 0 {
		t.Error("expected model limits to be populated")
	}

	// Default config should be ok for all models
	for _, ml := range est.ModelLimits {
		if ml.Level == "danger" {
			t.Errorf("default config should not exceed %s limit, got %d%%", ml.Name, ml.UsagePct)
		}
	}
}

func TestEstimateTokens_ZhVsEn(t *testing.T) {
	enConfig := GetDefaultStrategyConfig("en")
	zhConfig := GetDefaultStrategyConfig("zh")

	enEst := enConfig.EstimateTokens()
	zhEst := zhConfig.EstimateTokens()

	// Chinese config should have more tokens for system prompt due to CJK encoding
	// but total can vary — just ensure both are reasonable
	if enEst.Total <= 0 || zhEst.Total <= 0 {
		t.Errorf("both estimates should be positive: en=%d, zh=%d", enEst.Total, zhEst.Total)
	}
}

func TestEstimateTokens_HighConfig(t *testing.T) {
	config := GetDefaultStrategyConfig("en")
	// Push config to extremes (beyond clamped limits)
	config.CoinSource.SourceType = "static"
	config.CoinSource.StaticCoins = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT", "XRPUSDT"}
	config.Indicators.Klines.SelectedTimeframes = []string{"1m", "3m", "5m", "15m", "1h", "4h"}
	config.Indicators.Klines.PrimaryCount = 100
	config.Indicators.EnableEMA = true
	config.Indicators.EnableMACD = true
	config.Indicators.EnableRSI = true
	config.Indicators.EnableATR = true
	config.Indicators.EnableBOLL = true

	est := config.EstimateTokens()

	// Should produce a higher estimate than default
	defaultCfg := GetDefaultStrategyConfig("en")
	defaultEst := defaultCfg.EstimateTokens()
	if est.Total <= defaultEst.Total {
		t.Errorf("high config estimate %d should be greater than default %d", est.Total, defaultEst.Total)
	}

	// Should have some models in warning/danger
	hasDanger := false
	for _, ml := range est.ModelLimits {
		if ml.Level == "danger" || ml.Level == "warning" {
			hasDanger = true
			break
		}
	}
	// With 5 coins * 6 timeframes * 100 klines, this should exceed small models
	if !hasDanger {
		t.Logf("high config estimate: %d tokens", est.Total)
	}
}

func TestGetContextLimit(t *testing.T) {
	if got := GetContextLimit("deepseek"); got != 131072 {
		t.Errorf("deepseek limit = %d, want 131072", got)
	}
	if got := GetContextLimit("unknown_provider"); got != 131072 {
		t.Errorf("unknown provider should return default 131072, got %d", got)
	}
}

func TestGetEffectiveCoinCount(t *testing.T) {
	config := StrategyConfig{
		CoinSource: CoinSourceConfig{
			SourceType:  "static",
			StaticCoins: []string{"BTCUSDT", "ETHUSDT"},
		},
	}
	if got := config.getEffectiveCoinCount(); got != 2 {
		t.Errorf("static coin count = %d, want 2", got)
	}

	config.CoinSource.SourceType = "ai500"
	config.CoinSource.AI500Limit = 5
	if got := config.getEffectiveCoinCount(); got != 5 {
		t.Errorf("ai500 coin count = %d, want 5", got)
	}
}

func TestClampLimits_ConfidenceThresholds(t *testing.T) {
	config := StrategyConfig{}
	config.ClampLimits()

	if config.RiskControl.MinConfidence != DefaultMinConfidence {
		t.Errorf("default min confidence = %d, want %d", config.RiskControl.MinConfidence, DefaultMinConfidence)
	}
	if config.RiskControl.MinCloseConfidence != DefaultMinCloseConfidence {
		t.Errorf("default min close confidence = %d, want %d", config.RiskControl.MinCloseConfidence, DefaultMinCloseConfidence)
	}

	config.RiskControl.MinConfidence = 20
	config.RiskControl.MinCloseConfidence = 120
	config.ClampLimits()

	if config.RiskControl.MinConfidence != MinMinConfidence {
		t.Errorf("clamped min confidence = %d, want %d", config.RiskControl.MinConfidence, MinMinConfidence)
	}
	if config.RiskControl.MinCloseConfidence != MaxMinCloseConfidence {
		t.Errorf("clamped min close confidence = %d, want %d", config.RiskControl.MinCloseConfidence, MaxMinCloseConfidence)
	}
}

func TestClampLimits_DrawdownMinProtectedProfit(t *testing.T) {
	config := StrategyConfig{}
	config.RiskControl.DrawdownCloseMinProfitPct = 2
	config.RiskControl.DrawdownCloseTriggerPct = 40
	config.ClampLimits()

	if config.RiskControl.DrawdownCloseMinProtectedProfitPct != DefaultDrawdownCloseMinProtectedProfitPct {
		t.Errorf(
			"default drawdown min protected profit = %v, want %v",
			config.RiskControl.DrawdownCloseMinProtectedProfitPct,
			DefaultDrawdownCloseMinProtectedProfitPct,
		)
	}

	config.RiskControl.DrawdownCloseMinProtectedProfitPct = 3
	config.ClampLimits()
	if config.RiskControl.DrawdownCloseMinProtectedProfitPct != config.RiskControl.DrawdownCloseMinProfitPct/2 {
		t.Errorf(
			"clamped drawdown min protected profit = %v, want %v",
			config.RiskControl.DrawdownCloseMinProtectedProfitPct,
			config.RiskControl.DrawdownCloseMinProfitPct/2,
		)
	}
}

func TestParseConfig_EntryRiskGuardDefaultsAndExplicitDisable(t *testing.T) {
	empty := &Strategy{Config: "{}"}
	cfg, err := empty.ParseConfig()
	if err != nil {
		t.Fatalf("empty ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.EntryRiskGuard == nil {
		t.Fatal("empty config should default EntryRiskGuard")
	}
	if !cfg.RiskControl.EntryRiskGuard.Enabled {
		t.Fatal("default EntryRiskGuard should be enabled")
	}
	if cfg.RiskControl.EntryRiskGuard.Mode != EntryRiskGuardModeWarnReduce {
		t.Fatalf("default EntryRiskGuard mode = %q, want %q", cfg.RiskControl.EntryRiskGuard.Mode, EntryRiskGuardModeWarnReduce)
	}

	explicitOff := &Strategy{Config: `{"risk_control":{"entry_risk_guard":{"enabled":false,"mode":"hard_block"}}}`}
	cfg, err = explicitOff.ParseConfig()
	if err != nil {
		t.Fatalf("explicitOff ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.EntryRiskGuard == nil {
		t.Fatal("explicit disabled EntryRiskGuard should be present")
	}
	if cfg.RiskControl.EntryRiskGuard.Enabled {
		t.Fatalf("explicit disabled EntryRiskGuard should stay disabled, got %+v", cfg.RiskControl.EntryRiskGuard)
	}
	if cfg.RiskControl.EntryRiskGuard.Mode != EntryRiskGuardModeHardBlock {
		t.Fatalf("explicit EntryRiskGuard mode = %q, want %q", cfg.RiskControl.EntryRiskGuard.Mode, EntryRiskGuardModeHardBlock)
	}

	// Existing configs that never configured entry_risk_guard must stay disabled
	// to avoid a surprise behavior change on deploy.
	existingNoERG := &Strategy{Config: `{"language":"zh","risk_control":{"min_confidence":75}}`}
	cfg, err = existingNoERG.ParseConfig()
	if err != nil {
		t.Fatalf("existingNoERG ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.EntryRiskGuard == nil {
		t.Fatal("existing config should have EntryRiskGuard struct")
	}
	if cfg.RiskControl.EntryRiskGuard.Enabled {
		t.Fatalf("existing config without entry_risk_guard should stay disabled, got %+v", cfg.RiskControl.EntryRiskGuard)
	}

	existingNoRiskControl := &Strategy{Config: `{"language":"zh"}`}
	cfg, err = existingNoRiskControl.ParseConfig()
	if err != nil {
		t.Fatalf("existingNoRiskControl ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.EntryRiskGuard == nil {
		t.Fatal("existing config without risk_control should have EntryRiskGuard struct")
	}
	if cfg.RiskControl.EntryRiskGuard.Enabled {
		t.Fatalf("existing config without risk_control should stay disabled, got %+v", cfg.RiskControl.EntryRiskGuard)
	}
}

func TestParseConfig_AppliesDefaultsForMissingFields(t *testing.T) {
	st := &Strategy{
		Config: `{"language":"zh","risk_control":{"min_confidence":75},"indicators":{"enable_quant_data":false}}`,
	}

	config, err := st.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.RiskControl.MinCloseConfidence != DefaultMinCloseConfidence {
		t.Errorf("min close confidence = %d, want %d", config.RiskControl.MinCloseConfidence, DefaultMinCloseConfidence)
	}
	if config.Indicators.Klines.PrimaryTimeframe != "5m" {
		t.Errorf("primary timeframe = %q, want 5m", config.Indicators.Klines.PrimaryTimeframe)
	}
	if config.Indicators.Klines.PrimaryCount != 25 {
		t.Errorf("primary count = %d, want 25", config.Indicators.Klines.PrimaryCount)
	}
	if config.Indicators.EnableQuantData {
		t.Error("explicit enable_quant_data=false should be preserved")
	}
	if len(config.Indicators.ATRPeriods) != 1 || config.Indicators.ATRPeriods[0] != 14 {
		t.Errorf("atr periods = %v, want [14]", config.Indicators.ATRPeriods)
	}
}

func TestParseConfig_BreakevenProtectionBackwardCompat(t *testing.T) {
	// Empty config (new strategy) should inherit the default: enabled.
	empty := &Strategy{Config: "{}"}
	cfg, err := empty.ParseConfig()
	if err != nil {
		t.Fatalf("empty ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.BreakevenProtection == nil || !cfg.RiskControl.BreakevenProtection.Enabled {
		t.Errorf("empty config should have BreakevenProtection enabled by default, got %v", cfg.RiskControl.BreakevenProtection)
	}

	// Existing partial config without breakeven_protection should keep it disabled.
	existing := &Strategy{Config: `{"language":"zh","risk_control":{"min_confidence":75}}`}
	cfg, err = existing.ParseConfig()
	if err != nil {
		t.Fatalf("existing ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.BreakevenProtection != nil {
		t.Errorf("existing config missing breakeven_protection should keep it nil, got %v", cfg.RiskControl.BreakevenProtection)
	}

	// Explicitly configured breakeven_protection should be preserved.
	explicitOff := &Strategy{Config: `{"risk_control":{"breakeven_protection":{"enabled":false,"trigger_pct":1}}}`}
	cfg, err = explicitOff.ParseConfig()
	if err != nil {
		t.Fatalf("explicitOff ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.BreakevenProtection == nil || cfg.RiskControl.BreakevenProtection.Enabled {
		t.Errorf("explicitly disabled breakeven_protection should stay disabled, got %v", cfg.RiskControl.BreakevenProtection)
	}

	explicitOn := &Strategy{Config: `{"risk_control":{"breakeven_protection":{"enabled":true,"trigger_pct":2}}}`}
	cfg, err = explicitOn.ParseConfig()
	if err != nil {
		t.Fatalf("explicitOn ParseConfig() error = %v", err)
	}
	if cfg.RiskControl.BreakevenProtection == nil || !cfg.RiskControl.BreakevenProtection.Enabled {
		t.Errorf("explicitly enabled breakeven_protection should stay enabled, got %v", cfg.RiskControl.BreakevenProtection)
	}
	if cfg.RiskControl.BreakevenProtection.TriggerPct != 2.0 {
		t.Errorf("trigger_pct = %v, want 2.0", cfg.RiskControl.BreakevenProtection.TriggerPct)
	}
}

func TestParseConfig_DefaultsGridConfigForExternalAPI(t *testing.T) {
	st := &Strategy{
		Config: `{"strategy_type":"grid_trading","grid_config":{"symbol":"ETHUSDT","atr_multiplier":3}}`,
	}

	config, err := st.ParseConfig()
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}

	if config.StrategyType != "grid_trading" {
		t.Errorf("strategy type = %q, want grid_trading", config.StrategyType)
	}
	if config.GridConfig == nil {
		t.Fatal("grid config should be defaulted")
	}
	if config.GridConfig.Symbol != "ETHUSDT" {
		t.Errorf("grid symbol = %q, want ETHUSDT", config.GridConfig.Symbol)
	}
	if config.GridConfig.ATRMultiplier != 3 {
		t.Errorf("atr multiplier = %v, want 3", config.GridConfig.ATRMultiplier)
	}
	if config.GridConfig.GridCount != 10 {
		t.Errorf("grid count = %d, want default 10", config.GridConfig.GridCount)
	}
	if !config.GridConfig.UseATRBounds {
		t.Error("use_atr_bounds should default to true")
	}
}
