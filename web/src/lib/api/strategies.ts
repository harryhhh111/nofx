import type {
  Strategy,
  StrategyConfig,
  StrategyCompileResponse,
  StrategyCalibrationReport,
  StrategyEvolutionResult,
  StrategyMetadata,
  StrategyPreviewFlowResponse,
  StrategyTestRunResponse,
} from '../../types'
import { API_BASE, httpClient } from './helpers'

export const strategyApi = {
  async getStrategyMetadata(): Promise<StrategyMetadata> {
    const result = await httpClient.get<StrategyMetadata>(`${API_BASE}/strategies/metadata`)
    if (!result.success) throw new Error('Failed to fetch strategy metadata')
    return result.data!
  },

  async getStrategies(): Promise<Strategy[]> {
    const result = await httpClient.get<{ strategies: Strategy[] }>(`${API_BASE}/strategies`)
    if (!result.success) throw new Error('Failed to fetch strategy list')
    const strategies = result.data?.strategies
    return Array.isArray(strategies) ? strategies : []
  },

  async getStrategy(strategyId: string): Promise<Strategy> {
    const result = await httpClient.get<Strategy>(`${API_BASE}/strategies/${strategyId}`)
    if (!result.success) throw new Error('Failed to fetch strategy')
    return result.data!
  },

  async getActiveStrategy(): Promise<Strategy> {
    const result = await httpClient.get<Strategy>(`${API_BASE}/strategies/active`)
    if (!result.success) throw new Error('Failed to fetch active strategy')
    return result.data!
  },

  async getDefaultStrategyConfig(): Promise<StrategyConfig> {
    const result = await httpClient.get<StrategyConfig>(`${API_BASE}/strategies/default-config`)
    if (!result.success) throw new Error('Failed to fetch default strategy config')
    return result.data!
  },

  async createStrategy(data: {
    name: string
    description: string
    config: StrategyConfig
  }): Promise<Strategy> {
    const result = await httpClient.post<Strategy>(`${API_BASE}/strategies`, data)
    if (!result.success) throw new Error('Failed to create strategy')
    return result.data!
  },

  async updateStrategy(
    strategyId: string,
    data: {
      name?: string
      description?: string
      config?: StrategyConfig
    }
  ): Promise<Strategy> {
    const result = await httpClient.put<Strategy>(`${API_BASE}/strategies/${strategyId}`, data)
    if (!result.success) throw new Error('Failed to update strategy')
    return result.data!
  },

  async deleteStrategy(strategyId: string): Promise<void> {
    const result = await httpClient.delete(`${API_BASE}/strategies/${strategyId}`)
    if (!result.success) throw new Error('Failed to delete strategy')
  },

  async activateStrategy(strategyId: string): Promise<Strategy> {
    const result = await httpClient.post<Strategy>(`${API_BASE}/strategies/${strategyId}/activate`)
    if (!result.success) throw new Error('Failed to activate strategy')
    return result.data!
  },

  async duplicateStrategy(strategyId: string): Promise<Strategy> {
    const result = await httpClient.post<Strategy>(`${API_BASE}/strategies/${strategyId}/duplicate`)
    if (!result.success) throw new Error('Failed to duplicate strategy')
    return result.data!
  },

  async compileStrategyPrompt(data: {
    prompt: string
    context?: string
    ai_model_id: string
    strategy_id?: string
    strategy_version?: string
    persist?: boolean
  }): Promise<StrategyCompileResponse> {
    const result = await httpClient.request<StrategyCompileResponse>(
      `${API_BASE}/strategies/compile`,
      {
        method: 'POST',
        data,
        timeout: 120000,
      }
    )
    if (!result.success) throw new Error(result.message || 'Failed to compile strategy')
    return result.data!
  },

  async previewStrategyFlow(data: {
    config: StrategyConfig
  }): Promise<StrategyPreviewFlowResponse> {
    const result = await httpClient.post<StrategyPreviewFlowResponse>(
      `${API_BASE}/strategies/preview-flow`,
      data
    )
    if (!result.success) throw new Error(result.message || 'Failed to preview strategy flow')
    return result.data!
  },

  async testRunStrategy(data: {
    config: StrategyConfig
    prompt_variant?: string
    ai_model_id?: string
    run_real_ai?: boolean
  }): Promise<StrategyTestRunResponse> {
    const result = await httpClient.request<StrategyTestRunResponse>(
      `${API_BASE}/strategies/test-run`,
      {
        method: 'POST',
        data,
        timeout: 180000,
      }
    )
    if (!result.success) throw new Error(result.message || 'Failed to run strategy test')
    return result.data!
  },

  async getStrategyCalibrationReport(
    strategyId: string,
    limit = 1000
  ): Promise<StrategyCalibrationReport> {
    const result = await httpClient.get<StrategyCalibrationReport>(
      `${API_BASE}/strategies/${strategyId}/calibration-report?limit=${limit}`
    )
    if (!result.success) throw new Error(result.message || 'Failed to fetch calibration report')
    return result.data!
  },

  async evolveStrategy(
    strategyId: string,
    data: {
      ai_model_id: string
      trigger?: string
      user_instruction?: string
      limit?: number
    }
  ): Promise<StrategyEvolutionResult> {
    const result = await httpClient.request<StrategyEvolutionResult>(
      `${API_BASE}/strategies/${strategyId}/evolve`,
      {
        method: 'POST',
        data,
        timeout: 180000,
      }
    )
    if (!result.success) throw new Error(result.message || 'Failed to evolve strategy')
    return result.data!
  },
}
