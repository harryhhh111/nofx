export interface SystemStatus {
  trader_id: string
  trader_name: string
  ai_model: string
  is_running: boolean
  start_time: string
  runtime_minutes: number
  call_count: number
  initial_balance: number
  scan_interval: string
  stop_until: string
  last_reset_time: string
  ai_provider: string
  strategy_type?: 'ai_trading' | 'grid_trading'
  grid_symbol?: string
}

export interface AccountInfo {
  total_equity: number
  wallet_balance: number
  unrealized_profit: number // 鏈疄鐜扮泩浜忥紙浜ゆ槗鎵€API瀹樻柟鍊硷級
  available_balance: number
  total_pnl: number
  total_pnl_pct: number
  initial_balance: number
  daily_pnl: number
  position_count: number
  margin_used: number
  margin_used_pct: number
}

export interface Position {
  symbol: string
  side: string
  entry_price: number
  mark_price: number
  quantity: number
  leverage: number
  unrealized_pnl: number
  unrealized_pnl_pct: number
  liquidation_price: number
  margin_used: number
  position_id?: number
  entry_order_id?: string
  entry_time?: number
  entry_quantity?: number
  stop_loss_price?: number
  take_profit_price?: number
  stop_loss_order_id?: string
  take_profit_order_id?: string
  stop_loss_source?: string
  stop_loss_timeframe?: string
  stop_loss_anchor?: number
  stop_loss_policy?: string
  take_profit_source?: string
  take_profit_timeframe?: string
  take_profit_anchor?: number
  take_profit_policy?: string
  take_profit_candidate_count?: number
  take_profit_min_risk_reward?: number
  take_profit_min_atr_distance?: number
  take_profit_selected_risk_reward?: number
  take_profit_selected_atr_distance?: number
  take_profit_qualified?: boolean
  nearest_take_profit?: number
  nearest_take_profit_risk_reward?: number
  nearest_take_profit_atr_distance?: number
  protective_atr?: number
  protective_atr_timeframe?: string
  protective_atr_buffer?: number
  protective_risk_reward?: number
  execution_risk_reward?: number
  exit_price?: number
  exit_order_id?: string
  exit_time?: number
  close_reason?: string
  opening_setup?: string
  opening_reasoning?: string
  last_review_summary?: string
  last_review_cycle?: number
}

export interface DecisionAction {
  action: string
  symbol: string
  quantity: number
  leverage: number
  price: number
  stop_loss?: number // Stop loss price
  take_profit?: number // Take profit price
  stop_loss_source?: string
  stop_loss_timeframe?: string
  stop_loss_anchor?: number
  stop_loss_policy?: string
  take_profit_source?: string
  take_profit_timeframe?: string
  take_profit_anchor?: number
  take_profit_policy?: string
  take_profit_candidate_count?: number
  take_profit_min_risk_reward?: number
  take_profit_min_atr_distance?: number
  take_profit_selected_risk_reward?: number
  take_profit_selected_atr_distance?: number
  take_profit_qualified?: boolean
  nearest_take_profit?: number
  nearest_take_profit_risk_reward?: number
  nearest_take_profit_atr_distance?: number
  protective_atr?: number
  protective_atr_timeframe?: string
  protective_atr_buffer?: number
  protective_risk_reward?: number
  execution_risk_reward?: number
  confidence?: number // AI confidence (0-100)
  reasoning?: string // Brief reasoning
  order_id: number
  timestamp: string
  success: boolean
  error?: string
}

export interface AccountSnapshot {
  total_balance: number
  available_balance: number
  total_unrealized_profit: number
  position_count: number
  margin_used_pct: number
}

export interface DecisionRecord {
  timestamp: string
  cycle_number: number
  system_prompt: string
  input_prompt: string
  cot_trace: string
  cot_summary?: string
  judgement_summary?: string
  judgment_summary?: string
  user_decision_summary?: UserDecisionSummary
  decision_json: string
  account_state: AccountSnapshot
  positions: any[]
  candidate_coins: string[]
  decisions: DecisionAction[]
  execution_log: string[]
  success: boolean
  error_message?: string
}

export interface UserDecisionSummary {
  status: string
  headline: string
  steps?: UserDecisionSummaryStep[]
  symbols?: UserDecisionSymbolSummary[]
}

export interface UserDecisionSummaryStep {
  title: string
  status: string
  summary: string
}

export interface UserDecisionSymbolSummary {
  symbol: string
  decision: string
  reason: string
  details?: string[]
}

export interface Statistics {
  total_cycles: number
  successful_cycles: number
  failed_cycles: number
  total_open_positions: number
  total_close_positions: number
}

export interface BBMACDAccuracyBucket {
  total: number
  resolved_3: number
  correct_3: number
  accuracy_3: number
  avg_return_3: number
  resolved_5: number
  correct_5: number
  accuracy_5: number
  avg_return_5: number
  resolved_10: number
  correct_10: number
  accuracy_10: number
  avg_return_10: number
}

export interface BBMACDAccuracySummary {
  resolved: number
  correct: number
  accuracy: number
  avg_return: number
}

export interface BBMACDStateStat extends BBMACDAccuracyBucket {
  state: string
}

export interface BBMACDTimeframeStat {
  timeframe: string
  signals: number
  overall: BBMACDAccuracySummary
  effective: BBMACDAccuracySummary
}

export interface BBMACDConfig {
  use_custom: boolean
  fast: number
  slow: number
  signal: number
  boll_period: number
  boll_multiplier: number
}

export interface BBMACDAccuracyStats {
  trader_id: string
  days: number
  total: number
  effective_threshold_pct: number
  overall: BBMACDAccuracySummary
  effective: BBMACDAccuracySummary
  breakout_overall: BBMACDAccuracySummary
  breakout_effective: BBMACDAccuracySummary
  directional: BBMACDAccuracyBucket
  breakout: BBMACDAccuracyBucket
  by_state: BBMACDStateStat[]
  by_timeframe: BBMACDTimeframeStat[]
}

// AI Trading鐩稿叧绫诲瀷
export interface TradeMemory {
  id: number
  trader_id: string
  strategy_id?: string
  strategy_version?: string
  symbol: string
  side?: string
  action?: string
  scope: string
  source_type: string
  signal_id?: string
  decision_id?: number
  position_id?: number
  result?: string
  outcome_pnl?: number
  outcome_pnl_pct?: number
  summary: string
  evidence?: string
  lessons_json?: string
  tags_json?: string
  quality_score: number
  confidence: number
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface ExecutionAnalytics {
  id: number
  trader_id: string
  exchange_id?: string
  exchange_type?: string
  symbol: string
  action: string
  exchange_order_id?: string
  signal_generated_at?: number
  order_submitted_at?: number
  first_fill_at?: number
  final_fill_at?: number
  intended_price?: number
  intended_quantity?: number
  submitted_quantity?: number
  filled_quantity?: number
  avg_fill_price?: number
  best_bid?: number
  best_ask?: number
  spread_bps?: number
  expected_slippage_bps?: number
  realized_slippage_bps?: number
  partial_fill_ratio?: number
  status: string
  error_message?: string
  created_at: number
  updated_at: number
}

export interface NofxOSCallRecord {
  path?: string
  url?: string
  status?: number
  cost?: string
  started_at?: string
  finished_at?: string
  error?: string
  [key: string]: unknown
}

export interface NofxOSStatus {
  records: NofxOSCallRecord[]
  count: number
}

export interface AI500Coin {
  symbol: string
  [key: string]: unknown
}

export interface AI500CoinsResponse {
  coins: AI500Coin[]
  count: number
}

export interface TraderInfo {
  trader_id: string
  trader_name: string
  ai_model: string
  exchange_id?: string
  is_running?: boolean
  startup_warning?: string
  show_in_competition?: boolean
  strategy_id?: string
  strategy_name?: string
  use_ai500?: boolean
  use_oi_top?: boolean
}

// Competition related types
export interface CompetitionTraderData {
  trader_id: string
  trader_name: string
  ai_model: string
  exchange: string
  total_equity: number
  total_pnl: number
  total_pnl_pct: number
  position_count: number
  margin_used_pct: number
  is_running: boolean
}

export interface CompetitionData {
  traders: CompetitionTraderData[]
  count: number
}

// Trader Configuration Data for View Modal
export interface TraderConfigData {
  trader_id?: string
  trader_name: string
  ai_model: string
  exchange_id: string
  strategy_id?: string // 绛栫暐ID
  strategy_name?: string // 绛栫暐鍚嶇О
  is_cross_margin: boolean
  show_in_competition: boolean // 鏄惁鍦ㄧ珵鎶€鍦烘樉绀?
  scan_interval_minutes: number
  decision_language?: '' | 'en' | 'zh' // CoT language; "" (default) follows the strategy's own language
  initial_balance: number
  is_running: boolean
  // 浠ヤ笅涓烘棫鐗堝瓧娈碉紙鍚戝悗鍏煎锛?
  btc_eth_leverage?: number
  altcoin_leverage?: number
  trading_symbols?: string
  use_ai500?: boolean
  use_oi_top?: boolean
}

// Position History Types
export interface HistoricalPosition {
  id: number
  trader_id: string
  exchange_id: string
  exchange_type: string
  symbol: string
  side: string
  quantity: number
  entry_quantity: number
  entry_price: number
  entry_order_id: string
  entry_time: string
  exit_price: number
  exit_order_id: string
  exit_time: string
  realized_pnl: number
  fee: number
  leverage: number
  status: string
  close_reason: string
  opening_cycle?: number
  opening_reasoning?: string
  opening_decision_id?: number
  opening_signal_id?: string
  opening_rule_id?: string
  opening_setup?: string
  strategy_id?: string
  strategy_version?: string
  last_review_summary?: string
  last_review_cycle?: number
  stop_loss_source?: string
  stop_loss_timeframe?: string
  stop_loss_anchor?: number
  stop_loss_policy?: string
  take_profit_source?: string
  take_profit_timeframe?: string
  take_profit_anchor?: number
  take_profit_policy?: string
  take_profit_candidate_count?: number
  take_profit_min_risk_reward?: number
  take_profit_min_atr_distance?: number
  take_profit_selected_risk_reward?: number
  take_profit_selected_atr_distance?: number
  take_profit_qualified?: boolean
  nearest_take_profit?: number
  nearest_take_profit_risk_reward?: number
  nearest_take_profit_atr_distance?: number
  protective_atr?: number
  protective_atr_timeframe?: string
  protective_atr_buffer?: number
  protective_risk_reward?: number
  execution_risk_reward?: number
  created_at: string
  updated_at: string
}

// Matches Go TraderStats struct exactly
export interface TraderStats {
  total_trades: number
  win_trades: number
  loss_trades: number
  win_rate: number
  profit_factor: number
  sharpe_ratio: number
  total_pnl: number
  total_fee: number
  avg_win: number
  avg_loss: number
  max_drawdown_pct: number
}

// Matches Go SymbolStats struct exactly
export interface SymbolStats {
  symbol: string
  total_trades: number
  win_trades: number
  win_rate: number
  total_pnl: number
  avg_pnl: number
  avg_hold_mins: number
}

// Matches Go DirectionStats struct exactly
export interface DirectionStats {
  side: string
  trade_count: number
  win_rate: number
  total_pnl: number
  avg_pnl: number
}

export interface PositionHistoryResponse {
  positions: HistoricalPosition[]
  stats: TraderStats | null
  symbol_stats: SymbolStats[]
  direction_stats: DirectionStats[]
}

// Grid Risk Information for frontend display
export interface GridRiskInfo {
  // Leverage info
  current_leverage: number
  effective_leverage: number
  recommended_leverage: number

  // Position info
  current_position: number
  max_position: number
  position_percent: number

  // Liquidation info
  liquidation_price: number
  liquidation_distance: number

  // Market state
  regime_level: string

  // Box state
  short_box_upper: number
  short_box_lower: number
  mid_box_upper: number
  mid_box_lower: number
  long_box_upper: number
  long_box_lower: number
  current_price: number

  // Breakout state
  breakout_level: string
  breakout_direction: string
}
