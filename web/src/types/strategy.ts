// Strategy Studio Types
export interface Strategy {
  id: string;
  name: string;
  description: string;
  is_active: boolean;
  is_default: boolean;
  is_public: boolean;           // 是否在策略市场公开
  config_visible: boolean;      // 配置参数是否公开可见
  config: StrategyConfig;
  created_at: string;
  updated_at: string;
}

// 策略使用统计
export interface StrategyStats {
  clone_count: number;          // 被克隆次数
  active_users: number;         // 当前使用人数
  top_performers?: StrategyPerformer[];  // 收益排行
}

// 策略使用者收益排行
export interface StrategyPerformer {
  user_id: string;
  user_name: string;            // 脱敏后的用户名
  total_pnl_pct: number;        // 总收益率
  total_pnl: number;            // 总收益金额
  win_rate: number;             // 胜率
  trade_count: number;          // 交易次数
  using_since: string;          // 使用开始时间
  rank: number;                 // 排名
}

export interface StrategyConfig {
  // Strategy type: "ai_trading" (default) or "grid_trading"
  strategy_type?: 'ai_trading' | 'grid_trading';
  strategy_mode?: 'rule' | 'scoring' | 'hybrid';
  // Language setting: "zh" for Chinese, "en" for English
  language?: 'zh' | 'en';
  coin_source: CoinSourceConfig;
  indicators: IndicatorConfig;
  structure?: StructureFactorConfig;
  include_historical_context?: boolean;
  risk_control: RiskControlConfig;
  strategy_prompt?: string;
  compiled_rules?: CompiledStrategyRule[];
  scoring_config?: ScoringStrategyConfig;
  resolved_parameters?: ResolvedStrategyParameters;
  // Grid trading configuration (only used when strategy_type is 'grid_trading')
  grid_config?: GridStrategyConfig;
}

export interface ResolvedStrategyParameters {
  structure?: ResolvedStructureParameters;
  scoring?: ScoringStrategyConfig;
}

export interface ResolvedStructureParameters {
  fibonacci?: StructureFibonacciConfig;
  support_resistance?: StructureSupportResistanceConfig;
}

export interface CompiledStrategyRule {
  id: string;
  version: string;
  description?: string;
  symbols?: string[];
  timeframe?: string;
  conditions: CompiledRuleCondition[];
  action: string;
  execution: CompiledRuleExecution;
  enabled: boolean;
}

export interface CompiledRuleExecution {
  leverage?: number;
  position_size_usd?: number;
  stop_loss_pct?: number;
  take_profit_pct?: number;
  confidence?: number;
}

export interface CompiledRuleCondition {
  left: CompiledRuleOperand;
  operator: string;
  right: CompiledRuleOperand;
}

export interface CompiledRuleOperand {
  kind: 'indicator' | 'external_factor' | 'structure' | 'literal';
  name?: string;
  timeframe?: string;
  period?: number;
  field?: string;
  value?: number;
}

export interface ScoringStrategyConfig {
  enabled: boolean;
  selected_factors?: string[];
  factor_weights?: Record<string, number>;
  long_threshold?: number;
  short_threshold?: number;
  min_available_weight_ratio?: number;
  min_confidence?: number;
  timeframe?: string;
  symbols?: string[];
  execution: CompiledRuleExecution;
}

export interface StrategyEvolutionProposal {
  proposal_id: string;
  strategy_id: string;
  base_version?: string;
  proposed_version: string;
  trigger: string;
  summary: string;
  change_reasons?: string[];
  parameter_changes?: StrategyParamChange[];
  expected_impact?: string;
  risks?: string[];
  proposed_config_patch?: Record<string, unknown>;
  requires_approval: boolean;
  created_at: string;
}

export interface StrategyCompileResponse {
  strategy_prompt: string;
  strategy_mode?: 'rule' | 'scoring' | 'hybrid';
  compiled_rules?: CompiledStrategyRule[];
  scoring_config?: ScoringStrategyConfig;
  resolved_parameters?: ResolvedStrategyParameters;
  warnings?: string[];
  errors?: string[];
  persisted?: boolean;
}

export interface StrategyPreviewFlowResponse {
  [key: string]: unknown;
}

export interface StrategyTestRunResponse {
  [key: string]: unknown;
}

export interface StrategyMetadataTimeframe {
  value: string;
  label: string;
  category: string;
}

export interface StrategyMetadataIndicator {
  key: keyof IndicatorConfig;
  label: string;
  desc: string;
  color: string;
  period_key?: keyof IndicatorConfig;
  default_periods?: number[];
  operands?: string[];
}

export interface StrategyMetadata {
  timeframes: StrategyMetadataTimeframe[];
  technical_indicators: StrategyMetadataIndicator[];
  always_calculated_indicators?: string[];
  indicator_operands: string[];
  structure_operands: string[];
  external_factors: string[];
  external_factor_prefixes: string[];
  scoring_factors: string[];
}

export interface StrategyParamChange {
  path: string;
  old_value?: unknown;
  new_value?: unknown;
  reason?: string;
}

export interface StructureFactorConfig {
  enable_fibonacci: boolean;
  enable_support_resistance: boolean;
  fibonacci?: StructureFibonacciConfig;
  support_resistance?: StructureSupportResistanceConfig;
}

export interface StructureFibonacciConfig {
  timeframe?: string;
  lookback?: number;
  swing_window?: number;
  min_leg_bars?: number;
  min_leg_atr_multiple?: number;
  zigzag_threshold_pct?: number;
  levels?: number[];
  invalidate_on_break_base: boolean;
}

export interface StructureSupportResistanceConfig {
  timeframe?: string;
  lookback?: number;
  swing_window?: number;
  zone_width_atr?: number;
  min_touches?: number;
  min_distance_bars?: number;
}

// Grid trading specific configuration
export interface GridStrategyConfig {
  // Trading pair (e.g., "BTCUSDT")
  symbol: string;
  // Number of grid levels (5-50)
  grid_count: number;
  // Total investment in USDT
  total_investment: number;
  // Leverage (1-20)
  leverage: number;
  // Upper price boundary (0 = auto-calculate from ATR)
  upper_price: number;
  // Lower price boundary (0 = auto-calculate from ATR)
  lower_price: number;
  // Use ATR to auto-calculate bounds
  use_atr_bounds: boolean;
  // ATR multiplier for bound calculation (default 2.0)
  atr_multiplier: number;
  // Position distribution: "uniform" | "gaussian" | "pyramid"
  distribution: 'uniform' | 'gaussian' | 'pyramid';
  // Maximum drawdown percentage before emergency exit
  max_drawdown_pct: number;
  // Stop loss percentage per position
  stop_loss_pct: number;
  // Daily loss limit percentage
  daily_loss_limit_pct: number;
  // Use maker-only orders for lower fees
  use_maker_only: boolean;
  // Enable automatic grid direction adjustment based on box breakouts
  enable_direction_adjust?: boolean;
  // Direction bias ratio for long_bias/short_bias modes (default 0.7 = 70%/30%)
  direction_bias_ratio?: number;
}

export interface CoinSourceConfig {
  source_type: 'static' | 'ai500' | 'oi_top' | 'oi_low' | 'mixed';
  static_coins?: string[];
  excluded_coins?: string[];   // 排除的币种列表
  use_ai500: boolean;
  ai500_limit?: number;
  use_oi_top: boolean;
  oi_top_limit?: number;
  use_oi_low: boolean;
  oi_low_limit?: number;
  // Note: API URLs are now built automatically using nofxos_api_key from IndicatorConfig
}

export interface IndicatorConfig {
  klines: KlineConfig;
  // Raw OHLCV kline data - required for deterministic indicator computation
  enable_raw_klines: boolean;
  // Technical indicators (optional)
  enable_ema: boolean;
  enable_sma: boolean;
  enable_macd: boolean;
  enable_rsi: boolean;
  enable_atr: boolean;
  enable_adx: boolean;
  enable_sar: boolean;
  enable_boll: boolean;
  enable_session: boolean;
  enable_volume: boolean;
  enable_oi: boolean;
  enable_funding_rate: boolean;
  ema_periods?: number[];
  sma_periods?: number[];
  rsi_periods?: number[];
  atr_periods?: number[];
  boll_periods?: number[];
  macd_fast_period?: number;
  macd_slow_period?: number;
  macd_signal_period?: number;
  volume_periods?: number[];
  vwap_periods?: number[];
  donchian_periods?: number[];
  realized_vol_periods?: number[];
  price_change_windows?: number[];
  sessions?: SessionSpec[];
  external_data_sources?: ExternalDataSource[];

  // ========== NofxOS 数据源统一配置 ==========
  // Unified NofxOS API Key - used for all NofxOS data sources
  nofxos_api_key?: string;

  // 量化数据源（资金流向、持仓变化、价格变化）
  enable_quant_data?: boolean;
  enable_quant_oi?: boolean;
  enable_quant_netflow?: boolean;

  // OI 排行数据（市场持仓量增减排行）
  enable_oi_ranking?: boolean;
  oi_ranking_duration?: string;  // "1h", "4h", "24h"
  oi_ranking_limit?: number;

  // NetFlow 排行数据（机构/散户资金流向排行）
  enable_netflow_ranking?: boolean;
  netflow_ranking_duration?: string;  // "1h", "4h", "24h"
  netflow_ranking_limit?: number;

  // Price 排行数据（涨跌幅排行）
  enable_price_ranking?: boolean;
  price_ranking_duration?: string;  // "1h", "4h", "24h" or "1h,4h,24h"
  price_ranking_limit?: number;
}

export interface KlineConfig {
  primary_timeframe: string;
  primary_count: number;
  compute_lookback?: number;
  prompt_display_count?: number;
  include_open_bar?: boolean;
  longer_timeframe?: string;
  longer_count?: number;
  enable_multi_timeframe: boolean;
  // 新增：支持选择多个时间周期
  selected_timeframes?: string[];
}

export interface ExternalDataSource {
  name: string;
  type: 'api' | 'webhook';
  url: string;
  method: string;
  headers?: Record<string, string>;
  data_path?: string;
  refresh_secs?: number;
}

export interface SessionSpec {
  timezone: string;
  offset: string;
  duration: number;
}

export interface RiskControlConfig {
  // Max number of coins held simultaneously (CODE ENFORCED)
  max_positions: number;

  // Trading Leverage - exchange leverage for opening positions (AI guided)
  btc_eth_max_leverage: number;    // BTC/ETH max exchange leverage
  altcoin_max_leverage: number;    // Altcoin max exchange leverage

  // Position Value Ratio - single position notional value / account equity (CODE ENFORCED)
  // Max position value = equity × this ratio
  btc_eth_max_position_value_ratio?: number;     // default: 5 (BTC/ETH max position = 5x equity)
  altcoin_max_position_value_ratio?: number;     // default: 1 (Altcoin max position = 1x equity)

  // Risk Parameters
  max_margin_usage: number;        // Max margin utilization, e.g. 0.9 = 90% (CODE ENFORCED)
  min_position_size: number;       // Min position size in USDT (CODE ENFORCED)
  min_risk_reward_ratio: number;   // Min take_profit / stop_loss ratio (AI guided)
  min_confidence: number;          // Min AI confidence to open position (AI guided)
  min_close_confidence: number;    // Min AI confidence to proactively close early (AI guided)
  stop_loss_atr_buffer?: number;   // Stop loss ATR buffer multiplier (0 = use mode default)

  // Drawdown-based position close (risk monitor, runs every minute)
  drawdown_close_enabled?: boolean;         // Whether the mechanism is enabled (default: true)
  drawdown_close_min_profit_pct?: number;   // Min leveraged profit (%) before measuring drawdown (default: 5)
  drawdown_close_trigger_pct?: number;      // Drawdown % from peak that triggers action (default: 40)
  drawdown_close_use_ai?: boolean;          // false=close immediately, true=let AI decide (default: false)
}
