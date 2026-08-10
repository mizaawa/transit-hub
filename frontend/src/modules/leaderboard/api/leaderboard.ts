import { requestJson as sharedRequestJson } from '@/lib/apiClient'
import type { LeaderboardDateRange, LeaderboardEmbedConfig, LeaderboardResponse } from '../types'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.leaderboard.errors.network',
    request: 'admin.leaderboard.errors.unknown',
  })

export const getLeaderboard = async (range: LeaderboardDateRange): Promise<LeaderboardResponse> => {
  const params = new URLSearchParams({ start_date: range.startDate, end_date: range.endDate })
  return requestJson<LeaderboardResponse>(`/leaderboard/data?${params.toString()}`)
}

export const getLeaderboardEmbedConfig = async (): Promise<LeaderboardEmbedConfig> => (
  requestJson<LeaderboardEmbedConfig>('/leaderboard/embed-config')
)

export const rotateLeaderboardEmbedToken = async (): Promise<LeaderboardEmbedConfig> => (
  requestJson<LeaderboardEmbedConfig>('/leaderboard/embed-config/rotate-token', { method: 'POST' })
)
