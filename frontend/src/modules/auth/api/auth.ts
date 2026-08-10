import type {
  AuthTokenResponse,
  EmailCodeRequest,
  EmailCodeResponse,
  LoginRequest,
  RegisterRequest,
} from '../types/auth'
import { resetWorkspaceCheck } from '@/lib/workspaceGuard'

export const authTokenStorageKey = 'transithub.auth.accessToken'
export const authUnauthorizedErrorKey = 'auth.errors.unauthorized'

type ApiErrorPayload = {
  message?: string
}

/**
 * 解析 VITE_API_BASE_URL。空字符串必须回退到 '/api'，否则请求会丢掉 /api 前缀、
 * 命中前端 history 回退并拿到 index.html，导致 JSON 解析报
 * `Unexpected token '<', "<script sr"... is not valid JSON`。
 *
 * 这里刻意不从 @/lib/apiClient 引入：apiClient 依赖本文件的 token/登出能力，
 * 反向引用会形成模块循环。
 */
const resolveApiBaseUrl = (): string => {
  const raw = import.meta.env.VITE_API_BASE_URL
  const trimmed = typeof raw === 'string' ? raw.trim() : ''
  return trimmed === '' ? '/api' : trimmed
}

const apiBaseUrl = resolveApiBaseUrl()

const endpoint = (path: string): string => `${apiBaseUrl.replace(/\/$/, '')}${path}`

const requestJson = async <T>(path: string, options: RequestInit = {}, errorKey = 'auth.errors.unknown'): Promise<T> => {
  let response: Response
  try {
    response = await fetch(endpoint(path), {
      ...options,
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        ...(options.headers ?? {}),
      },
    })
  } catch (error) {
    throw new Error('auth.errors.network')
  }

  const text = await response.text()
  const contentType = response.headers.get('Content-Type') ?? ''
  let payload = {} as T & { message?: string }
  let parsed = text.trim() === ''
  if (!parsed && contentType.includes('json')) {
    try {
      payload = JSON.parse(text) as T & { message?: string }
      parsed = true
    } catch {
      parsed = false
    }
  }

  if (!response.ok) {
    throw new Error(payload.message ?? errorKey)
  }

  // 2xx 但响应体不是 JSON：通常是请求被错误地路由到了前端静态资源。
  // 报告模块自己的错误 key，不要把 SyntaxError 抛给界面。
  if (!parsed) {
    throw new Error(errorKey)
  }

  return payload
}

export const requestEmailCode = async (form: EmailCodeRequest): Promise<EmailCodeResponse> => (
  requestJson<EmailCodeResponse>('/auth/email-code', {
    method: 'POST',
    body: JSON.stringify(form),
  }, 'auth.register.errors.codeRequest')
)

export const registerWithEmail = async (form: RegisterRequest): Promise<AuthTokenResponse> => (
  requestJson<AuthTokenResponse>('/auth/register', {
    method: 'POST',
    body: JSON.stringify(form),
  }, 'auth.register.errors.register')
)

export const loginWithEmail = async (form: LoginRequest): Promise<AuthTokenResponse> => (
  requestJson<AuthTokenResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify(form),
  }, 'auth.login.errors.login')
)

export const storeAccessToken = (accessToken: string): void => {
  localStorage.setItem(authTokenStorageKey, accessToken)
}

export const getAccessToken = (): string | null => localStorage.getItem(authTokenStorageKey)

export const clearAccessToken = (): void => {
  localStorage.removeItem(authTokenStorageKey)
  // 清除 token 时同步重置 workspace 路由守卫缓存，
  // 防止下次登录（可能是不同用户）复用旧 workspace 状态。
  resetWorkspaceCheck()
}

export const isUnauthorizedApiResponse = (status: number, payload: ApiErrorPayload): boolean => (
  status === 401 || payload.message === authUnauthorizedErrorKey
)

// 登录状态过期时的统一处理：清除本地登录态并跳转登录页。
// 所有检测到 401 / authUnauthorizedErrorKey 的请求层都应调用它，
// 避免在各个页面/组件里各自重复实现跳转逻辑。
// 使用整页跳转（而非 router.push）以同时重置内存中的组件/store 状态，
// 并避免 auth.ts 与 router.ts 之间产生循环依赖。
export const handleAuthExpired = (): void => {
  clearAccessToken()
  if (typeof window === 'undefined') return
  if (window.location.pathname === '/login') return
  window.location.href = '/login'
}
