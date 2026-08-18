import type {
  EmailTemplate,
  NotificationChannelSettings,
  SaveEmailTemplatePayload,
  SaveSmtpSettingsPayload,
  SmtpSettings,
  StrategySettings,
  TestNotificationChannelPayload,
  TestNotificationChannelResponse,
  TestSmtpEmailPayload,
  TestSmtpEmailResponse,
  TestEmailTemplatePayload,
  TestEmailTemplateResponse,
  SecuritySettings,
} from '../types/settings'
import { requestJson as sharedRequestJson } from '@/lib/apiClient'

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.settings.errors.network',
    request: 'admin.settings.errors.request',
  })

export const testNotificationChannel = async (
  payload: TestNotificationChannelPayload,
): Promise<TestNotificationChannelResponse> => (
  requestJson<TestNotificationChannelResponse>('/settings/notification-channels/test', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
)

export const getStrategySettings = async (): Promise<StrategySettings> => (
  requestJson<StrategySettings>('/settings/strategy')
)

export const saveStrategySettings = async (settings: StrategySettings): Promise<StrategySettings> => (
  requestJson<StrategySettings>('/settings/strategy', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
)

export const getNotificationChannelSettings = async (): Promise<NotificationChannelSettings> => (
  requestJson<NotificationChannelSettings>('/settings/notification-channels')
)

export const saveNotificationChannelSettings = async (
  settings: NotificationChannelSettings,
): Promise<NotificationChannelSettings> => (
  requestJson<NotificationChannelSettings>('/settings/notification-channels', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
)

export const getEmailTemplates = async (): Promise<EmailTemplate[]> => (
  requestJson<EmailTemplate[]>('/settings/email-templates')
)

export const createEmailTemplate = async (payload: SaveEmailTemplatePayload): Promise<EmailTemplate> => (
  requestJson<EmailTemplate>('/settings/email-templates', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
)

export const updateEmailTemplate = async (id: string, payload: SaveEmailTemplatePayload): Promise<EmailTemplate> => (
  requestJson<EmailTemplate>(`/settings/email-templates/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
)

export const deleteEmailTemplate = async (id: string): Promise<Record<string, never>> => (
  requestJson<Record<string, never>>(`/settings/email-templates/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
)

export const testEmailTemplate = async (
  id: string,
  payload: TestEmailTemplatePayload,
): Promise<TestEmailTemplateResponse> => (
  requestJson<TestEmailTemplateResponse>(`/settings/email-templates/${encodeURIComponent(id)}/test-email`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
)

export const getSmtpSettings = async (): Promise<SmtpSettings> => (
  requestJson<SmtpSettings>('/settings/smtp')
)

export const saveSmtpSettings = async (payload: SaveSmtpSettingsPayload): Promise<SmtpSettings> => (
  requestJson<SmtpSettings>('/settings/smtp', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
)

export const testSmtpEmail = async (payload: TestSmtpEmailPayload): Promise<TestSmtpEmailResponse> => (
  requestJson<TestSmtpEmailResponse>('/settings/smtp/test-email', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
)

export const getSecuritySettings = async (): Promise<SecuritySettings> => (
  requestJson<SecuritySettings>('/settings/security')
)

export const saveSecuritySettings = async (settings: SecuritySettings): Promise<SecuritySettings> => (
  requestJson<SecuritySettings>('/settings/security', {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
)
