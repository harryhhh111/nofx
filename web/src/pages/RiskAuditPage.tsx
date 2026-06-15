import { useCallback, useEffect, useState } from 'react'
import { useLanguage } from '../contexts/LanguageContext'
import { useAuth } from '../contexts/AuthContext'
import { riskAuditI18n, ts } from '../i18n/strategy-translations'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import {
  AlertTriangle,
  ShieldCheck,
  ShieldX,
  Activity,
  BarChart3,
} from 'lucide-react'

// ----------------------------------------------------------------------
// Phase 6: Risk Audit dashboard. Surfaces Phase 1 telemetry to humans:
//   - per-(guard_type, action) hit count bar chart
//   - AI / code agreement rate
//   - top blocked symbols
//   - recent block list with cycle / decision_record_id for drill-down
// Designed to be self-contained: a trader_id is read from the URL
// query (?trader=xxx) and the page degrades gracefully when missing.
// ----------------------------------------------------------------------

type Window = '4h' | '24h' | '7d'

interface GuardTypeActionCount {
  guard_type: string
  action: string
  count: number
}
interface GuardSymbolCount {
  symbol: string
  count: number
}
interface AIAgreementStats {
  total: number
  agreed: number
  rate: number
  ai_blocked: number
  code_blocked: number
  both_block: number
  ai_only: number
  code_only: number
}
interface GuardStatsResponse {
  trader_id: string
  window_hours: number
  totals: { block: number; reduce: number; allow: number }
  by_type: GuardTypeActionCount[]
  top_blocked_symbols: GuardSymbolCount[]
  ai_agreement: AIAgreementStats
}
interface GuardEventRow {
  id: number
  trader_id: string
  cycle_number: number
  guard_type: string
  action: string
  reason: string
  symbol: string
  side: string
  triggered_at: string
}
interface GuardEventsResponse {
  events: GuardEventRow[]
  count: number
}

// Color palette for guard_type bars. Defined once here so the bar
// chart and the legend are guaranteed to match.
const GUARD_TYPE_COLORS: Record<string, string> = {
  hard_safety: '#ef4444', // red — leverage / position / SL-TP direction
  rr_check: '#f59e0b', // amber — risk/reward floor
  tp_anchor: '#8b5cf6', // violet — TP extrapolation
  entry_risk_guard: '#3b82f6', // blue — soft guard hits
  cooldown: '#6b7280', // gray — future
  lifecycle_exit: '#10b981', // green — future (trailing/time stop)
  candidate_pool: '#ec4899', // pink — Phase 3
}
const FALLBACK_COLOR = '#9ca3af'

export function RiskAuditPage() {
  const { language } = useLanguage()
  const { token } = useAuth()

  const [traderId, setTraderId] = useState<string>('')
  const [windowSel, setWindowSel] = useState<Window>('24h')
  const [stats, setStats] = useState<GuardStatsResponse | null>(null)
  const [events, setEvents] = useState<GuardEventRow[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Read ?trader= from the URL on mount so the page can be opened
  // directly from a trader's dashboard.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    setTraderId(params.get('trader') || '')
  }, [])

  const fetchAll = useCallback(async () => {
    if (!traderId) return
    setLoading(true)
    setError(null)
    try {
      const headers: Record<string, string> = {}
      if (token) headers.Authorization = `Bearer ${token}`
      const [statsRes, eventsRes] = await Promise.all([
        fetch(
          `/api/traders/${encodeURIComponent(traderId)}/guard-stats?window=${windowSel}&top_limit=10`,
          { headers }
        ),
        fetch(
          `/api/traders/${encodeURIComponent(traderId)}/guard-events?limit=50&action=block&since_hours=${
            windowSel === '4h' ? 4 : windowSel === '7d' ? 168 : 24
          }`,
          { headers }
        ),
      ])
      if (!statsRes.ok) throw new Error(`stats HTTP ${statsRes.status}`)
      if (!eventsRes.ok) throw new Error(`events HTTP ${eventsRes.status}`)
      const statsJson: GuardStatsResponse = await statsRes.json()
      const eventsJson: GuardEventsResponse = await eventsRes.json()
      setStats(statsJson)
      setEvents(eventsJson.events || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }, [traderId, windowSel, token])

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  // --- Derived UI data -------------------------------------------------

  // Aggregate per (guard_type) so the bar chart shows one bar per
  // guard_type (not per guard_type×action, which would be too dense
  // for a first cut). Color is taken from the palette.
  const bars =
    stats?.by_type.reduce<
      Record<string, { name: string; count: number; color: string }>
    >((acc, row) => {
      if (!acc[row.guard_type]) {
        acc[row.guard_type] = {
          name: row.guard_type,
          count: 0,
          color: GUARD_TYPE_COLORS[row.guard_type] || FALLBACK_COLOR,
        }
      }
      acc[row.guard_type].count += row.count
      return acc
    }, {}) ?? {}
  const barData = Object.values(bars).sort((a, b) => b.count - a.count)

  const agreement = stats?.ai_agreement
  const agreementPct = agreement ? Math.round(agreement.rate * 100) : 0

  // Pre-compute the agreement hint string outside the JSX so the
  // template literal's `{...}` tokens don't get confused with JSX
  // expression containers by the parser.
  const agreementHint = agreement
    ? ts(riskAuditI18n.aiAgreementHint, language)
        .replace('{both}', String(agreement.both_block))
        .replace('{ai}', String(agreement.ai_only))
        .replace('{code}', String(agreement.code_only))
    : undefined

  return (
    <div className="w-full px-4 md:px-8 py-6 max-w-7xl mx-auto">
      <header className="mb-6 flex flex-col md:flex-row md:items-end md:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-nofx-text-primary flex items-center gap-2">
            <ShieldCheck size={24} className="text-nofx-primary" />
            {ts(riskAuditI18n.title, language)}
          </h1>
          <p className="text-sm text-nofx-text-muted mt-1">
            {ts(riskAuditI18n.subtitle, language)}
          </p>
        </div>
        <div className="flex flex-col md:flex-row gap-2">
          <input
            type="text"
            value={traderId}
            onChange={(e) => setTraderId(e.target.value.trim())}
            placeholder={ts(riskAuditI18n.traderIdPlaceholder, language)}
            className="bg-nofx-bg-tertiary border border-nofx-border rounded px-3 py-1.5 text-sm font-mono"
          />
          <select
            value={windowSel}
            onChange={(e) => setWindowSel(e.target.value as Window)}
            className="bg-nofx-bg-tertiary border border-nofx-border rounded px-3 py-1.5 text-sm"
          >
            <option value="4h">4h</option>
            <option value="24h">24h</option>
            <option value="7d">7d</option>
          </select>
          <button
            onClick={fetchAll}
            disabled={loading || !traderId}
            className="px-3 py-1.5 rounded bg-nofx-primary text-nofx-bg-primary text-sm font-medium disabled:opacity-50"
          >
            {ts(riskAuditI18n.refresh, language)}
          </button>
        </div>
      </header>

      {error && (
        <div className="mb-4 p-3 rounded border border-red-500/40 bg-red-500/10 text-red-300 text-sm flex items-center gap-2">
          <AlertTriangle size={16} />
          {error}
        </div>
      )}

      {!traderId && (
        <div className="p-8 text-center text-nofx-text-muted text-sm border border-dashed border-nofx-border rounded">
          {ts(riskAuditI18n.noTraderHint, language)}
        </div>
      )}

      {traderId && stats && (
        <>
          {/* Summary cards */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-3 mb-6">
            <SummaryCard
              icon={<ShieldX size={18} className="text-red-400" />}
              label={ts(riskAuditI18n.totalBlocks, language)}
              value={stats.totals.block}
            />
            <SummaryCard
              icon={<Activity size={18} className="text-amber-400" />}
              label={ts(riskAuditI18n.totalReduces, language)}
              value={stats.totals.reduce}
            />
            <SummaryCard
              icon={<BarChart3 size={18} className="text-blue-400" />}
              label={ts(riskAuditI18n.totalEvents, language)}
              value={
                stats.totals.block + stats.totals.reduce + stats.totals.allow
              }
            />
            <SummaryCard
              icon={
                agreement && agreement.rate >= 0.8 ? (
                  <ShieldCheck size={18} className="text-emerald-400" />
                ) : (
                  <AlertTriangle size={18} className="text-amber-400" />
                )
              }
              label={ts(riskAuditI18n.aiAgreement, language)}
              value={
                agreement
                  ? `${agreementPct}% (${agreement.agreed}/${agreement.total})`
                  : '—'
              }
              hint={agreementHint}
            />
          </div>

          {/* Bar chart: hits per guard_type */}
          <section className="mb-6 p-4 rounded border border-nofx-border bg-nofx-bg-secondary">
            <h2 className="text-sm font-medium text-nofx-text-primary mb-3 flex items-center gap-2">
              <BarChart3 size={16} />
              {ts(riskAuditI18n.hitsByGuardType, language)}
            </h2>
            {barData.length === 0 ? (
              <div className="h-48 flex items-center justify-center text-nofx-text-muted text-sm">
                {ts(riskAuditI18n.noData, language)}
              </div>
            ) : (
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={barData}>
                    <CartesianGrid stroke="#262b35" strokeDasharray="3 3" />
                    <XAxis
                      dataKey="name"
                      stroke="#848e9c"
                      fontSize={11}
                      interval={0}
                      angle={-20}
                      textAnchor="end"
                      height={50}
                    />
                    <YAxis
                      stroke="#848e9c"
                      fontSize={11}
                      allowDecimals={false}
                    />
                    <Tooltip
                      contentStyle={{
                        background: '#1a1d24',
                        border: '1px solid #2a2e37',
                        fontSize: 12,
                      }}
                    />
                    <Bar dataKey="count" radius={[4, 4, 0, 0]}>
                      {barData.map((d) => (
                        <Cell key={d.name} fill={d.color} />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </section>

          {/* Top blocked symbols + recent blocks */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <section className="p-4 rounded border border-nofx-border bg-nofx-bg-secondary">
              <h2 className="text-sm font-medium text-nofx-text-primary mb-3">
                {ts(riskAuditI18n.topBlockedSymbols, language)}
              </h2>
              {(stats.top_blocked_symbols || []).length === 0 ? (
                <div className="text-nofx-text-muted text-sm">
                  {ts(riskAuditI18n.noData, language)}
                </div>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-nofx-text-muted text-xs uppercase">
                      <th className="text-left py-1">Symbol</th>
                      <th className="text-right py-1">Hits</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.top_blocked_symbols.map((s) => (
                      <tr
                        key={s.symbol}
                        className="border-t border-nofx-border/40"
                      >
                        <td className="py-1.5 font-mono">{s.symbol}</td>
                        <td className="py-1.5 text-right">{s.count}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>

            <section className="p-4 rounded border border-nofx-border bg-nofx-bg-secondary">
              <h2 className="text-sm font-medium text-nofx-text-primary mb-3">
                {ts(riskAuditI18n.recentBlocks, language)}
              </h2>
              {events.length === 0 ? (
                <div className="text-nofx-text-muted text-sm">
                  {ts(riskAuditI18n.noData, language)}
                </div>
              ) : (
                <div className="max-h-80 overflow-y-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="text-nofx-text-muted text-xs uppercase">
                        <th className="text-left py-1">Time</th>
                        <th className="text-left py-1">Type</th>
                        <th className="text-left py-1">Symbol</th>
                        <th className="text-left py-1">Reason</th>
                      </tr>
                    </thead>
                    <tbody>
                      {events.map((e) => (
                        <tr
                          key={e.id}
                          className="border-t border-nofx-border/40"
                        >
                          <td className="py-1.5 text-xs text-nofx-text-muted">
                            {new Date(e.triggered_at).toLocaleString()}
                          </td>
                          <td className="py-1.5 text-xs">
                            <span
                              className="px-1.5 py-0.5 rounded text-white"
                              style={{
                                background:
                                  GUARD_TYPE_COLORS[e.guard_type] ||
                                  FALLBACK_COLOR,
                              }}
                            >
                              {e.guard_type}
                            </span>
                          </td>
                          <td className="py-1.5 font-mono text-xs">
                            {e.symbol || '—'}
                          </td>
                          <td className="py-1.5 text-xs text-nofx-text-muted truncate max-w-[18rem]">
                            {e.reason}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </div>
        </>
      )}
    </div>
  )
}

function SummaryCard({
  icon,
  label,
  value,
  hint,
}: {
  icon: React.ReactNode
  label: string
  value: React.ReactNode
  hint?: string
}) {
  return (
    <div className="p-4 rounded border border-nofx-border bg-nofx-bg-secondary">
      <div className="flex items-center gap-2 text-xs text-nofx-text-muted uppercase">
        {icon}
        <span>{label}</span>
      </div>
      <div className="mt-1.5 text-2xl font-semibold text-nofx-text-primary">
        {value}
      </div>
      {hint && <div className="mt-1 text-xs text-nofx-text-muted">{hint}</div>}
    </div>
  )
}
