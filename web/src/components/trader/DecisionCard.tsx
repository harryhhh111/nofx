import { useMemo, useState } from 'react'
import type { DecisionRecord, DecisionAction } from '../../types'
import { t, type Language } from '../../i18n/translations'

interface DecisionCardProps {
  decision: DecisionRecord
  language: Language
  onSymbolClick?: (symbol: string) => void
}

// Action type configuration
const ACTION_CONFIG: Record<string, { color: string; bg: string; icon: string; label: string }> = {
  open_long: { color: '#0ECB81', bg: 'rgba(14, 203, 129, 0.15)', icon: '📈', label: 'LONG' },
  open_short: { color: '#F6465D', bg: 'rgba(246, 70, 93, 0.15)', icon: '📉', label: 'SHORT' },
  close_long: { color: '#F0B90B', bg: 'rgba(240, 185, 11, 0.15)', icon: '💰', label: 'CLOSE' },
  close_short: { color: '#F0B90B', bg: 'rgba(240, 185, 11, 0.15)', icon: '💰', label: 'CLOSE' },
  hold: { color: '#848E9C', bg: 'rgba(132, 142, 156, 0.15)', icon: '⏸️', label: 'HOLD' },
  wait: { color: '#848E9C', bg: 'rgba(132, 142, 156, 0.15)', icon: '⏳', label: 'WAIT' },
}

// Format price with proper decimals
function formatPrice(price: number | undefined): string {
  if (!price || price === 0) return '-'
  if (price >= 1000) return price.toFixed(2)
  if (price >= 1) return price.toFixed(4)
  return price.toFixed(6)
}

// Calculate percentage change
function calcPctChange(entry: number | undefined, target: number | undefined, isLong: boolean): string {
  if (!entry || !target || entry === 0) return '-'
  const pct = ((target - entry) / entry) * 100
  const adjustedPct = isLong ? pct : -pct
  return `${adjustedPct >= 0 ? '+' : ''}${adjustedPct.toFixed(2)}%`
}

// Get confidence color
function getConfidenceColor(confidence: number | undefined): string {
  if (!confidence) return '#848E9C'
  if (confidence >= 80) return '#0ECB81'
  if (confidence >= 60) return '#F0B90B'
  return '#F6465D'
}

function parseDecisionJson(raw: string | undefined): Record<string, any> | null {
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
}

function getArrayLength(value: unknown): number {
  return Array.isArray(value) ? value.length : 0
}

function getSnapshotSymbols(parsed: Record<string, any> | null): string[] {
  const snapshot = parsed?.factor_snapshot
  if (!snapshot || typeof snapshot !== 'object' || Array.isArray(snapshot)) return []
  return Object.keys(snapshot)
}

function getArray(value: unknown): any[] {
  return Array.isArray(value) ? value : []
}

function getObject(value: unknown): Record<string, any> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, any> : null
}

function summaryStepColor(status: unknown): string {
  switch (String(status || '').toLowerCase()) {
    case 'ok':
    case 'trade':
      return '#0ECB81'
    case 'warn':
    case 'reviewed':
      return '#F0B90B'
    case 'reject':
    case 'rejected':
    case 'error':
      return '#F6465D'
    default:
      return '#A7B0BC'
  }
}

function formatScore(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(1) : '-'
}

function formatWeight(value: unknown): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return value <= 1 ? value.toFixed(2) : value.toFixed(0)
}

function formatComponentValue(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(1) : String(value || '-')
}

function factorLabel(factor: string, language: Language): string {
  if (language !== 'zh') return factor
  const labels: Record<string, string> = {
    trend: '趋势',
    momentum: '动量',
    structure: '结构',
    derivatives: '衍生/资金',
  }
  return labels[factor] || factor
}

function statusText(status: unknown, language: Language): string {
  const value = String(status || '')
  if (!value) return ''
  if (language !== 'zh') return value || '-'
  const labels: Record<string, string> = {
    disabled: '未启用',
    available: '可用',
    enabled_but_unavailable_this_cycle: '本轮启用但不可用',
  }
  return labels[value] || value || '-'
}

function setupLabel(setup: string | undefined, language: Language): string {
  if (!setup) return language === 'zh' ? '未分类' : 'Unclassified'
  const zh: Record<string, string> = {
    no_trade_threshold_not_met: '未达到阈值',
    no_trade_insufficient_evidence: '证据不足',
    no_trade_chop: '震荡过滤',
    trend_continuation_long: '多头趋势延续',
    trend_continuation_short: '空头趋势延续',
    trend_pullback_long: '多头趋势回调',
    trend_pullback_short: '空头趋势回调',
    breakout_long: '多头突破',
    breakout_short: '空头突破',
    breakout_retest_long: '多头突破回踩',
    breakout_retest_short: '空头突破回踩',
    range_reversal_long: '区间多头反转',
    range_reversal_short: '区间空头反转',
    support_resistance_bounce_long: '支撑反弹',
    support_resistance_bounce_short: '阻力回落',
    momentum_exhaustion_long: '空头动量衰竭',
    momentum_exhaustion_short: '多头动量衰竭',
    unclassified: '未分类',
  }
  return language === 'zh' ? (zh[setup] || setup) : setup
}

function getTraceReason(trace: any): string {
  if (typeof trace?.reason === 'string' && trace.reason) return trace.reason
  if (typeof trace?.primary?.reason === 'string' && trace.primary.reason) return trace.primary.reason
  if (typeof trace?.entry?.reason === 'string' && trace.entry.reason) return trace.entry.reason
  return ''
}

function boolText(value: unknown, language: Language): string {
  return value ? (language === 'zh' ? '可用' : 'Available') : (language === 'zh' ? '不可用' : 'Unavailable')
}

function latestTimeText(value: unknown): string {
  if (!value) return '-'
  const time = new Date(String(value))
  return Number.isNaN(time.getTime()) ? '-' : time.toLocaleString()
}

function healthText(value: unknown, reason: unknown, language: Language): string {
  if (value === false) return language === 'zh' ? '不足' : 'Insufficient'
  const rawReason = String(reason || '')
  if (rawReason.includes('limited warm-up')) return language === 'zh' ? '最低满足' : 'Minimum OK'
  if (value === true) return language === 'zh' ? '充足' : 'Healthy'
  return '-'
}

function healthColor(value: unknown, reason: unknown): string {
  if (value === false) return '#F6465D'
  const rawReason = String(reason || '')
  if (rawReason.includes('limited warm-up')) return '#F0B90B'
  if (value === true) return '#0ECB81'
  return '#A7B0BC'
}

function marketRegimeLabel(value: unknown, language: Language): string {
  const raw = String(value || '-')
  if (language !== 'zh') return raw
  const labels: Record<string, string> = {
    risk_on: '风险偏好',
    risk_off: '风险规避 / 偏空',
    chop: '震荡',
    high_volatility: '高波动',
    overheated: '过热',
    unavailable: '不可用',
  }
  return labels[raw] || raw
}

function trendLabel(value: unknown, language: Language): string {
  const raw = String(value || '-')
  if (language !== 'zh') return raw
  const labels: Record<string, string> = {
    bullish: '偏多',
    bearish: '偏空',
    mixed: '混合',
    neutral: '中性',
    unavailable: '不可用',
  }
  return labels[raw] || raw
}

function directionBiasLabel(value: unknown, language: Language): string {
  const raw = String(value || '-')
  if (language !== 'zh') return raw
  const labels: Record<string, string> = {
    bullish: '偏多',
    bearish: '偏空',
    neutral: '中性',
    unavailable: '不可用',
  }
  return labels[raw] || raw
}

function formatRatioPercent(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? `${Math.round(value * 100)}%` : '-'
}

function marketDirectionReasons(marketContext: Record<string, any> | null, language: Language): string[] {
  if (!marketContext) return []
  const metrics = marketContext.metrics && typeof marketContext.metrics === 'object' ? marketContext.metrics : {}
  const reasons = [
    language === 'zh'
      ? `BTC ${trendLabel(marketContext.btc_trend, language)}，ETH ${trendLabel(marketContext.eth_trend, language)}`
      : `BTC ${trendLabel(marketContext.btc_trend, language)}, ETH ${trendLabel(marketContext.eth_trend, language)}`,
    language === 'zh'
      ? `候选币偏多比例 ${formatRatioPercent(metrics.bullish_breadth_ratio)}`
      : `Bullish breadth ${formatRatioPercent(metrics.bullish_breadth_ratio)}`,
  ]
  if (marketContext.funding_state) {
    reasons.push(language === 'zh' ? `资金费率 ${String(marketContext.funding_state)}` : `Funding ${String(marketContext.funding_state)}`)
  }
  if (typeof metrics.external_signal_score === 'number') {
    reasons.push(language === 'zh' ? `外部信号 ${metrics.external_signal_score.toFixed(2)}` : `External score ${metrics.external_signal_score.toFixed(2)}`)
  }
  return reasons
}

function volatilityRegimeLabel(value: unknown, language: Language): string {
  const raw = String(value || '-')
  if (language !== 'zh') return raw
  const labels: Record<string, string> = {
    high_volatility: '高波动',
    normal: '正常',
    unavailable: '不可用',
  }
  return labels[raw] || raw
}

function riskFlagLabel(value: unknown, language: Language): string {
  const raw = String(value || '')
  if (language !== 'zh') return raw
  const labels: Record<string, string> = {
    high_volatility: '高波动',
    market_regime_risk_off: '市场偏空',
    market_regime_high_volatility: '高波动市场',
    large_position: '仓位偏大',
    low_confidence: '置信度不足',
  }
  return labels[raw] || raw
}

function translateTraceReason(reason: string, language: Language): string {
  if (!reason || language !== 'zh') return reason
  return reason
    .replace(/^no setup:/, '未形成可执行机会：')
    .replace(/long not ready:/g, '开多未就绪：')
    .replace(/short not ready:/g, '开空未就绪：')
    .replace(/primary score/g, '主周期评分')
    .replace(/entry score/g, '入场周期评分')
    .replace(/primary /g, '主周期 ')
    .replace(/entry /g, '入场 ')
    .replace(/ or /g, ' 或 ')
    .replace(/confirmation timeframe evidence is unavailable/g, '确认周期证据不可用')
    .replace(/(\\d+) confirmation timeframe\\(s\\) available/g, '$1 个确认周期可用')
    .replace(/confirmation conflict for long/g, '确认周期与开多冲突')
    .replace(/confirmation conflict for short/g, '确认周期与开空冲突')
}

function FactorBreakdown({ trace, language }: { trace: any; language: Language }) {
  if (!trace || typeof trace !== 'object') return null
  const components = trace.components && typeof trace.components === 'object' ? trace.components : {}
  const factorWeights = trace.factor_weights && typeof trace.factor_weights === 'object' ? trace.factor_weights : {}
  const factors = Object.keys(factorWeights).length > 0
    ? Object.keys(factorWeights)
    : Object.keys(components)
  if (factors.length === 0) return null
  const availableFactors = Array.isArray(trace.available_factors) ? trace.available_factors : []
  const missingFactors = Array.isArray(trace.missing_factors) ? trace.missing_factors : []

  return (
    <div className="mt-2 rounded-md px-2 py-1.5" style={{ background: 'rgba(255,255,255,0.03)' }}>
      <div className="mb-1 flex flex-wrap items-center gap-2 text-[10px]" style={{ color: '#848E9C' }}>
        <span>
          {language === 'zh' ? '可用因子' : 'Available factors'}:
          <span className="ml-1 font-mono" style={{ color: trace.eligible ? '#0ECB81' : '#F6465D' }}>
            {availableFactors.length}/{trace.selected_factor_count ?? factors.length}
          </span>
        </span>
        <span>
          {language === 'zh' ? '最低要求' : 'Required'}:
          <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>
            {String(trace.required_factor_count ?? '-')}
          </span>
        </span>
        <span>
          {language === 'zh' ? '可用权重' : 'Available weight'}:
          <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>
            {formatWeight(trace.available_weight)} / {formatWeight(trace.total_weight)}
          </span>
        </span>
        {missingFactors.length > 0 && (
          <span style={{ color: '#F6465D' }}>
            {language === 'zh' ? '缺失' : 'Missing'}: {missingFactors.map((f: string) => factorLabel(f, language)).join(', ')}
          </span>
        )}
      </div>
      <div className="grid grid-cols-2 gap-1">
        {factors.map((factor) => {
          const raw = components[factor]
          const available = typeof raw === 'number' && Number.isFinite(raw)
          return (
            <div key={`${trace.symbol || ''}-${trace.timeframe || ''}-${factor}`} className="flex items-center justify-between gap-2 text-[10px]">
              <span style={{ color: available ? '#A7B0BC' : '#F6465D' }}>{factorLabel(factor, language)}</span>
              <span className="font-mono" style={{ color: available ? '#EAECEF' : '#F6465D' }}>
                {formatComponentValue(raw)}
                <span className="ml-1" style={{ color: '#848E9C' }}>w {formatWeight(factorWeights[factor])}</span>
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function getCycleStatus(decision: DecisionRecord, language: Language) {
  if (!decision.success) {
    return {
      label: t('failed', language),
      style: { background: 'rgba(246, 70, 93, 0.15)', color: '#F6465D', border: '1px solid rgba(246, 70, 93, 0.3)' },
    }
  }
  if (!decision.decisions || decision.decisions.length === 0) {
    return {
      label: language === 'zh' ? '无交易' : 'No Trade',
      style: { background: 'rgba(132, 142, 156, 0.15)', color: '#A7B0BC', border: '1px solid rgba(132, 142, 156, 0.3)' },
    }
  }
  return {
    label: language === 'zh' ? '已执行' : 'Executed',
    style: { background: 'rgba(14, 203, 129, 0.15)', color: '#0ECB81', border: '1px solid rgba(14, 203, 129, 0.3)' },
  }
}

// Single Action Card Component
function ActionCard({ action, language, onSymbolClick }: { action: DecisionAction; language: Language; onSymbolClick?: (symbol: string) => void }) {
  const config = ACTION_CONFIG[action.action] || ACTION_CONFIG.wait
  const isLong = action.action.includes('long')
  const isOpen = action.action.includes('open')

  return (
    <div
      className="rounded-lg p-4 transition-all duration-200 hover:scale-[1.01]"
      style={{
        background: 'linear-gradient(135deg, #1E2329 0%, #181C21 100%)',
        border: `1px solid ${config.color}33`,
        boxShadow: `0 4px 12px rgba(0, 0, 0, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.03)`,
      }}
    >
      {/* Header Row */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-3">
          <span className="text-xl">{config.icon}</span>
          <span
            className="font-mono font-bold text-lg cursor-pointer transition-all duration-200 hover:scale-110"
            style={{ color: '#EAECEF' }}
            onClick={() => onSymbolClick?.(action.symbol)}
            title="Click to view chart"
          >
            {action.symbol.replace('USDT', '')}
          </span>
          <span
            className="px-3 py-1 rounded-full text-xs font-bold uppercase tracking-wider"
            style={{ background: config.bg, color: config.color, border: `1px solid ${config.color}55` }}
          >
            {config.label}
          </span>
        </div>

        {/* Status Badge */}
        <div className="flex items-center gap-2">
          {action.confidence !== undefined && action.confidence > 0 && (
            <div
              className="px-2 py-1 rounded text-xs font-semibold"
              style={{
                background: `${getConfidenceColor(action.confidence)}22`,
                color: getConfidenceColor(action.confidence)
              }}
            >
              {action.confidence.toFixed(0)}%
            </div>
          )}
          <div
            className="w-2 h-2 rounded-full"
            style={{ background: action.success ? '#0ECB81' : '#F6465D' }}
          />
        </div>
      </div>

      {/* Trading Details Grid */}
      {isOpen && (
        <div className="grid grid-cols-4 gap-3 mt-3 pt-3" style={{ borderTop: '1px solid #2B3139' }}>
          {/* Entry Price */}
          <div className="text-center">
            <div className="text-xs mb-1" style={{ color: '#848E9C' }}>
              {t('entryPrice', language)}
            </div>
            <div className="font-mono font-semibold" style={{ color: '#EAECEF' }}>
              {formatPrice(action.price)}
            </div>
          </div>

          {/* Stop Loss */}
          <div className="text-center">
            <div className="text-xs mb-1" style={{ color: '#F6465D' }}>
              {t('stopLoss', language)}
            </div>
            <div className="font-mono font-semibold" style={{ color: '#F6465D' }}>
              {formatPrice(action.stop_loss)}
            </div>
            {action.stop_loss && action.price && (
              <div className="text-xs mt-0.5" style={{ color: '#848E9C' }}>
                {calcPctChange(action.price, action.stop_loss, isLong)}
              </div>
            )}
          </div>

          {/* Take Profit */}
          <div className="text-center">
            <div className="text-xs mb-1" style={{ color: '#0ECB81' }}>
              {t('takeProfit', language)}
            </div>
            <div className="font-mono font-semibold" style={{ color: '#0ECB81' }}>
              {formatPrice(action.take_profit)}
            </div>
            {action.take_profit && action.price && (
              <div className="text-xs mt-0.5" style={{ color: '#848E9C' }}>
                {calcPctChange(action.price, action.take_profit, isLong)}
              </div>
            )}
          </div>

          {/* Leverage */}
          <div className="text-center">
            <div className="text-xs mb-1" style={{ color: '#848E9C' }}>
              {t('leverage', language)}
            </div>
            <div className="font-mono font-semibold" style={{ color: '#F0B90B' }}>
              {action.leverage}x
            </div>
          </div>
        </div>
      )}

      {/* Risk/Reward Ratio for open positions */}
      {isOpen && action.stop_loss && action.take_profit && action.price && (
        <div className="mt-3 pt-3 flex items-center justify-between" style={{ borderTop: '1px solid #2B3139' }}>
          <span className="text-xs" style={{ color: '#848E9C' }}>{t('riskReward', language)}</span>
          <div className="flex items-center gap-2">
            {(() => {
              const slDist = Math.abs(action.price - action.stop_loss)
              const tpDist = Math.abs(action.take_profit - action.price)
              const ratio = slDist > 0 ? (tpDist / slDist) : 0
              const ratioColor = ratio >= 3 ? '#0ECB81' : ratio >= 2 ? '#F0B90B' : '#F6465D'
              return (
                <>
                  <div className="flex gap-1">
                    <span style={{ color: '#F6465D' }}>1</span>
                    <span style={{ color: '#848E9C' }}>:</span>
                    <span style={{ color: '#0ECB81' }}>{ratio.toFixed(1)}</span>
                  </div>
                  <div
                    className="h-1.5 rounded-full"
                    style={{
                      width: '60px',
                      background: '#2B3139',
                    }}
                  >
                    <div
                      className="h-full rounded-full transition-all duration-300"
                      style={{
                        width: `${Math.min(ratio / 5 * 100, 100)}%`,
                        background: ratioColor
                      }}
                    />
                  </div>
                </>
              )
            })()}
          </div>
        </div>
      )}

      {/* Reasoning */}
      {action.reasoning && (
        <div className="mt-3 pt-3" style={{ borderTop: '1px solid #2B3139' }}>
          <div className="text-xs line-clamp-2" style={{ color: '#848E9C' }}>
            💡 {action.reasoning}
          </div>
        </div>
      )}

      {/* Error Message */}
      {action.error && (
        <div
          className="mt-3 rounded p-2 text-xs"
          style={{
            background: 'rgba(246, 70, 93, 0.1)',
            border: '1px solid rgba(246, 70, 93, 0.3)',
            color: '#F6465D',
          }}
        >
          ❌ {action.error}
        </div>
      )}
    </div>
  )
}

export function DecisionCard({ decision, language, onSymbolClick }: DecisionCardProps) {
  const [showSystemPrompt, setShowSystemPrompt] = useState(false)
  const [showInputPrompt, setShowInputPrompt] = useState(false)
  const [showCoT, setShowCoT] = useState(false)
  const [showRawDecision, setShowRawDecision] = useState(false)
  const parsedDecision = useMemo(() => parseDecisionJson(decision.decision_json), [decision.decision_json])
  const snapshotSymbols = useMemo(() => getSnapshotSymbols(parsedDecision), [parsedDecision])
  const setupEvaluations = useMemo(() => getArray(parsedDecision?.setup_evaluations), [parsedDecision])
  const scoringEvaluations = useMemo(() => getArray(parsedDecision?.scoring_evaluations), [parsedDecision])
  const ruleEvaluations = useMemo(() => getArray(parsedDecision?.rule_evaluations), [parsedDecision])
  const reviews = useMemo(() => getArray(parsedDecision?.reviews), [parsedDecision])
  const riskRejected = useMemo(() => getArray(parsedDecision?.risk?.rejected), [parsedDecision])
  const riskApproved = useMemo(() => getArray(parsedDecision?.risk?.approved), [parsedDecision])
  const userDecisionSummary = useMemo(
    () => getObject(parsedDecision?.user_decision_summary) || getObject(decision.user_decision_summary),
    [parsedDecision, decision.user_decision_summary]
  )
  const inputAudit = parsedDecision?.input_audit && typeof parsedDecision.input_audit === 'object' ? parsedDecision.input_audit : null
  const marketContext = parsedDecision?.market_context && typeof parsedDecision.market_context === 'object' ? parsedDecision.market_context : null
  const signalCount = getArrayLength(parsedDecision?.signals)
  const candidateCount = decision.candidate_coins?.length || snapshotSymbols.length
  const actionCount = decision.decisions?.length || 0
  const status = getCycleStatus(decision, language)

  // Copy text to clipboard
  const copyToClipboard = async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text)
      alert(`${label} copied!`)
    } catch (err) {
      console.error('Failed to copy:', err)
    }
  }

  // Download text as file
  const downloadAsFile = (text: string, filename: string) => {
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  }

  return (
    <div
      className="rounded-xl p-5 transition-all duration-300 hover:translate-y-[-2px]"
      style={{
        border: '1px solid #2B3139',
        background: 'linear-gradient(180deg, #1E2329 0%, #181C21 100%)',
        boxShadow: '0 4px 16px rgba(0, 0, 0, 0.3)',
      }}
    >
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <div
            className="w-10 h-10 rounded-lg flex items-center justify-center"
            style={{ background: 'rgba(240, 185, 11, 0.15)' }}
          >
            <span className="text-xl">🤖</span>
          </div>
          <div>
            <div className="font-bold" style={{ color: '#EAECEF' }}>
              {t('cycle', language)} #{decision.cycle_number}
            </div>
            <div className="text-xs" style={{ color: '#848E9C' }}>
              {new Date(decision.timestamp).toLocaleString()}
            </div>
          </div>
        </div>
        <div
          className="px-4 py-1.5 rounded-full text-xs font-bold tracking-wider"
          style={status.style}
        >
          {status.label}
        </div>
      </div>

      <div className="grid grid-cols-3 gap-2 mb-4">
        <div className="rounded-lg p-3" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
          <div className="text-[10px]" style={{ color: '#848E9C' }}>{language === 'zh' ? '候选币' : 'Candidates'}</div>
          <div className="font-mono font-semibold" style={{ color: '#EAECEF' }}>{candidateCount}</div>
        </div>
        <div className="rounded-lg p-3" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
          <div className="text-[10px]" style={{ color: '#848E9C' }}>{language === 'zh' ? '信号' : 'Signals'}</div>
          <div className="font-mono font-semibold" style={{ color: signalCount > 0 ? '#F0B90B' : '#A7B0BC' }}>{signalCount}</div>
        </div>
        <div className="rounded-lg p-3" style={{ background: '#0B0E11', border: '1px solid #2B3139' }}>
          <div className="text-[10px]" style={{ color: '#848E9C' }}>{language === 'zh' ? '动作' : 'Actions'}</div>
          <div className="font-mono font-semibold" style={{ color: actionCount > 0 ? '#0ECB81' : '#A7B0BC' }}>{actionCount}</div>
        </div>
      </div>

      {userDecisionSummary && (
        <div
          className="rounded-lg p-3 mb-4"
          style={{ background: 'rgba(14, 203, 129, 0.06)', border: '1px solid rgba(14, 203, 129, 0.18)' }}
        >
          <div className="mb-2 flex items-center justify-between gap-3">
            <div className="text-xs font-semibold" style={{ color: '#EAECEF' }}>
              {language === 'zh' ? '决策摘要' : 'Decision Summary'}
            </div>
            <span className="rounded px-2 py-0.5 text-[10px] font-mono" style={{ color: summaryStepColor(userDecisionSummary.status), background: 'rgba(255,255,255,0.05)' }}>
              {String(userDecisionSummary.status || '-')}
            </span>
          </div>
          {typeof userDecisionSummary.headline === 'string' && userDecisionSummary.headline && (
            <div className="mb-3 text-sm leading-relaxed" style={{ color: '#EAECEF' }}>
              {userDecisionSummary.headline}
            </div>
          )}
          {getArray(userDecisionSummary.steps).length > 0 && (
            <div className="space-y-2">
              {getArray(userDecisionSummary.steps).slice(0, 4).map((step: any, index: number) => (
                <div key={`${String(step?.title || 'step')}-${index}`} className="rounded-md px-2 py-1.5" style={{ background: 'rgba(255,255,255,0.03)' }}>
                  <div className="mb-0.5 flex items-center gap-2 text-[11px]">
                    <span className="font-semibold" style={{ color: summaryStepColor(step?.status) }}>
                      {String(step?.title || '-')}
                    </span>
                    <span className="font-mono" style={{ color: '#848E9C' }}>{String(step?.status || '')}</span>
                  </div>
                  <div className="text-[11px] leading-relaxed" style={{ color: '#A7B0BC' }}>
                    {String(step?.summary || '-')}
                  </div>
                </div>
              ))}
            </div>
          )}
          {getArray(userDecisionSummary.symbols).length > 0 && (
            <div className="mt-3 space-y-1">
              {getArray(userDecisionSummary.symbols).slice(0, 3).map((item: any) => (
                <div key={String(item?.symbol || item?.reason)} className="text-[11px] leading-relaxed" style={{ color: '#A7B0BC' }}>
                  <span className="font-mono" style={{ color: '#EAECEF' }}>{String(item?.symbol || '-')}</span>
                  <span className="mx-1" style={{ color: summaryStepColor(item?.decision) }}>{String(item?.decision || '-')}</span>
                  <span>{String(item?.reason || '')}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {decision.success && actionCount === 0 && (
        <div
          className="rounded-lg p-3 mb-4 text-xs"
          style={{ background: 'rgba(132, 142, 156, 0.12)', border: '1px solid rgba(132, 142, 156, 0.25)', color: '#A7B0BC' }}
        >
          {language === 'zh'
            ? '本轮流程已完成，但代码没有产生可执行开/平仓信号，因此没有调用 AI 审核。下面可以查看代码评估过程。'
            : 'This cycle completed, but the deterministic engine produced no executable signal, so AI review was not called. Inspect the code evaluation below.'}
        </div>
      )}

      {(setupEvaluations.length > 0 || scoringEvaluations.length > 0 || ruleEvaluations.length > 0 || reviews.length > 0 || riskRejected.length > 0 || marketContext || inputAudit) && (
        <div
          className="rounded-lg p-3 mb-4"
          style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
        >
          <div className="mb-2 text-xs font-semibold" style={{ color: '#EAECEF' }}>
            {language === 'zh' ? '代码评估过程' : 'Code Evaluation'}
          </div>
          {marketContext && (
            <div
              className="mb-3 rounded-md p-2"
              style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)' }}
            >
              <div className="mb-1 text-[11px] font-semibold" style={{ color: '#EAECEF' }}>
                {language === 'zh' ? '市场状态' : 'Market Context'}
              </div>
              <div className="grid grid-cols-2 gap-2 text-[10px]" style={{ color: '#A7B0BC' }}>
                <div>
                  {language === 'zh' ? '状态' : 'Regime'}:
                  <span className="ml-1 font-mono" style={{ color: marketContext?.market_regime === 'risk_off' ? '#F6465D' : '#EAECEF' }}>
                    {marketRegimeLabel(marketContext?.market_regime, language)}
                  </span>
                </div>
                <div>
                  {language === 'zh' ? '方向' : 'Direction'}:
                  <span className="ml-1 font-mono" style={{ color: marketContext?.direction_bias === 'bearish' ? '#F6465D' : marketContext?.direction_bias === 'bullish' ? '#0ECB81' : '#EAECEF' }}>
                    {directionBiasLabel(marketContext?.direction_bias, language)}
                  </span>
                  <span className="ml-2">{language === 'zh' ? '波动' : 'Vol'}:</span>
                  <span className="ml-1 font-mono" style={{ color: marketContext?.volatility_regime === 'high_volatility' ? '#F0B90B' : '#EAECEF' }}>
                    {volatilityRegimeLabel(marketContext?.volatility_regime, language)}
                  </span>
                </div>
                <div>
                  BTC:
                  <span className="ml-1 font-mono" style={{ color: marketContext?.btc_trend === 'bearish' ? '#F6465D' : '#EAECEF' }}>
                    {trendLabel(marketContext?.btc_trend, language)}
                  </span>
                  <span className="ml-2">ETH:</span>
                  <span className="ml-1 font-mono" style={{ color: marketContext?.eth_trend === 'bearish' ? '#F6465D' : '#EAECEF' }}>
                    {trendLabel(marketContext?.eth_trend, language)}
                  </span>
                </div>
	                {Array.isArray(marketContext?.risk_flags) && marketContext.risk_flags.length > 0 && (
	                  <div className="col-span-2" style={{ color: '#F0B90B' }}>
	                    {language === 'zh' ? '风险标记' : 'Risk flags'}: {marketContext.risk_flags.map((flag: unknown) => riskFlagLabel(flag, language)).join(', ')}
	                  </div>
	                )}
	                <div className="col-span-2">
	                  {language === 'zh' ? '方向原因' : 'Direction reason'}:
	                  <span className="ml-1">{marketDirectionReasons(marketContext, language).join('；') || '-'}</span>
	                </div>
	                {typeof marketContext?.context_summary === 'string' && marketContext.context_summary && (
	                  <div className="col-span-2 font-mono" style={{ color: '#848E9C' }}>
	                    {marketContext.context_summary}
	                  </div>
	                )}
	              </div>
	            </div>
	          )}
          {inputAudit && (
            <div
              className="mb-3 rounded-md p-2"
              style={{ background: 'rgba(240, 185, 11, 0.06)', border: '1px solid rgba(240, 185, 11, 0.16)' }}
            >
              <div className="mb-2 text-[11px] font-semibold" style={{ color: '#F0B90B' }}>
                {language === 'zh' ? '输入审计' : 'Input Audit'}
              </div>
              <div className="grid grid-cols-2 gap-2 text-[10px]" style={{ color: '#A7B0BC' }}>
	                <div>
	                  {language === 'zh' ? 'K 线源' : 'K-line Source'}:
	                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{String(inputAudit?.klines?.market_data_source || '-')}</span>
	                </div>
	                <div>
	                  {language === 'zh' ? '请求周期' : 'Timeframes'}:
	                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>
                    {Array.isArray(inputAudit?.klines?.timeframes) ? inputAudit.klines.timeframes.join(', ') : '-'}
                  </span>
                </div>
                <div>
                  {language === 'zh' ? '主/入场/确认' : 'Primary/Entry/Confirm'}:
                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>
                    {String(inputAudit?.klines?.primary_timeframe || '-')} / {String(inputAudit?.klines?.entry_timeframe || '-')} / {Array.isArray(inputAudit?.klines?.confirmations) ? inputAudit.klines.confirmations.join(', ') || '-' : '-'}
                  </span>
                </div>
                <div>
                  {language === 'zh' ? '计算 K 线' : 'Compute Bars'}:
                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{String(inputAudit?.klines?.compute_lookback || '-')}</span>
                </div>
                <div>
                  {language === 'zh' ? '展示 K 线' : 'Display Bars'}:
                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{String(inputAudit?.klines?.display_count || '-')}</span>
                </div>
                <div>
                  {language === 'zh' ? '最低计算需求' : 'Required Bars'}:
                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{String(inputAudit?.klines?.required_lookback || '-')}</span>
                </div>
                <div>
                  {language === 'zh' ? '稳定建议' : 'Warm-up Target'}:
                  <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{String(inputAudit?.klines?.warmup_target || '-')}</span>
                </div>
                {Array.isArray(inputAudit?.klines?.unused_timeframes) && inputAudit.klines.unused_timeframes.length > 0 && (
                  <div className="col-span-2" style={{ color: '#F0B90B' }}>
                    {language === 'zh' ? '已拉取但未参与当前评分' : 'Fetched but not used by current scoring'}:
                    <span className="ml-1 font-mono">{inputAudit.klines.unused_timeframes.join(', ')}</span>
                  </div>
                )}
                <div>
                  OI Ranking:
                  <span className="ml-1" style={{ color: inputAudit?.external_data?.oi_ranking_available ? '#0ECB81' : '#F6465D' }}>
                    {statusText(inputAudit?.external_data?.statuses?.oi_ranking, language) || boolText(inputAudit?.external_data?.oi_ranking_available, language)}
                  </span>
                </div>
                <div>
                  NetFlow:
                  <span className="ml-1" style={{ color: inputAudit?.external_data?.netflow_available ? '#0ECB81' : '#F6465D' }}>
                    {statusText(inputAudit?.external_data?.statuses?.netflow, language) || boolText(inputAudit?.external_data?.netflow_available, language)}
                  </span>
                </div>
                <div>
                  Price Ranking:
                  <span className="ml-1" style={{ color: inputAudit?.external_data?.price_ranking_available ? '#0ECB81' : '#F6465D' }}>
                    {statusText(inputAudit?.external_data?.statuses?.price_ranking, language) || boolText(inputAudit?.external_data?.price_ranking_available, language)}
                  </span>
                </div>
                <div>
                  Quant:
                  <span className="ml-1 font-mono" style={{ color: (inputAudit?.external_data?.quant_symbols || 0) > 0 ? '#0ECB81' : '#F6465D' }}>
                    {String(inputAudit?.external_data?.quant_symbols ?? 0)}
                    <span className="ml-1" style={{ color: '#A7B0BC' }}>
                      {statusText(inputAudit?.external_data?.statuses?.quant, language)}
                    </span>
                  </span>
                </div>
              </div>
              {Array.isArray(inputAudit?.external_data?.data_fetch_errors) && inputAudit.external_data.data_fetch_errors.length > 0 && (
                <div className="mt-2 rounded px-2 py-1 text-[10px]" style={{ background: 'rgba(246,70,93,0.08)', color: '#F6465D' }}>
                  {language === 'zh' ? '数据错误' : 'Data errors'}: {inputAudit.external_data.data_fetch_errors.slice(0, 2).join('; ')}
                </div>
              )}
              {inputAudit?.symbols && typeof inputAudit.symbols === 'object' && (
                <div className="mt-2 space-y-1">
                  {Object.entries(inputAudit.symbols).slice(0, 6).map(([symbol, audit]: [string, any]) => (
                    <div key={`${symbol}-input-audit`} className="rounded px-2 py-1" style={{ background: 'rgba(255,255,255,0.03)' }}>
                      <div className="font-mono text-[10px]" style={{ color: '#EAECEF' }}>{symbol}</div>
                      <div className="mt-1 grid grid-cols-1 gap-1 text-[10px]" style={{ color: '#848E9C' }}>
                        {Object.entries(audit?.timeframes || {}).map(([tf, tfAudit]: [string, any]) => (
                          <div key={`${symbol}-${tf}`}>
                            <span className="font-mono" style={{ color: '#A7B0BC' }}>{tf}</span>
                            <span className="ml-2">
                              {language === 'zh' ? '实际/计算' : 'display/compute'} {String(tfAudit?.display_bars ?? '-')} / {String(tfAudit?.compute_bars ?? '-')}
                            </span>
                            <span className="ml-2">
                              {language === 'zh' ? '需/余量' : 'required/warmup'} {String(tfAudit?.required_lookback ?? '-')} / {String(tfAudit?.warmup_bars ?? '-')}
                            </span>
                            <span className="ml-2" style={{ color: healthColor(tfAudit?.calculation_healthy, tfAudit?.health_reason) }}>
                              {healthText(tfAudit?.calculation_healthy, tfAudit?.health_reason, language)}
                            </span>
                            <span className="ml-2">
                              {language === 'zh' ? '最新' : 'latest'} {latestTimeText(tfAudit?.latest_time)}
                            </span>
                            <span className="ml-2">
                              close {formatPrice(typeof tfAudit?.latest_close === 'number' ? tfAudit.latest_close : undefined)}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
          {ruleEvaluations.length > 0 && (
            <div className="space-y-2 mb-3">
              <div className="text-[11px] font-semibold" style={{ color: '#EAECEF' }}>
                {language === 'zh' ? '规则评估（为什么触发 / 未触发）' : 'Rule Evaluation (why fired / not)'}
              </div>
              {ruleEvaluations.slice(0, 12).map((trace: any, index: number) => (
                <div
                  key={`${String(trace?.rule_id || index)}-${String(trace?.symbol || '')}-rule`}
                  className="rounded-md p-2"
                  style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)' }}
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="font-mono text-xs" style={{ color: '#EAECEF' }}>
                      {String(trace?.symbol || '-')} · {String(trace?.rule_id || '-')}
                      <span className="ml-1 text-[10px]" style={{ color: '#848E9C' }}>{String(trace?.action || '')}</span>
                    </div>
                    <div
                      className="rounded px-2 py-0.5 text-[10px]"
                      style={{
                        color: trace?.matched ? '#0ECB81' : trace?.missing ? '#F0B90B' : '#A7B0BC',
                        background: trace?.matched ? 'rgba(14,203,129,0.12)' : trace?.missing ? 'rgba(240,185,11,0.12)' : 'rgba(132,142,156,0.12)',
                      }}
                    >
                      {trace?.matched
                        ? (language === 'zh' ? '已触发' : 'fired')
                        : trace?.missing
                          ? (language === 'zh' ? '数据缺失' : 'data missing')
                          : (language === 'zh' ? '未触发' : 'no trigger')}
                    </div>
                  </div>
                  <div className="mt-2 space-y-1">
                    {Array.isArray(trace?.conditions) && trace.conditions.map((c: any, ci: number) => (
                      <div
                        key={ci}
                        className="flex flex-wrap items-center gap-1 font-mono text-[10px]"
                        style={{ color: c?.passed ? '#0ECB81' : '#F6465D' }}
                      >
                        <span>{c?.passed ? '✓' : '✗'}</span>
                        <span style={{ color: '#EAECEF' }}>{String(c?.left || '')}</span>
                        <span style={{ color: '#A7B0BC' }}>
                          {c?.left_available && typeof c?.left_value === 'number' ? `(${c.left_value.toFixed(2)})` : '(n/a)'}
                        </span>
                        <span style={{ color: '#848E9C' }}>{String(c?.operator || '')}</span>
                        <span style={{ color: '#EAECEF' }}>{String(c?.right || '')}</span>
                        <span style={{ color: '#A7B0BC' }}>
                          {c?.right_available && typeof c?.right_value === 'number' ? `(${c.right_value.toFixed(2)})` : ''}
                        </span>
                      </div>
                    ))}
                  </div>
                  {trace?.reason && (
                    <div className="mt-1 text-[10px]" style={{ color: '#848E9C' }}>{String(trace.reason)}</div>
                  )}
                </div>
              ))}
            </div>
          )}
          {(reviews.length > 0 || riskRejected.length > 0 || riskApproved.length > 0) && (
            <div className="space-y-2 mb-3">
              <div className="text-[11px] font-semibold" style={{ color: '#EAECEF' }}>
                {language === 'zh' ? '信号复核 / 风控门（为什么被拒 / 放行）' : 'Review / Risk Gate (why rejected / approved)'}
              </div>
              <div className="flex flex-wrap gap-2 text-[10px]" style={{ color: '#A7B0BC' }}>
                <span>{language === 'zh' ? '放行' : 'Approved'}: <span className="font-mono" style={{ color: '#0ECB81' }}>{riskApproved.length}</span></span>
                <span>{language === 'zh' ? '拒绝' : 'Rejected'}: <span className="font-mono" style={{ color: '#F6465D' }}>{riskRejected.length}</span></span>
                <span>{language === 'zh' ? '复核' : 'Reviews'}: <span className="font-mono" style={{ color: '#EAECEF' }}>{reviews.length}</span></span>
              </div>
              {riskRejected.slice(0, 12).map((r: any, index: number) => {
                const review = reviews.find((rv: any) => rv?.signal_id === r?.signal_id)
                const rawReason = String(r?.reason || '')
                const stage = rawReason.startsWith('llm_review_rejected')
                  ? (language === 'zh' ? 'LLM 复核拒绝' : 'LLM review rejected')
                  : rawReason.startsWith('market_context_rejected')
                    ? (language === 'zh' ? '市场上下文硬拒' : 'Market context reject')
                    : rawReason.startsWith('risk_gate_rejected')
                      ? (language === 'zh' ? '确定性风控拒绝' : 'Risk gate reject')
                      : (language === 'zh' ? '被拒' : 'Rejected')
                return (
                  <div
                    key={`${String(r?.signal_id || index)}-rej`}
                    className="rounded-md p-2"
                    style={{ background: 'rgba(246,70,93,0.06)', border: '1px solid rgba(246,70,93,0.18)' }}
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="font-mono text-[10px]" style={{ color: '#EAECEF' }}>{String(r?.signal_id || '-')}</div>
                      <div className="rounded px-2 py-0.5 text-[10px]" style={{ color: '#F6465D', background: 'rgba(246,70,93,0.12)' }}>{stage}</div>
                    </div>
                    <div className="mt-1 font-mono text-[10px] break-all" style={{ color: '#A7B0BC' }}>{rawReason || '-'}</div>
                    {review && (review.summary || (Array.isArray(review.reasons) && review.reasons.length > 0)) && (
                      <div className="mt-1 text-[10px]" style={{ color: '#848E9C' }}>
                        {language === 'zh' ? 'LLM' : 'LLM'} [{String(review.status || '')}]: {String(review.summary || '')}
                        {Array.isArray(review.reasons) && review.reasons.length > 0 ? ` (${review.reasons.join('; ')})` : ''}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
          <div className="space-y-2">
            {setupEvaluations.slice(0, 6).map((trace, index) => {
              const primary = trace?.primary || {}
              const entry = trace?.entry || {}
              const timeframes = trace?.timeframes || {}
              const reason = getTraceReason(trace)
              return (
                <div
                  key={`${String(trace?.symbol || index)}-setup-trace`}
                  className="rounded-md p-2"
                  style={{ background: 'rgba(255, 255, 255, 0.03)', border: '1px solid rgba(255, 255, 255, 0.08)' }}
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="font-mono text-xs" style={{ color: '#EAECEF' }}>
                      {String(trace?.symbol || '-')}
                    </div>
                    <div
                      className="rounded px-2 py-0.5 text-[10px]"
                      style={{
                        color: trace?.eligible ? '#0ECB81' : '#A7B0BC',
                        background: trace?.eligible ? 'rgba(14, 203, 129, 0.12)' : 'rgba(132, 142, 156, 0.12)',
                      }}
                    >
                      {setupLabel(String(trace?.setup || ''), language)}
                    </div>
                  </div>
                  <div className="mt-2 grid grid-cols-3 gap-2 text-[10px]" style={{ color: '#A7B0BC' }}>
                    <div>
                      {language === 'zh' ? '主周期' : 'Primary'} {String(timeframes?.primary || '-')}
                      <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{formatScore(primary?.score)}</span>
                    </div>
                    <div>
                      {language === 'zh' ? '入场' : 'Entry'} {String(timeframes?.entry || '-')}
                      <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{formatScore(entry?.score)}</span>
                    </div>
                    <div>
                      {language === 'zh' ? '确认' : 'Confirm'} {Array.isArray(timeframes?.confirmations) && timeframes.confirmations.length > 0 ? timeframes.confirmations.join(', ') : '-'}
                    </div>
                  </div>
                  {reason && (
                    <div className="mt-1 text-[10px] leading-relaxed" style={{ color: '#848E9C' }}>
                      {translateTraceReason(reason, language)}
                    </div>
                  )}
                  <div className="mt-2 grid grid-cols-1 gap-2">
                    <div>
                      <div className="text-[10px] font-semibold" style={{ color: '#A7B0BC' }}>
                        {language === 'zh' ? '主周期因子拆分' : 'Primary factor breakdown'}
                      </div>
                      <FactorBreakdown trace={primary} language={language} />
                    </div>
                    <div>
                      <div className="text-[10px] font-semibold" style={{ color: '#A7B0BC' }}>
                        {language === 'zh' ? '入场周期因子拆分' : 'Entry factor breakdown'}
                      </div>
                      <FactorBreakdown trace={entry} language={language} />
                    </div>
                    {Array.isArray(trace?.confirmations) && trace.confirmations.length > 0 && (
                      <div>
                        <div className="text-[10px] font-semibold" style={{ color: '#A7B0BC' }}>
                          {language === 'zh' ? '确认周期因子拆分' : 'Confirmation factor breakdown'}
                        </div>
                        <div className="space-y-1">
                          {trace.confirmations.map((confirm: any, confirmIndex: number) => (
                            <div key={`${String(trace?.symbol || index)}-confirm-${confirmIndex}`}>
                              <div className="mt-1 text-[10px]" style={{ color: '#848E9C' }}>
                                {String(confirm?.timeframe || '-')}
                                <span className="ml-1 font-mono" style={{ color: '#EAECEF' }}>{formatScore(confirm?.score)}</span>
                              </div>
                              <FactorBreakdown trace={confirm} language={language} />
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
          {setupEvaluations.length === 0 && scoringEvaluations.length > 0 && (
            <div className="text-[11px]" style={{ color: '#A7B0BC' }}>
              {language === 'zh' ? '本轮只有评分评估，没有 setup 评估。' : 'This cycle has scoring evaluations but no setup evaluation.'}
            </div>
          )}
        </div>
      )}

      {/* Decision Actions - Beautiful Grid */}
      {decision.decisions && decision.decisions.length > 0 && (
        <div className="space-y-3 mb-4">
          {decision.decisions.map((action, index) => (
            <ActionCard key={`${action.symbol}-${index}`} action={action} language={language} onSymbolClick={onSymbolClick} />
          ))}
        </div>
      )}

      {/* Collapsible Sections */}
      <div className="space-y-2">
        {parsedDecision && (
          <div>
            <button
              onClick={() => setShowRawDecision(!showRawDecision)}
              className="flex items-center gap-2 text-sm transition-colors w-full justify-between p-2 rounded hover:bg-white/5"
            >
              <div className="flex items-center gap-2">
                <span className="text-base">📊</span>
                <span className="font-semibold" style={{ color: '#F0B90B' }}>
                  {language === 'zh' ? '结构化数据' : 'Structured Data'}
                </span>
                {snapshotSymbols.length > 0 && (
                  <span className="text-xs" style={{ color: '#848E9C' }}>
                    {snapshotSymbols.slice(0, 4).join(', ')}
                  </span>
                )}
              </div>
              <span
                className="text-xs px-2 py-0.5 rounded"
                style={{ background: 'rgba(240, 185, 11, 0.15)', color: '#F0B90B' }}
              >
                {showRawDecision ? t('collapse', language) : t('expand', language)}
              </span>
            </button>
            {showRawDecision && (
              <div
                className="mt-2 rounded-lg p-4 text-xs font-mono whitespace-pre-wrap max-h-96 overflow-y-auto"
                style={{
                  background: '#0B0E11',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              >
                {JSON.stringify(parsedDecision, null, 2)}
              </div>
            )}
          </div>
        )}

        {/* System Prompt */}
        {decision.system_prompt && (
          <div>
            <button
              onClick={() => setShowSystemPrompt(!showSystemPrompt)}
              className="flex items-center gap-2 text-sm transition-colors w-full justify-between p-2 rounded hover:bg-white/5"
            >
              <div className="flex items-center gap-2">
                <span className="text-base">⚙️</span>
                <span className="font-semibold" style={{ color: '#a78bfa' }}>
                  System Prompt
                </span>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    copyToClipboard(decision.system_prompt, 'System Prompt')
                  }}
                  className="text-xs px-2.5 py-1 rounded hover:opacity-80 transition-opacity flex items-center gap-1"
                  style={{ background: 'rgba(167, 139, 250, 0.2)', color: '#a78bfa', border: '1px solid rgba(167, 139, 250, 0.3)' }}
                  title="Copy to clipboard"
                >
                  <span>📋</span>
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    downloadAsFile(decision.system_prompt, `system-prompt-cycle-${decision.cycle_number}.txt`)
                  }}
                  className="text-xs px-2.5 py-1 rounded hover:opacity-80 transition-opacity flex items-center gap-1"
                  style={{ background: 'rgba(167, 139, 250, 0.2)', color: '#a78bfa', border: '1px solid rgba(167, 139, 250, 0.3)' }}
                  title="Download as file"
                >
                  <span>💾</span>
                </button>
                <span
                  className="text-xs px-2 py-0.5 rounded"
                  style={{ background: 'rgba(167, 139, 250, 0.15)', color: '#a78bfa' }}
                >
                  {showSystemPrompt ? t('collapse', language) : t('expand', language)}
                </span>
              </div>
            </button>
            {showSystemPrompt && (
              <div
                className="mt-2 rounded-lg p-4 text-sm font-mono whitespace-pre-wrap max-h-96 overflow-y-auto"
                style={{
                  background: '#0B0E11',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              >
                {decision.system_prompt}
              </div>
            )}
          </div>
        )}

        {/* User/Input Prompt */}
        {decision.input_prompt && (
          <div>
            <button
              onClick={() => setShowInputPrompt(!showInputPrompt)}
              className="flex items-center gap-2 text-sm transition-colors w-full justify-between p-2 rounded hover:bg-white/5"
            >
              <div className="flex items-center gap-2">
                <span className="text-base">📥</span>
                <span className="font-semibold" style={{ color: '#60a5fa' }}>
                  User Prompt
                </span>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    copyToClipboard(decision.input_prompt, 'User Prompt')
                  }}
                  className="text-xs px-2.5 py-1 rounded hover:opacity-80 transition-opacity flex items-center gap-1"
                  style={{ background: 'rgba(96, 165, 250, 0.2)', color: '#60a5fa', border: '1px solid rgba(96, 165, 250, 0.3)' }}
                  title="Copy to clipboard"
                >
                  <span>📋</span>
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    downloadAsFile(decision.input_prompt, `user-prompt-cycle-${decision.cycle_number}.txt`)
                  }}
                  className="text-xs px-2.5 py-1 rounded hover:opacity-80 transition-opacity flex items-center gap-1"
                  style={{ background: 'rgba(96, 165, 250, 0.2)', color: '#60a5fa', border: '1px solid rgba(96, 165, 250, 0.3)' }}
                  title="Download as file"
                >
                  <span>💾</span>
                </button>
                <span
                  className="text-xs px-2 py-0.5 rounded"
                  style={{ background: 'rgba(96, 165, 250, 0.15)', color: '#60a5fa' }}
                >
                  {showInputPrompt ? t('collapse', language) : t('expand', language)}
                </span>
              </div>
            </button>
            {showInputPrompt && (
              <div
                className="mt-2 rounded-lg p-4 text-sm font-mono whitespace-pre-wrap max-h-96 overflow-y-auto"
                style={{
                  background: '#0B0E11',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              >
                {decision.input_prompt}
              </div>
            )}
          </div>
        )}

        {/* AI Thinking */}
        {decision.cot_trace && (
          <div>
            <button
              onClick={() => setShowCoT(!showCoT)}
              className="flex items-center gap-2 text-sm transition-colors w-full justify-between p-2 rounded hover:bg-white/5"
            >
              <div className="flex items-center gap-2">
                <span className="text-base">🧠</span>
                <span className="font-semibold" style={{ color: '#F0B90B' }}>
                  {t('aiThinking', language)}
                </span>
              </div>
              <span
                className="text-xs px-2 py-0.5 rounded"
                style={{ background: 'rgba(240, 185, 11, 0.15)', color: '#F0B90B' }}
              >
                {showCoT ? t('collapse', language) : t('expand', language)}
              </span>
            </button>
            {showCoT && (
              <div
                className="mt-2 rounded-lg p-4 text-sm font-mono whitespace-pre-wrap max-h-96 overflow-y-auto"
                style={{
                  background: '#0B0E11',
                  border: '1px solid #2B3139',
                  color: '#EAECEF',
                }}
              >
                {decision.cot_trace}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Execution Log */}
      {decision.execution_log && decision.execution_log.length > 0 && (
        <div
          className="rounded-lg p-3 mt-4 text-xs font-mono space-y-1"
          style={{ background: '#0B0E11', border: '1px solid #2B3139' }}
        >
          {decision.execution_log.map((log, index) => (
            <div key={`${log}-${index}`} style={{ color: '#EAECEF' }}>
              {log}
            </div>
          ))}
        </div>
      )}

      {/* Error Message */}
      {decision.error_message && (
        <div
          className="rounded-lg p-3 mt-4 text-sm"
          style={{
            background: 'rgba(246, 70, 93, 0.1)',
            border: '1px solid rgba(246, 70, 93, 0.4)',
            color: '#F6465D',
          }}
        >
          ❌ {decision.error_message}
        </div>
      )}
    </div>
  )
}
