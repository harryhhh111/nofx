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
} from 'lucide-react'
import type { Strategy, StrategyConfig, AIModel, StrategyCompileResponse, StrategyEvolutionProposal, AI500CoinsResponse, NofxOSStatus } from '../types'
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
  const [activeRightTab, setActiveRightTab] = useState<'structured' | 'flow' | 'test'>('structured')
  const [flowPreview, setFlowPreview] = useState<Record<string, unknown> | null>(null)
  const [isLoadingFlowPreview, setIsLoadingFlowPreview] = useState(false)
  const [selectedVariant, setSelectedVariant] = useState('balanced')
  const [compileResult, setCompileResult] = useState<StrategyCompileResponse | null>(null)
  const [isCompilingStrategy, setIsCompilingStrategy] = useState(false)
  const [evolutionProposal, setEvolutionProposal] = useState<StrategyEvolutionProposal | null>(null)
  const [isEvolvingStrategy, setIsEvolvingStrategy] = useState(false)
  const [ai500Preview, setAI500Preview] = useState<AI500CoinsResponse | null>(null)
  const [nofxOSStatus, setNofxOSStatus] = useState<NofxOSStatus | null>(null)
  const [isLoadingDataStatus, setIsLoadingDataStatus] = useState(false)

  // AI Test Run states
  const [aiTestResult, setAiTestResult] = useState<Record<string, unknown> | null>(null)
  const [isRunningAiTest, setIsRunningAiTest] = useState(false)

  const toggleSection = (section: keyof typeof expandedSections) => {
    setExpandedSections((prev) => ({
      ...prev,
      [section]: !prev[section],
    }))
  }

  // Fetch AI Models
  const fetchAiModels = useCallback(async () => {
    if (!token) return
    try {
      const allModels = await api.getModelConfigs()
      const enabledModels = allModels.filter((m: AIModel) => m.enabled && m.provider !== 'claw402')
      setAiModels(enabledModels)
      if (enabledModels.length > 0 && !selectedModelId) {
        setSelectedModelId(enabledModels[0].id)
      }
    } catch (err) {
      console.error('Failed to fetch AI models:', err)
    }
  }, [token, selectedModelId])

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

      // Select active or first strategy
      const active = data.strategies?.find((s: Strategy) => s.is_active)
      if (active) {
        setSelectedStrategy(active)
        setEditingConfig(active.config)
      } else if (data.strategies?.length > 0) {
        setSelectedStrategy(data.strategies[0])
        setEditingConfig(data.strategies[0].config)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setIsLoading(false)
    }
  }, [token])

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
        setEditingConfig(defaultConfig)
        setHasChanges(false)
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
          notify.error(`Strategy is in use by: ${names}`)
          return
        }
      }
    } catch {
      // fetch failed 闂?proceed, backend will guard
    }

    const confirmed = await confirmToast(
      tr('confirmDeleteStrategy'),
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
        notify.error(data.error || 'Failed to delete strategy')
        return
      }
      notify.success(tr('strategyDeleted'))
      if (selectedStrategy?.id === id) {
        setSelectedStrategy(null)
        setEditingConfig(null)
        setHasChanges(false)
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
    setEditingConfig({
      ...editingConfig,
      [section]: value,
    })
    setHasChanges(true)
  }

  const updateStrategyPrompt = (prompt: string) => {
    if (!editingConfig) return
    setEditingConfig({
      ...editingConfig,
      strategy_prompt: prompt,
    })
    setHasChanges(true)
  }

  const compileStrategyPrompt = async () => {
    if (!token || !editingConfig || !selectedModelId) return
    const prompt = (editingConfig.strategy_prompt || '').trim()
    if (!prompt) {
      notify.warning(language === 'zh' ? '请先填写策略 Prompt' : 'Enter a strategy prompt first')
      return
    }
    setIsCompilingStrategy(true)
    setCompileResult(null)
    try {
      const result = await api.compileStrategyPrompt({
        strategy_id: selectedStrategy?.id,
        strategy_version: buildStrategyVersion(),
        prompt,
        ai_model_id: selectedModelId,
        persist: false,
      })
      setCompileResult(result)
      setEditingConfig({
        ...editingConfig,
        strategy_prompt: result.strategy_prompt || prompt,
        strategy_mode: result.strategy_mode || 'rule',
        compiled_rules: result.compiled_rules || [],
        scoring_config: result.scoring_config,
        resolved_parameters: result.resolved_parameters,
      })
      setHasChanges(true)
      setActiveRightTab('structured')
      notify.success(language === 'zh' ? '策略已编译，保存后生效' : 'Strategy compiled. Save to apply.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
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
      setAiTestResult({
        error: err instanceof Error ? err.message : 'Unknown error',
      })
    } finally {
      setIsRunningAiTest(false)
    }
  }

  const loadDataSourceStatus = async () => {
    setIsLoadingDataStatus(true)
    try {
      const [ai500, nofxos] = await Promise.all([
        api.getAI500Coins(10, true).catch(() => ({ coins: [], count: 0 })),
        api.getNofxOSStatus(true).catch(() => ({ records: [], count: 0 })),
      ])
      setAI500Preview(ai500)
      setNofxOSStatus(nofxos)
    } finally {
      setIsLoadingDataStatus(false)
    }
  }

  const generateEvolutionProposal = async () => {
    if (!selectedStrategy || !selectedModelId) return
    setIsEvolvingStrategy(true)
    setEvolutionProposal(null)
    try {
      const proposal = await api.evolveStrategy(selectedStrategy.id, {
        ai_model_id: selectedModelId,
        trigger: 'manual',
        base_version: buildStrategyVersion(),
        notes: 'Manual Strategy Studio review',
      })
      setEvolutionProposal(proposal)
      setActiveRightTab('structured')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setIsEvolvingStrategy(false)
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
                ? '先写策略意图和指标范围，选择推理模型编译成结构化规则，保存后由程序计算指标并执行；右侧可查看交易流和真实 AI 测试结果。'
                : 'Describe the strategy intent and indicator scope, compile it into structured rules with the reasoning model, then save. Runtime indicators are calculated by the program; use the right panel to inspect flow and real AI test output.'}
            </div>
          </div>
          <textarea
            value={editingConfig.strategy_prompt || ''}
            onChange={(e) => updateStrategyPrompt(e.target.value)}
            disabled={selectedStrategy?.is_default}
            rows={7}
            className="w-full resize-none rounded-lg px-3 py-2 text-sm bg-nofx-bg border border-nofx-gold/20 text-nofx-text outline-none focus:border-purple-500 disabled:opacity-50"
            placeholder={language === 'zh'
              ? '写策略意图：使用哪些指标、希望捕捉什么行情、开平仓要求。可以只指定指标类型，参数由 AI 在编译阶段建议，程序保存后固定执行。'
              : 'Describe the strategy intent, indicators, market setup, and execution requirements. AI can suggest parameters during compile; runtime execution stays deterministic.'}
          />
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
              disabled={selectedStrategy?.is_default || isCompilingStrategy || !selectedModelId || !(editingConfig.strategy_prompt || '').trim()}
              className="flex items-center justify-center gap-2 px-3 py-2 rounded-lg text-xs font-medium bg-purple-600 hover:bg-purple-700 text-white disabled:opacity-50"
            >
              {isCompilingStrategy ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
              {language === 'zh' ? '编译为结构化策略' : 'Compile Structured Strategy'}
            </button>
          </div>
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
                  AI500 / NofxOS live data check
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
                  ? '开启：可使用亏损、胜率、近期平仓记录等经验。关闭：只根据当前市场和持仓判断。'
                  : 'On: AI can use losses, win rate, and recent closed trades. Off: AI decides from current positions and current market structure only.'}
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
              <h1 className="text-lg font-bold text-nofx-text">{tr('strategyStudio')}</h1>
              <p className="text-xs text-nofx-text-muted">{tr('subtitle')}</p>
            </div>
          </div>
          {error && (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs bg-nofx-danger/10 text-nofx-danger">
              {error}
              <button onClick={() => setError(null)} className="hover:underline">×</button>
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
                  onClick={() => {
                    setSelectedStrategy(strategy)
                    setEditingConfig(strategy.config)
                    setHasChanges(false)
                    setFlowPreview(null)
                    setAiTestResult(null)
                    setCompileResult(null)
                    setEvolutionProposal(null)
                  }}
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
                    <span className="text-xs text-nofx-gold">闂?{tr('unsaved')}</span>
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

                <div className="rounded-lg bg-nofx-bg border border-white/10 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-xs font-medium text-nofx-text">
                        {language === 'zh' ? '策略进化提案' : 'Strategy Evolution'}
                      </div>
                      <div className="text-[11px] text-nofx-text-muted">
                        Proposal only. It will not change live config.
                      </div>
                    </div>
                    <button
                      onClick={generateEvolutionProposal}
                      disabled={!selectedStrategy || !selectedModelId || isEvolvingStrategy}
                      className="flex items-center gap-1 rounded bg-nofx-gold px-2 py-1 text-[11px] font-medium text-black disabled:opacity-50"
                    >
                      {isEvolvingStrategy ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
                      {language === 'zh' ? '生成' : 'Generate'}
                    </button>
                  </div>
                  {evolutionProposal && (
                    <div className="mt-3 rounded border border-white/10 bg-black/20 p-2 text-[11px]">
                      <div className="font-medium text-nofx-text">{evolutionProposal.summary}</div>
                      {evolutionProposal.change_reasons && evolutionProposal.change_reasons.length > 0 && (
                        <ul className="mt-2 space-y-1 text-nofx-text-muted">
                          {evolutionProposal.change_reasons.slice(0, 3).map((reason, index) => (
                            <li key={index}>- {reason}</li>
                          ))}
                        </ul>
                      )}
                    </div>
                  )}
                </div>

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
                    <div className="text-xs font-medium text-nofx-text mb-2">{language === 'zh' ? '评分配置' : 'Scoring Config'}</div>
                    <div className="grid grid-cols-2 gap-2 text-[11px]">
                      <div className="text-nofx-text-muted">long <span className="text-nofx-text">{editingConfig.scoring_config.long_threshold ?? '-'}</span></div>
                      <div className="text-nofx-text-muted">short <span className="text-nofx-text">{editingConfig.scoring_config.short_threshold ?? '-'}</span></div>
                      <div className="text-nofx-text-muted">conf <span className="text-nofx-text">{editingConfig.scoring_config.min_confidence ?? '-'}</span></div>
                      <div className="text-nofx-text-muted">tf <span className="text-nofx-text">{editingConfig.scoring_config.timeframe || '-'}</span></div>
                    </div>
                    <pre className="mt-2 p-2 rounded text-[10px] overflow-auto bg-black/30 text-nofx-text max-h-48">
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
                  <pre
                    className="p-2 rounded-lg text-[11px] font-mono overflow-auto bg-nofx-bg border border-nofx-gold/20 text-nofx-text"
                    style={{ maxHeight: '520px' }}
                  >
                    {JSON.stringify(flowPreview, null, 2)}
                  </pre>
                ) : (
                  <div className="flex flex-col items-center justify-center py-12 text-nofx-text-muted">
                    <Eye className="w-10 h-10 mb-2 opacity-30" />
                    <p className="text-sm">{language === 'zh' ? '生成交易流预览' : 'Generate a trading flow preview'}</p>
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
                  <pre
                    className="p-2 rounded-lg text-[10px] font-mono overflow-auto whitespace-pre-wrap bg-nofx-bg border border-nofx-gold/20 text-nofx-text"
                    style={{ maxHeight: '520px' }}
                  >
                    {JSON.stringify(aiTestResult, null, 2)}
                  </pre>
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
