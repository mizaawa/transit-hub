import type {
  SiteSettings,
  SyncStreamEvent,
  UpstreamSiteForm,
  UpstreamSiteResponse,
} from '../types/upstream'
import {
  authUnauthorizedErrorKey,
  handleAuthExpired,
  isUnauthorizedApiResponse,
} from '@/modules/auth/api/auth'
import { authHeaders, endpoint, requestJson as sharedRequestJson } from '@/lib/apiClient'

const STREAM_CONNECT_TIMEOUT_MS = 15_000
// The backend request middleware allows up to 60 seconds for a slow upstream.
// Keep a small margin so a valid but quiet stream is not aborted prematurely.
const STREAM_READ_TIMEOUT_MS = 75_000

interface StreamAbortController {
  controller: AbortController
  cleanup: () => void
}

/** Merge a caller cancellation signal into the stream's own timeout controller. */
const createStreamAbortController = (callerSignal?: AbortSignal): StreamAbortController => {
  const controller = new AbortController()
  let removeCallerListener: (() => void) | undefined

  const abortFromCaller = () => controller.abort()
  if (callerSignal) {
    if (callerSignal.aborted) {
      controller.abort()
    } else {
      callerSignal.addEventListener('abort', abortFromCaller, { once: true })
      removeCallerListener = () => callerSignal.removeEventListener('abort', abortFromCaller)
    }
  }

  return {
    controller,
    cleanup: () => removeCallerListener?.(),
  }
}

// 共享请求层负责：空 base URL 回退、非 JSON 响应降级、401 统一登出。
const requestJson = async <T>(path: string, options: RequestInit = {}): Promise<T> =>
  sharedRequestJson<T>(path, options, {
    network: 'admin.upstream.errors.network',
    request: 'admin.upstream.errors.request',
  })

export const listUpstreamSites = async (): Promise<UpstreamSiteResponse[]> => requestJson<UpstreamSiteResponse[]>('/upstream-sites')

export const createUpstreamSite = async (form: UpstreamSiteForm): Promise<UpstreamSiteResponse> => (
  requestJson<UpstreamSiteResponse>('/upstream-sites', {
    method: 'POST',
    body: JSON.stringify(form),
  })
)

export const updateUpstreamSite = async (id: string, form: UpstreamSiteForm): Promise<UpstreamSiteResponse> => (
  requestJson<UpstreamSiteResponse>(`/upstream-sites/${id}`, {
    method: 'PUT',
    body: JSON.stringify(form),
  })
)

export const syncUpstreamSite = async (id: string): Promise<UpstreamSiteResponse> => (
  requestJson<UpstreamSiteResponse>(`/upstream-sites/${id}/sync`, { method: 'POST' })
)

export const syncAllUpstreamSites = async (): Promise<UpstreamSiteResponse[]> => (
  requestJson<UpstreamSiteResponse[]>('/upstream-sites/sync-all', { method: 'POST' })
)

export const removeUpstreamSite = async (id: string): Promise<void> => {
  await requestJson<{ success: boolean }>(`/upstream-sites/${id}`, { method: 'DELETE' })
}

export const updateSiteSettings = async (id: string, settings: SiteSettings): Promise<UpstreamSiteResponse> => (
  requestJson<UpstreamSiteResponse>(`/upstream-sites/${id}/settings`, {
    method: 'PATCH',
    body: JSON.stringify(settings),
  })
)

/** 以 SSE 流方式逐站同步，每个站点的进度通过 onEvent 回调实时推送。 */
export const streamSyncAllUpstreamSites = async (
  onEvent: (event: SyncStreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> => {
  const streamAbort = createStreamAbortController(signal)
  const controller = streamAbort.controller
  let connectTimeoutId: ReturnType<typeof setTimeout> | undefined
  let response: Response
  try {
    connectTimeoutId = setTimeout(() => controller.abort(), STREAM_CONNECT_TIMEOUT_MS)
    response = await fetch(endpoint('/upstream-sites/sync-stream'), {
      headers: { Accept: 'text/event-stream', ...authHeaders() },
      signal: controller.signal,
    })
  } catch (error) {
    if (connectTimeoutId !== undefined) clearTimeout(connectTimeoutId)
    streamAbort.cleanup()
    const normalizedError = new Error('admin.upstream.errors.network')
    normalizedError.name = 'AbortError'
    ;(normalizedError as Error & { cause?: unknown }).cause = error
    throw normalizedError
  }
  if (connectTimeoutId !== undefined) clearTimeout(connectTimeoutId)

  if (!response.ok) {
    if (isUnauthorizedApiResponse(response.status, {})) {
      handleAuthExpired()
      streamAbort.cleanup()
      throw new Error(authUnauthorizedErrorKey)
    }
    streamAbort.cleanup()
    throw new Error('admin.upstream.errors.request')
  }

  const reader = response.body?.getReader()
  if (!reader) {
    streamAbort.cleanup()
    throw new Error('admin.upstream.errors.network')
  }

  const decoder = new TextDecoder()
  let buffer = ''
  let completed = false

  try {
    while (!completed) {
      let readTimeoutId: ReturnType<typeof setTimeout> | undefined
      const timeoutPromise = new Promise<never>((_, reject) => {
        readTimeoutId = setTimeout(() => {
          controller.abort()
          const timeoutError = new Error('admin.upstream.errors.network')
          timeoutError.name = 'AbortError'
          reject(timeoutError)
        }, STREAM_READ_TIMEOUT_MS)
      })

      let result: ReadableStreamReadResult<Uint8Array>
      try {
        result = await Promise.race([reader.read(), timeoutPromise])
      } finally {
        if (readTimeoutId !== undefined) clearTimeout(readTimeoutId)
      }

      if (result.done) break
      buffer += decoder.decode(result.value, { stream: true })
      const parts = buffer.split('\n\n')
      buffer = parts.pop()!
      for (const part of parts) {
        const dataLine = part.split('\n').find(l => l.startsWith('data: '))
        if (!dataLine) continue
        try {
          const event = JSON.parse(dataLine.slice(6)) as SyncStreamEvent
          onEvent(event)
          if (event.event === 'complete') {
            completed = true
            break
          }
        } catch { /* skip malformed lines */ }
      }
    }
  } finally {
    try {
      // AbortController already closes the network body on timeout/cancel.
      // Do not await cancel: a broken body stream must not keep the caller's
      // refresh state pending indefinitely.
      void reader.cancel().catch(() => undefined)
    } catch {
      // The stream may already be closed or aborted.
    }
    try {
      reader.releaseLock()
    } catch {
      // Ignore a stream that has already released its lock.
    }
    streamAbort.cleanup()
  }
}
