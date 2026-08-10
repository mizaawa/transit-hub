import type {
  CreateMassEmailBatchRequest,
  MassEmailBatch,
  MassEmailUsersQuery,
  PaginatedMassEmailBatchItemsResponse,
  PaginatedMassEmailBatchesResponse,
  PaginatedMassEmailUsersResponse,
} from '../types/massEmail'
import { requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.massEmail.errors.network',
    request: 'admin.massEmail.errors.request',
  })

type PaginatedWire<T> = {
  items?: T[]
  total?: number
  page?: number
  pageSize?: number
  page_size?: number
  pages?: number
  totalPages?: number
  total_pages?: number
}

const normalizePaginated = <T>(payload: PaginatedWire<T>): { items: T[]; total: number; page: number; pageSize: number; totalPages: number } => ({
  items: payload.items ?? [],
  total: payload.total ?? 0,
  page: payload.page ?? 1,
  pageSize: payload.pageSize ?? payload.page_size ?? 20,
  totalPages: payload.pages ?? payload.totalPages ?? payload.total_pages ?? 1,
})

export const listMassEmailUsers = async (
  query: MassEmailUsersQuery,
): Promise<PaginatedMassEmailUsersResponse> => {
  const params = new URLSearchParams({
    page: query.page.toString(),
    page_size: query.pageSize.toString(),
    sort_by: query.sortBy,
    sort_order: query.sortOrder,
    timezone: query.timezone,
  })
  if (query.status) params.set('status', query.status)
  if (query.role) params.set('role', query.role)
  if (query.search) params.set('search', query.search)

  const payload = await requestJson<PaginatedWire<PaginatedMassEmailUsersResponse['items'][number]>>(`/mass-email/users?${params.toString()}`)
  return normalizePaginated(payload)
}

export const createMassEmailBatch = async (payload: CreateMassEmailBatchRequest): Promise<MassEmailBatch> => (
  requestJson<MassEmailBatch>('/mass-email/batches', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
)

export const listMassEmailBatches = async (page = 1, pageSize = 10): Promise<PaginatedMassEmailBatchesResponse> => {
  const params = new URLSearchParams({
    page: page.toString(),
    page_size: pageSize.toString(),
  })
  const payload = await requestJson<PaginatedWire<MassEmailBatch>>(`/mass-email/batches?${params.toString()}`)
  return normalizePaginated(payload)
}

export const getMassEmailBatch = async (id: string): Promise<MassEmailBatch> => (
  requestJson<MassEmailBatch>(`/mass-email/batches/${encodeURIComponent(id)}`)
)

export const listMassEmailBatchItems = async (
  id: string,
  page = 1,
  pageSize = 20,
): Promise<PaginatedMassEmailBatchItemsResponse> => {
  const params = new URLSearchParams({
    page: page.toString(),
    page_size: pageSize.toString(),
  })
  const payload = await requestJson<PaginatedWire<PaginatedMassEmailBatchItemsResponse['items'][number]>>(
    `/mass-email/batches/${encodeURIComponent(id)}/items?${params.toString()}`,
  )
  return normalizePaginated(payload)
}

export const cancelMassEmailBatch = async (id: string): Promise<MassEmailBatch> => (
  requestJson<MassEmailBatch>(`/mass-email/batches/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
  })
)
