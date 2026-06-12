import type {
  SystemStatus,
  AccountInfo,
  Position,
  DecisionRecord,
  Statistics,
  BBMACDAccuracyStats,
  BBMACDConfig,
  CompetitionData,
  PositionHistoryResponse,
  TradeMemory,
  ExecutionAnalytics,
  NofxOSStatus,
  AI500CoinsResponse,
} from '../../types'
import { API_BASE, httpClient } from './helpers'

export const dataApi = {
  async getStatus(traderId?: string, silent?: boolean): Promise<SystemStatus> {
    const url = traderId
      ? `${API_BASE}/status?trader_id=${traderId}`
      : `${API_BASE}/status`
    const result = await httpClient.request<SystemStatus>(url, { silent })
    if (!result.success) throw new Error('Failed to fetch system status')
    return result.data!
  },

  async getAccount(traderId?: string, silent?: boolean): Promise<AccountInfo> {
    const url = traderId
      ? `${API_BASE}/account?trader_id=${traderId}`
      : `${API_BASE}/account`
    const result = await httpClient.request<AccountInfo>(url, { silent })
    if (!result.success) throw new Error('Failed to fetch account info')
    return result.data!
  },

  async getPositions(traderId?: string, silent?: boolean): Promise<Position[]> {
    const url = traderId
      ? `${API_BASE}/positions?trader_id=${traderId}`
      : `${API_BASE}/positions`
    const result = await httpClient.request<Position[]>(url, { silent })
    if (!result.success) throw new Error('Failed to fetch positions')
    return result.data!
  },

  async getDecisions(traderId?: string): Promise<DecisionRecord[]> {
    const url = traderId
      ? `${API_BASE}/decisions?trader_id=${traderId}`
      : `${API_BASE}/decisions`
    const result = await httpClient.get<DecisionRecord[]>(url)
    if (!result.success) throw new Error('Failed to fetch decision logs')
    return result.data!
  },

  async getLatestDecisions(
    traderId?: string,
    limit: number = 5,
    silent?: boolean
  ): Promise<DecisionRecord[]> {
    const params = new URLSearchParams()
    if (traderId) {
      params.append('trader_id', traderId)
    }
    params.append('limit', limit.toString())

    const result = await httpClient.request<DecisionRecord[]>(
      `${API_BASE}/decisions/latest?${params}`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch latest decisions')
    return result.data!
  },

  async getDecisionById(
    traderId: string,
    decisionId: number,
    silent?: boolean
  ): Promise<DecisionRecord> {
    const params = new URLSearchParams()
    params.append('trader_id', traderId)
    const result = await httpClient.request<DecisionRecord>(
      `${API_BASE}/decisions/${decisionId}?${params}`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch decision record')
    return result.data!
  },

  async getStatistics(traderId?: string, silent?: boolean): Promise<Statistics> {
    const url = traderId
      ? `${API_BASE}/statistics?trader_id=${traderId}`
      : `${API_BASE}/statistics`
    const result = await httpClient.request<Statistics>(url, { silent })
    if (!result.success) throw new Error('Failed to fetch statistics')
    return result.data!
  },

  async getBBMACDStats(
    traderId?: string,
    days: number = 0,
    silent?: boolean
  ): Promise<BBMACDAccuracyStats> {
    const params = new URLSearchParams()
    if (traderId) {
      params.append('trader_id', traderId)
    }
    params.append('days', days.toString())

    const result = await httpClient.request<BBMACDAccuracyStats>(
      `${API_BASE}/bbmacd/stats?${params}`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch BB MACD stats')
    return result.data!
  },

  async getBBMACDConfig(silent?: boolean): Promise<BBMACDConfig> {
    const result = await httpClient.request<BBMACDConfig>(
      `${API_BASE}/bbmacd/config`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch BB MACD config')
    return result.data!
  },

  async updateBBMACDConfig(
    traderId: string | undefined,
    config: BBMACDConfig,
    reset: boolean = true
  ): Promise<{ config: BBMACDConfig; reset: boolean }> {
    const params = new URLSearchParams()
    if (traderId) {
      params.append('trader_id', traderId)
    }
    const result = await httpClient.request<{ config: BBMACDConfig; reset: boolean }>(
      `${API_BASE}/bbmacd/config?${params}`,
      {
        method: 'PUT',
        data: { config, reset },
      }
    )
    if (!result.success) throw new Error(result.message || 'Failed to update BB MACD config')
    return result.data!
  },

  async getTradeMemories(
    traderId: string,
    symbol?: string,
    limit: number = 20,
    silent?: boolean
  ): Promise<TradeMemory[]> {
    const params = new URLSearchParams()
    params.append('trader_id', traderId)
    params.append('limit', limit.toString())
    if (symbol) params.append('symbol', symbol)

    const result = await httpClient.request<TradeMemory[]>(
      `${API_BASE}/trade-memories?${params}`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch trade memories')
    return Array.isArray(result.data) ? result.data : []
  },

  async getExecutionAnalytics(
    traderId: string,
    symbol?: string,
    limit: number = 30,
    silent?: boolean
  ): Promise<ExecutionAnalytics[]> {
    const params = new URLSearchParams()
    params.append('trader_id', traderId)
    params.append('limit', limit.toString())
    if (symbol) params.append('symbol', symbol)

    const result = await httpClient.request<ExecutionAnalytics[]>(
      `${API_BASE}/execution-analytics?${params}`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch execution analytics')
    return Array.isArray(result.data) ? result.data : []
  },

  async getNofxOSStatus(silent?: boolean): Promise<NofxOSStatus> {
    const result = await httpClient.request<NofxOSStatus>(
      `${API_BASE}/nofxos/status`,
      { silent }
    )
    if (!result.success) throw new Error(result.message || 'Failed to fetch NofxOS status')
    return result.data || { records: [], count: 0 }
  },

  async getAI500Coins(limit: number = 20, silent?: boolean): Promise<AI500CoinsResponse> {
    const result = await httpClient.request<AI500CoinsResponse>(
      `${API_BASE}/ai500/coins?limit=${limit}`,
      { silent }
    )
    if (!result.success) throw new Error(result.message || 'Failed to fetch AI500 coins')
    return result.data || { coins: [], count: 0 }
  },

  async getEquityHistory(traderId?: string, silent?: boolean): Promise<any[]> {
    const url = traderId
      ? `${API_BASE}/equity-history?trader_id=${traderId}`
      : `${API_BASE}/equity-history`
    const result = await httpClient.request<any[]>(url, { silent })
    if (!result.success) throw new Error('Failed to fetch equity history')
    return result.data!
  },

  async getEquityHistoryBatch(traderIds: string[], hours?: number): Promise<any> {
    const result = await httpClient.post<any>(
      `${API_BASE}/equity-history-batch`,
      { trader_ids: traderIds, hours: hours || 0 }
    )
    if (!result.success) throw new Error('Failed to fetch batch equity history')
    return result.data!
  },

  async getTopTraders(): Promise<any[]> {
    const result = await httpClient.get<any[]>(`${API_BASE}/top-traders`)
    if (!result.success) throw new Error('Failed to fetch top traders')
    return result.data!
  },

  async getPublicTraderConfig(traderId: string): Promise<any> {
    const result = await httpClient.get<any>(
      `${API_BASE}/trader/${traderId}/config`
    )
    if (!result.success) throw new Error('Failed to fetch public trader config')
    return result.data!
  },

  async getCompetition(): Promise<CompetitionData> {
    const result = await httpClient.get<CompetitionData>(
      `${API_BASE}/competition`
    )
    if (!result.success) throw new Error('Failed to fetch competition data')
    return result.data!
  },

  async getPositionHistory(
    traderId: string,
    limit: number = 100,
    silent?: boolean
  ): Promise<PositionHistoryResponse> {
    const result = await httpClient.request<PositionHistoryResponse>(
      `${API_BASE}/positions/history?trader_id=${traderId}&limit=${limit}`,
      { silent }
    )
    if (!result.success) throw new Error('Failed to fetch position history')
    return result.data!
  },
}
