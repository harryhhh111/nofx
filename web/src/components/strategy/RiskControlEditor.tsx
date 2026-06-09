import { Shield, AlertTriangle, TrendingDown } from 'lucide-react'
import type { RiskControlConfig } from '../../types'
import { riskControl, ts } from '../../i18n/strategy-translations'

interface RiskControlEditorProps {
  config: RiskControlConfig
  onChange: (config: RiskControlConfig) => void
  disabled?: boolean
  language: string
}

export function RiskControlEditor({
  config,
  onChange,
  disabled,
  language,
}: RiskControlEditorProps) {
  const updateField = <K extends keyof RiskControlConfig>(
    key: K,
    value: RiskControlConfig[K]
  ) => {
    if (!disabled) {
      onChange({ ...config, [key]: value })
    }
  }

  return (
    <div className="space-y-6">
      {/* Position Limits */}
      <div>
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-5 h-5" style={{ color: '#F0B90B' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {ts(riskControl.positionLimits, language)}
          </h3>
        </div>

        <div className="grid grid-cols-1 gap-4 mb-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.maxPositions, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.maxPositionsDesc, language)}
            </p>
            <input
              type="number"
              value={config.max_positions ?? 3}
              onChange={(e) =>
                updateField('max_positions', parseInt(e.target.value) || 3)
              }
              disabled={disabled}
              min={1}
              max={3}
              className="w-32 px-3 py-2 rounded"
              style={{
                background: '#1E2329',
                border: '1px solid #2B3139',
                color: '#EAECEF',
              }}
            />
          </div>
        </div>

        {/* Trading Leverage (Exchange) */}
        <div className="mb-2">
          <p className="text-xs font-medium mb-2" style={{ color: '#F0B90B' }}>
            {ts(riskControl.tradingLeverage, language)}
          </p>
        </div>
        <div className="grid grid-cols-2 gap-4 mb-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.btcEthLeverage, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.btcEthLeverageDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.btc_eth_max_leverage ?? 5}
                onChange={(e) =>
                  updateField('btc_eth_max_leverage', parseInt(e.target.value))
                }
                disabled={disabled}
                min={1}
                max={20}
                className="flex-1 accent-yellow-500"
              />
              <span
                className="w-12 text-center font-mono"
                style={{ color: '#F0B90B' }}
              >
                {config.btc_eth_max_leverage ?? 5}x
              </span>
            </div>
          </div>

          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.altcoinLeverage, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.altcoinLeverageDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.altcoin_max_leverage ?? 5}
                onChange={(e) =>
                  updateField('altcoin_max_leverage', parseInt(e.target.value))
                }
                disabled={disabled}
                min={1}
                max={20}
                className="flex-1 accent-yellow-500"
              />
              <span
                className="w-12 text-center font-mono"
                style={{ color: '#F0B90B' }}
              >
                {config.altcoin_max_leverage ?? 5}x
              </span>
            </div>
          </div>
        </div>

        {/* Position Value Ratio (Risk Control - CODE ENFORCED) */}
        <div className="mb-2">
          <p className="text-xs font-medium" style={{ color: '#0ECB81' }}>
            {ts(riskControl.positionValueRatio, language)}
          </p>
          <p className="text-xs mt-1" style={{ color: '#848E9C' }}>
            {ts(riskControl.positionValueRatioDesc, language)}
          </p>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.btcEthPositionValueRatio, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.btcEthPositionValueRatioDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.btc_eth_max_position_value_ratio ?? 5}
                onChange={(e) =>
                  updateField('btc_eth_max_position_value_ratio', parseFloat(e.target.value))
                }
                disabled={disabled}
                min={0.5}
                max={10}
                step={0.5}
                className="flex-1 accent-green-500"
              />
              <span
                className="w-12 text-center font-mono"
                style={{ color: '#0ECB81' }}
              >
                {config.btc_eth_max_position_value_ratio ?? 5}x
              </span>
            </div>
          </div>

          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.altcoinPositionValueRatio, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.altcoinPositionValueRatioDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.altcoin_max_position_value_ratio ?? 1}
                onChange={(e) =>
                  updateField('altcoin_max_position_value_ratio', parseFloat(e.target.value))
                }
                disabled={disabled}
                min={0.5}
                max={10}
                step={0.5}
                className="flex-1 accent-green-500"
              />
              <span
                className="w-12 text-center font-mono"
                style={{ color: '#0ECB81' }}
              >
                {config.altcoin_max_position_value_ratio ?? 1}x
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Risk Parameters */}
      <div>
        <div className="flex items-center gap-2 mb-4">
          <AlertTriangle className="w-5 h-5" style={{ color: '#F6465D' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {ts(riskControl.riskParameters, language)}
          </h3>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.minRiskReward, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.minRiskRewardDesc, language)}
            </p>
            <div className="flex items-center">
              <span style={{ color: '#848E9C' }}>1:</span>
              <input
                type="number"
                value={config.min_risk_reward_ratio ?? 3}
                onChange={(e) =>
                  updateField('min_risk_reward_ratio', parseFloat(e.target.value) || 3)
                }
                disabled={disabled}
                min={1}
                max={10}
                step={0.5}
                className="w-20 px-3 py-2 rounded ml-2"
                style={{
                  background: '#1E2329',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              />
            </div>
          </div>

          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #0ECB81' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.maxMarginUsage, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.maxMarginUsageDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={(config.max_margin_usage ?? 0.9) * 100}
                onChange={(e) =>
                  updateField('max_margin_usage', parseInt(e.target.value) / 100)
                }
                disabled={disabled}
                min={10}
                max={100}
                className="flex-1 accent-green-500"
              />
              <span className="w-12 text-center font-mono" style={{ color: '#0ECB81' }}>
                {Math.round((config.max_margin_usage ?? 0.9) * 100)}%
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Entry Requirements */}
      <div>
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-5 h-5" style={{ color: '#0ECB81' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {ts(riskControl.entryRequirements, language)}
          </h3>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.minPositionSize, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.minPositionSizeDesc, language)}
            </p>
            <div className="flex items-center">
              <input
                type="number"
                value={config.min_position_size ?? 12}
                onChange={(e) =>
                  updateField('min_position_size', parseFloat(e.target.value) || 12)
                }
                disabled={disabled}
                min={10}
                max={1000}
                className="w-24 px-3 py-2 rounded"
                style={{
                  background: '#1E2329',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              />
              <span className="ml-2" style={{ color: '#848E9C' }}>
                USDT
              </span>
            </div>
          </div>

          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.minConfidence, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.minConfidenceDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.min_confidence ?? 75}
                onChange={(e) =>
                  updateField('min_confidence', parseInt(e.target.value))
                }
                disabled={disabled}
                min={50}
                max={100}
                className="flex-1 accent-green-500"
              />
              <span className="w-12 text-center font-mono" style={{ color: '#0ECB81' }}>
                {config.min_confidence ?? 75}
              </span>
            </div>
          </div>

          <div
            className="p-4 rounded-lg"
            style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
          >
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.minCloseConfidence, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.minCloseConfidenceDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.min_close_confidence ?? 75}
                onChange={(e) =>
                  updateField('min_close_confidence', parseInt(e.target.value))
                }
                disabled={disabled}
                min={60}
                max={95}
                className="flex-1 accent-yellow-500"
              />
              <span className="w-12 text-center font-mono" style={{ color: '#F0B90B' }}>
                {config.min_close_confidence ?? 85}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Stop Loss ATR Buffer */}
      <div className="p-5 rounded-xl" style={{ background: '#1E2329', border: '1px solid #2B3139' }}>
        <div className="flex items-center gap-2 mb-2">
          <AlertTriangle className="w-5 h-5" style={{ color: '#F6465D' }} />
          <h3 className="font-medium" style={{ color: '#EAECEF' }}>
            {language === 'zh' ? '止损 ATR 缓冲 (AI 引导)' : 'Stop Loss ATR Buffer (AI Guided)'}
          </h3>
        </div>
        <p className="text-xs mb-4" style={{ color: '#848E9C' }}>
          {language === 'zh'
            ? '止损价与支撑/阻力位之间的缓冲空间（ATR14倍数），防止正常波动假突破扫掉止损。0 = 按交易模式自动取默认值'
            : 'Buffer between stop-loss and support/resistance level (ATR14 multiplier), prevents false breakout sweeps. 0 = auto default by trading mode'}
        </p>
        <div className="flex items-center gap-3">
          <input
            type="number"
            value={config.stop_loss_atr_buffer ?? 0}
            onChange={(e) => updateField('stop_loss_atr_buffer', parseFloat(e.target.value) || 0)}
            disabled={disabled}
            min={0}
            max={3}
            step={0.1}
            className="w-24 px-3 py-2 rounded text-center"
            style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
          />
          <span className="text-sm" style={{ color: '#848E9C' }}>
            × ATR14
          </span>
          <span className="text-xs ml-2" style={{ color: '#5E6673' }}>
            {language === 'zh'
              ? '(0=自动: 保守1.5 / 平衡1.0 / 激进0.5 / 剥头皮0.3)'
              : '(0=auto: Conservative 1.5 / Balanced 1.0 / Aggressive 0.5 / Scalping 0.3)'}
          </span>
        </div>
      </div>

      {/* Drawdown Close Monitor */}
      <div className="p-5 rounded-xl" style={{ background: '#1E2329', border: '1px solid #F6465D33' }}>
        <div className="flex items-center justify-between mb-2">
          <div className="flex items-center gap-2">
            <TrendingDown className="w-5 h-5" style={{ color: '#F6465D' }} />
            <h3 className="font-medium" style={{ color: '#EAECEF' }}>
              {ts(riskControl.drawdownClose, language)}
            </h3>
          </div>
          {/* Enable / disable toggle */}
          <button
            type="button"
            onClick={() => !disabled && updateField('drawdown_close_enabled', !(config.drawdown_close_enabled ?? true))}
            disabled={disabled}
            className="flex items-center gap-2 px-3 py-1 rounded-full text-xs font-medium transition-colors"
            style={{
              background: (config.drawdown_close_enabled ?? true) ? '#F6465D22' : '#2B3139',
              border: `1px solid ${(config.drawdown_close_enabled ?? true) ? '#F6465D' : '#5E6673'}`,
              color: (config.drawdown_close_enabled ?? true) ? '#F6465D' : '#848E9C',
              cursor: disabled ? 'not-allowed' : 'pointer',
            }}
          >
            <span
              className="w-2 h-2 rounded-full"
              style={{ background: (config.drawdown_close_enabled ?? true) ? '#F6465D' : '#5E6673' }}
            />
            {(config.drawdown_close_enabled ?? true)
              ? ts(riskControl.drawdownCloseEnabled, language)
              : (language === 'zh' ? '已禁用' : 'Disabled')}
          </button>
        </div>

        <p className="text-xs mb-4" style={{ color: '#848E9C' }}>
          {ts(riskControl.drawdownCloseDesc, language)}
        </p>

        <div className="grid grid-cols-2 gap-4 mb-4" style={{ opacity: (config.drawdown_close_enabled ?? true) ? 1 : 0.4, pointerEvents: (config.drawdown_close_enabled ?? true) ? 'auto' : 'none' }}>
          {/* Min profit to activate */}
          <div className="p-4 rounded-lg" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.drawdownCloseMinProfit, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.drawdownCloseMinProfitDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.drawdown_close_min_profit_pct ?? 5}
                onChange={(e) => updateField('drawdown_close_min_profit_pct', parseFloat(e.target.value))}
                disabled={disabled}
                min={1}
                max={30}
                step={0.5}
                className="flex-1 accent-red-500"
              />
              <span className="w-14 text-center font-mono" style={{ color: '#F6465D' }}>
                {config.drawdown_close_min_profit_pct ?? 5}%
              </span>
            </div>
          </div>

          {/* Drawdown trigger */}
          <div className="p-4 rounded-lg" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
            <label className="block text-sm mb-1" style={{ color: '#EAECEF' }}>
              {ts(riskControl.drawdownCloseTrigger, language)}
            </label>
            <p className="text-xs mb-2" style={{ color: '#848E9C' }}>
              {ts(riskControl.drawdownCloseTriggerDesc, language)}
            </p>
            <div className="flex items-center gap-2">
              <input
                type="range"
                value={config.drawdown_close_trigger_pct ?? 40}
                onChange={(e) => updateField('drawdown_close_trigger_pct', parseFloat(e.target.value))}
                disabled={disabled}
                min={10}
                max={90}
                step={5}
                className="flex-1 accent-red-500"
              />
              <span className="w-14 text-center font-mono" style={{ color: '#F6465D' }}>
                {config.drawdown_close_trigger_pct ?? 40}%
              </span>
            </div>
          </div>
        </div>

        {/* Action mode: Auto Close vs AI Decide */}
        <div style={{ opacity: (config.drawdown_close_enabled ?? true) ? 1 : 0.4, pointerEvents: (config.drawdown_close_enabled ?? true) ? 'auto' : 'none' }}>
          <p className="text-xs font-medium mb-2" style={{ color: '#EAECEF' }}>
            {ts(riskControl.drawdownCloseMode, language)}
          </p>
          <div className="grid grid-cols-2 gap-3">
            {/* Auto Close */}
            <button
              type="button"
              onClick={() => !disabled && updateField('drawdown_close_use_ai', false)}
              disabled={disabled}
              className="p-3 rounded-lg text-left transition-colors"
              style={{
                background: !(config.drawdown_close_use_ai ?? false) ? '#F6465D22' : '#0B0E11',
                border: `1px solid ${!(config.drawdown_close_use_ai ?? false) ? '#F6465D' : '#2B3139'}`,
                cursor: disabled ? 'not-allowed' : 'pointer',
              }}
            >
              <p className="text-sm font-medium mb-1" style={{ color: !(config.drawdown_close_use_ai ?? false) ? '#F6465D' : '#848E9C' }}>
                {ts(riskControl.drawdownCloseModeAuto, language)}
              </p>
              <p className="text-xs" style={{ color: '#5E6673' }}>
                {ts(riskControl.drawdownCloseModeAutoDesc, language)}
              </p>
            </button>

            {/* AI Decide */}
            <button
              type="button"
              onClick={() => !disabled && updateField('drawdown_close_use_ai', true)}
              disabled={disabled}
              className="p-3 rounded-lg text-left transition-colors"
              style={{
                background: (config.drawdown_close_use_ai ?? false) ? '#F0B90B22' : '#0B0E11',
                border: `1px solid ${(config.drawdown_close_use_ai ?? false) ? '#F0B90B' : '#2B3139'}`,
                cursor: disabled ? 'not-allowed' : 'pointer',
              }}
            >
              <p className="text-sm font-medium mb-1" style={{ color: (config.drawdown_close_use_ai ?? false) ? '#F0B90B' : '#848E9C' }}>
                {ts(riskControl.drawdownCloseModeAI, language)}
              </p>
              <p className="text-xs" style={{ color: '#5E6673' }}>
                {ts(riskControl.drawdownCloseModeAIDesc, language)}
              </p>
            </button>
          </div>
        </div>

        {/* ── Consecutive Loss Brake ──────────────────────── */}
        <div className="mt-6 p-4 rounded-lg" style={{ background: 'rgba(14, 203, 129, 0.04)', border: '1px solid rgba(14, 203, 129, 0.15)' }}>
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium" style={{ color: '#EAECEF' }}>{ts(riskControl.consecutiveLossBrake, language)}</span>
            </div>
            <button
              onClick={() => !disabled && updateField('consecutive_loss_brake', (config.consecutive_loss_brake?.enabled ?? true) ? undefined : { enabled: true, max_losses: 3, cool_down_cycles: 3 })}
              disabled={disabled}
              className="relative w-11 h-6 rounded-full transition-colors"
              style={{ background: (config.consecutive_loss_brake?.enabled ?? true) ? '#0ECB81' : '#2B3139' }}
            >
              <span className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${(config.consecutive_loss_brake?.enabled ?? true) ? 'translate-x-5' : ''}`} />
            </button>
          </div>
          <p className="text-xs mb-3" style={{ color: '#848E9C' }}>{ts(riskControl.consecutiveLossBrakeDesc, language)}</p>
          <div style={{ opacity: (config.consecutive_loss_brake?.enabled ?? true) ? 1 : 0.4, pointerEvents: (config.consecutive_loss_brake?.enabled ?? true) ? 'auto' : 'none' }}>
            <div className="grid grid-cols-2 gap-3">
              <div className="p-3 rounded-lg" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
                <label className="block text-xs mb-1" style={{ color: '#EAECEF' }}>{ts(riskControl.consecutiveLossBrakeMaxLosses, language)}</label>
                <div className="flex items-center gap-2">
                  <input type="range" value={config.consecutive_loss_brake?.max_losses ?? 3}
                    onChange={(e) => updateField('consecutive_loss_brake', { ...(config.consecutive_loss_brake || { enabled: true, max_losses: 3, cool_down_cycles: 3 }), max_losses: parseInt(e.target.value) })}
                    disabled={disabled} min={2} max={10} step={1}
                    className="flex-1" style={{ accentColor: '#0ECB81' }} />
                  <span className="w-8 text-center font-mono text-sm" style={{ color: '#0ECB81' }}>{config.consecutive_loss_brake?.max_losses ?? 3}</span>
                </div>
              </div>
              <div className="p-3 rounded-lg" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
                <label className="block text-xs mb-1" style={{ color: '#EAECEF' }}>{ts(riskControl.consecutiveLossBrakeCooldown, language)}</label>
                <div className="flex items-center gap-2">
                  <input type="range" value={config.consecutive_loss_brake?.cool_down_cycles ?? 3}
                    onChange={(e) => updateField('consecutive_loss_brake', { ...(config.consecutive_loss_brake || { enabled: true, max_losses: 3, cool_down_cycles: 3 }), cool_down_cycles: parseInt(e.target.value) })}
                    disabled={disabled} min={1} max={20} step={1}
                    className="flex-1" style={{ accentColor: '#0ECB81' }} />
                  <span className="w-8 text-center font-mono text-sm" style={{ color: '#0ECB81' }}>{config.consecutive_loss_brake?.cool_down_cycles ?? 3}</span>
                </div>
              </div>
            </div>
          </div>
        </div>

      </div>
    </div>
  )
}
