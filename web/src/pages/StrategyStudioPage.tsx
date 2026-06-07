import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '../contexts/AuthContext'
import { useLanguage } from '../contexts/LanguageContext'
import {
  Plus,
  Copy,
  Trash2,
  Check,
  ChevronDown,
  ChevronRight,
  BarChart3,
  Target,
  Shield,
  Zap,
  Activity,
  Save,
  Sparkles,
  Eye,
  Play,
  Loader2,
  RefreshCw,
  Clock,
  Bot,
  Send,
  Download,
  Upload,
  Globe,
  FileJson,
  AlertTriangle,
} from 'lucide-react'
import type { Strategy, StrategyConfig, AIModel, StrategyCompileResponse, StrategyCalibrationReport, AI500CoinsResponse, NofxOSStatus } from '../types'
import { api } from '../lib/api'
import { confirmToast, notify } from '../lib/notify'
import { CoinSourceEditor } from '../components/strategy/CoinSourceEditor'
import { IndicatorEditor } from '../components/strategy/IndicatorEditor'
import { RiskControlEditor } from '../components/strategy/RiskControlEditor'
import { PublishSettingsEditor } from '../components/strategy/PublishSettingsEditor'
import { GridConfigEditor, defaultGridConfig } from '../components/strategy/GridConfigEditor'
import { TokenEstimateBar } from '../components/strategy/TokenEstimateBar'
import { DeepVoidBackground } from '../components/common/DeepVoidBackground'
import { t } from '../i18n/translations'

const API_BASE = import.meta.env.VITE_API_BASE || ''
const COMPILE_DRAFT_STORAGE_PREFIX = 'nofx:strategyCompileDraft:v1:'
const LAST_SELECTED_STRATEGY_KEY = 'nofx:strategyStudio:lastSelectedStrategy:v1'

type StrategyCompileDraft = {
  strategyUpdatedAt: string
  savedAt: string
  config: StrategyConfig
  compileResult: StrategyCompileResponse | null
}

function getCompileDraftKey(strategyId: string) {
  return `${COMPILE_DRAFT_STORAGE_PREFIX}${strategyId}`
}

function readCompileDraft(strategy: Strategy): StrategyCompileDraft | null {
  if (typeof window === 'undefined' || strategy.is_default) return null
  try {
    const raw = window.localStorage.getItem(getCompileDraftKey(strategy.id))
    if (!raw) return null
    const draft = JSON.parse(raw) as StrategyCompileDraft
    if (!draft?.config || draft.strategyUpdatedAt !== strategy.updated_at) {
      window.localStorage.removeItem(getCompileDraftKey(strategy.id))
      return null
    }
    return draft
  } catch {
    window.localStorage.removeItem(getCompileDraftKey(strategy.id))
    return null
  }
}

function writeCompileDraft(strategy: Strategy, config: StrategyConfig, compileResult: StrategyCompileResponse | null) {
  if (typeof window === 'undefined' || strategy.is_default) return null
  const draft: StrategyCompileDraft = {
    strategyUpdatedAt: strategy.updated_at,
    savedAt: new Date().toISOString(),
    config,
    compileResult,
  }
  window.localStorage.setItem(getCompileDraftKey(strategy.id), JSON.stringify(draft))
  return draft
}

function clearCompileDraft(strategyId: string) {
  if (typeof window === 'undefined') return
  window.localStorage.removeItem(getCompileDraftKey(strategyId))
}

function readLastSelectedStrategyID() {
  if (typeof window === 'undefined') return ''
  return window.localStorage.getItem(LAST_SELECTED_STRATEGY_KEY) || ''
}

function writeLastSelectedStrategyID(strategyId: string) {
  if (typeof window === 'undefined' || !strategyId) return
  window.localStorage.setItem(LAST_SELECTED_STRATEGY_KEY, strategyId)
}

function clearLastSelectedStrategyID(strategyId: string) {
  if (typeof window === 'undefined') return
  if (window.localStorage.getItem(LAST_SELECTED_STRATEGY_KEY) === strategyId) {
    window.localStorage.removeItem(LAST_SELECTED_STRATEGY_KEY)
  }
}

function hasGeneratedStrategy(config?: StrategyConfig | null) {
  if (!config) return false
  return Boolean(
    (config.compiled_rules && config.compiled_rules.length > 0) ||
    config.scoring_config?.enabled
  )
}

function buildStrategyVersion() {
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  return [
    now.getUTCFullYear(),
    pad(now.getUTCMonth() + 1),
    pad(now.getUTCDate()),
    pad(now.getUTCHours()),
    pad(now.getUTCMinutes()),
    pad(now.getUTCSeconds()),
  ].join('')
}

function getDependencyCheck(flowPreview: Record<string, unknown> | null): { required: string[]; missing: string[] } | null {
  const value = flowPreview?.dependency_check
  if (!value || typeof value !== 'object') return null
  const check = value as { required?: unknown; missing?: unknown }
  return {
    required: Array.isArray(check.required) ? check.required.map(String) : [],
    missing: Array.isArray(check.missing) ? check.missing.map(String) : [],
  }
}

function getResultArray<T = Record<string, unknown>>(result: Record<string, unknown> | null, key: string): T[] {
  const value = result?.[key]
  return Array.isArray(value) ? value as T[] : []
}

function getResultObject<T = Record<string, unknown>>(result: Record<string, unknown> | null, key: string): T | null {
  const value = result?.[key]
  return value && typeof value === 'object' && !Array.isArray(value) ? value as T : null
}

function formatPreviewValue(value: unknown) {
  return typeof value === 'number' ? Number(value).toFixed(4) : '-'
}

function formatRatio(value: unknown) {
  if (typeof value !== 'number') return '-'
  return `${Math.round(value * 100)}%`
}

function marketTrendLabel(value: unknown, language: string) {
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

function marketDirectionLabel(value: unknown, language: string) {
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

function formatStrategyError(error: unknown, language: string) {
  const raw = error instanceof Error ? error.message : String(error || 'Unknown error')
  if (raw.includes('cannot be decrypted with the current DATA_ENCRYPTION_KEY')) {
    return language === 'zh'
      ? '当前模型 API Key 无法解密。请到 Config > AI Model 重新粘贴并保存该模型的 API Key，然后再生成策略。'
      : 'This model API key cannot be decrypted. Re-save the API key in Config > AI Model, then generate the strategy again.'
  }
  if (raw.includes('Missing Authentication header') || raw.includes('status 401')) {
    return language === 'zh'
      ? '模型认证失败。请检查 Config > AI Model 中的 API Key、Base URL 和模型名称是否正确。'
      : 'Model authentication failed. Check the API key, Base URL, and model name in Config > AI Model.'
  }
  if (raw.includes('timeout') || raw.includes('Network error')) {
    return language === 'zh'
      ? 'AI 测试请求超时或连接中断。通常是外部行情/NofxOS 数据或模型调用耗时过长；请稍后重试，或先关闭 NofxOS 排名/量化数据后预演。'
      : 'AI test timed out or the connection was interrupted. External market/NofxOS data or model calls may be taking too long; retry later or disable NofxOS ranking/quant data for preview.'
  }
  return raw
}

function clampNumber(value: number, min: number, max: number) {
  if (Number.isNaN(value)) return min
  return Math.min(max, Math.max(min, value))
}

function normalizeWeightMap(weights: Record<string, number>, factors: string[]) {
  const next: Record<string, number> = {}
  let total = 0
  for (const factor of factors) {
    const value = clampNumber(Number(weights[factor] ?? 0.01), 0.01, 1)
    next[factor] = value
    total += value
  }
  if (total <= 0) {
    const equal = factors.length > 0 ? 1 / factors.length : 0
    for (const factor of factors) next[factor] = Number(equal.toFixed(4))
    return next
  }
  for (const factor of factors) {
    next[factor] = Number((next[factor] / total).toFixed(4))
  }
  return next
}

function formatPeriods(periods?: number[]) {
  return periods && periods.length > 0 ? `(${periods.join('/')})` : ''
}

function getSelectedIndicatorLabels(config: StrategyConfig | null): string[] {
  const indicators = config?.indicators
  if (!indicators) return []
  const selected: string[] = []
  if (indicators.enable_ema) selected.push(`EMA${formatPeriods(indicators.ema_periods)}`)
  if (indicators.enable_sma) selected.push(`SMA${formatPeriods(indicators.sma_periods)}`)
  if (indicators.enable_macd) selected.push(`MACD(${indicators.macd_fast_period || 12}/${indicators.macd_slow_period || 26}/${indicators.macd_signal_period || 9})`)
  if (indicators.enable_rsi) selected.push(`RSI${formatPeriods(indicators.rsi_periods)}`)
  if (indicators.enable_atr) selected.push(`ATR${formatPeriods(indicators.atr_periods)}`)
  if (indicators.enable_adx) selected.push('ADX/+DI/-DI')
  if (indicators.enable_sar) selected.push('Parabolic SAR')
  if (indicators.enable_boll) selected.push(`BOLL${formatPeriods(indicators.boll_periods)}`)
  if (indicators.enable_volume) selected.push(`Volume${formatPeriods(indicators.volume_periods)}`)
  if (indicators.enable_session) selected.push('Session')
  if (indicators.enable_oi) selected.push('Open Interest')
  if (indicators.enable_funding_rate) selected.push('Funding Rate')
  if (indicators.enable_oi_ranking) selected.push(`OI Ranking(${indicators.oi_ranking_duration || '1h'})`)
  if (indicators.enable_netflow_ranking) selected.push(`NetFlow Ranking(${indicators.netflow_ranking_duration || '1h'})`)
  if (indicators.enable_price_ranking) selected.push(`Price Ranking(${indicators.price_ranking_duration || '1h'})`)
  if (config?.structure?.enable_fibonacci) selected.push('Fibonacci Structure')
  if (config?.structure?.enable_support_resistance) selected.push('Support/Resistance Structure')
  return selected
}

function buildIndicatorDrivenPrompt(config: StrategyConfig, language: string) {
  const indicators = getSelectedIndicatorLabels(config)
  if (indicators.length === 0) return ''
  const kline = config.indicators.klines
  const timeframes = kline.selected_timeframes?.length
    ? kline.selected_timeframes.join(', ')
    : [kline.primary_timeframe, kline.longer_timeframe].filter(Boolean).join(', ')
  const entryTimeframe = kline.entry_timeframe || kline.primary_timeframe
  const primaryTimeframe = kline.primary_timeframe
  const confirmationTimeframes = kline.confirmation_timeframes?.length
    ? kline.confirmation_timeframes.join(', ')
    : 'none'
  const coinSource = config.coin_source.source_type
  const risk = config.risk_control
  const positionSize = risk.min_position_size || 12
  const leverage = Math.min(risk.btc_eth_max_leverage || 2, risk.altcoin_max_leverage || risk.btc_eth_max_leverage || 2)
  const minConfidence = risk.min_confidence || 70
  const minRiskReward = risk.min_risk_reward_ratio || 2

  if (language === 'zh') {
    return [
      '用户没有填写自然语言策略，只勾选了指标。请基于这些已启用指标生成可执行的结构化交易策略。',
      `已启用指标/因子：${indicators.join('、')}。`,
      `交易周期：${timeframes || '使用配置中的主周期'}。币种来源：${coinSource}。`,
      `周期角色：主周期=${primaryTimeframe || '未设置'}，入场周期=${entryTimeframe || '未设置'}，确认周期=${confirmationTimeframes}。`,
      '如果没有明确的数值触发条件，请优先生成 strategy_mode="scoring" 的评分策略，而不是编造精确规则。',
      '评分因子只作为场景模板的结构化证据；程序会按主周期识别机会、按入场周期确认触发、按确认周期过滤方向冲突。',
      '评分因子应只使用已启用指标能够支持的 trend、momentum、structure、derivatives，不要使用未启用或不可用的数据。',
      `执行约束：最大持仓数 ${risk.max_positions || 1}，开仓杠杆不超过 ${leverage}，position_size_usd 使用 ${positionSize}，min_confidence 不低于 ${minConfidence}，止盈止损至少满足风险收益比 ${minRiskReward}。`,
      '程序负责计算 K 线和指标；AI 只负责编译策略结构，不要要求 AI 计算原始 K 线或指标值。',
    ].join('\n')
  }

  return [
    'The user did not write a natural-language strategy and only selected indicators. Generate an executable structured trading strategy from the enabled indicators.',
    `Enabled indicators/factors: ${indicators.join(', ')}.`,
    `Timeframes: ${timeframes || 'use configured primary timeframe'}. Coin source: ${coinSource}.`,
    `Timeframe roles: primary=${primaryTimeframe || 'unset'}, entry=${entryTimeframe || 'unset'}, confirmations=${confirmationTimeframes}.`,
    'If there are no exact numeric trigger conditions, prefer strategy_mode="scoring" instead of inventing precise rules.',
    'Scoring factors are structured evidence for setup templates; the program detects opportunities on the primary timeframe, confirms triggers on the entry timeframe, and filters direction conflicts on confirmation timeframes.',
    'Use only scoring factors supported by enabled data: trend, momentum, structure, derivatives. Do not use unavailable data.',
    `Execution constraints: max positions ${risk.max_positions || 1}, leverage no more than ${leverage}, position_size_usd ${positionSize}, min_confidence at least ${minConfidence}, risk/reward at least ${minRiskReward}.`,
    'The program calculates K-lines and indicators; AI only compiles strategy structure and must not calculate raw K-line indicators.',
  ].join('\n')
}

function isIndicatorDrivenPrompt(prompt: string, config: StrategyConfig | null | undefined, language: string) {
  if (!config) return false
  const generated = buildIndicatorDrivenPrompt(config, language).trim()
  return generated !== '' && prompt.trim() === generated
}

function getScoringFactorDisplay(factor: string, language: string) {
  const labels: Record<string, { zh: string; en: string; descZh: string; descEn: string }> = {
    trend: {
      zh: '趋势',
      en: 'Trend',
      descZh: '均线、MACD、ADX 等方向证据，判断行情是否顺势。',
      descEn: 'Directional evidence from EMA/SMA, MACD, ADX and similar indicators.',
    },
    momentum: {
      zh: '动量',
      en: 'Momentum',
      descZh: 'RSI、BOLL、成交量等强弱证据，判断推动力是否足够。',
      descEn: 'Strength evidence from RSI, BOLL, volume and similar indicators.',
    },
    structure: {
      zh: '结构',
      en: 'Structure',
      descZh: '斐波那契、支撑阻力等位置证据，判断入场位置是否合理。',
      descEn: 'Location evidence from Fibonacci, support/resistance and similar structures.',
    },
    derivatives: {
      zh: '衍生品数据',
      en: 'Derivatives',
      descZh: '持仓量、资金费率、AI500、资金流等外部数据证据。',
      descEn: 'External evidence from open interest, funding rate, AI500 and fund flow data.',
    },
  }
  const item = labels[factor] || { zh: factor, en: factor, descZh: factor, descEn: factor }
  return {
    label: language === 'zh' ? item.zh : item.en,
    code: factor,
    desc: language === 'zh' ? item.descZh : item.descEn,
  }
}

function getScoringFieldLabel(key: string, language: string) {
  const labels: Record<string, { zh: string; en: string; descZh: string; descEn: string }> = {
    long_threshold: {
      zh: '做多触发分',
      en: 'Long threshold',
      descZh: '主周期综合分达到该值才允许产生做多候选。',
      descEn: 'Primary timeframe score required before a long candidate can be created.',
    },
    short_threshold: {
      zh: '做空触发分',
      en: 'Short threshold',
      descZh: '主周期综合分低于该负值才允许产生做空候选。',
      descEn: 'Primary timeframe score required before a short candidate can be created.',
    },
    min_available_weight_ratio: {
      zh: '最小证据覆盖',
      en: 'Min evidence',
      descZh: '可用因子权重占比低于该值时，证据不足，不触发。',
      descEn: 'Minimum available factor-weight coverage required before triggering.',
    },
    min_confidence: {
      zh: '最小信心度',
      en: 'Min confidence',
      descZh: '候选信号低于该信心度会被过滤。',
      descEn: 'Candidate signals below this confidence are filtered out.',
    },
  }
  const item = labels[key]
  return {
    label: language === 'zh' ? item.zh : item.en,
    desc: language === 'zh' ? item.descZh : item.descEn,
  }
}

export function StrategyStudioPage() {
  const { token } = useAuth()
  const { language } = useLanguage()

  const [strategies, setStrategies] = useState<Strategy[]>([])
  const [selectedStrategy, setSelectedStrategy] = useState<Strategy | null>(null)
  const [editingConfig, setEditingConfig] = useState<StrategyConfig | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [estimatedTokens, setEstimatedTokens] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [hasChanges, setHasChanges] = useState(false)

  // AI Models for test run
  const [aiModels, setAiModels] = useState<AIModel[]>([])
  const [selectedModelId, setSelectedModelId] = useState<string>('')

  // Accordion states for left panel
  const [expandedSections, setExpandedSections] = useState({
    strategyPrompt: true,
    gridConfig: true,
    coinSource: true,
    indicators: false,
    riskControl: false,
    historyContext: false,
    publishSettings: false,
  })

  // Right panel states
  const [activeRightTab, setActiveRightTab] = useState<'structured' | 'flow' | 'calibration' | 'test'>('structured')
  const [flowPreview, setFlowPreview] = useState<Record<string, unknown> | null>(null)
  const [isLoadingFlowPreview, setIsLoadingFlowPreview] = useState(false)
  const [selectedVariant, setSelectedVariant] = useState('balanced')
  const [compileResult, setCompileResult] = useState<StrategyCompileResponse | null>(null)
  const [compileDraftSavedAt, setCompileDraftSavedAt] = useState<string | null>(null)
  const [isCompilingStrategy, setIsCompilingStrategy] = useState(false)
  const [ai500Preview, setAI500Preview] = useState<AI500CoinsResponse | null>(null)
  const [nofxOSStatus, setNofxOSStatus] = useState<NofxOSStatus | null>(null)
  const [dataSourceError, setDataSourceError] = useState<string | null>(null)
  const [isLoadingDataStatus, setIsLoadingDataStatus] = useState(false)
  const [calibrationReport, setCalibrationReport] = useState<StrategyCalibrationReport | null>(null)
  const [isLoadingCalibration, setIsLoadingCalibration] = useState(false)

  // AI Test Run states
  const [aiTestResult, setAiTestResult] = useState<Record<string, unknown> | null>(null)
  const [isRunningAiTest, setIsRunningAiTest] = useState(false)
  const dependencyCheck = getDependencyCheck(flowPreview)

  const toggleSection = (section: keyof typeof expandedSections) => {
    setExpandedSections((prev) => ({
      ...prev,
      [section]: !prev[section],
    }))
  }

  const selectStrategyForEdit = useCallback((strategy: Strategy) => {
    const draft = readCompileDraft(strategy)
    writeLastSelectedStrategyID(strategy.id)
    setSelectedStrategy(strategy)
    setEditingConfig(draft?.config || strategy.config)
    setHasChanges(Boolean(draft))
    setFlowPreview(null)
    setAiTestResult(null)
    setCalibrationReport(null)
    setCompileResult(draft?.compileResult || null)
    setCompileDraftSavedAt(draft?.savedAt || null)
  }, [])

  // Fetch AI Models
  const fetchAiModels = useCallback(async () => {
    if (!token) return
    try {
      const allModels = await api.getModelConfigs()
      const enabledModels = allModels.filter((m: AIModel) => m.enabled && m.provider !== 'claw402')
      setAiModels(enabledModels)
      if (enabledModels.length > 0) {
        setSelectedModelId((current) => current || enabledModels[0].id)
      }
    } catch (err) {
      console.error('Failed to fetch AI models:', err)
    }
  }, [token])

  // Fetch strategies
  const fetchStrategies = useCallback(async () => {
    if (!token) return
    try {
      const response = await fetch(`${API_BASE}/api/strategies`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!response.ok) throw new Error('Failed to fetch strategies')
      const data = await response.json()
      setStrategies(data.strategies || [])

      // Select the last edited strategy first, then active, then first.
      const lastSelectedID = readLastSelectedStrategyID()
      const lastSelected = data.strategies?.find((s: Strategy) => s.id === lastSelectedID)
      const active = data.strategies?.find((s: Strategy) => s.is_active)
      const nextSelected = lastSelected || active || data.strategies?.[0]
      if (nextSelected) {
        selectStrategyForEdit(nextSelected)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setIsLoading(false)
    }
  }, [token, selectStrategyForEdit])

  useEffect(() => {
    fetchStrategies()
    fetchAiModels()
  }, [fetchStrategies, fetchAiModels])

  // Create new strategy
  const handleCreateStrategy = async () => {
    if (!token) return
    try {
      const configResponse = await fetch(
        `${API_BASE}/api/strategies/default-config?lang=${language}`,
        { headers: { Authorization: `Bearer ${token}` } }
      )
      if (!configResponse.ok) throw new Error('Failed to fetch default config')
      const defaultConfig = await configResponse.json()

      const response = await fetch(`${API_BASE}/api/strategies`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: tr('newStrategyName'),
          description: '',
          config: defaultConfig,
        }),
      })
      if (!response.ok) throw new Error('Failed to create strategy')
      const result = await response.json()
      await fetchStrategies()
      // Auto-select the newly created strategy
      if (result.id) {
        const now = new Date().toISOString()
        const newStrategy = {
          id: result.id,
          name: tr('newStrategyName'),
          description: '',
          is_active: false,
          is_default: false,
          is_public: false,
          config_visible: true,
          config: defaultConfig,
          created_at: now,
          updated_at: now,
        }
        setSelectedStrategy(newStrategy)
        writeLastSelectedStrategyID(newStrategy.id)
        setEditingConfig(defaultConfig)
        setHasChanges(false)
        setCompileResult(null)
        setCompileDraftSavedAt(null)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  // Delete strategy
  const handleDeleteStrategy = async (id: string) => {
    if (!token) return

    // Check if strategy is in use by any trader before showing dialog
    try {
      const tradersResp = await fetch(`${API_BASE}/api/my-traders`, {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (tradersResp.ok) {
        const traderList = await tradersResp.json()
	        const using = traderList.filter((t: any) => t.strategy_id === id)
	        if (using.length > 0) {
	          const names = using.map((t: any) => t.trader_name).join(', ')
	          notify.error(t('strategyStudio.strategyInUseTitle', language), {
	            description: t('strategyStudio.strategyInUseDescription', language, { names }),
	            duration: 8000,
	          })
	          return
	        }
	      }
	    } catch {
	      // If the local pre-check fails, the backend still enforces delete safety.
	    }

	    const confirmed = await confirmToast(
	      t('strategyStudio.confirmDeleteStrategy', language, {
	        name: strategies.find((strategy) => strategy.id === id)?.name || tr('newStrategyName'),
	      }),
	      {
	        title: tr('confirmDelete'),
        okText: tr('delete'),
        cancelText: tr('cancel'),
      }
    )
    if (!confirmed) return

    try {
      const response = await fetch(`${API_BASE}/api/strategies/${id}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
	      })
	      if (!response.ok) {
	        const data = await response.json().catch(() => ({}))
	        if (data.error_key === 'strategy.delete.in_use') {
	          notify.error(t('strategyStudio.strategyInUseTitle', language), {
	            description: t('strategyStudio.strategyInUseBackendDescription', language),
	            duration: 8000,
	          })
	        } else if (data.error_key === 'strategy.delete.default') {
	          notify.error(t('strategyStudio.defaultStrategyCannotDelete', language))
	        } else {
	          notify.error(data.error || t('strategyStudio.deleteStrategyFailed', language))
	        }
	        return
	      }
      notify.success(tr('strategyDeleted'))
      clearCompileDraft(id)
      clearLastSelectedStrategyID(id)
      if (selectedStrategy?.id === id) {
        setSelectedStrategy(null)
        setEditingConfig(null)
        setHasChanges(false)
        setCompileResult(null)
        setCompileDraftSavedAt(null)
      }
      await fetchStrategies()
    } catch (err) {
      notify.error(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  // Duplicate strategy
  const handleDuplicateStrategy = async (id: string) => {
    if (!token) return
    try {
      const response = await fetch(`${API_BASE}/api/strategies/${id}/duplicate`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: tr('strategyCopy'),
        }),
      })
      if (!response.ok) throw new Error('Failed to duplicate strategy')
      await fetchStrategies()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  // Activate strategy
  const handleActivateStrategy = async (id: string) => {
    if (!token) return
    try {
      const response = await fetch(`${API_BASE}/api/strategies/${id}/activate`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!response.ok) throw new Error('Failed to activate strategy')
      await fetchStrategies()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  // Export strategy as JSON file
  const handleExportStrategy = (strategy: Strategy) => {
    const exportData = {
      name: strategy.name,
      description: strategy.description,
      config: strategy.config,
      exported_at: new Date().toISOString(),
      version: '1.0',
    }
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `strategy_${strategy.name.replace(/\s+/g, '_')}_${new Date().toISOString().split('T')[0]}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    notify.success(tr('strategyExported'))
  }

  // Import strategy from JSON file
  const handleImportStrategy = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file || !token) return

    try {
      const text = await file.text()
      const importData = JSON.parse(text)

      // Validate imported data
      if (!importData.config || !importData.name) {
        throw new Error(tr('invalidStrategyFile'))
      }

      // Create new strategy with imported config
      const response = await fetch(`${API_BASE}/api/strategies`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: `${importData.name} (${tr('imported')})`,
          description: importData.description || '',
          config: importData.config,
        }),
      })
      if (!response.ok) throw new Error('Failed to import strategy')

      notify.success(tr('strategyImported'))
      await fetchStrategies()
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Unknown error'
      notify.error(errorMsg)
    } finally {
      // Reset file input
      event.target.value = ''
    }
  }

  // Save strategy
  const handleSaveStrategy = async () => {
    if (!token || !selectedStrategy || !editingConfig) return
    if (estimatedTokens >= 128000 && currentStrategyType === 'ai_trading') {
      notify.warning(tr('tokenExceedWarning'))
      // continue with save
    }
    setIsSaving(true)
    try {
      // Always sync the config language with the current interface language
      const configWithLanguage = {
        ...editingConfig,
        language: language as 'zh' | 'en',
      }
      const response = await fetch(
        `${API_BASE}/api/strategies/${selectedStrategy.id}`,
        {
          method: 'PUT',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({
            name: selectedStrategy.name,
            description: selectedStrategy.description,
            config: configWithLanguage,
            is_public: selectedStrategy.is_public,
            config_visible: selectedStrategy.config_visible,
          }),
        }
      )
      if (!response.ok) throw new Error('Failed to save strategy')
      clearCompileDraft(selectedStrategy.id)
      setCompileDraftSavedAt(null)
      setHasChanges(false)
      notify.success(tr('strategySaved'))
      await fetchStrategies()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setIsSaving(false)
    }
  }

  // Update config section
  const updateConfig = <K extends keyof StrategyConfig>(
    section: K,
    value: StrategyConfig[K]
  ) => {
    if (!editingConfig) return
    const currentPrompt = (editingConfig.strategy_prompt || '').trim()
    const wasUsingIndicatorPrompt = isIndicatorDrivenPrompt(currentPrompt, editingConfig, language)
    const nextConfig = {
      ...editingConfig,
      [section]: value,
    }
    if (wasUsingIndicatorPrompt) {
      nextConfig.strategy_prompt = buildIndicatorDrivenPrompt(nextConfig, language)
    }
    setEditingConfig(nextConfig)
    if (selectedStrategy && !selectedStrategy.is_default) {
      const draft = writeCompileDraft(selectedStrategy, nextConfig, null)
      setCompileDraftSavedAt(draft?.savedAt || null)
      setCompileResult(null)
    }
    setHasChanges(true)
  }

  const updateStrategyPrompt = (prompt: string) => {
    if (!editingConfig) return
    const nextConfig = {
      ...editingConfig,
      strategy_prompt: prompt,
    }
    setEditingConfig(nextConfig)
    if (selectedStrategy && !selectedStrategy.is_default) {
      const draft = writeCompileDraft(selectedStrategy, nextConfig, null)
      setCompileDraftSavedAt(draft?.savedAt || null)
      setCompileResult(null)
    }
    setHasChanges(true)
  }

  const updateScoringConfig = (updates: Partial<NonNullable<StrategyConfig['scoring_config']>>) => {
    if (!editingConfig?.scoring_config) return
    const nextScoring = {
      ...editingConfig.scoring_config,
      ...updates,
    }
    const nextConfig = {
      ...editingConfig,
      scoring_config: nextScoring,
      resolved_parameters: {
        ...editingConfig.resolved_parameters,
        scoring: nextScoring,
      },
    }
    setEditingConfig(nextConfig)
    if (selectedStrategy && !selectedStrategy.is_default) {
      const draft = writeCompileDraft(selectedStrategy, nextConfig, compileResult)
      setCompileDraftSavedAt(draft?.savedAt || null)
    }
    setHasChanges(true)
  }

  const updateScoringWeight = (factor: string, weight: number) => {
    if (!editingConfig?.scoring_config) return
    const factors = editingConfig.scoring_config.selected_factors || Object.keys(editingConfig.scoring_config.factor_weights || {})
    updateScoringConfig({
      factor_weights: normalizeWeightMap({
        ...(editingConfig.scoring_config.factor_weights || {}),
        [factor]: clampNumber(weight, 0.01, 1),
      }, factors),
    })
  }

  const compileStrategyPrompt = async () => {
    if (!token || !editingConfig || !selectedModelId) return
    const manualPrompt = (editingConfig.strategy_prompt || '').trim()
    const indicatorPrompt = buildIndicatorDrivenPrompt(editingConfig, language)
    const storedIndicatorPrompt = selectedStrategy ? buildIndicatorDrivenPrompt(selectedStrategy.config, language) : ''
    const shouldUseIndicatorPrompt =
      !manualPrompt ||
      manualPrompt === indicatorPrompt.trim() ||
      (storedIndicatorPrompt.trim() !== '' && manualPrompt === storedIndicatorPrompt.trim())
    const prompt = shouldUseIndicatorPrompt ? indicatorPrompt : manualPrompt
    if (!prompt) {
      notify.warning(language === 'zh' ? '请先填写策略 Prompt，或至少勾选一个指标/因子' : 'Enter a strategy prompt or select at least one indicator/factor')
      return
    }
    setIsCompilingStrategy(true)
    setCompileResult(null)
    try {
      const shouldPersistFirstCompile = Boolean(
        selectedStrategy &&
        !selectedStrategy.is_default &&
        !compileDraftSavedAt &&
        !hasGeneratedStrategy(selectedStrategy.config)
      )
      const result = await api.compileStrategyPrompt({
        strategy_id: selectedStrategy?.id,
        strategy_version: buildStrategyVersion(),
        prompt,
        ai_model_id: selectedModelId,
        persist: shouldPersistFirstCompile,
      })
      const rolePrimaryTimeframe = editingConfig.indicators.klines.primary_timeframe
      const nextScoringConfig = result.scoring_config
        ? {
            ...result.scoring_config,
            timeframe: rolePrimaryTimeframe || result.scoring_config.timeframe,
          }
        : undefined
      const nextConfig = {
        ...editingConfig,
        strategy_prompt: result.strategy_prompt || prompt,
        strategy_mode: result.strategy_mode || 'rule',
        compiled_rules: result.compiled_rules || [],
        scoring_config: nextScoringConfig,
        resolved_parameters: {
          ...result.resolved_parameters,
          scoring: nextScoringConfig,
        },
      }
      setCompileResult(result)
      setEditingConfig(nextConfig)
      if (selectedStrategy && shouldPersistFirstCompile) {
        clearCompileDraft(selectedStrategy.id)
        setCompileDraftSavedAt(null)
      } else if (selectedStrategy) {
        const draft = writeCompileDraft(selectedStrategy, nextConfig, result)
        setCompileDraftSavedAt(draft?.savedAt || null)
      }
      setHasChanges(!shouldPersistFirstCompile)
      setActiveRightTab('structured')
      notify.success(
        shouldPersistFirstCompile
          ? (language === 'zh' ? '策略已生成并保存' : 'Strategy generated and saved.')
          : (language === 'zh' ? '策略已生成草稿，保存后生效' : 'Strategy draft generated. Save to apply.')
      )
      if (shouldPersistFirstCompile && selectedStrategy) {
        const refreshedStrategy = await api.getStrategy(selectedStrategy.id)
        setSelectedStrategy(refreshedStrategy)
        setEditingConfig(refreshedStrategy.config)
      }
    } catch (err) {
      const message = formatStrategyError(err, language)
      notify.error(message, { duration: 8000 })
    } finally {
      setIsCompilingStrategy(false)
    }
  }

  // Fetch structured trading flow preview
  const fetchFlowPreview = async () => {
    if (!token || !editingConfig) return
    setIsLoadingFlowPreview(true)
    try {
      const data = await api.previewStrategyFlow({
        config: editingConfig,
      })
      setFlowPreview(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setIsLoadingFlowPreview(false)
    }
  }

	const fetchCalibrationReport = async () => {
	  if (!selectedStrategy) return
	  setIsLoadingCalibration(true)
    try {
      const report = await api.getStrategyCalibrationReport(selectedStrategy.id, 1000)
      setCalibrationReport(report)
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unknown error'
      notify.error(message, { duration: 6000 })
    } finally {
      setIsLoadingCalibration(false)
    }
  }

  // Run AI test with real AI model
  const runAiTest = async () => {
    if (!token || !editingConfig || !selectedModelId) return
    setIsRunningAiTest(true)
    setAiTestResult(null)
    try {
      const data = await api.testRunStrategy({
        config: editingConfig,
        prompt_variant: selectedVariant,
        ai_model_id: selectedModelId,
        run_real_ai: true,
      })
      setAiTestResult(data)
    } catch (err) {
      const message = formatStrategyError(err, language)
      notify.error(message, { duration: 8000 })
      setAiTestResult({
        error: message,
      })
    } finally {
      setIsRunningAiTest(false)
    }
  }

  const runDeterministicPreview = async () => {
    if (!token || !editingConfig) return
    setIsRunningAiTest(true)
    setAiTestResult(null)
    try {
      const data = await api.testRunStrategy({
        config: editingConfig,
        prompt_variant: selectedVariant,
        run_real_ai: false,
      })
      setAiTestResult(data)
    } catch (err) {
      setAiTestResult({
        error: err instanceof Error ? err.message : 'Unknown error',
      })
    } finally {
      setIsRunningAiTest(false)
    }
  }

  const loadDataSourceStatus = async () => {
    setIsLoadingDataStatus(true)
    setDataSourceError(null)
    try {
      const [ai500Result, nofxosResult] = await Promise.allSettled([
        api.getAI500Coins(10, true),
        api.getNofxOSStatus(true),
      ])
      const ai500 = ai500Result.status === 'fulfilled' ? ai500Result.value : { coins: [], count: 0 }
      const nofxos = nofxosResult.status === 'fulfilled' ? nofxosResult.value : { records: [], count: 0 }
      setAI500Preview(ai500)
      setNofxOSStatus(nofxos)

      const errors: string[] = []
      if (ai500Result.status === 'rejected') {
        errors.push(ai500Result.reason instanceof Error ? ai500Result.reason.message : 'AI500 check failed')
      }
      if (nofxosResult.status === 'rejected') {
        errors.push(nofxosResult.reason instanceof Error ? nofxosResult.reason.message : 'NofxOS status check failed')
      }
      if (errors.length > 0) {
        setDataSourceError(errors.join('; '))
      }
    } finally {
      setIsLoadingDataStatus(false)
    }
  }

  const tr = (key: string) => t(`strategyStudio.${key}`, language)

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[70vh]">
        <div className="text-center">
          <div className="relative">
            <div className="w-16 h-16 rounded-full border-4 border-yellow-500/20 border-t-yellow-500 animate-spin" />
            <Zap className="w-6 h-6 text-yellow-500 absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2" />
          </div>
        </div>
      </div>
    )
  }

  // Get current strategy type (default to ai_trading if not set)
  const currentStrategyType = editingConfig?.strategy_type || 'ai_trading'
  const hasStructuredStrategy = hasGeneratedStrategy(editingConfig)
  const timeframeRoles = editingConfig?.indicators?.klines
    ? {
        primary: editingConfig.indicators.klines.primary_timeframe || editingConfig.scoring_config?.timeframe || '-',
        entry: editingConfig.indicators.klines.entry_timeframe || editingConfig.indicators.klines.primary_timeframe || '-',
        confirmations: editingConfig.indicators.klines.confirmation_timeframes || [],
      }
    : null
  const promptText = (editingConfig?.strategy_prompt || '').trim()
  const currentIndicatorPrompt = editingConfig ? buildIndicatorDrivenPrompt(editingConfig, language).trim() : ''
  const storedIndicatorPrompt = selectedStrategy ? buildIndicatorDrivenPrompt(selectedStrategy.config, language).trim() : ''
  const isUsingIndicatorPrompt =
    !promptText ||
    (currentIndicatorPrompt !== '' && promptText === currentIndicatorPrompt) ||
    (storedIndicatorPrompt !== '' && promptText === storedIndicatorPrompt)

  const configSections = [
    {
      key: 'strategyPrompt' as const,
      icon: FileJson,
      color: '#a855f7',
      title: language === 'zh' ? '策略 Prompt 编译' : 'Strategy Prompt Compile',
      forStrategyType: 'ai_trading' as const,
      content: editingConfig && (
        <div className="space-y-3">
          <div className="rounded-lg border border-purple-500/20 bg-purple-500/10 p-3 text-[11px] text-nofx-text-muted">
            <div className="mb-1 text-xs font-medium text-nofx-text">
              {language === 'zh' ? '使用流程' : 'Workflow'}
            </div>
            <div>
              {language === 'zh'
                ? '可直接勾选指标生成评分策略，也可以补充策略意图让 AI 编译成结构化规则。保存后由程序计算指标并执行；右侧可查看交易流和真实 AI 测试结果。'
                : 'Select indicators to generate a scoring strategy, or add strategy intent so AI can compile structured rules. Runtime indicators are calculated by the program; use the right panel to inspect flow and real AI test output.'}
            </div>
          </div>
          <textarea
            value={editingConfig.strategy_prompt || ''}
            onChange={(e) => updateStrategyPrompt(e.target.value)}
            disabled={selectedStrategy?.is_default}
            rows={7}
            className="w-full resize-none rounded-lg px-3 py-2 text-sm bg-nofx-bg border border-nofx-gold/20 text-nofx-text outline-none focus:border-purple-500 disabled:opacity-50"
            placeholder={language === 'zh'
              ? '可选：写策略意图、希望捕捉什么行情、开平仓偏好。不填写时，将根据已勾选指标生成评分策略。'
              : 'Optional: describe market setup and execution preference. If empty, the selected indicators will be used to generate a scoring strategy.'}
          />
          <div className="text-[10px] text-nofx-text-muted">
            {language === 'zh'
              ? '提示词模式也会经过代码支持的指标/结构校验；编译后可在右侧交易流查看依赖是否缺失。'
              : 'Prompt mode is validated against supported indicator and structure operands; check missing dependencies in the Flow tab after compile.'}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <select
              value={selectedModelId}
              onChange={(e) => setSelectedModelId(e.target.value)}
              disabled={selectedStrategy?.is_default || aiModels.length === 0}
              className="px-3 py-2 rounded-lg text-xs bg-nofx-bg border border-nofx-gold/20 text-nofx-text outline-none disabled:opacity-50"
            >
              {aiModels.length === 0 ? (
                <option value="">{language === 'zh' ? '没有可用模型' : 'No model'}</option>
              ) : aiModels.map((model) => (
                <option key={model.id} value={model.id}>
                  {model.name} ({model.provider})
                </option>
              ))}
            </select>
            <button
              onClick={compileStrategyPrompt}
              disabled={selectedStrategy?.is_default || isCompilingStrategy || !selectedModelId || (!(editingConfig.strategy_prompt || '').trim() && getSelectedIndicatorLabels(editingConfig).length === 0)}
              className="flex items-center justify-center gap-2 px-3 py-2 rounded-lg text-xs font-medium bg-purple-600 hover:bg-purple-700 text-white disabled:opacity-50"
            >
              {isCompilingStrategy ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
              {isUsingIndicatorPrompt
                ? (language === 'zh' ? '根据指标重新生成策略' : 'Regenerate from Indicators')
                : (language === 'zh' ? '编译为结构化策略' : 'Compile Structured Strategy')}
            </button>
          </div>
          {(selectedStrategy?.is_default || aiModels.length === 0) && (
            <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-[11px] text-yellow-100">
              <div className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-yellow-400" />
                <div>
                  <div className="font-medium text-yellow-300">
                    {language === 'zh' ? '当前不能生成策略' : 'Strategy generation is not available'}
                  </div>
                  <div className="mt-1 text-yellow-100/80">
                    {selectedStrategy?.is_default
                      ? (language === 'zh'
                        ? '系统默认策略是只读模板。请先复制或新建策略，再生成和保存结构化策略。'
                        : 'System default strategies are read-only templates. Duplicate or create a strategy before generating and saving a structured strategy.')
                      : (language === 'zh'
                        ? '当前没有加载到可用 AI 模型。请确认已登录并在 Config > AI Model 启用了模型。'
                        : 'No enabled AI model is loaded. Confirm that you are logged in and have an enabled model in Config > AI Model.')}
                  </div>
                </div>
              </div>
            </div>
          )}
          <div className="grid grid-cols-3 gap-2 text-[11px]">
            <div className="rounded bg-nofx-bg border border-white/10 p-2">
              <div className="text-nofx-text-muted">{language === 'zh' ? '模式' : 'Mode'}</div>
              <div className="text-nofx-text font-medium">{editingConfig.strategy_mode || 'rule'}</div>
            </div>
            <div className="rounded bg-nofx-bg border border-white/10 p-2">
              <div className="text-nofx-text-muted">{language === 'zh' ? '规则' : 'Rules'}</div>
              <div className="text-nofx-text font-medium">{editingConfig.compiled_rules?.length || 0}</div>
            </div>
            <div className="rounded bg-nofx-bg border border-white/10 p-2">
              <div className="text-nofx-text-muted">{language === 'zh' ? '评分' : 'Scoring'}</div>
              <div className="text-nofx-text font-medium">{editingConfig.scoring_config?.enabled ? 'on' : 'off'}</div>
            </div>
          </div>
          <div className="rounded-lg bg-nofx-bg border border-white/10 p-2">
            <div className="flex items-center justify-between gap-2">
              <div>
                <div className="text-xs font-medium text-nofx-text">
                  {language === 'zh' ? '数据源状态' : 'Data Sources'}
                </div>
                <div className="text-[11px] text-nofx-text-muted">
                  {language === 'zh' ? 'AI500 / NofxOS 钱包扣费数据检查' : 'AI500 / NofxOS wallet-billed data check'}
                </div>
              </div>
              <button
                onClick={loadDataSourceStatus}
                disabled={isLoadingDataStatus}
                className="flex items-center gap-1 rounded border border-white/10 px-2 py-1 text-[11px] text-nofx-text hover:border-nofx-gold/40 disabled:opacity-50"
              >
                {isLoadingDataStatus ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
                {language === 'zh' ? '检查' : 'Check'}
              </button>
            </div>
            {dataSourceError && (
              <div className="mt-2 rounded border border-red-500/30 bg-red-500/10 p-2 text-[11px] text-red-200">
                <div className="font-medium">
                  {language === 'zh' ? '数据源检查失败' : 'Data source check failed'}
                </div>
                <div className="mt-1 font-mono">
                  {dataSourceError}
                </div>
              </div>
            )}
            {(ai500Preview || nofxOSStatus) && (
              <div className="mt-2 grid grid-cols-2 gap-2 text-[11px]">
                <div className="rounded border border-white/10 bg-black/20 p-2">
                  <div className="text-nofx-text-muted">AI500</div>
                  <div className="mt-1 font-mono text-nofx-text">
                    {ai500Preview?.coins?.slice(0, 5).map((coin) => coin.symbol).join(', ') || '--'}
                  </div>
                </div>
                <div className="rounded border border-white/10 bg-black/20 p-2">
                  <div className="text-nofx-text-muted">NofxOS</div>
                  <div className="mt-1 font-mono text-nofx-text">
                    {nofxOSStatus?.count ?? 0} calls
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      ),
    },
    // Grid Config - only for grid_trading
    {
      key: 'gridConfig' as const,
      icon: Activity,
      color: '#0ECB81',
      title: tr('gridConfig'),
      forStrategyType: 'grid_trading' as const,
      content: editingConfig?.grid_config && (
        <GridConfigEditor
          config={editingConfig.grid_config}
          onChange={(gridConfig) => updateConfig('grid_config', gridConfig)}
          disabled={selectedStrategy?.is_default}
          language={language}
        />
      ),
    },
    // AI Trading sections
    {
      key: 'coinSource' as const,
      icon: Target,
      color: '#F0B90B',
      title: tr('coinSource'),
      forStrategyType: 'ai_trading' as const,
      content: editingConfig && (
        <CoinSourceEditor
          config={editingConfig.coin_source}
          onChange={(coinSource) => updateConfig('coin_source', coinSource)}
          disabled={selectedStrategy?.is_default}
          language={language}
        />
      ),
    },
    {
      key: 'indicators' as const,
      icon: BarChart3,
      color: '#0ECB81',
      title: tr('indicators'),
      forStrategyType: 'ai_trading' as const,
      content: editingConfig && (
        <IndicatorEditor
          config={editingConfig.indicators}
          onChange={(indicators) => updateConfig('indicators', indicators)}
          disabled={selectedStrategy?.is_default}
          language={language}
        />
      ),
    },
    {
      key: 'riskControl' as const,
      icon: Shield,
      color: '#F6465D',
      title: tr('riskControl'),
      forStrategyType: 'ai_trading' as const,
      content: editingConfig && (
        <RiskControlEditor
          config={editingConfig.risk_control}
          onChange={(riskControl) => updateConfig('risk_control', riskControl)}
          disabled={selectedStrategy?.is_default}
          language={language}
        />
      ),
    },
    {
      key: 'historyContext' as const,
      icon: Clock,
      color: '#60a5fa',
      title: language === 'zh' ? '历史上下文' : 'Historical Context',
      forStrategyType: 'ai_trading' as const,
      content: editingConfig && (
        <div className="space-y-3">
          <div>
            <p className="text-sm font-medium text-nofx-text">
              {language === 'zh' ? '历史交易影响' : 'Historical PnL Influence'}
            </p>
            <p className="text-xs text-nofx-text-muted mt-1">
              {language === 'zh'
                ? '启用后，交易复盘和历史表现可以作为结构化审查上下文；关闭后，只使用当前持仓和实时市场结构。'
                : 'When disabled, AI no longer sees closed-trade history or performance stats. Current positions and live market context still remain available.'}
            </p>
          </div>

          <label className="flex items-start justify-between gap-4 p-3 rounded-lg bg-nofx-bg border border-nofx-gold/20">
            <div className="min-w-0">
              <div className="text-sm font-medium text-nofx-text">
                {language === 'zh' ? '包含历史交易上下文' : 'Include Historical Trading Context'}
              </div>
              <div className="text-xs text-nofx-text-muted mt-1">
                {language === 'zh'
                  ? '开启：AI Review 可使用 Trade Memory、胜率、近期平仓记录等经验。关闭：只根据当前市场、候选信号和持仓判断。'
                  : 'On: AI Review can use Trade Memory, win rate, and recent closed trades. Off: AI reviews only current market, candidate signals, and positions.'}
              </div>
            </div>

            <input
              type="checkbox"
              checked={editingConfig.include_historical_context ?? true}
              onChange={(e) => updateConfig('include_historical_context', e.target.checked)}
              disabled={selectedStrategy?.is_default}
              className="mt-1 h-4 w-4 rounded border-nofx-gold/30 bg-nofx-bg text-nofx-gold focus:ring-nofx-gold disabled:opacity-50"
            />
          </label>
        </div>
      ),
    },    {
      key: 'publishSettings' as const,
      icon: Globe,
      color: '#0ECB81',
      title: tr('publishSettings'),
      forStrategyType: 'both' as const,
      content: selectedStrategy && (
        <PublishSettingsEditor
          isPublic={selectedStrategy.is_public ?? false}
          configVisible={selectedStrategy.config_visible ?? true}
          onIsPublicChange={(value) => {
            setSelectedStrategy({ ...selectedStrategy, is_public: value })
            setHasChanges(true)
          }}
          onConfigVisibleChange={(value) => {
            setSelectedStrategy({ ...selectedStrategy, config_visible: value })
            setHasChanges(true)
          }}
          disabled={selectedStrategy?.is_default}
          language={language}
        />
      ),
    },
  ].filter(section =>
    section.forStrategyType === 'both' || section.forStrategyType === currentStrategyType
  )

  return (
    <DeepVoidBackground className="h-[calc(100vh-64px)] flex flex-col bg-nofx-bg relative overflow-hidden">

      {/* Header */}
      {/* Header */}
      <div className="flex-shrink-0 px-4 py-3 border-b border-nofx-gold/20 bg-nofx-bg/60 backdrop-blur-md z-10">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-gradient-to-br from-nofx-gold to-yellow-500">
              <Sparkles className="w-5 h-5 text-black" />
            </div>
            <div>
              <h1 className="text-lg font-bold text-nofx-text">{tr('title')}</h1>
              <p className="text-xs text-nofx-text-muted">{tr('subtitle')}</p>
            </div>
          </div>
          {error && (
            <div className="ml-4 flex max-w-[560px] items-start gap-3 rounded-lg bg-nofx-danger/10 px-3 py-2 text-xs text-nofx-danger">
              <span className="min-w-0 whitespace-normal break-words leading-relaxed">{error}</span>
              <button
                onClick={() => setError(null)}
                className="shrink-0 rounded px-1 text-sm leading-none hover:bg-white/10"
                aria-label={language === 'zh' ? '关闭错误提示' : 'Dismiss error'}
              >
                ×
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Main Content - Three Columns */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left Column - Strategy List */}
        <div className="w-48 flex-shrink-0 border-r border-nofx-gold/20 overflow-y-auto bg-nofx-bg/30 backdrop-blur-sm z-10">
          <div className="p-2">
            <div className="flex items-center justify-between mb-2 px-2">
              <span className="text-xs font-medium text-nofx-text-muted">{tr('strategies')}</span>
              <div className="flex items-center gap-1">
                {/* Import button with hidden file input */}
                <label className="p-1 rounded hover:bg-white/10 transition-colors cursor-pointer text-nofx-text-muted hover:text-white" title={tr('importStrategy')}>
                  <Upload className="w-4 h-4" />
                  <input
                    type="file"
                    accept=".json"
                    onChange={handleImportStrategy}
                    className="hidden"
                  />
                </label>
                <button
                  onClick={handleCreateStrategy}
                  className="p-1 rounded hover:bg-white/10 transition-colors text-nofx-gold"
                  title={tr('newStrategyTooltip')}
                >
                  <Plus className="w-4 h-4" />
                </button>
              </div>
            </div>
            <div className="space-y-2">
              {strategies.map((strategy) => (
                <div
                  key={strategy.id}
                  onClick={() => selectStrategyForEdit(strategy)}
                  className={`group px-2 py-2 rounded-lg cursor-pointer transition-all ${selectedStrategy?.id === strategy.id
                    ? 'ring-1 ring-nofx-gold/50 bg-nofx-gold/10 shadow-[0_0_15px_rgba(240,185,11,0.1)]'
                    : 'hover:bg-nofx-bg-lighter/60 ring-1 ring-white/10 hover:ring-nofx-gold/20 bg-transparent'
                    }`}
                >
                  <div className="flex items-start justify-between">
                    <span className={`line-clamp-2 text-nofx-text ${language === 'zh' ? 'text-sm' : 'text-xs'}`}>{strategy.name}</span>
                    <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                      <button
                        onClick={(e) => { e.stopPropagation(); handleExportStrategy(strategy) }}
                        className="p-1 rounded hover:bg-white/10 text-nofx-text-muted hover:text-white"
                        title={tr('export')}
                      >
                        <Download className="w-3 h-3" />
                      </button>
                      {!strategy.is_default && (
                        <>
                          <button
                            onClick={(e) => { e.stopPropagation(); handleDuplicateStrategy(strategy.id) }}
                            className="p-1 rounded hover:bg-white/10 text-nofx-text-muted hover:text-white"
                            title={tr('duplicate')}
                          >
                            <Copy className="w-3 h-3" />
                          </button>
                          <button
                            onClick={(e) => { e.stopPropagation(); handleDeleteStrategy(strategy.id) }}
                            className="p-1 rounded hover:bg-nofx-danger/20 text-nofx-danger"
                            title={tr('deleteTooltip')}
                          >
                            <Trash2 className="w-3 h-3" />
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-1 mt-1 flex-wrap">
                    {strategy.is_active && (
                      <span className="px-1.5 py-0.5 text-[10px] rounded bg-nofx-success/15 text-nofx-success">
                        {tr('active')}
                      </span>
                    )}
                    {strategy.is_default && (
                      <span className="px-1.5 py-0.5 text-[10px] rounded bg-nofx-gold/15 text-nofx-gold">
                        {tr('default')}
                      </span>
                    )}
                    {strategy.is_public && (
                      <span className="px-1.5 py-0.5 text-[10px] rounded flex items-center gap-0.5 bg-blue-400/15 text-blue-400">
                        <Globe className="w-2.5 h-2.5" />
                        {tr('public')}
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Middle Column - Config Editor */}
        <div className="flex-1 min-w-0 overflow-y-auto border-r border-nofx-gold/20">
          {selectedStrategy && editingConfig ? (
            <div className="p-4">
              {/* Strategy Name & Actions */}
              <div className="flex items-center justify-between mb-4">
                <div className="flex-1 min-w-0">
                  <input
                    type="text"
                    value={selectedStrategy.name}
                    onChange={(e) => {
                      setSelectedStrategy({ ...selectedStrategy, name: e.target.value })
                      setHasChanges(true)
                    }}
                    disabled={selectedStrategy.is_default}
                    className="text-lg font-bold bg-transparent border-none outline-none w-full text-nofx-text placeholder-nofx-text-muted"
                  />
                  <input
                    type="text"
                    value={selectedStrategy.description || ''}
                    onChange={(e) => {
                      setSelectedStrategy({ ...selectedStrategy, description: e.target.value })
                      setHasChanges(true)
                    }}
                    disabled={selectedStrategy.is_default}
                    placeholder={tr('addDescription')}
                    className="text-xs bg-transparent border-none outline-none w-full text-nofx-text-muted placeholder-nofx-text-muted/50 mt-1"
                  />
                  {hasChanges && (
                    <span className="text-xs text-nofx-gold">{tr('unsaved')}</span>
                  )}
                </div>
                <div className="flex items-center gap-2 flex-shrink-0">
                  {!selectedStrategy.is_active && (
                    <button
                      onClick={() => handleActivateStrategy(selectedStrategy.id)}
                      className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-xs transition-colors bg-nofx-success/10 border border-nofx-success/30 text-nofx-success hover:bg-nofx-success/20"
                    >
                      <Check className="w-3 h-3" />
                      {tr('activate')}
                    </button>
                  )}
                  {!selectedStrategy.is_default && (
                    <button
                      onClick={handleSaveStrategy}
                      disabled={isSaving || !hasChanges}
                      className={`flex items-center gap-1 px-3 py-1.5 rounded-lg text-xs font-medium transition-colors disabled:opacity-50
                        ${hasChanges ? 'bg-nofx-gold text-black hover:bg-yellow-500' : 'bg-nofx-bg-lighter text-nofx-text-muted cursor-not-allowed'}`}
                    >
                      <Save className="w-3 h-3" />
                      {isSaving ? tr('saving') : tr('save')}
                    </button>
                  )}
                </div>
              </div>

              {compileDraftSavedAt && (
                <div className="mb-4 rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-[11px] text-yellow-100">
                  <div className="flex items-start gap-2">
                    <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-yellow-400" />
                    <div>
                      <div className="font-medium text-yellow-300">
                        {language === 'zh' ? '有未保存的策略生成草稿' : 'Unsaved generated strategy draft'}
                      </div>
                      <div className="mt-1 text-yellow-100/80">
                        {language === 'zh'
                          ? '当前结构化策略和评分配置只是草稿；刷新页面会继续保留这份草稿，但只有点击 Save 后才会写入数据库并用于交易。'
                          : 'The structured strategy and scoring config are still a draft. Refresh will keep this local draft, but only Save writes it to the database for trading.'}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* Token Estimate Bar */}
              {currentStrategyType === 'ai_trading' && (
                <div className="mb-4">
                  <TokenEstimateBar config={editingConfig} language={language} onTokenCountChange={setEstimatedTokens} />
                </div>
              )}

              {/* Strategy Type Selector */}
              {editingConfig && (
                <div className="mb-4 p-4 rounded-lg bg-nofx-bg-lighter border border-nofx-gold/20">
                  <div className="flex items-center gap-2 mb-3">
                    <Zap className="w-4 h-4" style={{ color: '#F0B90B' }} />
                    <span className="text-sm font-medium text-nofx-text">{tr('strategyType')}</span>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <button
                      onClick={() => {
                        if (!selectedStrategy?.is_default) {
                          updateConfig('strategy_type', 'ai_trading')
                          // Clear grid config when switching to AI trading
                          updateConfig('grid_config', undefined)
                        }
                      }}
                      disabled={selectedStrategy?.is_default}
                      className={`p-3 rounded-lg border transition-all ${
                        (!editingConfig.strategy_type || editingConfig.strategy_type === 'ai_trading')
                          ? 'border-nofx-gold bg-nofx-gold/10'
                          : 'border-nofx-border hover:border-nofx-gold/50'
                      }`}
                    >
                      <div className="flex items-center gap-2 mb-1">
                        <Bot className="w-4 h-4" style={{ color: '#F0B90B' }} />
                        <span className="text-sm font-medium text-nofx-text">{tr('aiTrading')}</span>
                      </div>
                      <p className="text-xs text-nofx-text-muted text-left">{tr('aiTradingDesc')}</p>
                    </button>
                    <button
                      onClick={() => {
                        if (!selectedStrategy?.is_default) {
                          updateConfig('strategy_type', 'grid_trading')
                          // Initialize grid config if not exists
                          if (!editingConfig.grid_config) {
                            updateConfig('grid_config', defaultGridConfig)
                          }
                        }
                      }}
                      disabled={selectedStrategy?.is_default}
                      className={`p-3 rounded-lg border transition-all ${
                        editingConfig.strategy_type === 'grid_trading'
                          ? 'border-nofx-gold bg-nofx-gold/10'
                          : 'border-nofx-border hover:border-nofx-gold/50'
                      }`}
                    >
                      <div className="flex items-center gap-2 mb-1">
                        <Activity className="w-4 h-4" style={{ color: '#0ECB81' }} />
                        <span className="text-sm font-medium text-nofx-text">{tr('gridTrading')}</span>
                      </div>
                      <p className="text-xs text-nofx-text-muted text-left">{tr('gridTradingDesc')}</p>
                    </button>
                  </div>
                </div>
              )}

              {/* Config Sections */}
              <div className="space-y-2">
                {configSections.map(({ key, icon: Icon, color, title, content }) => (
                  <div
                    key={key}
                    className="rounded-lg overflow-hidden bg-nofx-bg-lighter border border-nofx-gold/20"
                  >
                    <button
                      onClick={() => toggleSection(key)}
                      className="w-full flex items-center justify-between px-3 py-2.5 hover:bg-white/5 transition-colors"
                    >
                      <div className="flex items-center gap-2">
                        <Icon className="w-4 h-4" style={{ color }} />
                        <span className="text-sm font-medium text-nofx-text">{title}</span>
                      </div>
                      {expandedSections[key] ? (
                        <ChevronDown className="w-4 h-4 text-nofx-text-muted" />
                      ) : (
                        <ChevronRight className="w-4 h-4 text-nofx-text-muted" />
                      )}
                    </button>
                    {expandedSections[key] && (
                      <div className="px-3 pb-3">
                        {content}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="flex items-center justify-center h-full">
              <div className="text-center">
                <Activity className="w-12 h-12 mx-auto mb-2 opacity-30 text-nofx-text-muted" />
                <p className="text-sm text-nofx-text-muted">
                  {tr('selectOrCreate')}
                </p>
              </div>
            </div>
          )}
        </div>

        {/* Right Column - Structured Flow & AI Test */}
        <div className="w-[420px] flex-shrink-0 flex flex-col overflow-hidden">
          {/* Tabs */}
          <div className="flex-shrink-0 flex border-b border-nofx-gold/20">
            <button
              onClick={() => setActiveRightTab('structured')}
              className={`flex-1 flex items-center justify-center gap-2 px-3 py-2.5 text-sm font-medium transition-colors ${activeRightTab === 'structured' ? 'border-b-2 border-yellow-500 text-yellow-500' : 'opacity-60 hover:opacity-100 text-nofx-text-muted'
                }`}
            >
              <FileJson className="w-4 h-4" />
              {language === 'zh' ? '结构化' : 'Structured'}
            </button>
            <button
              onClick={() => setActiveRightTab('flow')}
              className={`flex-1 flex items-center justify-center gap-2 px-3 py-2.5 text-sm font-medium transition-colors ${activeRightTab === 'flow' ? 'border-b-2 border-purple-500 text-purple-500' : 'opacity-60 hover:opacity-100 text-nofx-text-muted'
                }`}
            >
              <Eye className="w-4 h-4" />
              {language === 'zh' ? '交易流' : 'Flow'}
            </button>
            <button
              onClick={() => setActiveRightTab('test')}
              className={`flex-1 flex items-center justify-center gap-2 px-3 py-2.5 text-sm font-medium transition-colors ${activeRightTab === 'test' ? 'border-b-2 border-green-500 text-green-500' : 'opacity-60 hover:opacity-100 text-nofx-text-muted'
                }`}
            >
              <Play className="w-4 h-4" />
              {tr('aiTestRun')}
            </button>
            <button
              onClick={() => setActiveRightTab('calibration')}
              className={`flex-1 flex items-center justify-center gap-2 px-3 py-2.5 text-sm font-medium transition-colors ${activeRightTab === 'calibration' ? 'border-b-2 border-blue-500 text-blue-400' : 'opacity-60 hover:opacity-100 text-nofx-text-muted'
                }`}
            >
              <BarChart3 className="w-4 h-4" />
              {language === 'zh' ? '校准' : 'Calib'}
            </button>
          </div>

          {/* Tab Content */}
          <div className="flex-1 overflow-y-auto">
            {activeRightTab === 'structured' ? (
              <div className="p-3 space-y-3">
                <div className="grid grid-cols-3 gap-2">
                  <div className="rounded-lg bg-nofx-bg border border-nofx-gold/20 p-3">
                    <div className="text-[10px] text-nofx-text-muted">{language === 'zh' ? '策略模式' : 'Mode'}</div>
                    <div className="text-sm font-semibold text-nofx-text">{editingConfig?.strategy_mode || 'rule'}</div>
                  </div>
                  <div className="rounded-lg bg-nofx-bg border border-nofx-gold/20 p-3">
                    <div className="text-[10px] text-nofx-text-muted">{language === 'zh' ? '规则数' : 'Rules'}</div>
                    <div className="text-sm font-semibold text-nofx-text">{editingConfig?.compiled_rules?.length || 0}</div>
                  </div>
                  <div className="rounded-lg bg-nofx-bg border border-nofx-gold/20 p-3">
                    <div className="text-[10px] text-nofx-text-muted">{language === 'zh' ? '评分' : 'Scoring'}</div>
                    <div className="text-sm font-semibold text-nofx-text">{editingConfig?.scoring_config?.enabled ? 'on' : 'off'}</div>
                  </div>
                </div>

                {compileDraftSavedAt && (
                  <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-[11px] text-yellow-100">
                    <div className="flex items-start gap-2">
                      <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-yellow-400" />
                      <div>
                        <div className="font-medium text-yellow-300">
                          {language === 'zh' ? '草稿未保存' : 'Draft not saved'}
                        </div>
                        <div className="mt-1 text-yellow-100/80">
                          {language === 'zh'
                            ? '下面的结构化策略会在本机临时保留，点击 Save 后才会成为交易员实际使用的策略。'
                            : 'The structured result below is kept locally for now. Click Save before any trader can use it.'}
                        </div>
                      </div>
                    </div>
                  </div>
                )}

                {!hasStructuredStrategy && (
                  <div className="rounded-lg border border-white/10 bg-nofx-bg p-3 text-[11px] text-nofx-text-muted">
                    <div className="flex items-start gap-2">
                      <FileJson className="mt-0.5 h-4 w-4 flex-shrink-0 text-nofx-gold" />
                      <div>
                        <div className="font-medium text-nofx-text">
                          {language === 'zh' ? '还没有可执行的结构化策略' : 'No executable structured strategy yet'}
                        </div>
                        <div className="mt-1">
                          {language === 'zh'
                            ? '当前只显示基础解析参数。请在可编辑策略中选择 AI 模型并生成策略；首次生成会自动保存，后续生成需要手动 Save。'
                            : 'Only base resolved parameters are shown. Select an AI model in an editable strategy and generate a strategy. The first generation is saved automatically; later generations require Save.'}
                        </div>
                      </div>
                    </div>
                  </div>
                )}

                {compileResult?.warnings && compileResult.warnings.length > 0 && (
                  <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3">
                    <div className="text-xs font-medium text-yellow-400 mb-1">{language === 'zh' ? '编译警告' : 'Compile Warnings'}</div>
                    <ul className="space-y-1 text-[11px] text-yellow-100">
                      {compileResult.warnings.map((warning, index) => (
                        <li key={index}>{warning}</li>
                      ))}
                    </ul>
                  </div>
                )}

                {compileResult?.errors && compileResult.errors.length > 0 && (
                  <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3">
                    <div className="text-xs font-medium text-red-400 mb-1">{language === 'zh' ? '编译错误' : 'Compile Errors'}</div>
                    <ul className="space-y-1 text-[11px] text-red-100">
                      {compileResult.errors.map((compileError, index) => (
                        <li key={index}>{compileError}</li>
                      ))}
                    </ul>
                  </div>
                )}

                {editingConfig?.compiled_rules && editingConfig.compiled_rules.length > 0 && (
                  <div className="space-y-2">
                    <div className="text-xs font-medium text-nofx-text">{language === 'zh' ? '确定性规则' : 'Deterministic Rules'}</div>
                    {editingConfig.compiled_rules.map((rule) => (
                      <div key={rule.id} className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                        <div className="flex items-center justify-between gap-2">
                          <div className="min-w-0">
                            <div className="text-sm font-medium text-nofx-text truncate">{rule.description || rule.id}</div>
                            <div className="text-[10px] text-nofx-text-muted">{rule.action} · {rule.timeframe || '-'}</div>
                          </div>
                          <span className={`text-[10px] px-2 py-1 rounded ${rule.enabled ? 'bg-green-500/15 text-green-400' : 'bg-white/10 text-nofx-text-muted'}`}>
                            {rule.enabled ? 'enabled' : 'off'}
                          </span>
                        </div>
                        <div className="mt-2 grid grid-cols-3 gap-2 text-[10px]">
                          <div className="text-nofx-text-muted">lev <span className="text-nofx-text">{rule.execution?.leverage || '-'}</span></div>
                          <div className="text-nofx-text-muted">size <span className="text-nofx-text">{rule.execution?.position_size_usd || '-'}</span></div>
                          <div className="text-nofx-text-muted">conf <span className="text-nofx-text">{rule.execution?.confidence || '-'}</span></div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                {editingConfig?.scoring_config?.enabled && (
                  <div className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <div className="text-xs font-medium text-nofx-text">{language === 'zh' ? '评分配置' : 'Scoring Config'}</div>
                        <div className="mt-1 text-[11px] text-nofx-text-muted">
                          {language === 'zh'
                            ? 'LLM 生成后的可选微调；程序按主周期识别机会、入场周期确认触发、确认周期过滤方向冲突。'
                            : 'Optional tuning after LLM generation. The program detects setups on the primary timeframe, confirms entry on the entry timeframe, and filters conflicts on confirmation timeframes.'}
                        </div>
                      </div>
                      <span className="rounded bg-blue-500/15 px-2 py-1 text-[10px] text-blue-300">
                        {editingConfig.scoring_config.timeframe || '-'}
                      </span>
                    </div>

                    <div className="mt-3 space-y-3">
                      {timeframeRoles && (
                        <div className="grid grid-cols-3 gap-2 rounded-lg border border-white/10 bg-black/20 p-2 text-[10px]">
                          <div>
                            <div className="text-nofx-text-muted">{language === 'zh' ? '主周期 / 找机会' : 'Primary / setup'}</div>
                            <div className="mt-1 font-mono text-nofx-text">{timeframeRoles.primary}</div>
                          </div>
                          <div>
                            <div className="text-nofx-text-muted">{language === 'zh' ? '入场周期 / 触发' : 'Entry / trigger'}</div>
                            <div className="mt-1 font-mono text-nofx-text">{timeframeRoles.entry}</div>
                          </div>
                          <div>
                            <div className="text-nofx-text-muted">{language === 'zh' ? '确认周期 / 过滤' : 'Confirm / filter'}</div>
                            <div className="mt-1 font-mono text-nofx-text">
                              {timeframeRoles.confirmations.length > 0 ? timeframeRoles.confirmations.join(', ') : '-'}
                            </div>
                          </div>
                        </div>
                      )}
                      {(editingConfig.scoring_config.selected_factors || Object.keys(editingConfig.scoring_config.factor_weights || {})).map((factor) => {
                        const weight = editingConfig.scoring_config?.factor_weights?.[factor] ?? 1
                        const factorDisplay = getScoringFactorDisplay(factor, language)
                        return (
                          <div key={factor} className="space-y-1">
                            <div className="flex items-center justify-between gap-2">
                              <div>
                                <div className="text-[11px] font-medium text-nofx-text">
                                  {factorDisplay.label}
                                  <span className="ml-1 font-mono text-[10px] text-nofx-text-muted">({factorDisplay.code})</span>
                                </div>
                                <div className="mt-0.5 text-[10px] text-nofx-text-muted">{factorDisplay.desc}</div>
                              </div>
                              <input
                                type="number"
                                min={0.01}
                                max={1}
                                step={0.01}
                                value={weight}
                                disabled={selectedStrategy?.is_default}
                                onChange={(e) => updateScoringWeight(factor, parseFloat(e.target.value))}
                                className="w-16 rounded border border-white/10 bg-black/20 px-2 py-1 text-right text-[11px] text-nofx-text disabled:opacity-50"
                              />
                            </div>
                            <input
                              type="range"
                              min={0.01}
                              max={1}
                              step={0.01}
                              value={weight}
                              disabled={selectedStrategy?.is_default}
                              onChange={(e) => updateScoringWeight(factor, parseFloat(e.target.value))}
                              className="w-full accent-yellow-500 disabled:opacity-50"
                            />
                          </div>
                        )
                      })}
                    </div>

                    <div className="mt-3 grid grid-cols-2 gap-2">
                      <label className="space-y-1">
                        <span className="text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('long_threshold', language).label}
                        </span>
                        <input
                          type="number"
                          min={1}
                          max={100}
                          step={1}
                          value={editingConfig.scoring_config.long_threshold ?? 60}
                          disabled={selectedStrategy?.is_default}
                          onChange={(e) => updateScoringConfig({ long_threshold: clampNumber(parseFloat(e.target.value), 1, 100) })}
                          className="w-full rounded border border-white/10 bg-black/20 px-2 py-1 text-[11px] text-nofx-text disabled:opacity-50"
                        />
                        <span className="block text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('long_threshold', language).desc}
                        </span>
                      </label>
                      <label className="space-y-1">
                        <span className="text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('short_threshold', language).label}
                        </span>
                        <input
                          type="number"
                          min={-100}
                          max={-1}
                          step={1}
                          value={editingConfig.scoring_config.short_threshold ?? -60}
                          disabled={selectedStrategy?.is_default}
                          onChange={(e) => updateScoringConfig({ short_threshold: clampNumber(parseFloat(e.target.value), -100, -1) })}
                          className="w-full rounded border border-white/10 bg-black/20 px-2 py-1 text-[11px] text-nofx-text disabled:opacity-50"
                        />
                        <span className="block text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('short_threshold', language).desc}
                        </span>
                      </label>
                      <label className="space-y-1">
                        <span className="text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('min_available_weight_ratio', language).label}
                        </span>
                        <input
                          type="number"
                          min={0.5}
                          max={1}
                          step={0.05}
                          value={editingConfig.scoring_config.min_available_weight_ratio ?? 0.5}
                          disabled={selectedStrategy?.is_default}
                          onChange={(e) => updateScoringConfig({ min_available_weight_ratio: clampNumber(parseFloat(e.target.value), 0.5, 1) })}
                          className="w-full rounded border border-white/10 bg-black/20 px-2 py-1 text-[11px] text-nofx-text disabled:opacity-50"
                        />
                        <span className="block text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('min_available_weight_ratio', language).desc}
                        </span>
                      </label>
                      <label className="space-y-1">
                        <span className="text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('min_confidence', language).label}
                        </span>
                        <input
                          type="number"
                          min={50}
                          max={90}
                          step={1}
                          value={editingConfig.scoring_config.min_confidence ?? 70}
                          disabled={selectedStrategy?.is_default}
                          onChange={(e) => updateScoringConfig({ min_confidence: Math.round(clampNumber(parseFloat(e.target.value), 50, 90)) })}
                          className="w-full rounded border border-white/10 bg-black/20 px-2 py-1 text-[11px] text-nofx-text disabled:opacity-50"
                        />
                        <span className="block text-[10px] text-nofx-text-muted">
                          {getScoringFieldLabel('min_confidence', language).desc}
                        </span>
                      </label>
                    </div>

                    <pre className="mt-3 p-2 rounded text-[10px] overflow-auto bg-black/30 text-nofx-text max-h-36">
                      {JSON.stringify(editingConfig.scoring_config.factor_weights || {}, null, 2)}
                    </pre>
                  </div>
                )}

                <div>
                  <div className="text-xs font-medium text-nofx-text mb-2">{language === 'zh' ? '生效参数 / 原始结构' : 'Resolved / Raw Structure'}</div>
                  <pre className="p-2 rounded-lg text-[10px] font-mono overflow-auto bg-nofx-bg border border-nofx-gold/20 text-nofx-text max-h-[360px]">
                    {JSON.stringify({
                      strategy_prompt: editingConfig?.strategy_prompt,
                      strategy_mode: editingConfig?.strategy_mode,
                      compiled_rules: editingConfig?.compiled_rules,
                      scoring_config: editingConfig?.scoring_config,
                      resolved_parameters: editingConfig?.resolved_parameters,
                    }, null, 2)}
                  </pre>
                </div>
              </div>
            ) : activeRightTab === 'flow' ? (
              /* Structured Flow Preview Tab */
              <div className="p-3 space-y-3">
                {/* Controls */}
                <div className="flex items-center gap-2 flex-wrap">
                  <button
                    onClick={fetchFlowPreview}
                    disabled={isLoadingFlowPreview || !editingConfig}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors disabled:opacity-50 bg-purple-600 hover:bg-purple-700 text-white"
                  >
                    {isLoadingFlowPreview ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
                    {flowPreview ? (language === 'zh' ? '刷新' : 'Refresh') : (language === 'zh' ? '生成预览' : 'Preview')}
                  </button>
                </div>

                {flowPreview ? (
                  <>
                    {dependencyCheck && dependencyCheck.missing.length > 0 && (
                      <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-xs text-red-200">
                        <div className="font-medium">
                          {language === 'zh' ? '规则依赖未满足' : 'Rule dependencies missing'}
                        </div>
                        <div className="mt-1 font-mono text-[11px]">
                          {dependencyCheck.missing.join(', ')}
                        </div>
                      </div>
                    )}
                    {dependencyCheck && dependencyCheck.missing.length === 0 && dependencyCheck.required.length > 0 && (
                      <div className="rounded-lg border border-green-500/30 bg-green-500/10 p-3 text-xs text-green-200">
                        {language === 'zh' ? '规则依赖已满足' : 'Rule dependencies satisfied'}
                      </div>
                    )}
                    <pre
                      className="p-2 rounded-lg text-[11px] font-mono overflow-auto bg-nofx-bg border border-nofx-gold/20 text-nofx-text"
                      style={{ maxHeight: '520px' }}
                    >
                      {JSON.stringify(flowPreview, null, 2)}
                    </pre>
                  </>
                ) : (
                  <div className="flex flex-col items-center justify-center py-12 text-nofx-text-muted">
                    <Eye className="w-10 h-10 mb-2 opacity-30" />
                    <p className="text-sm">{language === 'zh' ? '生成交易流预览' : 'Generate a trading flow preview'}</p>
                  </div>
                )}
              </div>
            ) : activeRightTab === 'calibration' ? (
              <div className="p-3 space-y-3">
                <div className="flex items-center justify-between gap-2">
                  <div>
                    <div className="text-xs font-medium text-nofx-text">
                      {language === 'zh' ? '校准样本报告' : 'Calibration Sample Report'}
                    </div>
                    <div className="text-[11px] text-nofx-text-muted">
                      {language === 'zh'
                        ? '只读统计；基于运行中记录的 setup/信号样本和已关联模拟盘平仓结果，不等同于完整历史回测，也不会修改策略。'
                        : 'Read-only statistics from recorded setup/signal samples and linked paper outcomes. This is not a full historical backtest and does not change the strategy.'}
                    </div>
                  </div>
	                  <button
	                    onClick={fetchCalibrationReport}
	                    disabled={isLoadingCalibration || !selectedStrategy}
                    className="flex items-center gap-1.5 rounded bg-blue-600 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
                  >
                    {isLoadingCalibration ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}
                    {calibrationReport ? (language === 'zh' ? '刷新' : 'Refresh') : (language === 'zh' ? '读取' : 'Load')}
                  </button>
                </div>

                {calibrationReport ? (
                  <div className="space-y-3">
                    <div className={`rounded-lg border p-3 text-xs ${calibrationReport.quality_gate === 'paper_ready'
                      ? 'border-green-500/30 bg-green-500/10 text-green-100'
                      : calibrationReport.quality_gate === 'blocked'
                        ? 'border-red-500/30 bg-red-500/10 text-red-100'
                        : 'border-yellow-500/30 bg-yellow-500/10 text-yellow-100'
                      }`}>
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-medium">
                          {language === 'zh' ? '质量闸门' : 'Quality Gate'}
                        </span>
                        <span className="rounded bg-black/20 px-2 py-1 font-mono text-[10px]">
                          {calibrationReport.quality_gate}
                        </span>
                      </div>
                      <div className="mt-2 leading-relaxed text-[11px] opacity-90">
                        {calibrationReport.recommendation}
                      </div>
                    </div>

	                    <div className="grid grid-cols-2 gap-2">
	                      {[
	                        [language === 'zh' ? '样本数' : 'Samples', calibrationReport.sample_count],
	                        [language === 'zh' ? '候选信号' : 'Signals', calibrationReport.signal_count],
	                        [language === 'zh' ? '已通过' : 'Approved', calibrationReport.approved_count],
	                        [language === 'zh' ? '未触发' : 'No signal', calibrationReport.no_signal_count],
                      ].map(([label, value]) => (
                        <div key={String(label)} className="rounded-lg border border-white/10 bg-black/20 p-3">
                          <div className="text-[10px] text-nofx-text-muted">{label}</div>
                          <div className="mt-1 font-mono text-lg font-semibold text-nofx-text">{String(value)}</div>
                        </div>
	                      ))}
	                    </div>

	                    <div className="rounded-lg border border-white/10 bg-black/20 p-3">
	                      <div className="mb-2 text-xs font-medium text-nofx-text">
	                        {language === 'zh' ? '模拟盘结果' : 'Paper Outcomes'}
	                      </div>
	                      <div className="grid grid-cols-2 gap-2 text-[11px]">
	                        {[
	                          [language === 'zh' ? '已平仓' : 'Closed', calibrationReport.closed_trade_count ?? 0],
	                          [language === 'zh' ? '胜率' : 'Win rate', `${(((calibrationReport.win_rate ?? 0) as number) * 100).toFixed(1)}%`],
	                          [language === 'zh' ? '总 PnL' : 'Total PnL', (calibrationReport.total_pnl ?? 0).toFixed(2)],
	                          [language === 'zh' ? '平均 PnL' : 'Avg PnL', (calibrationReport.average_pnl ?? 0).toFixed(2)],
	                        ].map(([label, value]) => (
	                          <div key={String(label)} className="rounded border border-white/10 bg-nofx-bg p-2">
	                            <div className="text-[10px] text-nofx-text-muted">{label}</div>
	                            <div className="mt-1 font-mono text-sm font-semibold text-nofx-text">{String(value)}</div>
	                          </div>
	                        ))}
	                      </div>
	                      <div className="mt-2 text-[10px] text-nofx-text-muted">
	                        {language === 'zh'
	                          ? `模拟盘结果阈值 ${calibrationReport.closed_trade_count ?? 0}/${calibrationReport.min_required_outcomes ?? 30}，只统计已关联 strategy_id 的平仓记录；不能替代历史回放回测。`
	                          : `Paper outcome threshold ${(calibrationReport.closed_trade_count ?? 0)}/${calibrationReport.min_required_outcomes ?? 30}; only linked closed positions are counted. This does not replace historical replay backtesting.`}
	                      </div>
	                    </div>

                    <div className="rounded-lg border border-white/10 bg-black/20 p-3">
                      <div className="mb-2 text-xs font-medium text-nofx-text">
                        {language === 'zh' ? '状态分布' : 'Status Distribution'}
                      </div>
                      <div className="space-y-1 text-[11px] text-nofx-text-muted">
                        {Object.entries(calibrationReport.risk_status_counts || {}).map(([key, value]) => (
                          <div key={key} className="flex justify-between gap-3">
                            <span className="font-mono">{key}</span>
                            <span className="text-nofx-text">{value}</span>
                          </div>
                        ))}
                      </div>
                    </div>

                    <div className="rounded-lg border border-white/10 bg-black/20 p-3">
                      <div className="mb-2 text-xs font-medium text-nofx-text">
                        {language === 'zh' ? 'Setup 分布' : 'Setup Distribution'}
                      </div>
                      <div className="space-y-2">
                        {(calibrationReport.setup_stats || []).slice(0, 8).map((stat) => (
                          <div key={stat.setup} className="rounded border border-white/10 bg-nofx-bg p-2">
                            <div className="flex items-center justify-between gap-2">
                              <span className="font-mono text-[11px] text-nofx-text">{stat.setup}</span>
                              <span className="text-[10px] text-nofx-text-muted">{stat.samples}</span>
                            </div>
	                            <div className="mt-1 grid grid-cols-3 gap-1 text-[10px] text-nofx-text-muted">
	                              <span>{language === 'zh' ? '候选' : 'eligible'} {stat.eligible}</span>
	                              <span>{language === 'zh' ? '通过' : 'approved'} {stat.approved}</span>
	                              <span>{language === 'zh' ? '拒绝' : 'rejected'} {stat.risk_rejected + stat.review_rejected}</span>
	                            </div>
	                            <div className="mt-1 grid grid-cols-3 gap-1 text-[10px] text-nofx-text-muted">
	                              <span>{language === 'zh' ? '交易' : 'trades'} {stat.closed_trades ?? 0}</span>
	                              <span>{language === 'zh' ? '胜率' : 'win'} {(((stat.win_rate ?? 0) as number) * 100).toFixed(1)}%</span>
	                              <span>PnL {(stat.total_pnl ?? 0).toFixed(2)}</span>
	                            </div>
	                          </div>
                        ))}
                        {(calibrationReport.setup_stats || []).length === 0 && (
                          <div className="py-6 text-center text-xs text-nofx-text-muted">
                            {language === 'zh' ? '暂无 setup 样本' : 'No setup samples yet'}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center py-12 text-nofx-text-muted">
                    <BarChart3 className="w-10 h-10 mb-2 opacity-30" />
                    <p className="text-sm">
                      {language === 'zh' ? '读取当前策略的校准样本报告' : 'Load calibration samples for this strategy'}
                    </p>
                  </div>
                )}
              </div>
            ) : (
              /* AI Test Tab */
              <div className="p-3 space-y-3">
                {/* Controls */}
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Bot className="w-4 h-4 text-green-500" />
                    <span className="text-xs font-medium text-nofx-text">{tr('selectModel')}</span>
                  </div>
                  {aiModels.length > 0 ? (
                    <select
                      value={selectedModelId}
                      onChange={(e) => setSelectedModelId(e.target.value)}
                      className="w-full px-3 py-2 rounded-lg text-sm bg-nofx-bg border border-nofx-gold/20 text-nofx-text"
                    >
                      {aiModels.map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.name} ({model.provider})
                        </option>
                      ))}
                    </select>
                  ) : (
                    <div className="px-3 py-2 rounded-lg text-sm bg-nofx-danger/10 text-nofx-danger">
                      {tr('noModel')}
                    </div>
                  )}

                  <div className="flex items-center gap-2">
                    <select
                      value={selectedVariant}
                      onChange={(e) => setSelectedVariant(e.target.value)}
                      className="px-2 py-1.5 rounded text-xs bg-nofx-bg border border-nofx-gold/20 text-nofx-text"
                    >
                      <option value="balanced">{tr('balanced')}</option>
                      <option value="aggressive">{tr('aggressive')}</option>
                      <option value="conservative">{tr('conservative')}</option>
                    </select>
                    <button
                      onClick={runDeterministicPreview}
                      disabled={isRunningAiTest || !editingConfig}
                      className="flex items-center justify-center gap-2 px-3 py-2 rounded-lg text-sm font-medium transition-all disabled:opacity-50 bg-nofx-bg border border-blue-500/30 text-blue-300"
                    >
                      {isRunningAiTest ? <Loader2 className="w-4 h-4 animate-spin" /> : <Activity className="w-4 h-4" />}
                      {language === 'zh' ? '预演' : 'Preview'}
                    </button>
                    <button
                      onClick={runAiTest}
                      disabled={isRunningAiTest || !editingConfig || !selectedModelId}
                      className="flex-1 flex items-center justify-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all disabled:opacity-50 text-white shadow-lg shadow-green-500/20 bg-gradient-to-br from-green-500 to-green-600"
                    >
                      {isRunningAiTest ? (
                        <>
                          <Loader2 className="w-4 h-4 animate-spin" />
                          {tr('running')}
                        </>
                      ) : (
                        <>
                          <Send className="w-4 h-4" />
                          {tr('runTest')}
                        </>
                      )}
                    </button>
                  </div>
                  <p className="text-[10px] text-nofx-text-muted">{tr('testNote')}</p>
                </div>

                {/* Test Results */}
                {aiTestResult ? (
                  <div className="space-y-3">
                    {aiTestResult.error ? (
                      <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-xs text-red-200">
                        {String(aiTestResult.error)}
                      </div>
                    ) : (
                      <>
                        <div className="grid grid-cols-3 gap-2">
                          <div className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                            <div className="text-[10px] text-nofx-text-muted">{language === 'zh' ? '候选币' : 'Candidates'}</div>
                            <div className="text-sm font-semibold text-nofx-text">{String(aiTestResult.candidate_count ?? 0)}</div>
                          </div>
                          <div className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                            <div className="text-[10px] text-nofx-text-muted">{language === 'zh' ? '快照' : 'Snapshots'}</div>
                            <div className="text-sm font-semibold text-nofx-text">{String(aiTestResult.factor_snapshot_count ?? 0)}</div>
                          </div>
                          <div className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                            <div className="text-[10px] text-nofx-text-muted">{language === 'zh' ? '信号' : 'Signals'}</div>
                            <div className="text-sm font-semibold text-nofx-text">{String(aiTestResult.signal_count ?? 0)}</div>
                          </div>
	                        </div>

	                        {(() => {
	                          const inputAudit = getResultObject<Record<string, unknown>>(aiTestResult, 'input_audit')
	                          const klines = inputAudit?.klines && typeof inputAudit.klines === 'object' ? inputAudit.klines as Record<string, unknown> : null
	                          if (!klines) return null
	                          return (
	                            <div className="rounded-lg bg-nofx-bg border border-nofx-gold/20 p-3 text-[10px] text-nofx-text-muted">
	                              <div className="mb-2 text-xs font-medium text-nofx-text">{language === 'zh' ? 'K 线输入审计' : 'K-line Input Audit'}</div>
	                              <div className="grid grid-cols-2 gap-2">
	                                <div>{language === 'zh' ? '数据源' : 'Source'} <span className="text-nofx-text">{String(klines.market_data_source || '-')}</span></div>
	                                <div>{language === 'zh' ? '周期' : 'Timeframes'} <span className="text-nofx-text">{Array.isArray(klines.timeframes) ? klines.timeframes.join(', ') : '-'}</span></div>
	                                <div>{language === 'zh' ? '计算 K 线' : 'Compute'} <span className="text-nofx-text">{String(klines.compute_lookback || '-')}</span></div>
	                                <div>{language === 'zh' ? '展示 K 线' : 'Display'} <span className="text-nofx-text">{String(klines.display_count || '-')}</span></div>
	                              </div>
	                            </div>
	                          )
		                        })()}

		                        {(() => {
		                          const marketContext = getResultObject<Record<string, unknown>>(aiTestResult, 'market_context')
		                          const metrics = marketContext?.metrics && typeof marketContext.metrics === 'object' ? marketContext.metrics as Record<string, unknown> : {}
		                          if (!marketContext) return null
		                          return (
		                            <div className="rounded-lg bg-nofx-bg border border-white/10 p-3 text-[10px] text-nofx-text-muted">
		                              <div className="mb-2 text-xs font-medium text-nofx-text">{language === 'zh' ? '市场方向原因' : 'Market Direction Reason'}</div>
		                              <div className="grid grid-cols-2 gap-2">
		                                <div>{language === 'zh' ? '方向' : 'Direction'} <span className="text-nofx-text">{marketDirectionLabel(marketContext.direction_bias, language)}</span></div>
		                                <div>{language === 'zh' ? '偏多比例' : 'Bullish breadth'} <span className="text-nofx-text">{formatRatio(metrics.bullish_breadth_ratio)}</span></div>
		                                <div>BTC <span className="text-nofx-text">{marketTrendLabel(marketContext.btc_trend, language)}</span></div>
		                                <div>ETH <span className="text-nofx-text">{marketTrendLabel(marketContext.eth_trend, language)}</span></div>
		                              </div>
		                              {String(marketContext.context_summary || '') && (
		                                <div className="mt-2 font-mono text-[10px] text-nofx-text-muted">{String(marketContext.context_summary)}</div>
		                              )}
		                            </div>
		                          )
		                        })()}

		                        {String(aiTestResult.signal_preview_error || '') && (
	                          <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-xs text-yellow-100">
	                            {String(aiTestResult.signal_preview_error)}
	                          </div>
	                        )}

	                        {getResultArray(aiTestResult, 'external_data_warnings').length > 0 && (
	                          <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-xs text-yellow-100 space-y-1">
	                            <div className="font-medium">{language === 'zh' ? '外部数据提示' : 'External Data'}</div>
	                            {getResultArray(aiTestResult, 'external_data_warnings').map((warning, index) => (
	                              <div key={index}>{String(warning)}</div>
	                            ))}
	                          </div>
	                        )}

	                        {getResultArray(aiTestResult, 'market_data_warnings').length > 0 && (
	                          <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/10 p-3 text-xs text-yellow-100 space-y-1">
	                            <div className="font-medium">{language === 'zh' ? '市场数据提示' : 'Market Data'}</div>
	                            {getResultArray(aiTestResult, 'market_data_warnings').map((warning, index) => (
	                              <div key={index}>{String(warning)}</div>
	                            ))}
	                          </div>
	                        )}

	                        {getResultArray(aiTestResult, 'signals').length > 0 && (
                          <div className="space-y-2">
                            <div className="text-xs font-medium text-nofx-text">{language === 'zh' ? '候选信号' : 'Candidate Signals'}</div>
                            {getResultArray(aiTestResult, 'signals').map((signal, index) => (
                              <div key={`${String(signal.id || index)}`} className="rounded-lg bg-nofx-bg border border-green-500/20 p-3">
                                <div className="flex items-center justify-between gap-2">
                                  <div className="text-sm font-semibold text-nofx-text">{String(signal.symbol || '-')}</div>
                                  <span className="rounded bg-green-500/15 px-2 py-1 text-[10px] text-green-300">{String(signal.action || '-')}</span>
                                </div>
                                <div className="mt-2 grid grid-cols-3 gap-2 text-[10px] text-nofx-text-muted">
                                  <div>entry <span className="text-nofx-text">{formatPreviewValue(signal.entry_price)}</span></div>
                                  <div>conf <span className="text-nofx-text">{String(signal.confidence ?? '-')}</span></div>
                                  <div>rule <span className="text-nofx-text">{String(signal.rule_id || '-')}</span></div>
                                </div>
                                <div className="mt-2 text-[11px] text-nofx-text-muted">{String(signal.trigger_reason || '')}</div>
                              </div>
                            ))}
                          </div>
                        )}

                        {getResultArray(aiTestResult, 'setup_evaluations').length > 0 && (
                          <div className="space-y-2">
                            <div className="text-xs font-medium text-nofx-text">{language === 'zh' ? '场景判定' : 'Setup Checks'}</div>
                            {getResultArray(aiTestResult, 'setup_evaluations').slice(0, 20).map((trace, index) => {
                              const timeframes = trace.timeframes as Record<string, unknown> | undefined
                              const confirmations = Array.isArray(timeframes?.confirmations) ? timeframes.confirmations.join(', ') : '-'
                              return (
                                <div key={`${String(trace.symbol || index)}-setup`} className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                                  <div className="flex items-center justify-between gap-2">
                                    <div className="text-xs font-medium text-nofx-text">
                                      {String(trace.symbol || '-')} · {String(trace.setup || (language === 'zh' ? '未触发' : 'no setup'))}
                                    </div>
                                    <span className={`rounded px-2 py-1 text-[10px] ${Boolean(trace.eligible) ? 'bg-green-500/15 text-green-300' : 'bg-yellow-500/15 text-yellow-300'}`}>
                                      {String(trace.action || '') || (language === 'zh' ? '无候选' : 'no candidate')}
                                    </span>
                                  </div>
                                  <div className="mt-2 grid grid-cols-3 gap-2 text-[10px] text-nofx-text-muted">
                                    <div>{language === 'zh' ? '入场' : 'entry'} <span className="text-nofx-text">{String(timeframes?.entry || '-')}</span></div>
                                    <div>{language === 'zh' ? '主周期' : 'primary'} <span className="text-nofx-text">{String(timeframes?.primary || '-')}</span></div>
                                    <div>{language === 'zh' ? '确认' : 'confirm'} <span className="text-nofx-text">{confirmations}</span></div>
                                  </div>
                                  {String(trace.reason || '') && (
                                    <div className="mt-2 text-[11px] text-nofx-text-muted">{String(trace.reason)}</div>
                                  )}
                                </div>
                              )
                            })}
                          </div>
                        )}

                        {getResultArray(aiTestResult, 'scoring_evaluations').length > 0 && (
                          <div className="space-y-2">
                            <div className="text-xs font-medium text-nofx-text">{language === 'zh' ? '评分判定' : 'Scoring Checks'}</div>
                            {getResultArray(aiTestResult, 'scoring_evaluations').slice(0, 20).map((trace, index) => (
                              <div key={`${String(trace.symbol || index)}-scoring`} className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                                <div className="flex items-center justify-between gap-2">
                                  <div className="text-xs font-medium text-nofx-text">{String(trace.symbol || '-')} · {formatPreviewValue(trace.score)}</div>
                                  <span className={`rounded px-2 py-1 text-[10px] ${String(trace.action || '') ? 'bg-green-500/15 text-green-300' : Boolean(trace.eligible) ? 'bg-blue-500/15 text-blue-300' : 'bg-yellow-500/15 text-yellow-300'}`}>
                                    {String(trace.action || '') || (Boolean(trace.eligible) ? (language === 'zh' ? '未达阈值' : 'below threshold') : (language === 'zh' ? '证据不足' : 'insufficient evidence'))}
                                  </span>
                                </div>
                                <div className="mt-2 grid grid-cols-3 gap-2 text-[10px] text-nofx-text-muted">
                                  <div>{language === 'zh' ? '证据权重' : 'evidence'} <span className="text-nofx-text">{formatRatio(trace.available_weight_ratio)}</span></div>
                                  <div>{language === 'zh' ? '最低要求' : 'required'} <span className="text-nofx-text">{formatRatio(trace.min_available_weight_ratio)}</span></div>
                                  <div>{language === 'zh' ? '因子' : 'factors'} <span className="text-nofx-text">{String(trace.available_factor_count ?? 0)}/{String(trace.required_factor_count ?? 0)}</span></div>
                                </div>
                                <div className="mt-2 text-[10px] text-nofx-text-muted">
                                  ok: {Array.isArray(trace.available_factors) ? trace.available_factors.join(', ') || '-' : '-'}
                                </div>
                                <div className="mt-1 text-[10px] text-nofx-text-muted">
                                  missing: {Array.isArray(trace.missing_factors) ? trace.missing_factors.join(', ') || '-' : '-'}
                                </div>
                                {String(trace.reason || '') && (
                                  <div className="mt-2 text-[11px] text-yellow-200">{String(trace.reason)}</div>
                                )}
                              </div>
                            ))}
                          </div>
                        )}

                        {getResultArray(aiTestResult, 'rule_evaluations').length > 0 && (
                          <div className="space-y-2">
                            <div className="text-xs font-medium text-nofx-text">{language === 'zh' ? '条件判定' : 'Condition Checks'}</div>
                            {getResultArray(aiTestResult, 'rule_evaluations').slice(0, 20).map((trace, index) => (
                              <div key={`${String(trace.rule_id || index)}-${String(trace.symbol || index)}`} className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                                <div className="flex items-center justify-between gap-2">
                                  <div className="text-xs font-medium text-nofx-text">{String(trace.symbol || '-')} · {String(trace.rule_id || '-')}</div>
                                  <span className={`rounded px-2 py-1 text-[10px] ${Boolean(trace.matched) ? 'bg-green-500/15 text-green-300' : Boolean(trace.missing) ? 'bg-yellow-500/15 text-yellow-300' : 'bg-white/10 text-nofx-text-muted'}`}>
                                    {Boolean(trace.matched) ? (language === 'zh' ? '触发' : 'matched') : Boolean(trace.missing) ? (language === 'zh' ? '缺数据' : 'missing') : (language === 'zh' ? '未触发' : 'no match')}
                                  </span>
                                </div>
                                <div className="mt-2 space-y-1">
                                  {getResultArray(trace, 'conditions').map((condition, conditionIndex) => (
                                    <div key={conditionIndex} className="rounded border border-white/5 bg-black/20 px-2 py-1 text-[10px] text-nofx-text-muted">
                                      <span className={Boolean(condition.passed) ? 'text-green-300' : 'text-nofx-text-muted'}>{Boolean(condition.passed) ? '✓' : '×'}</span>
                                      {' '}{String(condition.left)} {formatPreviewValue(condition.left_value)} {String(condition.operator)} {String(condition.right)} {formatPreviewValue(condition.right_value)}
                                    </div>
                                  ))}
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </>
                    )}
	                    <details className="rounded-lg border border-nofx-gold/20 bg-nofx-bg">
	                      <summary className="cursor-pointer px-3 py-2 text-xs text-nofx-text-muted hover:text-nofx-text">
	                        {language === 'zh' ? '原始 JSON' : 'Raw JSON'}
	                      </summary>
	                      <pre
	                        className="border-t border-white/10 p-2 text-[10px] font-mono overflow-auto whitespace-pre-wrap text-nofx-text"
	                        style={{ maxHeight: '360px' }}
	                      >
	                        {JSON.stringify(aiTestResult, null, 2)}
	                      </pre>
	                    </details>
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center py-12 text-nofx-text-muted">
                    <Play className="w-10 h-10 mb-2 opacity-30" />
                    <p className="text-sm">{tr('runAiTestHint')}</p>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </DeepVoidBackground>
  )
}

export default StrategyStudioPage
