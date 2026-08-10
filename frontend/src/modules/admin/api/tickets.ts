import type {
  AdminTicketDetail,
  AdminTicketListResponse,
  Sub2apiUserProfile,
  TicketEmbedConfig,
  TicketStatus,
  TicketsQuery,
  UpdateTicketEmbedConfigRequest,
} from '../types/tickets'
import {
  authUnauthorizedErrorKey,
  handleAuthExpired,
  isUnauthorizedApiResponse,
} from '@/modules/auth/api/auth'
import { authHeaders, endpoint, requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.tickets.errors.network',
    request: 'admin.tickets.errors.unknown',
  })

export const listTickets = async (query: TicketsQuery): Promise<AdminTicketListResponse> => {
  const params = new URLSearchParams({
    page: query.page.toString(),
    pageSize: query.pageSize.toString(),
  })
  if (query.status) params.set('status', query.status)

  return requestJson<AdminTicketListResponse>(`/tickets?${params.toString()}`)
}

export const getTicket = async (id: string): Promise<AdminTicketDetail> => (
  requestJson<AdminTicketDetail>(`/tickets/${encodeURIComponent(id)}`)
)

export const replyTicket = async (id: string, body: string): Promise<AdminTicketDetail> => (
  requestJson<AdminTicketDetail>(`/tickets/${encodeURIComponent(id)}/messages`, {
    method: 'POST',
    body: JSON.stringify({ body }),
  })
)

export const updateTicketStatus = async (id: string, status: TicketStatus): Promise<AdminTicketDetail> => (
  requestJson<AdminTicketDetail>(`/tickets/${encodeURIComponent(id)}/status`, {
    method: 'PUT',
    body: JSON.stringify({ status }),
  })
)

export const getEmbedConfig = async (): Promise<TicketEmbedConfig> => (
  requestJson<TicketEmbedConfig>('/tickets/embed-config')
)

export const updateEmbedConfig = async (
  request: UpdateTicketEmbedConfigRequest,
): Promise<TicketEmbedConfig> => (
  requestJson<TicketEmbedConfig>('/tickets/embed-config', {
    method: 'PUT',
    body: JSON.stringify(request),
  })
)

export const rotateEmbedToken = async (): Promise<TicketEmbedConfig> => (
  requestJson<TicketEmbedConfig>('/tickets/embed-config/rotate-token', {
    method: 'POST',
  })
)

export const getSub2apiUserProfile = async (ticketId: string): Promise<Sub2apiUserProfile> => (
  requestJson<Sub2apiUserProfile>(`/tickets/${encodeURIComponent(ticketId)}/sub2api-user-profile`)
)

// fetchAttachmentBlob 用当前后台登录态拉取附件二进制内容。<img> 标签无法附带 Authorization
// 请求头，所以图片必须先用 fetch 取回 blob，再由调用方通过 URL.createObjectURL 转成可以赋给
// <img src> 的临时地址（并在不再需要时 revokeObjectURL 释放）。
export const fetchAttachmentBlob = async (id: string): Promise<Blob> => {
  let response: Response
  try {
    response = await fetch(endpoint(`/tickets/attachments/${encodeURIComponent(id)}`), {
      headers: authHeaders(),
    })
  } catch (error) {
    throw new Error('admin.tickets.errors.network')
  }
  if (!response.ok) {
    if (isUnauthorizedApiResponse(response.status, {})) {
      handleAuthExpired()
      throw new Error(authUnauthorizedErrorKey)
    }
    throw new Error('admin.tickets.errors.attachmentLoadFailed')
  }
  return response.blob()
}
