import type { GroupRateHistoryQuery, GroupRateHistoryRow, GroupRatesQuery, PaginatedGroupRatesResponse, UpdateGroupRateTypeRequest } from '../types/groupRates'
import { requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.groupRates.errors.network',
    request: 'admin.groupRates.errors.request',
  })

export const listGroupRates = async (query: GroupRatesQuery): Promise<PaginatedGroupRatesResponse> => {
  const params = new URLSearchParams({
    page: query.page.toString(),
    search: query.search.trim(),
    type: query.type,
    platform: query.platform,
    status: query.status,
    sort: query.sort,
  })

  return requestJson<PaginatedGroupRatesResponse>(`/group-rates?${params.toString()}`)
}

export const listGroupRateHistory = async (query: GroupRateHistoryQuery): Promise<GroupRateHistoryRow[]> => {
  const params = new URLSearchParams({
    siteId: query.siteId,
    groupName: query.groupId || query.groupName,
  })

  if (query.platform) params.set('platform', query.platform)

  return requestJson<GroupRateHistoryRow[]>(`/group-rates/history?${params.toString()}`)
}

export const updateGroupRateType = async (request: UpdateGroupRateTypeRequest): Promise<void> => {
  await requestJson<{ success: boolean }>('/group-rates/type', {
    method: 'PATCH',
    body: JSON.stringify(request),
  })
}
