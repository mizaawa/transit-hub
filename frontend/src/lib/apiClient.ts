import {
  authUnauthorizedErrorKey,
  getAccessToken,
  handleAuthExpired,
  isUnauthorizedApiResponse,
  refreshAccessTokenIfNeeded,
} from '@/modules/auth/api/auth'
import { withRetry } from './requestRetry'

/**
 * 所有请求层共用的错误载荷：后端统一返回 { message: i18n key }。
 */
export type ApiErrorPayload = {
  message?: string
}

/**
 * 解析 VITE_API_BASE_URL。
 *
 * 这里必须用「空值也回退」而不是 `?? '/api'`：`??` 只在 null/undefined 时回退，
 * 当 .env 写成 `VITE_API_BASE_URL=`（空字符串）时会得到 ''，于是 `/dashboard/admin/refresh`
 * 这类请求不再带 `/api` 前缀，被后端当成前端路由返回 index.html，
 * 前端再对 HTML 做 JSON.parse 就会抛出
 * `Unexpected token '<', "<script sr"... is not valid JSON`。
 */
const resolveApiBaseUrl = (): string => {
  const raw = import.meta.env.VITE_API_BASE_URL
  const trimmed = typeof raw === 'string' ? raw.trim() : ''
  return trimmed === '' ? '/api' : trimmed
}

export const apiBaseUrl = resolveApiBaseUrl()

// A request that never produces a response must not leave an admin view in a
// permanent loading state.  The timeout is applied per retry attempt below.
export const DEFAULT_API_REQUEST_TIMEOUT_MS = 30_000

interface RequestSignal {
  signal: AbortSignal
  cleanup: () => void
}

/** Combine a caller cancellation signal with the default request deadline. */
const createRequestSignal = (
  callerSignal: AbortSignal | null | undefined,
  timeoutMs = DEFAULT_API_REQUEST_TIMEOUT_MS,
): RequestSignal => {
  const controller = new AbortController()
  let timeoutId: ReturnType<typeof setTimeout> | undefined
  let removeCallerListener: (() => void) | undefined

  const abortFromCaller = () => controller.abort(callerSignal?.reason)
  if (callerSignal) {
    if (callerSignal.aborted) {
      controller.abort(callerSignal.reason)
    } else {
      callerSignal.addEventListener('abort', abortFromCaller, { once: true })
      removeCallerListener = () => callerSignal.removeEventListener('abort', abortFromCaller)
    }
  }

  timeoutId = setTimeout(() => controller.abort(), timeoutMs)

  return {
    signal: controller.signal,
    cleanup: () => {
      if (timeoutId !== undefined) clearTimeout(timeoutId)
      removeCallerListener?.()
    },
  }
}

const isAbortError = (error: unknown): boolean => (
  error instanceof Error && error.name === 'AbortError'
)

const normalizedAbortError = (networkErrorKey: string, cause: unknown): Error => {
  const error = new Error(networkErrorKey)
  // requestRetry deliberately does not retry caller cancellation or a timeout.
  error.name = 'AbortError'
  ;(error as Error & { cause?: unknown }).cause = cause
  return error
}

const waitForAbort = (signal: AbortSignal, networkErrorKey: string): { promise: Promise<never>; cleanup: () => void } => {
  let onAbort: () => void = () => undefined
  const promise = new Promise<never>((_, reject) => {
    onAbort = () => reject(normalizedAbortError(networkErrorKey, signal.reason))
    if (signal.aborted) onAbort()
    else signal.addEventListener('abort', onAbort, { once: true })
  })
  return {
    promise,
    cleanup: () => signal.removeEventListener('abort', onAbort),
  }
}

/** 拼接完整请求地址，去掉 base 末尾多余的斜杠。 */
export const endpoint = (path: string): string => `${apiBaseUrl.replace(/\/$/, '')}${path}`

/** 已登录时附带 TransitHub 鉴权头；未登录时返回空对象。 */
export const authHeaders = (): Record<string, string> => {
  const token = getAccessToken()
  if (!token) return {}
  return { Authorization: `Bearer ${token}` }
}

/**
 * 每个模块的错误 i18n key。network 用于 fetch 本身失败，
 * request 用于非 2xx 或响应体不是合法 JSON。
 */
export interface ApiErrorKeys {
  network: string
  request: string
}

/**
 * 带 HTTP 状态码的错误。message 仍然是 i18n key，保持既有调用方
 * `t(error.message)` 的用法不变；status 供需要区分「接口不存在」和
 * 「请求被拒绝」的调用方使用（例如新旧后端滚动升级时的降级逻辑）。
 */
export class ApiRequestError extends Error {
  readonly status: number

  constructor(messageKey: string, status: number) {
    super(messageKey)
    this.name = 'ApiRequestError'
    this.status = status
  }
}

/** 判断错误是否代表「后端没有这个接口/方法」，用于滚动升级降级。 */
export const isEndpointUnsupported = (error: unknown): boolean => (
  error instanceof ApiRequestError && (error.status === 404 || error.status === 405)
)

/**
 * 判断响应体是否声明为 JSON。反向代理故障、网关错误页和 SPA 回退
 * 都会返回 text/html，此时不应该尝试 JSON.parse。
 */
const declaresJson = (response: Response): boolean => {
  const contentType = response.headers.get('Content-Type') ?? ''
  return contentType.includes('json')
}

/**
 * 统一的 JSON 请求封装。
 *
 * 健壮性要点：
 *  - fetch 抛错（断网/CORS）归一化为 network key；
 *  - 响应体为空或不是合法 JSON 时不把 SyntaxError 泄漏到界面，
 *    而是降级成模块自己的 request key，由调用方 t() 渲染；
 *  - 401 或 message 为未授权 key 时统一走 handleAuthExpired，
 *    即使响应体是 HTML（会话过期后被网关重定向到登录页）也能正确跳转；
 *  - FormData 请求不设置 Content-Type，交给浏览器生成带 boundary 的值；
 *  - 网络错误自动重试 3 次，提高容错能力。
 */
export const requestJson = async <T>(
  path: string,
  options: RequestInit = {},
  errorKeys: ApiErrorKeys,
): Promise<T> => {
  return withRetry(async () => {
    const requestSignal = createRequestSignal(options.signal)
    const abortWait = waitForAbort(requestSignal.signal, errorKeys.network)
    try {
      if (requestSignal.signal.aborted) {
        throw normalizedAbortError(errorKeys.network, requestSignal.signal.reason)
      }

      // 请求前尝试刷新即将过期的 token，避免请求到一半 token 失效。
      // The internal signal still bounds the fetch/body read below.
      await Promise.race([refreshAccessTokenIfNeeded(), abortWait.promise])
      if (requestSignal.signal.aborted) {
        throw normalizedAbortError(errorKeys.network, requestSignal.signal.reason)
      }

      const isFormData = typeof FormData !== 'undefined' && options.body instanceof FormData

      let response: Response
      try {
        response = await Promise.race([
          fetch(endpoint(path), {
            ...options,
            signal: requestSignal.signal,
            headers: {
              Accept: 'application/json',
              ...(isFormData ? {} : { 'Content-Type': 'application/json' }),
              ...authHeaders(),
              ...(options.headers ?? {}),
            },
          }),
          abortWait.promise,
        ])
      } catch (error) {
        if (requestSignal.signal.aborted || isAbortError(error)) {
          throw normalizedAbortError(errorKeys.network, error)
        }
        throw new Error(errorKeys.network)
      }

      let text: string
      try {
        // Some Response implementations do not reject text() when the
        // underlying signal is aborted. Race the body read explicitly so a
        // stalled proxy/body cannot leave callers loading forever.
        text = await Promise.race([response.text(), abortWait.promise])
      } catch (error) {
        if (requestSignal.signal.aborted || isAbortError(error)) {
          throw normalizedAbortError(errorKeys.network, error)
        }
        throw new Error(errorKeys.network)
      }

      let payload = {} as T & ApiErrorPayload
      let parsed = text.trim() === ''
      if (!parsed && declaresJson(response)) {
        try {
          payload = JSON.parse(text) as T & ApiErrorPayload
          parsed = true
        } catch {
          parsed = false
        }
      }

      if (!response.ok) {
        // 会话过期的响应有时并非 JSON（例如被网关改写成登录页），
        // 因此仅凭状态码也要能触发登出跳转。
        if (isUnauthorizedApiResponse(response.status, payload)) {
          handleAuthExpired()
          throw new Error(authUnauthorizedErrorKey)
        }
        throw new ApiRequestError(payload.message ?? errorKeys.request, response.status)
      }

      if (!parsed) {
        // 2xx 但响应体不是 JSON：几乎总是请求被错误地路由到了前端静态资源
        // （base URL 配置为空、反向代理规则缺少 /api 转发）。
        throw new ApiRequestError(errorKeys.request, response.status)
      }

      return payload
    } finally {
      abortWait.cleanup()
      requestSignal.cleanup()
    }
  })
}
