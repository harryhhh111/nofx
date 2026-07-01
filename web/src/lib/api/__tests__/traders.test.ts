import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { traderApi } from '../traders'
import { httpClient } from '../helpers'

vi.mock('../helpers', async () => {
  const actual = await vi.importActual<typeof import('../helpers')>('../helpers')
  return {
    ...actual,
    httpClient: {
      request: vi.fn(),
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
    },
  }
})

describe('traderApi URL encoding for trader IDs containing slashes', () => {
  const mockPost = httpClient.post as ReturnType<typeof vi.fn>

  beforeEach(() => {
    mockPost.mockReset()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('should encode trader IDs that contain slashes in startTrader URL', async () => {
    const rawTraderId =
      'c32f1f5c_a0806eec-bf69-469f-9148-d384074dfd61_qwen_qwen/qwen3.5-plus-20260420_1782888046'
    const encodedTraderId = encodeURIComponent(rawTraderId)

    mockPost.mockResolvedValue({ success: true })

    await traderApi.startTrader(rawTraderId)

    expect(mockPost).toHaveBeenCalledTimes(1)
    const calledUrl = mockPost.mock.calls[0][0]
    expect(calledUrl).toBe(`/api/traders/${encodedTraderId}/start`)
    expect(calledUrl).not.toContain('/qwen3.5')
    expect(calledUrl).toContain('%2Fqwen3.5')
  })

  it('should not double-encode already encoded trader IDs', async () => {
    const rawTraderId = 'plain-id-without-special-chars'
    mockPost.mockResolvedValue({ success: true })

    await traderApi.startTrader(rawTraderId)

    expect(mockPost).toHaveBeenCalledTimes(1)
    const calledUrl = mockPost.mock.calls[0][0]
    expect(calledUrl).toBe('/api/traders/plain-id-without-special-chars/start')
  })
})
