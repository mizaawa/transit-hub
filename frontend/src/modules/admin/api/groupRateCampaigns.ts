import type {
  CampaignDetail,
  CreateGroupRateCampaignRequest,
  CampaignPreviewResponse,
  GroupRateCampaignsQuery,
  PaginatedGroupRateCampaignsResponse,
} from '../types/groupRateCampaigns'
import { requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.groupRateCampaigns.errors.network',
    request: 'admin.groupRateCampaigns.errors.request',
  })

export const listGroupRateCampaigns = async (
  query: GroupRateCampaignsQuery,
): Promise<PaginatedGroupRateCampaignsResponse> => {
  const params = new URLSearchParams({
    page: query.page.toString(),
    pageSize: query.pageSize.toString(),
  })
  if (query.status) params.set('status', query.status)

  return requestJson<PaginatedGroupRateCampaignsResponse>(`/group-rate-campaigns?${params.toString()}`)
}

export const previewGroupRateCampaign = async (
  request: CreateGroupRateCampaignRequest,
): Promise<CampaignPreviewResponse> => (
  requestJson<CampaignPreviewResponse>('/group-rate-campaigns/preview', {
    method: 'POST',
    body: JSON.stringify(request),
  })
)

export const createGroupRateCampaign = async (
  request: CreateGroupRateCampaignRequest,
): Promise<CampaignDetail> => (
  requestJson<CampaignDetail>('/group-rate-campaigns', {
    method: 'POST',
    body: JSON.stringify(request),
  })
)

export const getGroupRateCampaign = async (id: string): Promise<CampaignDetail> => (
  requestJson<CampaignDetail>(`/group-rate-campaigns/${encodeURIComponent(id)}`)
)

export const startGroupRateCampaign = async (id: string): Promise<CampaignDetail> => (
  requestJson<CampaignDetail>(`/group-rate-campaigns/${encodeURIComponent(id)}/start`, {
    method: 'POST',
  })
)

export const endGroupRateCampaign = async (id: string): Promise<CampaignDetail> => (
  requestJson<CampaignDetail>(`/group-rate-campaigns/${encodeURIComponent(id)}/end`, {
    method: 'POST',
  })
)

export const cancelGroupRateCampaign = async (id: string): Promise<CampaignDetail> => (
  requestJson<CampaignDetail>(`/group-rate-campaigns/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
  })
)
