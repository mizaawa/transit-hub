import {
  type AdminAccount,
  type DeleteAdminAccountResponse,
  type WorkspaceDeleteConfirmation,
} from '../types/adminAccounts'
import { requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.adminAccounts.errors.network',
    request: 'admin.adminAccounts.errors.request',
  })

export const listAdminAccounts = async (): Promise<AdminAccount[]> =>
  requestJson<AdminAccount[]>('/admin-accounts')

export const getCurrentAdminAccount = async (): Promise<AdminAccount> =>
  requestJson<AdminAccount>('/admin-accounts/current')

export const switchAdminAccount = async (id: string): Promise<AdminAccount> =>
  requestJson<AdminAccount>('/admin-accounts/current', {
    method: 'POST',
    body: JSON.stringify({ id }),
  })

export const updateAdminAccount = async (id: string, displayName: string): Promise<AdminAccount> =>
  requestJson<AdminAccount>(`/admin-accounts/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ displayName }),
  })

export const deleteAdminAccount = async (
  id: string,
  confirmation: WorkspaceDeleteConfirmation,
): Promise<DeleteAdminAccountResponse> =>
  requestJson<DeleteAdminAccountResponse>(`/admin-accounts/${id}`, {
    method: 'DELETE',
    body: JSON.stringify({ confirmation }),
  })
