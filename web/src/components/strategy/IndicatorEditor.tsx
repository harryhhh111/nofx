import { useEffect, useState } from 'react'
import { Clock, Activity, TrendingUp, BarChart2, Info, Lock, ExternalLink, Zap, Check } from 'lucide-react'
import type { IndicatorConfig, StrategyMetadata, StrategyMetadataIndicator, StrategyMetadataTimeframe } from '../../types'
import { indicator, ts } from '../../i18n/strategy-translations'
import { NofxSelect } from '../ui/select'
import { api } from '../../lib/api'

interface IndicatorEditorProps {
  config: IndicatorConfig
  onChange: (config: IndicatorConfig) => void
  disabled?: boolean
  language: string
}

// All available timeframes
const fallbackTimeframes: StrategyMetadataTimeframe[] = [
  { value: '1m', label: '1m', category: 'scalp' },
  { value: '3m', label: '3m', category: 'scalp' },
  { value: '5m', label: '5m', category: 'scalp' },
  { value: '15m', label: '15m', category: 'intraday' },
  { value: '30m', label: '30m', category: 'intraday' },
  { value: '1h', label: '1h', category: 'intraday' },
  { value: '2h', label: '2h', category: 'swing' },
  { value: '4h', label: '4h', category: 'swing' },
  { value: '6h', label: '6h', category: 'swing' },
  { value: '8h', label: '8h', category: 'swing' },
  { value: '12h', label: '12h', category: 'swing' },
  { value: '1d', label: '1D', category: 'position' },
  { value: '3d', label: '3D', category: 'position' },
  { value: '1w', label: '1W', category: 'position' },
]

const fallbackTechnicalIndicators: StrategyMetadataIndicator[] = [
  { key: 'enable_ema', label: 'ema', desc: 'emaDesc', color: '#F0B90B', period_key: 'ema_periods', default_periods: [20, 50] },
  { key: 'enable_sma', label: 'sma', desc: 'smaDesc', color: '#4ade80', period_key: 'sma_periods', default_periods: [5, 20, 50] },
  { key: 'enable_macd', label: 'macd', desc: 'macdDesc', color: '#a855f7' },
  { key: 'enable_rsi', label: 'rsi', desc: 'rsiDesc', color: '#F6465D', period_key: 'rsi_periods', default_periods: [7, 14] },
  { key: 'enable_atr', label: 'atr', desc: 'atrDesc', color: '#60a5fa', period_key: 'atr_periods', default_periods: [14] },
  { key: 'enable_adx', label: 'adx', desc: 'adxDesc', color: '#f97316' },
  { key: 'enable_sar', label: 'sar', desc: 'sarDesc', color: '#06b6d4' },
  { key: 'enable_boll', label: 'boll', desc: 'bollDesc', color: '#ec4899', period_key: 'boll_periods', default_periods: [20] },
  { key: 'enable_session', label: 'session', desc: 'sessionDesc', color: '#84cc16' },
  { key: 'enable_opening_range', label: 'opening_range', desc: 'openingRangeDesc', color: '#38bdf8', period_key: 'opening_range_minutes' },
  { key: 'enable_rbreaker', label: 'rbreaker', desc: 'rbreakerDesc', color: '#fb923c' },
  { key: 'enable_mtsi', label: 'mtsi', desc: 'mtsiDesc', color: '#c084fc' },
  { key: 'enable_vwap', label: 'vwap', desc: 'vwapDesc', color: '#22d3ee', period_key: 'vwap_periods', default_periods: [20] },
  { key: 'enable_donchian', label: 'donchian', desc: 'donchianDesc', color: '#f472b6', period_key: 'donchian_periods', default_periods: [20] },
  { key: 'enable_volume', label: 'volume', desc: 'volumeDesc', color: '#8b5cf6', period_key: 'volume_periods', default_periods: [20] },
  { key: 'enable_volume_spike', label: 'volumeSpike', desc: 'volumeSpikeDesc', color: '#a78bfa', period_key: 'volume_periods', default_periods: [20] },
  { key: 'enable_rolling_percentile', label: 'rolling_percentile', desc: 'rollingPercentileDesc', color: '#34d399', period_key: 'rolling_percentile_periods', default_periods: [20] },
]

const marketDataSources = [
  { value: 'auto', label: 'Auto fallback' },
  { value: 'binance', label: 'Binance Futures' },
  { value: 'bybit', label: 'Bybit Linear' },
  { value: 'okx', label: 'OKX Swap' },
  { value: 'aster', label: 'Aster Futures' },
  { value: 'hyperliquid', label: 'Hyperliquid' },
]

const MIN_DISPLAY_KLINE_COUNT = 10
const MAX_DISPLAY_KLINE_COUNT = 100
const MIN_COMPUTE_KLINE_COUNT = 120
const MAX_COMPUTE_KLINE_COUNT = 1000

const indicatorTranslationAliases: Record<string, keyof typeof indicator> = {
  volume_spike: 'volumeSpike',
  rolling_percentile_desc: 'rollingPercentileDesc',
}

function clampNumber(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) return min
  return Math.min(max, Math.max(min, value))
}

function indicatorText(key: string | undefined, language: string) {
  if (!key) return ''
  const translationKey = indicatorTranslationAliases[key] || key
  const entry = indicator[translationKey as keyof typeof indicator]
  return entry ? ts(entry, language) : key
}

export function IndicatorEditor({
  config,
  onChange,
  disabled,
  language,
}: IndicatorEditorProps) {
  const [metadata, setMetadata] = useState<StrategyMetadata | null>(null)
  const timeframes = metadata?.timeframes?.length ? metadata.timeframes : fallbackTimeframes
  const technicalIndicators = metadata?.technical_indicators?.length ? metadata.technical_indicators : fallbackTechnicalIndicators

  useEffect(() => {
    let mounted = true
    api.getStrategyMetadata()
      .then((data) => {
        if (mounted) setMetadata(data)
      })
      .catch(() => {
        if (mounted) setMetadata(null)
      })
    return () => {
      mounted = false
    }
  }, [])

  // Get currently selected timeframes
  const selectedTimeframes =
    config.klines.selected_timeframes || [config.klines.primary_timeframe]
  const entryTimeframe =
    config.klines.entry_timeframe ||
    config.klines.primary_timeframe ||
    selectedTimeframes[0]
  const confirmationTimeframes =
    config.klines.confirmation_timeframes ||
    selectedTimeframes.filter(
      (tf) => tf !== config.klines.primary_timeframe && tf !== entryTimeframe
    )
  const displayKlineCount = config.klines.prompt_display_count || config.klines.primary_count || 30
  const computeKlineCount = config.klines.compute_lookback || 300
  const updateDisplayKlineCount = (rawValue: string) => {
    if (disabled) return
    const next = clampNumber(parseInt(rawValue, 10), MIN_DISPLAY_KLINE_COUNT, MAX_DISPLAY_KLINE_COUNT)
    onChange({
      ...config,
      klines: {
        ...config.klines,
        primary_count: next,
        prompt_display_count: next,
      },
    })
  }
  const updateComputeKlineCount = (rawValue: string) => {
    if (disabled) return
    const next = clampNumber(parseInt(rawValue, 10), MIN_COMPUTE_KLINE_COUNT, MAX_COMPUTE_KLINE_COUNT)
    onChange({
      ...config,
      klines: {
        ...config.klines,
        compute_lookback: next,
      },
    })
  }

  // Toggle timeframe selection
  const toggleTimeframe = (tf: string) => {
    if (disabled) return
    const current = [...selectedTimeframes]
    const index = current.indexOf(tf)

    if (index >= 0) {
      if (current.length > 1) {
        current.splice(index, 1)
        const newPrimary =
          tf === config.klines.primary_timeframe
            ? current[0]
            : config.klines.primary_timeframe
        const newEntry =
          tf === entryTimeframe
            ? newPrimary
            : entryTimeframe
        const newConfirmations = confirmationTimeframes.filter(
          (item) => item !== tf && item !== newPrimary && item !== newEntry
        )
        onChange({
          ...config,
          klines: {
            ...config.klines,
            selected_timeframes: current,
            primary_timeframe: newPrimary,
            entry_timeframe: newEntry,
            confirmation_timeframes: newConfirmations,
            enable_multi_timeframe: current.length > 1,
          },
        })
      }
    } else {
      if (current.length >= 5) {
        // Show toast notification
        const toast = document.createElement('div')
        toast.textContent = language === 'zh' ? '最多选择 4 个时间维度' : 'Maximum 4 timeframes allowed'
        toast.textContent = language === 'zh' ? '最多选择 5 个时间维度' : 'Maximum 5 timeframes allowed'
        toast.className = 'fixed top-4 left-1/2 -translate-x-1/2 px-4 py-2 rounded-lg text-sm z-50 shadow-lg'
        toast.style.cssText = 'background:#F6465D;color:#fff;'
        document.body.appendChild(toast)
        setTimeout(() => toast.remove(), 2000)
        return
      }
      current.push(tf)
      onChange({
        ...config,
        klines: {
          ...config.klines,
          selected_timeframes: current,
          entry_timeframe:
            config.klines.entry_timeframe ||
            config.klines.primary_timeframe ||
            current[0],
          enable_multi_timeframe: current.length > 1,
        },
      })
    }
  }

  // Set primary timeframe
  const setPrimaryTimeframe = (tf: string) => {
    if (disabled) return
    onChange({
      ...config,
      klines: {
        ...config.klines,
        primary_timeframe: tf,
        confirmation_timeframes: confirmationTimeframes.filter((item) => item !== tf && item !== entryTimeframe),
      },
    })
  }

  const setEntryTimeframe = (tf: string) => {
    if (disabled) return
    onChange({
      ...config,
      klines: {
        ...config.klines,
        entry_timeframe: tf,
        confirmation_timeframes: confirmationTimeframes.filter((item) => item !== tf && item !== config.klines.primary_timeframe),
      },
    })
  }

  const toggleConfirmationTimeframe = (tf: string) => {
    if (disabled || tf === config.klines.primary_timeframe || tf === entryTimeframe) return
    const next = confirmationTimeframes.includes(tf)
      ? confirmationTimeframes.filter((item) => item !== tf)
      : [...confirmationTimeframes, tf]
    onChange({
      ...config,
      klines: {
        ...config.klines,
        confirmation_timeframes: next,
      },
    })
  }

  const categoryColors: Record<string, string> = {
    scalp: '#F6465D',
    intraday: '#F0B90B',
    swing: '#0ECB81',
    position: '#60a5fa',
  }

  useEffect(() => {
    if (!config.enable_raw_klines) {
      onChange({ ...config, enable_raw_klines: true })
    }
  }, [config, onChange])

  // Check if any NofxOS feature is enabled
  const hasNofxosEnabled = config.enable_oi_ranking || config.enable_netflow_ranking || config.enable_price_ranking

  return (
    <div className="space-y-5">
      {/* ============================================ */}
      {/* NofxOS Data Provider - Top Configuration    */}
      {/* ============================================ */}
      <div
        className="rounded-lg overflow-hidden relative"
        style={{
          background: 'linear-gradient(135deg, rgba(99, 102, 241, 0.08) 0%, rgba(168, 85, 247, 0.08) 50%, rgba(236, 72, 153, 0.08) 100%)',
          border: '1px solid rgba(139, 92, 246, 0.3)',
        }}
      >
        {/* Decorative gradient line at top */}
        <div
          className="absolute top-0 left-0 right-0 h-[2px]"
          style={{ background: 'linear-gradient(90deg, #6366f1, #a855f7, #ec4899)' }}
        />

        <div className="p-4">
          {/* Header Row */}
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <div
                className="w-8 h-8 rounded-lg flex items-center justify-center"
                style={{ background: 'linear-gradient(135deg, #6366f1, #a855f7)' }}
              >
                <Zap className="w-4 h-4 text-white" />
              </div>
              <div>
                <h3 className="text-sm font-semibold" style={{ color: '#EAECEF' }}>
                  {ts(indicator.nofxosTitle, language)}
                </h3>
                <span className="text-[10px]" style={{ color: '#848E9C' }}>
                  {ts(indicator.nofxosFeatures, language)}
                </span>
              </div>
            </div>

            {/* Status & Docs */}
            <div className="flex items-center gap-2">
              <span className="flex items-center gap-1 text-[10px] px-2 py-1 rounded-full" style={{ background: 'rgba(14, 203, 129, 0.15)', color: '#0ECB81' }}>
                <Check className="w-3 h-3" />
                {ts(indicator.walletBilling, language)}
              </span>
              <a
                href="https://nofxos.ai/api-docs"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 text-[10px] px-2 py-1 rounded-full transition-all hover:scale-[1.02]"
                style={{
                  background: 'rgba(139, 92, 246, 0.2)',
                  color: '#a855f7',
                }}
              >
                <ExternalLink className="w-3 h-3" />
                {ts(indicator.viewApiDocs, language)}
              </a>
            </div>
          </div>

          <div
            className="rounded-lg px-3 py-2 text-[11px]"
            style={{
              background: 'rgba(30, 35, 41, 0.65)',
              border: '1px solid rgba(139, 92, 246, 0.25)',
              color: '#B7BDC6',
            }}
          >
            {ts(indicator.walletBillingDesc, language)}
          </div>

          {/* NofxOS Data Sources Grid */}
          <div className="mt-4">
            <div className="text-[10px] font-medium mb-2" style={{ color: '#848E9C' }}>
              {ts(indicator.nofxosDataSources, language)}
            </div>
            <div className="grid grid-cols-2 gap-2">
              {/* OI Ranking */}
              <div
                className="p-2.5 rounded-lg transition-all cursor-pointer"
                style={{
                  background: config.enable_oi_ranking ? 'rgba(34, 197, 94, 0.1)' : 'rgba(30, 35, 41, 0.5)',
                  border: config.enable_oi_ranking ? '1px solid rgba(34, 197, 94, 0.3)' : '1px solid rgba(43, 49, 57, 0.5)',
                  opacity: disabled ? 0.5 : 1,
                }}
                onClick={() => !disabled && onChange({
                  ...config,
                  enable_oi_ranking: !config.enable_oi_ranking,
                  ...(!config.enable_oi_ranking && !config.oi_ranking_duration ? { oi_ranking_duration: '1h' } : {}),
                  ...(!config.enable_oi_ranking && !config.oi_ranking_limit ? { oi_ranking_limit: 10 } : {}),
                })}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full" style={{ background: '#22c55e' }} />
                    <span className="text-xs font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.oiRanking, language)}</span>
                  </div>
                  <input
                    type="checkbox"
                    checked={config.enable_oi_ranking || false}
                    onChange={(e) => { e.stopPropagation(); !disabled && onChange({
                      ...config,
                      enable_oi_ranking: e.target.checked,
                      ...(e.target.checked && !config.oi_ranking_duration ? { oi_ranking_duration: '1h' } : {}),
                      ...(e.target.checked && !config.oi_ranking_limit ? { oi_ranking_limit: 10 } : {}),
                    }) }}
                    disabled={disabled}
                    className="w-3.5 h-3.5 rounded accent-green-500"
                  />
                </div>
                <p className="text-[10px] mt-1" style={{ color: '#5E6673' }}>{ts(indicator.oiRankingDesc, language)}</p>
                {config.enable_oi_ranking && (
                  <div className="flex gap-2 mt-2" onClick={(e) => e.stopPropagation()}>
                    <NofxSelect
                      value={config.oi_ranking_duration || '1h'}
                      onChange={(val) => !disabled && onChange({ ...config, oi_ranking_duration: val })}
                      disabled={disabled}
                      className="flex-1 px-2 py-1 rounded text-[10px]"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                      options={[{ value: '1h', label: '1h' }, { value: '4h', label: '4h' }, { value: '24h', label: '24h' }]}
                    />
                    <NofxSelect
                      value={config.oi_ranking_limit || 10}
                      onChange={(val) => !disabled && onChange({ ...config, oi_ranking_limit: parseInt(val) })}
                      disabled={disabled}
                      className="w-14 px-2 py-1 rounded text-[10px]"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                      options={[5, 10, 15, 20].map(n => ({ value: n, label: String(n) }))}
                    />
                  </div>
                )}
              </div>

              {/* NetFlow Ranking */}
              <div
                className="p-2.5 rounded-lg transition-all cursor-pointer"
                style={{
                  background: config.enable_netflow_ranking ? 'rgba(245, 158, 11, 0.1)' : 'rgba(30, 35, 41, 0.5)',
                  border: config.enable_netflow_ranking ? '1px solid rgba(245, 158, 11, 0.3)' : '1px solid rgba(43, 49, 57, 0.5)',
                  opacity: disabled ? 0.5 : 1,
                }}
                onClick={() => !disabled && onChange({
                  ...config,
                  enable_netflow_ranking: !config.enable_netflow_ranking,
                  ...(!config.enable_netflow_ranking && !config.netflow_ranking_duration ? { netflow_ranking_duration: '1h' } : {}),
                  ...(!config.enable_netflow_ranking && !config.netflow_ranking_limit ? { netflow_ranking_limit: 10 } : {}),
                })}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full" style={{ background: '#f59e0b' }} />
                    <span className="text-xs font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.netflowRanking, language)}</span>
                  </div>
                  <input
                    type="checkbox"
                    checked={config.enable_netflow_ranking || false}
                    onChange={(e) => { e.stopPropagation(); !disabled && onChange({
                      ...config,
                      enable_netflow_ranking: e.target.checked,
                      ...(e.target.checked && !config.netflow_ranking_duration ? { netflow_ranking_duration: '1h' } : {}),
                      ...(e.target.checked && !config.netflow_ranking_limit ? { netflow_ranking_limit: 10 } : {}),
                    }) }}
                    disabled={disabled}
                    className="w-3.5 h-3.5 rounded accent-amber-500"
                  />
                </div>
                <p className="text-[10px] mt-1" style={{ color: '#5E6673' }}>{ts(indicator.netflowRankingDesc, language)}</p>
                {config.enable_netflow_ranking && (
                  <div className="flex gap-2 mt-2" onClick={(e) => e.stopPropagation()}>
                    <NofxSelect
                      value={config.netflow_ranking_duration || '1h'}
                      onChange={(val) => !disabled && onChange({ ...config, netflow_ranking_duration: val })}
                      disabled={disabled}
                      className="flex-1 px-2 py-1 rounded text-[10px]"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                      options={[{ value: '1h', label: '1h' }, { value: '4h', label: '4h' }, { value: '24h', label: '24h' }]}
                    />
                    <NofxSelect
                      value={config.netflow_ranking_limit || 10}
                      onChange={(val) => !disabled && onChange({ ...config, netflow_ranking_limit: parseInt(val) })}
                      disabled={disabled}
                      className="w-14 px-2 py-1 rounded text-[10px]"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                      options={[5, 10, 15, 20].map(n => ({ value: n, label: String(n) }))}
                    />
                  </div>
                )}
              </div>

              {/* Price Ranking */}
              <div
                className="p-2.5 rounded-lg transition-all cursor-pointer"
                style={{
                  background: config.enable_price_ranking ? 'rgba(236, 72, 153, 0.1)' : 'rgba(30, 35, 41, 0.5)',
                  border: config.enable_price_ranking ? '1px solid rgba(236, 72, 153, 0.3)' : '1px solid rgba(43, 49, 57, 0.5)',
                  opacity: disabled ? 0.5 : 1,
                }}
                onClick={() => !disabled && onChange({
                  ...config,
                  enable_price_ranking: !config.enable_price_ranking,
                  ...(!config.enable_price_ranking && !config.price_ranking_duration ? { price_ranking_duration: '1h,4h,24h' } : {}),
                  ...(!config.enable_price_ranking && !config.price_ranking_limit ? { price_ranking_limit: 10 } : {}),
                })}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full" style={{ background: '#ec4899' }} />
                    <span className="text-xs font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.priceRanking, language)}</span>
                  </div>
                  <input
                    type="checkbox"
                    checked={config.enable_price_ranking || false}
                    onChange={(e) => { e.stopPropagation(); !disabled && onChange({
                      ...config,
                      enable_price_ranking: e.target.checked,
                      ...(e.target.checked && !config.price_ranking_duration ? { price_ranking_duration: '1h,4h,24h' } : {}),
                      ...(e.target.checked && !config.price_ranking_limit ? { price_ranking_limit: 10 } : {}),
                    }) }}
                    disabled={disabled}
                    className="w-3.5 h-3.5 rounded accent-pink-500"
                  />
                </div>
                <p className="text-[10px] mt-1" style={{ color: '#5E6673' }}>{ts(indicator.priceRankingDesc, language)}</p>
                {config.enable_price_ranking && (
                  <div className="flex gap-2 mt-2" onClick={(e) => e.stopPropagation()}>
                    <NofxSelect
                      value={config.price_ranking_duration || '1h,4h,24h'}
                      onChange={(val) => !disabled && onChange({ ...config, price_ranking_duration: val })}
                      disabled={disabled}
                      className="flex-1 px-2 py-1 rounded text-[10px]"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                      options={[
                        { value: '1h', label: '1h' },
                        { value: '4h', label: '4h' },
                        { value: '24h', label: '24h' },
                        { value: '1h,4h,24h', label: ts(indicator.priceRankingMulti, language) },
                      ]}
                    />
                    <NofxSelect
                      value={config.price_ranking_limit || 10}
                      onChange={(val) => !disabled && onChange({ ...config, price_ranking_limit: parseInt(val) })}
                      disabled={disabled}
                      className="w-14 px-2 py-1 rounded text-[10px]"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                      options={[5, 10, 15, 20].map(n => ({ value: n, label: String(n) }))}
                    />
                  </div>
                )}
              </div>
            </div>

            {hasNofxosEnabled && (
              <div className="flex items-center gap-2 mt-3 p-2 rounded-lg" style={{ background: 'rgba(14, 203, 129, 0.1)', border: '1px solid rgba(14, 203, 129, 0.2)' }}>
                <Info className="w-4 h-4 flex-shrink-0" style={{ color: '#0ECB81' }} />
                <span className="text-[10px]" style={{ color: '#B7BDC6' }}>
                  {ts(indicator.configureWallet, language)}
                </span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* ============================================ */}
      {/* Section 1: Market Data (Required)           */}
      {/* ============================================ */}
      <div className="rounded-lg overflow-hidden" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
        <div className="px-3 py-2 flex items-center gap-2" style={{ background: '#1E2329', borderBottom: '1px solid #2B3139' }}>
          <BarChart2 className="w-4 h-4" style={{ color: '#F0B90B' }} />
          <span className="text-sm font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.marketData, language)}</span>
          <span className="text-xs" style={{ color: '#848E9C' }}>- {ts(indicator.marketDataDesc, language)}</span>
        </div>

        <div className="p-3 space-y-4">
          {/* Raw Klines - Required, Always On */}
          <div className="flex items-center justify-between p-3 rounded-lg" style={{ background: 'rgba(240, 185, 11, 0.08)', border: '1px solid rgba(240, 185, 11, 0.2)' }}>
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg flex items-center justify-center" style={{ background: 'rgba(240, 185, 11, 0.15)' }}>
                <TrendingUp className="w-4 h-4" style={{ color: '#F0B90B' }} />
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.rawKlines, language)}</span>
                  <span className="px-1.5 py-0.5 rounded text-[10px] font-medium flex items-center gap-1" style={{ background: 'rgba(240, 185, 11, 0.2)', color: '#F0B90B' }}>
                    <Lock className="w-2.5 h-2.5" />
                    {ts(indicator.required, language)}
                  </span>
                </div>
                <p className="text-xs mt-0.5" style={{ color: '#848E9C' }}>{ts(indicator.rawKlinesDesc, language)}</p>
              </div>
            </div>
            <input
              type="checkbox"
              checked={true}
              disabled={true}
              className="w-5 h-5 rounded accent-yellow-500 cursor-not-allowed"
            />
          </div>

          <div className="rounded-lg p-3" style={{ background: 'rgba(30, 35, 41, 0.55)', border: '1px solid #2B3139' }}>
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-xs font-medium" style={{ color: '#EAECEF' }}>
                  {language === 'zh' ? 'K 线数据源' : 'K-line data source'}
                </div>
                <p className="mt-1 text-[10px]" style={{ color: '#848E9C' }}>
                  {language === 'zh'
                    ? '只用于 OHLCV / 指标计算，和下单交易所分开配置。'
                    : 'Used only for OHLCV and indicator calculations; separate from the execution exchange.'}
                </p>
              </div>
              <select
                value={config.klines.market_data_source || 'binance'}
                disabled={disabled}
                onChange={(e) =>
                  !disabled &&
                  onChange({
                    ...config,
                    klines: { ...config.klines, market_data_source: e.target.value as 'auto' | 'binance' | 'bybit' | 'okx' | 'aster' | 'hyperliquid' },
                  })
                }
                className="min-w-[150px] px-2 py-1.5 rounded text-xs"
                style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
              >
                {marketDataSources.map((source) => (
                  <option key={source.value} value={source.value}>{source.label}</option>
                ))}
              </select>
            </div>
          </div>

          {/* Timeframe Selection */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <div className="flex items-center gap-2">
                <Clock className="w-3.5 h-3.5" style={{ color: '#848E9C' }} />
                <span className="text-xs font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.timeframes, language)}</span>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <span className="text-[10px]" style={{ color: '#848E9C' }}>{ts(indicator.displayKlineCount, language)}:</span>
                <input
                  type="number"
                  value={displayKlineCount}
                  onChange={(e) => updateDisplayKlineCount(e.target.value)}
                  disabled={disabled}
                  min={MIN_DISPLAY_KLINE_COUNT}
                  max={MAX_DISPLAY_KLINE_COUNT}
                  className="w-16 px-2 py-1 rounded text-xs text-center"
                  style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                />
                <span className="text-[10px]" style={{ color: '#848E9C' }}>{ts(indicator.computeKlineCount, language)}:</span>
                <input
                  type="number"
                  value={computeKlineCount}
                  onChange={(e) => updateComputeKlineCount(e.target.value)}
                  disabled={disabled}
                  min={MIN_COMPUTE_KLINE_COUNT}
                  max={MAX_COMPUTE_KLINE_COUNT}
                  step={20}
                  className="w-20 px-2 py-1 rounded text-xs text-center"
                  style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                />
              </div>
            </div>
            <p className="text-[10px] mb-2" style={{ color: '#5E6673' }}>{ts(indicator.timeframesDesc, language)}</p>
            <p className="text-[10px] mb-2" style={{ color: '#5E6673' }}>{ts(indicator.computeKlineCountDesc, language)}</p>

            {/* Timeframe Grid */}
            <div className="space-y-1.5">
              {(['scalp', 'intraday', 'swing', 'position'] as const).map((category) => {
                const categoryTfs = timeframes.filter((tf) => tf.category === category)
                return (
                  <div key={category} className="flex items-center gap-2">
                    <span className="text-[10px] w-10 flex-shrink-0" style={{ color: categoryColors[category] }}>
                      {ts(indicator[category], language)}
                    </span>
                    <div className="flex flex-wrap gap-1">
                      {categoryTfs.map((tf) => {
                        const isSelected = selectedTimeframes.includes(tf.value)
                        const isPrimary = config.klines.primary_timeframe === tf.value
                        return (
                          <button
                            key={tf.value}
                            onClick={() => toggleTimeframe(tf.value)}
                            onDoubleClick={() => setPrimaryTimeframe(tf.value)}
                            disabled={disabled}
                            className={`px-2 py-1 rounded text-xs font-medium transition-all ${
                              isSelected ? '' : 'opacity-40 hover:opacity-70'
                            }`}
                            style={{
                              background: isSelected ? `${categoryColors[category]}15` : 'transparent',
                              border: `1px solid ${isSelected ? categoryColors[category] : '#2B3139'}`,
                              color: isSelected ? categoryColors[category] : '#848E9C',
                              boxShadow: isPrimary ? `0 0 0 2px ${categoryColors[category]}` : undefined,
                            }}
                            title={isPrimary ? `${tf.label} (Primary)` : tf.label}
                          >
                            {tf.label}
                            {isPrimary && <span className="ml-0.5 text-[8px]">★</span>}
                          </button>
                        )
                      })}
                    </div>
                  </div>
                )
              })}
            </div>

            <div className="mt-3 rounded-lg p-3" style={{ background: 'rgba(30, 35, 41, 0.55)', border: '1px solid #2B3139' }}>
              <div className="text-[10px] font-medium mb-2" style={{ color: '#EAECEF' }}>
                {language === 'zh' ? '周期角色' : 'Timeframe roles'}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                <label className="space-y-1">
                  <span className="text-[10px]" style={{ color: '#848E9C' }}>{language === 'zh' ? '主周期：识别机会' : 'Primary: setup'}</span>
                  <select
                    value={config.klines.primary_timeframe}
                    disabled={disabled}
                    onChange={(e) => setPrimaryTimeframe(e.target.value)}
                    className="w-full px-2 py-1.5 rounded text-xs"
                    style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
                  >
                    {selectedTimeframes.map((tf) => <option key={tf} value={tf}>{tf}</option>)}
                  </select>
                </label>
                <label className="space-y-1">
                  <span className="text-[10px]" style={{ color: '#848E9C' }}>{language === 'zh' ? '入场周期：确认触发' : 'Entry: trigger'}</span>
                  <select
                    value={entryTimeframe}
                    disabled={disabled}
                    onChange={(e) => setEntryTimeframe(e.target.value)}
                    className="w-full px-2 py-1.5 rounded text-xs"
                    style={{ background: '#0B0E11', border: '1px solid #2B3139', color: '#EAECEF' }}
                  >
                    {selectedTimeframes.map((tf) => <option key={tf} value={tf}>{tf}</option>)}
                  </select>
                </label>
              </div>
              <div className="mt-2">
                <div className="text-[10px] mb-1" style={{ color: '#848E9C' }}>
                  {language === 'zh' ? '确认周期：过滤方向冲突' : 'Confirmations: direction filter'}
                </div>
                <div className="flex flex-wrap gap-1">
                  {selectedTimeframes.map((tf) => {
                    const disabledRole = tf === config.klines.primary_timeframe || tf === entryTimeframe
                    const active = confirmationTimeframes.includes(tf)
                    return (
                      <button
                        key={`${tf}-confirm`}
                        type="button"
                        disabled={disabled || disabledRole}
                        onClick={() => toggleConfirmationTimeframe(tf)}
                        className="px-2 py-1 rounded text-[10px] transition-all"
                        style={{
                          background: active ? 'rgba(14, 203, 129, 0.16)' : 'transparent',
                          border: `1px solid ${active ? '#0ECB81' : '#2B3139'}`,
                          color: disabledRole ? '#5E6673' : active ? '#0ECB81' : '#848E9C',
                        }}
                      >
                        {tf}
                      </button>
                    )
                  })}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* ============================================ */}
      {/* Section 2: Technical Indicators (Optional)  */}
      {/* ============================================ */}
      <div className="rounded-lg overflow-hidden" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
        <div className="px-3 py-2 flex items-center gap-2" style={{ background: '#1E2329', borderBottom: '1px solid #2B3139' }}>
          <Activity className="w-4 h-4" style={{ color: '#0ECB81' }} />
          <span className="text-sm font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.technicalIndicators, language)}</span>
          <span className="text-xs" style={{ color: '#848E9C' }}>- {ts(indicator.technicalIndicatorsDesc, language)}</span>
        </div>

        <div className="p-3">
          {/* Tip */}
          <div className="flex items-start gap-2 mb-3 p-2 rounded" style={{ background: 'rgba(14, 203, 129, 0.05)' }}>
            <Info className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" style={{ color: '#0ECB81' }} />
            <p className="text-[10px]" style={{ color: '#848E9C' }}>{ts(indicator.aiCanCalculate, language)}</p>
          </div>

          {/* Indicator Grid */}
          <div className="grid grid-cols-2 gap-2">
            {technicalIndicators.map(({ key, label, desc, color, period_key: periodKey, default_periods: defaultPeriods }) => (
              <div
                key={key}
                className="p-2.5 rounded-lg transition-all"
                style={{
                  background: config[key as keyof IndicatorConfig] ? `${color}08` : 'transparent',
                  border: `1px solid ${config[key as keyof IndicatorConfig] ? `${color}30` : '#2B3139'}`,
                }}
              >
                <div className="flex items-center justify-between mb-1">
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full" style={{ background: color }} />
                    <span className="text-xs font-medium" style={{ color: '#EAECEF' }}>{indicatorText(label, language)}</span>
                  </div>
                  <input
                    type="checkbox"
                    checked={config[key as keyof IndicatorConfig] as boolean || false}
                    onChange={(e) => !disabled && onChange({ ...config, [key]: e.target.checked })}
                    disabled={disabled}
                    className="w-4 h-4 rounded accent-yellow-500"
                  />
                </div>
                <p className="text-[10px] mb-1.5" style={{ color: '#5E6673' }}>{indicatorText(desc, language)}</p>
                {periodKey && config[key as keyof IndicatorConfig] && (
                  Array.isArray(config[periodKey as keyof IndicatorConfig]) ? (
                    <input
                      type="text"
                      value={(config[periodKey as keyof IndicatorConfig] as number[])?.join(',') || defaultPeriods?.join(',') || ''}
                      onChange={(e) => {
                        if (disabled) return
                        const periods = e.target.value
                          .split(',')
                          .map((s) => parseInt(s.trim()))
                          .filter((n) => !isNaN(n) && n > 0)
                        onChange({ ...config, [periodKey]: periods })
                      }}
                      disabled={disabled}
                      placeholder={defaultPeriods?.join(',') || ''}
                      className="w-full px-2 py-1 rounded text-[10px] text-center"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                    />
                  ) : (
                    <input
                      type="number"
                      value={(config[periodKey as keyof IndicatorConfig] as number) || defaultPeriods?.[0] || ''}
                      onChange={(e) => {
                        if (disabled) return
                        const val = parseInt(e.target.value)
                        onChange({ ...config, [periodKey]: isNaN(val) || val <= 0 ? defaultPeriods?.[0] || 0 : val })
                      }}
                      disabled={disabled}
                      placeholder={String(defaultPeriods?.[0] || '')}
                      className="w-full px-2 py-1 rounded text-[10px] text-center"
                      style={{ background: '#1E2329', border: '1px solid #2B3139', color: '#EAECEF' }}
                    />
                  )
                )}
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* ============================================ */}
      {/* Section 3: Market Sentiment                 */}
      {/* ============================================ */}
      <div className="rounded-lg overflow-hidden" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
        <div className="px-3 py-2 flex items-center gap-2" style={{ background: '#1E2329', borderBottom: '1px solid #2B3139' }}>
          <TrendingUp className="w-4 h-4" style={{ color: '#22c55e' }} />
          <span className="text-sm font-medium" style={{ color: '#EAECEF' }}>{ts(indicator.marketSentiment, language)}</span>
          <span className="text-xs" style={{ color: '#848E9C' }}>- {ts(indicator.marketSentimentDesc, language)}</span>
        </div>

        <div className="p-3">
          <div className="grid grid-cols-3 gap-2">
            {[
              { key: 'enable_volume', label: 'volume', desc: 'volumeDesc', color: '#c084fc' },
              { key: 'enable_oi', label: 'oi', desc: 'oiDesc', color: '#34d399' },
              { key: 'enable_funding_rate', label: 'fundingRate', desc: 'fundingRateDesc', color: '#fbbf24' },
            ].map(({ key, label, desc, color }) => (
              <div
                key={key}
                className="p-2.5 rounded-lg transition-all"
                style={{
                  background: config[key as keyof IndicatorConfig] ? `${color}08` : 'transparent',
                  border: `1px solid ${config[key as keyof IndicatorConfig] ? `${color}30` : '#2B3139'}`,
                }}
              >
                <div className="flex items-center justify-between mb-1">
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full" style={{ background: color }} />
                    <span className="text-xs font-medium" style={{ color: '#EAECEF' }}>{ts(indicator[label as keyof typeof indicator], language)}</span>
                  </div>
                  <input
                    type="checkbox"
                    checked={config[key as keyof IndicatorConfig] as boolean || false}
                    onChange={(e) => !disabled && onChange({ ...config, [key]: e.target.checked })}
                    disabled={disabled}
                    className="w-4 h-4 rounded accent-yellow-500"
                  />
                </div>
                <p className="text-[10px]" style={{ color: '#5E6673' }}>{ts(indicator[desc as keyof typeof indicator], language)}</p>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
