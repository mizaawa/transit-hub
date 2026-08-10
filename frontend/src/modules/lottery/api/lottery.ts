import { requestJson as sharedRequestJson } from '@/lib/apiClient'
import type {
  LotteryAuditResponse,
  LotteryCampaign,
  LotteryCampaignRequest,
  LotteryCampaignsResponse,
  LotteryEmbedConfig,
  LotteryEntriesResponse,
  LotteryOkResponse,
  LotterySubscriptionGroupsResponse,
} from '../types'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.lottery.errors.network',
    request: 'admin.lottery.errors.request',
  })

export const getLotteryEmbedConfig = async (): Promise<LotteryEmbedConfig> => (
  requestJson<LotteryEmbedConfig>('/lottery/embed-config')
)

export const rotateLotteryEmbedToken = async (): Promise<LotteryEmbedConfig> => (
  requestJson<LotteryEmbedConfig>('/lottery/embed-config/rotate-token', { method: 'POST' })
)

export const listLotteryCampaigns = async (): Promise<LotteryCampaignsResponse> => (
  requestJson<LotteryCampaignsResponse>('/lottery/campaigns')
)

export const listLotterySubscriptionGroups = async (): Promise<LotterySubscriptionGroupsResponse> => (
  requestJson<LotterySubscriptionGroupsResponse>('/lottery/subscription-groups')
)

export const createLotteryCampaign = async (request: LotteryCampaignRequest): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>('/lottery/campaigns', { method: 'POST', body: JSON.stringify(request) })
)

export const getLotteryCampaign = async (id: string): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>(`/lottery/campaigns/${encodeURIComponent(id)}`)
)

export const updateLotteryCampaign = async (id: string, request: LotteryCampaignRequest): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>(`/lottery/campaigns/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(request) })
)

export const publishLotteryCampaign = async (id: string): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>(`/lottery/campaigns/${encodeURIComponent(id)}/publish`, { method: 'POST' })
)

export const closeLotteryCampaign = async (id: string): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>(`/lottery/campaigns/${encodeURIComponent(id)}/close`, { method: 'POST' })
)

export const drawLotteryCampaign = async (id: string): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>(`/lottery/campaigns/${encodeURIComponent(id)}/draw`, { method: 'POST' })
)

export const cancelLotteryCampaign = async (id: string): Promise<LotteryCampaign> => (
  requestJson<LotteryCampaign>(`/lottery/campaigns/${encodeURIComponent(id)}/cancel`, { method: 'POST' })
)

export const listLotteryEntries = async (id: string): Promise<LotteryEntriesResponse> => (
  requestJson<LotteryEntriesResponse>(`/lottery/campaigns/${encodeURIComponent(id)}/entries`)
)

export const listLotteryAudit = async (id: string): Promise<LotteryAuditResponse> => (
  requestJson<LotteryAuditResponse>(`/lottery/campaigns/${encodeURIComponent(id)}/audit`)
)

export const retryLotteryReward = async (id: string): Promise<LotteryOkResponse> => (
  requestJson<LotteryOkResponse>(`/lottery/reward-jobs/${encodeURIComponent(id)}/retry`, { method: 'POST' })
)

export const completeManualLotteryReward = async (id: string): Promise<LotteryOkResponse> => (
  requestJson<LotteryOkResponse>(`/lottery/reward-jobs/${encodeURIComponent(id)}/complete`, { method: 'POST' })
)
