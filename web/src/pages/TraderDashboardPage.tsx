import { useEffect, useState, useRef, type ReactNode } from 'react'
import useSWR, { mutate } from 'swr'
import { api } from '../lib/api'
import { ChartTabs } from '../components/charts/ChartTabs'
import { DecisionCard } from '../components/trader/DecisionCard'
import { PositionHistory } from '../components/trader/PositionHistory'
import { PunkAvatar, getTraderAvatar } from '../components/common/PunkAvatar'
import { confirmToast, notify } from '../lib/notify'
import { formatPrice, formatQuantity } from '../utils/format'
import { t, type Language } from '../i18n/translations'
import {
  LogOut,
  Loader2,
  Eye,
  EyeOff,
  Copy,
  Check,
  Settings,
} from 'lucide-react'
import { DeepVoidBackground } from '../components/common/DeepVoidBackground'
import { NofxSelect } from '../components/ui/select'
import { GridRiskPanel } from '../components/strategy/GridRiskPanel'
import type {
  SystemStatus,
  AccountInfo,
  Position,
  DecisionRecord,
  Statistics,
  BBMACDAccuracyStats,
  BBMACDConfig,
  TraderInfo,
  Exchange,
} from '../types'

// --- Helper Functions ---

// Get friendly AI model display name
function getModelDisplayName(modelId: string): string {
  switch (modelId.toLowerCase()) {
    case 'deepseek':
      return 'DeepSeek'
    case 'qwen':
      return 'Qwen'
    case 'claude':
      return 'Claude'
    default:
      return modelId.toUpperCase()
  }
}

// Helper function to get exchange display name from exchange ID (UUID)
function getExchangeDisplayNameFromList(
  exchangeId: string | undefined,
  exchanges: Exchange[] | undefined
): string {
  if (!exchangeId) return 'Unknown'
  const exchange = exchanges?.find((e) => e.id === exchangeId)
  if (!exchange) return exchangeId.substring(0, 8).toUpperCase() + '...'
  const typeName = exchange.exchange_type?.toUpperCase() || exchange.name
  return exchange.account_name
    ? `${typeName} - ${exchange.account_name}`
    : typeName
}

// Helper function to get exchange type from exchange ID (UUID) - for kline charts
function getExchangeTypeFromList(
  exchangeId: string | undefined,
  exchanges: Exchange[] | undefined
): string {
  if (!exchangeId) return 'binance'
  const exchange = exchanges?.find((e) => e.id === exchangeId)
  if (!exchange) return 'binance' // Default to binance for charts
  return exchange.exchange_type?.toLowerCase() || 'binance'
}

// Helper function to check if exchange is a perp-dex type (wallet-based)
function isPerpDexExchange(exchangeType: string | undefined): boolean {
  if (!exchangeType) return false
  const perpDexTypes = ['hyperliquid', 'lighter', 'aster']
  return perpDexTypes.includes(exchangeType.toLowerCase())
}

// Helper function to get wallet address for perp-dex exchanges
function getWalletAddress(exchange: Exchange | undefined): string | undefined {
  if (!exchange) return undefined
  const type = exchange.exchange_type?.toLowerCase()
  switch (type) {
    case 'hyperliquid':
      return exchange.hyperliquidWalletAddr
    case 'lighter':
      return exchange.lighterWalletAddr
    case 'aster':
      return exchange.asterSigner
    default:
      return undefined
  }
}

// Helper function to truncate wallet address for display
function truncateAddress(address: string, startLen = 6, endLen = 4): string {
  if (address.length <= startLen + endLen + 3) return address
  return `${address.slice(0, startLen)}...${address.slice(-endLen)}`
}

function formatBBMACDAccuracy(stats?: BBMACDAccuracyStats): string {
  if ((stats?.breakout_effective?.resolved ?? 0) > 0) {
    return `${stats!.breakout_effective.accuracy.toFixed(1)}`
  }
  if ((stats?.breakout_overall?.resolved ?? 0) > 0) {
    return `${stats!.breakout_overall.accuracy.toFixed(1)}`
  }
  return '--'
}

function formatBBMACDSubtitle(stats?: BBMACDAccuracyStats): string {
  if (!stats) return 'ALL | -- samples'
  const rawAccuracy =
    stats.breakout_overall.resolved > 0
      ? `${stats.breakout_overall.accuracy.toFixed(1)}%`
      : '--'
  const threshold = stats.effective_threshold_pct?.toFixed(1) ?? '0.3'
  const label =
    stats.breakout_effective.resolved > 0 ? 'breakout eff' : 'breakout raw'
  return `${label} | raw ${rawAccuracy} | eff>${threshold}% ${stats.breakout_effective.resolved}/${stats.breakout_overall.resolved}`
}

const defaultBBMACDConfig: BBMACDConfig = {
  use_custom: false,
  fast: 8,
  slow: 21,
  signal: 5,
  boll_period: 20,
  boll_multiplier: 2,
}

// --- Components ---

interface TraderDashboardPageProps {
  selectedTrader?: TraderInfo
  traders?: TraderInfo[]
  tradersError?: Error
  selectedTraderId?: string
  onTraderSelect: (traderId: string) => void
  onNavigateToTraders: () => void
  status?: SystemStatus
  account?: AccountInfo
  accountFailed?: boolean
  positions?: Position[]
  positionsFailed?: boolean
  decisions?: DecisionRecord[]
  decisionsFailed?: boolean
  decisionsLimit: number
  onDecisionsLimitChange: (limit: number) => void
  stats?: Statistics
  lastUpdate: string
  language: Language
  exchanges?: Exchange[]
}

export function TraderDashboardPage({
  selectedTrader,
  status,
  account,
  accountFailed,
  positions,
  positionsFailed,
  decisions,
  decisionsFailed,
  decisionsLimit,
  onDecisionsLimitChange,
  lastUpdate,
  language,
  traders,
  tradersError,
  selectedTraderId,
  onTraderSelect,
  onNavigateToTraders,
  exchanges,
}: TraderDashboardPageProps) {
  const [closingPosition, setClosingPosition] = useState<string | null>(null)
  const [selectedChartSymbol, setSelectedChartSymbol] = useState<
    string | undefined
  >(undefined)
  const [chartUpdateKey, setChartUpdateKey] = useState<number>(0)
  const chartSectionRef = useRef<HTMLDivElement>(null)
  const [showWalletAddress, setShowWalletAddress] = useState<boolean>(false)
  const [copiedAddress, setCopiedAddress] = useState<boolean>(false)
  const [showBBMACDSettings, setShowBBMACDSettings] = useState(false)
  const [bbmacdDraft, setBBMACDDraft] =
    useState<BBMACDConfig>(defaultBBMACDConfig)
  const [savingBBMACD, setSavingBBMACD] = useState(false)

  // Current positions pagination
  const [positionsPageSize, setPositionsPageSize] = useState<number>(20)
  const [positionsCurrentPage, setPositionsCurrentPage] = useState<number>(1)
  const { data: bbmacdStats, error: bbmacdStatsError } = useSWR(
    selectedTraderId ? `bbmacd-stats-${selectedTraderId}` : null,
    () => api.getBBMACDStats(selectedTraderId, 0, true),
    { refreshInterval: 60000 }
  )
  const { data: bbmacdConfig } = useSWR(
    'bbmacd-config',
    () => api.getBBMACDConfig(true),
    { refreshInterval: 60000 }
  )

  useEffect(() => {
    if (bbmacdConfig) {
      setBBMACDDraft(bbmacdConfig)
    }
  }, [bbmacdConfig])

  const saveBBMACDConfig = async () => {
    if (!selectedTraderId) return
    setSavingBBMACD(true)
    try {
      await api.updateBBMACDConfig(selectedTraderId, bbmacdDraft, true)
      await mutate(`bbmacd-stats-${selectedTraderId}`)
      await mutate('bbmacd-config')
      setShowBBMACDSettings(false)
      notify.success('BB MACD 参数已保存，历史统计已清空')
    } catch (error) {
      notify.error(
        error instanceof Error ? error.message : 'BB MACD 参数保存失败'
      )
    } finally {
      setSavingBBMACD(false)
    }
  }

  // Calculate paginated positions
  const totalPositions = positions?.length || 0
  const totalPositionPages = Math.ceil(totalPositions / positionsPageSize)
  const paginatedPositions =
    positions?.slice(
      (positionsCurrentPage - 1) * positionsPageSize,
      positionsCurrentPage * positionsPageSize
    ) || []

  // Reset page when positions change
  useEffect(() => {
    setPositionsCurrentPage(1)
  }, [selectedTraderId, positionsPageSize])

  // Auto-set chart symbol for grid trading
  useEffect(() => {
    if (status?.strategy_type === 'grid_trading' && status?.grid_symbol) {
      setSelectedChartSymbol(status.grid_symbol)
    }
  }, [status?.strategy_type, status?.grid_symbol])

  // Get current exchange info for perp-dex wallet display
  const currentExchange = exchanges?.find(
    (e) => e.id === selectedTrader?.exchange_id
  )
  const walletAddress = getWalletAddress(currentExchange)
  const isPerpDex = isPerpDexExchange(currentExchange?.exchange_type)

  // Copy wallet address to clipboard
  const handleCopyAddress = async () => {
    if (!walletAddress) return
    try {
      await navigator.clipboard.writeText(walletAddress)
      setCopiedAddress(true)
      setTimeout(() => setCopiedAddress(false), 2000)
    } catch (err) {
      console.error('Failed to copy address:', err)
    }
  }

  // Handle symbol click from Decision Card
  const handleSymbolClick = (symbol: string) => {
    // Set the selected symbol
    setSelectedChartSymbol(symbol)
    // Scroll to chart section
    setTimeout(() => {
      chartSectionRef.current?.scrollIntoView({
        behavior: 'smooth',
        block: 'start',
      })
    }, 100)
  }

  // Close position handler
  const handleClosePosition = async (symbol: string, side: string) => {
    if (!selectedTraderId) return

    const sideLabel = side === 'LONG' ? 'LONG' : 'SHORT'
    const confirmMsg = t('traderDashboard.confirmClosePosition', language, {
      symbol,
      side: sideLabel,
    })

    const confirmed = await confirmToast(confirmMsg, {
      title: t('traderDashboard.confirmClose', language),
      okText: t('traderDashboard.confirm', language),
      cancelText: t('traderDashboard.cancel', language),
    })

    if (!confirmed) return

    setClosingPosition(symbol)
    try {
      await api.closePosition(selectedTraderId, symbol, side)
      notify.success(t('traderDashboard.positionClosed', language))
      // Use SWR mutate to refresh data instead of reloading page
      await Promise.all([
        mutate(`positions-${selectedTraderId}`),
        mutate(`account-${selectedTraderId}`),
      ])
    } catch (err: unknown) {
      const errorMsg =
        err instanceof Error
          ? err.message
          : t('traderDashboard.closeFailed', language)
      notify.error(errorMsg)
    } finally {
      setClosingPosition(null)
    }
  }

  // If API failed with error, show empty state (likely backend not running)
  if (tradersError) {
    return (
      <div className="flex items-center justify-center min-h-[60vh] relative z-10">
        <div className="text-center max-w-md mx-auto px-6">
          <div
            className="w-24 h-24 mx-auto mb-6 rounded-full flex items-center justify-center nofx-glass"
            style={{
              background: 'rgba(240, 185, 11, 0.1)',
              borderColor: 'rgba(240, 185, 11, 0.3)',
            }}
          >
            <svg
              className="w-12 h-12 text-nofx-gold"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
              />
            </svg>
          </div>
          <h2 className="text-2xl font-bold mb-3 text-nofx-text-main">
            {t('traderDashboard.connectionFailed', language)}
          </h2>
          <p className="text-base mb-6 text-nofx-text-muted">
            {t('traderDashboard.connectionFailedDesc', language)}
          </p>
          <button
            onClick={() => window.location.reload()}
            className="px-6 py-3 rounded-lg font-semibold transition-all hover:scale-105 active:scale-95 nofx-glass border border-nofx-gold/30 text-nofx-gold hover:bg-nofx-gold/10"
          >
            {t('traderDashboard.retry', language)}
          </button>
        </div>
      </div>
    )
  }

  // If traders is loaded and empty, show empty state
  if (traders && traders.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[60vh] relative z-10">
        <div className="text-center max-w-md mx-auto px-6">
          <div
            className="w-24 h-24 mx-auto mb-6 rounded-full flex items-center justify-center nofx-glass"
            style={{
              background: 'rgba(240, 185, 11, 0.1)',
              borderColor: 'rgba(240, 185, 11, 0.3)',
            }}
          >
            <svg
              className="w-12 h-12 text-nofx-gold"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
              />
            </svg>
          </div>
          <h2 className="text-2xl font-bold mb-3 text-nofx-text-main">
            {t('dashboardEmptyTitle', language)}
          </h2>
          <p className="text-base mb-6 text-nofx-text-muted">
            {t('dashboardEmptyDescription', language)}
          </p>
          <button
            onClick={onNavigateToTraders}
            className="px-6 py-3 rounded-lg font-semibold transition-all hover:scale-105 active:scale-95 nofx-glass border border-nofx-gold/30 text-nofx-gold hover:bg-nofx-gold/10"
          >
            {t('goToTradersPage', language)}
          </button>
        </div>
      </div>
    )
  }

  // If traders is still loading or selectedTrader is not ready, show skeleton
  if (!selectedTrader) {
    return (
      <div className="space-y-6 relative z-10">
        <div className="nofx-glass p-6 animate-pulse">
          <div className="h-8 w-48 mb-3 bg-nofx-bg/50 rounded"></div>
          <div className="flex gap-4">
            <div className="h-4 w-32 bg-nofx-bg/50 rounded"></div>
            <div className="h-4 w-24 bg-nofx-bg/50 rounded"></div>
            <div className="h-4 w-28 bg-nofx-bg/50 rounded"></div>
          </div>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="nofx-glass p-5 animate-pulse">
              <div className="h-4 w-24 mb-3 bg-nofx-bg/50 rounded"></div>
              <div className="h-8 w-32 bg-nofx-bg/50 rounded"></div>
            </div>
          ))}
        </div>
        <div className="nofx-glass p-6 animate-pulse">
          <div className="h-6 w-40 mb-4 bg-nofx-bg/50 rounded"></div>
          <div className="h-64 w-full bg-nofx-bg/50 rounded"></div>
        </div>
      </div>
    )
  }

  return (
    <DeepVoidBackground className="min-h-screen pb-12" disableAnimation>
      <div className="w-full px-4 md:px-8 relative z-10 pt-6">
        {/* Trader Header */}
        <div
          className="mb-6 rounded-lg p-6 animate-scale-in nofx-glass group"
          style={{
            background:
              'linear-gradient(135deg, rgba(15, 23, 42, 0.6) 0%, rgba(15, 23, 42, 0.4) 100%)',
          }}
        >
          <div className="flex items-start justify-between mb-4">
            <h2 className="text-2xl font-bold flex items-center gap-4 text-nofx-text-main">
              <div className="relative">
                <PunkAvatar
                  seed={getTraderAvatar(
                    selectedTrader.trader_id,
                    selectedTrader.trader_name
                  )}
                  size={56}
                  className="rounded-xl border-2 border-nofx-gold/30 shadow-[0_0_15px_rgba(240,185,11,0.2)]"
                />
                <div className="absolute -bottom-1 -right-1 w-4 h-4 bg-nofx-green rounded-full border-2 border-[#0B0E11] shadow-[0_0_8px_rgba(14,203,129,0.8)] animate-pulse" />
              </div>
              <div className="flex flex-col">
                <span className="text-3xl tracking-tight text-nofx-text font-semibold">
                  {selectedTrader.trader_name}
                </span>
                <span className="text-xs font-mono text-nofx-text-muted opacity-60 flex items-center gap-2">
                  <div className="w-1.5 h-1.5 bg-nofx-gold rounded-full" />
                  ID: {selectedTrader.trader_id.slice(0, 8)}...
                </span>
              </div>
            </h2>

            <div className="flex items-center gap-4">
              {/* Trader Selector */}
              {traders && traders.length > 0 && (
                <div className="flex items-center gap-2 nofx-glass px-1 py-1 rounded-lg border border-white/5">
                  <NofxSelect
                    value={selectedTraderId || ''}
                    onChange={(val) => onTraderSelect(val)}
                    options={traders.map((t) => ({
                      value: t.trader_id,
                      label: t.trader_name,
                    }))}
                    className="bg-transparent text-sm font-medium cursor-pointer transition-colors text-nofx-text-main px-2 py-1"
                  />
                </div>
              )}

              {/* Wallet Address Display for Perp-DEX */}
              {exchanges && isPerpDex && (
                <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg nofx-glass border border-nofx-gold/20">
                  {walletAddress ? (
                    <>
                      <span className="text-xs font-mono text-nofx-gold">
                        {showWalletAddress
                          ? walletAddress
                          : truncateAddress(walletAddress)}
                      </span>
                      <button
                        type="button"
                        onClick={() => setShowWalletAddress(!showWalletAddress)}
                        className="p-1 rounded hover:bg-white/10 transition-colors"
                        title={
                          showWalletAddress
                            ? t('traderDashboard.hideAddress', language)
                            : t('traderDashboard.showFullAddress', language)
                        }
                      >
                        {showWalletAddress ? (
                          <EyeOff className="w-3.5 h-3.5 text-nofx-text-muted" />
                        ) : (
                          <Eye className="w-3.5 h-3.5 text-nofx-text-muted" />
                        )}
                      </button>
                      <button
                        type="button"
                        onClick={handleCopyAddress}
                        className="p-1 rounded hover:bg-white/10 transition-colors"
                        title={t('traderDashboard.copyAddress', language)}
                      >
                        {copiedAddress ? (
                          <Check className="w-3.5 h-3.5 text-nofx-green" />
                        ) : (
                          <Copy className="w-3.5 h-3.5 text-nofx-text-muted" />
                        )}
                      </button>
                    </>
                  ) : (
                    <span className="text-xs text-nofx-text-muted">
                      {t('traderDashboard.noAddressConfigured', language)}
                    </span>
                  )}
                </div>
              )}
            </div>
          </div>
          <div className="flex items-center gap-6 text-sm flex-wrap text-nofx-text-muted font-mono pl-2">
            <span className="flex items-center gap-2">
              <span className="opacity-60">AI Model:</span>
              <span
                className="font-bold px-2 py-0.5 rounded text-xs tracking-wide"
                style={{
                  background: selectedTrader.ai_model.includes('qwen')
                    ? 'rgba(192, 132, 252, 0.15)'
                    : 'rgba(96, 165, 250, 0.15)',
                  color: selectedTrader.ai_model.includes('qwen')
                    ? '#c084fc'
                    : '#60a5fa',
                  border: `1px solid ${selectedTrader.ai_model.includes('qwen') ? '#c084fc' : '#60a5fa'}40`,
                }}
              >
                {getModelDisplayName(
                  selectedTrader.ai_model.split('_').pop() ||
                    selectedTrader.ai_model
                )}
              </span>
            </span>
            <span className="w-px h-3 bg-white/10 hidden md:block" />
            <span className="flex items-center gap-2">
              <span className="opacity-60">Exchange:</span>
              <span className="text-nofx-text-main font-semibold">
                {getExchangeDisplayNameFromList(
                  selectedTrader.exchange_id,
                  exchanges
                )}
              </span>
            </span>
            <span className="w-px h-3 bg-white/10 hidden md:block" />
            <span className="flex items-center gap-2">
              <span className="opacity-60">Strategy:</span>
              <span className="text-nofx-gold font-semibold tracking-wide">
                {selectedTrader.strategy_name || 'No Strategy'}
              </span>
            </span>
            {status && (
              <div className="hidden md:contents">
                <span className="w-px h-3 bg-white/10" />
                <span>
                  Cycles:{' '}
                  <span className="text-nofx-text-main">
                    {status.call_count}
                  </span>
                </span>
                <span className="w-px h-3 bg-white/10" />
                <span>
                  Runtime:{' '}
                  <span className="text-nofx-text-main">
                    {status.runtime_minutes} min
                  </span>
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Debug Info */}
        <div className="mb-4 px-3 py-1.5 rounded bg-black/40 border border-white/5 text-[10px] font-mono text-nofx-text-muted flex justify-between items-center opacity-60 hover:opacity-100 transition-opacity">
          <span style={{ color: '#0ECB81' }}>SYSTEM_STATUS::ONLINE</span>
          {account ? (
            <div className="flex gap-4">
              <span>LAST_UPDATE::{lastUpdate}</span>
              <span>EQ::{account.total_equity?.toFixed(2)}</span>
              <span>PNL::{account.total_pnl?.toFixed(2)}</span>
            </div>
          ) : accountFailed ? (
            <span style={{ color: '#F6465D' }}>
              {t('traderDashboard.accountFetchFailed', language)}
            </span>
          ) : (
            <div className="flex gap-4">
              <span className="inline-block w-32 h-3 rounded bg-white/5 animate-pulse" />
              <span className="inline-block w-16 h-3 rounded bg-white/5 animate-pulse" />
              <span className="inline-block w-16 h-3 rounded bg-white/5 animate-pulse" />
            </div>
          )}
        </div>

        {/* Account Overview */}
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4 mb-8">
          <StatCard
            title={t('totalEquity', language)}
            value={
              accountFailed && !account
                ? '--'
                : `${account?.total_equity?.toFixed(2) ?? '--'}`
            }
            unit="USDT"
            change={account ? account.total_pnl_pct || 0 : undefined}
            positive={(account?.total_pnl ?? 0) > 0}
            icon="💰"
            loading={!account && !accountFailed}
          />
          <StatCard
            title={t('availableBalance', language)}
            value={
              accountFailed && !account
                ? '--'
                : `${account?.available_balance?.toFixed(2) ?? '--'}`
            }
            unit="USDT"
            subtitle={
              accountFailed && !account
                ? '--'
                : `${account?.available_balance && account?.total_equity ? ((account.available_balance / account.total_equity) * 100).toFixed(1) : '--'}% ${t('free', language)}`
            }
            icon="💳"
            loading={!account && !accountFailed}
          />
          <StatCard
            title={t('totalPnL', language)}
            value={
              accountFailed && !account
                ? '--'
                : `${account?.total_pnl !== undefined && account.total_pnl >= 0 ? '+' : ''}${account?.total_pnl?.toFixed(2) ?? '--'}`
            }
            unit="USDT"
            change={account ? account.total_pnl_pct || 0 : undefined}
            positive={(account?.total_pnl ?? 0) >= 0}
            icon="📈"
            loading={!account && !accountFailed}
            tooltip={t('accountPnLYieldTooltip', language)}
          />
          <StatCard
            title={t('positions', language)}
            value={
              accountFailed && !account
                ? '--'
                : `${account?.position_count ?? '--'}`
            }
            unit="ACTIVE"
            subtitle={
              accountFailed && !account
                ? `${t('margin', language)}: --`
                : `${t('margin', language)}: ${account?.margin_used_pct?.toFixed(1) ?? '--'}%`
            }
            icon="📊"
            loading={!account && !accountFailed}
          />
          <StatCard
            title="BB MACD"
            value={bbmacdStatsError ? '--' : formatBBMACDAccuracy(bbmacdStats)}
            unit={
              bbmacdStats?.breakout_effective?.resolved ||
              bbmacdStats?.breakout_overall?.resolved
                ? '%'
                : undefined
            }
            subtitle={
              bbmacdStatsError
                ? 'ALL | -- samples'
                : formatBBMACDSubtitle(bbmacdStats)
            }
            icon="BB"
            loading={!bbmacdStats && !bbmacdStatsError}
            tooltip="主数字只看 BB MACD 突破上轨/下轨信号，并且每个信号只选一个验证窗口，避免 3/5/10 重复计数。eff>0.3% 只统计后续涨跌超过 0.3% 的有效样本；没有有效样本时回退显示突破原始准确率。"
            actionIcon={<Settings className="w-3.5 h-3.5" />}
            onAction={() => setShowBBMACDSettings(true)}
          />
        </div>

        {showBBMACDSettings && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4">
            <div className="w-full max-w-md rounded-lg border border-white/10 bg-[#101418] p-5 shadow-2xl">
              <div className="mb-4 flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-semibold text-nofx-text-main">
                    BB MACD 参数
                  </h3>
                  <p className="mt-1 text-xs text-nofx-text-muted">
                    保存后会清空当前统计，后续重新累计。
                  </p>
                </div>
                <button
                  className="rounded border border-white/10 px-2 py-1 text-xs text-nofx-text-muted hover:text-white"
                  onClick={() => setShowBBMACDSettings(false)}
                >
                  关闭
                </button>
              </div>

              <label className="mb-4 flex items-center gap-2 text-xs text-nofx-text-main">
                <input
                  type="checkbox"
                  checked={bbmacdDraft.use_custom}
                  onChange={(e) =>
                    setBBMACDDraft({
                      ...bbmacdDraft,
                      use_custom: e.target.checked,
                    })
                  }
                />
                使用自定义参数；关闭时使用动态参数
              </label>

              <div className="grid grid-cols-2 gap-3">
                {(
                  [
                    ['fast', 'Fast'],
                    ['slow', 'Slow'],
                    ['signal', 'Signal'],
                    ['boll_period', 'BOLL Period'],
                    ['boll_multiplier', 'BOLL Multiplier'],
                  ] as Array<[keyof BBMACDConfig, string]>
                ).map(([key, label]) => (
                  <label key={key} className="text-xs text-nofx-text-muted">
                    {label}
                    <input
                      type="number"
                      step={key === 'boll_multiplier' ? '0.1' : '1'}
                      value={bbmacdDraft[key] as number}
                      onChange={(e) => {
                        const raw =
                          key === 'boll_multiplier'
                            ? parseFloat(e.target.value)
                            : parseInt(e.target.value)
                        setBBMACDDraft({
                          ...bbmacdDraft,
                          [key]: Number.isFinite(raw) ? raw : 0,
                        })
                      }}
                      className="mt-1 w-full rounded border border-white/10 bg-black/30 px-3 py-2 text-sm text-nofx-text-main outline-none focus:border-nofx-gold/60"
                    />
                  </label>
                ))}
              </div>

              <div className="mt-4 rounded border border-white/10 bg-black/20 p-3 text-xs leading-relaxed text-nofx-text-muted">
                上行趋势可先试 8-21-5；更慢的趋势过滤可试 5-35-5。参数只影响 BB
                MACD 统计，不注入 AI 决策。
              </div>

              <div className="mt-5 flex justify-end gap-2">
                <button
                  className="rounded border border-white/10 px-4 py-2 text-xs text-nofx-text-muted hover:text-white"
                  onClick={() => setShowBBMACDSettings(false)}
                >
                  取消
                </button>
                <button
                  disabled={savingBBMACD}
                  className="rounded bg-nofx-gold px-4 py-2 text-xs font-semibold text-black disabled:opacity-50"
                  onClick={saveBBMACDConfig}
                >
                  {savingBBMACD ? '保存中...' : '保存并清空统计'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Grid Risk Panel - Only show for grid trading strategy */}
        {status?.strategy_type === 'grid_trading' && selectedTraderId && (
          <div
            className="mb-8 animate-slide-in"
            style={{ animationDelay: '0.05s' }}
          >
            <GridRiskPanel
              traderId={selectedTraderId}
              language={language}
              refreshInterval={5000}
            />
          </div>
        )}

        {/* Main Content Area */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
          {/* Left Column: Charts + Positions */}
          <div className="space-y-6">
            {/* Chart Tabs (Equity / K-line) */}
            <div
              ref={chartSectionRef}
              className="chart-container animate-slide-in scroll-mt-32 backdrop-blur-sm"
              style={{ animationDelay: '0.1s' }}
            >
              <ChartTabs
                traderId={selectedTrader.trader_id}
                selectedSymbol={selectedChartSymbol}
                updateKey={chartUpdateKey}
                exchangeId={getExchangeTypeFromList(
                  selectedTrader.exchange_id,
                  exchanges
                )}
              />
            </div>

            {/* Current Positions */}
            <div
              className="nofx-glass p-6 animate-slide-in relative overflow-hidden group"
              style={{ animationDelay: '0.15s' }}
            >
              <div className="absolute top-0 right-0 p-3 opacity-10 group-hover:opacity-20 transition-opacity">
                <div className="w-24 h-24 rounded-full bg-blue-500 blur-3xl" />
              </div>
              <div className="flex items-center justify-between mb-5 relative z-10">
                <h2 className="text-lg font-bold flex items-center gap-2 text-nofx-text-main uppercase tracking-wide">
                  <span className="text-blue-500">◈</span>{' '}
                  {t('currentPositions', language)}
                </h2>
                {positions && positions.length > 0 && (
                  <div className="text-xs px-2 py-1 rounded bg-nofx-gold/10 text-nofx-gold border border-nofx-gold/20 font-mono shadow-[0_0_10px_rgba(240,185,11,0.1)]">
                    {positions.length} {t('active', language)}
                  </div>
                )}
              </div>
              {positions && positions.length > 0 ? (
                <div>
                  <div className="overflow-x-auto">
                    <table className="w-full text-xs">
                      <thead className="text-left border-b border-white/5">
                        <tr>
                          <th className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-left">
                            {t('symbol', language)}
                          </th>
                          <th className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-center">
                            {t('side', language)}
                          </th>
                          <th className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-center">
                            {t('traderDashboard.action', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-right hidden md:table-cell"
                            title={t('entryPrice', language)}
                          >
                            {t('traderDashboard.entry', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-right hidden md:table-cell"
                            title={t('markPrice', language)}
                          >
                            {t('traderDashboard.mark', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-right"
                            title={t('quantity', language)}
                          >
                            {t('traderDashboard.qty', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-right hidden md:table-cell"
                            title={t('positionValue', language)}
                          >
                            {t('traderDashboard.value', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-center hidden md:table-cell"
                            title={t('leverage', language)}
                          >
                            {t('traderDashboard.lev', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-right"
                            title={t('unrealizedPnL', language)}
                          >
                            {t('traderDashboard.uPnL', language)}
                          </th>
                          <th
                            className="px-1 pb-3 font-semibold text-nofx-text-muted whitespace-nowrap text-right hidden md:table-cell"
                            title={t('liqPrice', language)}
                          >
                            {t('traderDashboard.liq', language)}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {paginatedPositions.map((pos, i) => (
                          <tr
                            key={i}
                            className="border-b border-white/5 last:border-0 transition-all hover:bg-white/5 cursor-pointer group/row"
                            onClick={() => {
                              setSelectedChartSymbol(pos.symbol)
                              setChartUpdateKey(Date.now())
                              if (chartSectionRef.current) {
                                chartSectionRef.current.scrollIntoView({
                                  behavior: 'smooth',
                                  block: 'start',
                                })
                              }
                            }}
                          >
                            <td className="px-1 py-3 font-mono font-semibold whitespace-nowrap text-left text-nofx-text-main group-hover/row:text-white transition-colors">
                              {pos.symbol}
                            </td>
                            <td className="px-1 py-3 whitespace-nowrap text-center">
                              <span
                                className={`px-1.5 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${pos.side === 'long' ? 'bg-nofx-green/10 text-nofx-green shadow-[0_0_8px_rgba(14,203,129,0.2)]' : 'bg-nofx-red/10 text-nofx-red shadow-[0_0_8px_rgba(246,70,93,0.2)]'}`}
                              >
                                {t(
                                  pos.side === 'long' ? 'long' : 'short',
                                  language
                                )}
                              </span>
                            </td>
                            <td className="px-1 py-3 whitespace-nowrap text-center">
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation()
                                  handleClosePosition(
                                    pos.symbol,
                                    pos.side.toUpperCase()
                                  )
                                }}
                                disabled={closingPosition === pos.symbol}
                                className="inline-flex items-center gap-1 px-2 py-1 rounded text-[10px] font-semibold transition-all hover:scale-105 disabled:opacity-50 disabled:cursor-not-allowed mx-auto bg-nofx-red/10 text-nofx-red border border-nofx-red/30 hover:bg-nofx-red/20"
                                title={t(
                                  'traderDashboard.closePosition',
                                  language
                                )}
                              >
                                {closingPosition === pos.symbol ? (
                                  <Loader2 className="w-3 h-3 animate-spin" />
                                ) : (
                                  <LogOut className="w-3 h-3" />
                                )}
                                {t('traderDashboard.close', language)}
                              </button>
                            </td>
                            <td className="px-1 py-3 font-mono whitespace-nowrap text-right text-nofx-text-main hidden md:table-cell">
                              {formatPrice(pos.entry_price)}
                            </td>
                            <td className="px-1 py-3 font-mono whitespace-nowrap text-right text-nofx-text-main hidden md:table-cell">
                              {formatPrice(pos.mark_price)}
                            </td>
                            <td className="px-1 py-3 font-mono whitespace-nowrap text-right text-nofx-text-main">
                              {formatQuantity(pos.quantity)}
                            </td>
                            <td className="px-1 py-3 font-mono font-bold whitespace-nowrap text-right text-nofx-text-main hidden md:table-cell">
                              {(pos.quantity * pos.mark_price).toFixed(2)}
                            </td>
                            <td className="px-1 py-3 font-mono whitespace-nowrap text-center text-nofx-gold hidden md:table-cell">
                              {pos.leverage}x
                            </td>
                            <td className="px-1 py-3 font-mono whitespace-nowrap text-right">
                              <div className="flex flex-col items-end leading-tight">
                                <span
                                  className={`font-bold ${pos.unrealized_pnl >= 0 ? 'text-nofx-green shadow-nofx-green' : 'text-nofx-red shadow-nofx-red'}`}
                                  style={{
                                    textShadow:
                                      pos.unrealized_pnl >= 0
                                        ? '0 0 10px rgba(14,203,129,0.3)'
                                        : '0 0 10px rgba(246,70,93,0.3)',
                                  }}
                                >
                                  {pos.unrealized_pnl >= 0 ? '+' : ''}
                                  {pos.unrealized_pnl.toFixed(2)} USDT
                                </span>
                                <span className="text-[10px] text-nofx-text-muted mt-0.5">
                                  {account && account.total_equity > 0 ? (
                                    <span title={t('uPnLAccount', language)}>
                                      {(() => {
                                        const pct =
                                          (pos.unrealized_pnl /
                                            account.total_equity) *
                                          100
                                        return `${pct >= 0 ? '+' : ''}${pct.toFixed(2)}%`
                                      })()}
                                    </span>
                                  ) : (
                                    '--'
                                  )}
                                  {' / '}
                                  <span
                                    title={t('riskControlYield', language)}
                                    className="text-nofx-gold/80"
                                  >
                                    {(() => {
                                      const leveraged =
                                        pos.unrealized_pnl_pct_leveraged ??
                                        pos.unrealized_pnl_pct
                                      return `${leveraged >= 0 ? '+' : ''}${leveraged.toFixed(2)}% ${t('riskControlYield', language)}`
                                    })()}
                                  </span>
                                </span>
                              </div>
                            </td>
                            <td className="px-1 py-3 font-mono whitespace-nowrap text-right text-nofx-text-muted hidden md:table-cell">
                              {formatPrice(pos.liquidation_price)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  {/* Pagination footer */}
                  {totalPositions > 10 && (
                    <div className="flex flex-wrap items-center justify-between gap-3 pt-4 mt-4 text-xs border-t border-white/5 text-nofx-text-muted">
                      <span>
                        {t('traderDashboard.showingPositions', language, {
                          shown: paginatedPositions.length,
                          total: totalPositions,
                        })}
                      </span>
                      <div className="flex items-center gap-3">
                        <div className="flex items-center gap-2">
                          <span>{t('traderDashboard.perPage', language)}:</span>
                          <NofxSelect
                            value={positionsPageSize}
                            onChange={(val) =>
                              setPositionsPageSize(Number(val))
                            }
                            options={[
                              { value: 20, label: '20' },
                              { value: 50, label: '50' },
                              { value: 100, label: '100' },
                            ]}
                            className="bg-black/40 border border-white/10 rounded px-2 py-1 text-xs text-nofx-text-main transition-colors"
                          />
                        </div>
                        {totalPositionPages > 1 && (
                          <div className="flex items-center gap-1">
                            {[
                              '«',
                              '‹',
                              `${positionsCurrentPage} / ${totalPositionPages}`,
                              '›',
                              '»',
                            ].map((label, idx) => {
                              const isText = idx === 2
                              const isFirst = idx === 0
                              const isPrev = idx === 1
                              const isNext = idx === 3
                              const isLast = idx === 4
                              if (isText)
                                return (
                                  <span
                                    key={idx}
                                    className="px-3 text-nofx-text-main"
                                  >
                                    {label}
                                  </span>
                                )

                              let onClick = () => {}
                              let disabled = false

                              if (isFirst) {
                                onClick = () => setPositionsCurrentPage(1)
                                disabled = positionsCurrentPage === 1
                              }
                              if (isPrev) {
                                onClick = () =>
                                  setPositionsCurrentPage((p) =>
                                    Math.max(1, p - 1)
                                  )
                                disabled = positionsCurrentPage === 1
                              }
                              if (isNext) {
                                onClick = () =>
                                  setPositionsCurrentPage((p) =>
                                    Math.min(totalPositionPages, p + 1)
                                  )
                                disabled =
                                  positionsCurrentPage === totalPositionPages
                              }
                              if (isLast) {
                                onClick = () =>
                                  setPositionsCurrentPage(totalPositionPages)
                                disabled =
                                  positionsCurrentPage === totalPositionPages
                              }

                              return (
                                <button
                                  key={idx}
                                  onClick={onClick}
                                  disabled={disabled}
                                  className={`px-2 py-1 rounded transition-colors ${disabled ? 'opacity-30 cursor-not-allowed' : 'hover:bg-white/10 text-nofx-text-main bg-white/5'}`}
                                >
                                  {label}
                                </button>
                              )
                            })}
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              ) : positionsFailed ? (
                <div className="text-center py-16 text-nofx-text-muted opacity-60">
                  <div className="text-4xl mb-4">⚠️</div>
                  <div className="text-lg font-semibold mb-2">
                    {t('traderDashboard.positionsFetchFailed', language)}
                  </div>
                </div>
              ) : (
                <div className="text-center py-16 text-nofx-text-muted opacity-60">
                  <div className="text-6xl mb-4 opacity-50 grayscale">📊</div>
                  <div className="text-lg font-semibold mb-2">
                    {t('noPositions', language)}
                  </div>
                  <div className="text-sm">
                    {t('noActivePositions', language)}
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Right Column: Recent Decisions */}
          <div
            className="nofx-glass p-6 animate-slide-in h-fit lg:sticky lg:top-24 lg:max-h-[calc(100vh-120px)] flex flex-col"
            style={{ animationDelay: '0.2s' }}
          >
            {/* Header */}
            <div className="flex items-center gap-3 mb-5 pb-4 border-b border-white/5 shrink-0">
              <div
                className="w-10 h-10 rounded-xl flex items-center justify-center text-xl shadow-[0_4px_14px_rgba(99,102,241,0.4)]"
                style={{
                  background:
                    'linear-gradient(135deg, #6366F1 0%, #8B5CF6 100%)',
                }}
              >
                🧠
              </div>
              <div className="flex-1">
                <h2 className="text-xl font-bold text-nofx-text-main">
                  {t('recentDecisions', language)}
                </h2>
                {decisions && decisions.length > 0 && (
                  <div className="text-xs text-nofx-text-muted">
                    {t('lastCycles', language, { count: decisions.length })}
                  </div>
                )}
              </div>
              {/* Limit Selector */}
              <NofxSelect
                value={decisionsLimit}
                onChange={(val) => onDecisionsLimitChange(Number(val))}
                options={[
                  { value: 5, label: '5' },
                  { value: 10, label: '10' },
                  { value: 20, label: '20' },
                  { value: 50, label: '50' },
                  { value: 100, label: '100' },
                ]}
                className="px-3 py-1.5 rounded-lg text-sm font-medium cursor-pointer transition-all bg-black/40 text-nofx-text-main border border-white/10 hover:border-nofx-accent"
              />
            </div>

            {/* Decisions List - Scrollable */}
            <div
              className="space-y-4 overflow-y-auto pr-2 custom-scrollbar"
              style={{ maxHeight: 'calc(100vh - 280px)' }}
            >
              {decisions && decisions.length > 0 ? (
                decisions.map((decision, i) => (
                  <DecisionCard
                    key={i}
                    decision={decision}
                    language={language}
                    onSymbolClick={handleSymbolClick}
                  />
                ))
              ) : decisionsFailed ? (
                <div className="py-16 text-center text-nofx-text-muted opacity-60">
                  <div className="text-4xl mb-4">⚠️</div>
                  <div className="text-lg font-semibold mb-2">
                    {t('traderDashboard.decisionsFetchFailed', language)}
                  </div>
                </div>
              ) : (
                <div className="py-16 text-center text-nofx-text-muted opacity-60">
                  <div className="text-6xl mb-4 opacity-30 grayscale">🧠</div>
                  <div className="text-lg font-semibold mb-2 text-nofx-text-main">
                    {t('noDecisionsYet', language)}
                  </div>
                  <div className="text-sm">
                    {t('aiDecisionsWillAppear', language)}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Position History Section */}
        {selectedTraderId && (
          <div
            className="nofx-glass p-6 animate-slide-in"
            style={{ animationDelay: '0.25s' }}
          >
            <div className="flex items-center justify-between mb-5">
              <h2 className="text-xl font-bold flex items-center gap-2 text-nofx-text-main">
                <span className="text-2xl">📜</span>
                {t('positionHistory.title', language)}
              </h2>
            </div>
            <PositionHistory traderId={selectedTraderId} />
          </div>
        )}
      </div>
    </DeepVoidBackground>
  )
}

// Stat Card Component - Deep Void Style
function StatCard({
  title,
  value,
  unit,
  change,
  positive,
  subtitle,
  icon,
  loading,
  tooltip,
  actionIcon,
  onAction,
}: {
  title: string
  value: string
  unit?: string
  change?: number
  positive?: boolean
  subtitle?: string
  icon?: string
  loading?: boolean
  tooltip?: string
  actionIcon?: ReactNode
  onAction?: () => void
}) {
  return (
    <div className="group nofx-glass p-5 rounded-lg transition-all duration-300 hover:bg-white/5 hover:translate-y-[-2px] border border-white/5 hover:border-nofx-gold/20 relative overflow-visible">
      {tooltip && (
        <div className="pointer-events-none absolute left-4 right-4 bottom-full z-20 mb-2 rounded-md border border-white/10 bg-black/90 px-3 py-2 text-xs leading-relaxed text-nofx-text-main opacity-0 shadow-xl transition-opacity duration-200 group-hover:opacity-100">
          {tooltip}
        </div>
      )}
      {onAction && (
        <button
          type="button"
          className="absolute right-3 top-3 z-10 rounded border border-white/10 bg-black/30 p-1.5 text-nofx-text-muted transition-colors hover:border-nofx-gold/40 hover:text-nofx-gold"
          onClick={(event) => {
            event.stopPropagation()
            onAction()
          }}
          title="Settings"
        >
          {actionIcon}
        </button>
      )}
      <div className="absolute top-0 right-0 p-4 opacity-5 group-hover:opacity-10 transition-opacity text-4xl grayscale group-hover:grayscale-0">
        {icon}
      </div>
      <div className="text-xs mb-2 font-mono uppercase tracking-wider text-nofx-text-muted flex items-center gap-2">
        {title}
      </div>
      {loading ? (
        <div className="space-y-2">
          <div className="h-7 w-24 rounded bg-white/5 animate-pulse" />
          <div className="h-3 w-16 rounded bg-white/5 animate-pulse" />
        </div>
      ) : (
        <>
          <div className="flex items-baseline gap-1 mb-1">
            <div className="text-2xl font-bold font-mono text-nofx-text-main tracking-tight group-hover:text-white transition-colors">
              {value}
            </div>
            {unit && (
              <span className="text-xs font-mono text-nofx-text-muted opacity-60">
                {unit}
              </span>
            )}
          </div>
          {change !== undefined && (
            <div className="flex items-center gap-1">
              <div
                className={`text-sm mono font-bold flex items-center gap-1 ${positive ? 'text-nofx-green' : 'text-nofx-red'}`}
              >
                <span>{positive ? '▲' : '▼'}</span>
                <span>
                  {positive ? '+' : ''}
                  {change.toFixed(2)}%
                </span>
              </div>
            </div>
          )}
          {subtitle && (
            <div className="text-xs mt-2 mono text-nofx-text-muted opacity-80">
              {subtitle}
            </div>
          )}
        </>
      )}
    </div>
  )
}
