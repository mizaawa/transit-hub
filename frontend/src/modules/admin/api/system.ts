import { requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.system.errors.network',
    request: 'admin.system.errors.request',
  })

export interface SystemVersionResponse {
  version: string
}

export const getSystemVersion = async (): Promise<SystemVersionResponse> => (
  requestJson<SystemVersionResponse>('/system/version')
)
