// API 请求重试机制
interface RetryConfig {
  maxRetries?: number
  baseDelay?: number
  maxDelay?: number
  shouldRetry?: (error: any, attempt: number) => boolean
}

const defaultConfig: Required<RetryConfig> = {
  maxRetries: 3,
  baseDelay: 1000,
  maxDelay: 10000,
  shouldRetry: (error: any, attempt: number) => {
    // 网络错误或 5xx 错误才重试
    if (error.name === 'TypeError' || error.message?.includes('fetch')) {
      return true
    }
    if (error.status >= 500 && error.status < 600) {
      return true
    }
    // 408 Request Timeout, 429 Too Many Requests, 503 Service Unavailable
    if ([408, 429, 503].includes(error.status)) {
      return true
    }
    return false
  }
}

function calculateDelay(attempt: number, baseDelay: number, maxDelay: number): number {
  // 指数退避 + 随机抖动
  const exponentialDelay = Math.min(baseDelay * Math.pow(2, attempt), maxDelay)
  const jitter = Math.random() * 0.3 * exponentialDelay
  return exponentialDelay + jitter
}

export async function fetchWithRetry<T>(
  url: string,
  options: RequestInit = {},
  retryConfig: RetryConfig = {}
): Promise<T> {
  const config = { ...defaultConfig, ...retryConfig }
  let lastError: any

  for (let attempt = 0; attempt <= config.maxRetries; attempt++) {
    try {
      const response = await fetch(url, {
        ...options,
        signal: options.signal || AbortSignal.timeout(30000)
      })

      if (!response.ok) {
        const error: any = new Error(`HTTP ${response.status}: ${response.statusText}`)
        error.status = response.status
        error.response = response
        throw error
      }

      return await response.json()
    } catch (error: any) {
      lastError = error

      // 最后一次尝试失败，直接抛出
      if (attempt === config.maxRetries) {
        console.error(`[api-retry] Request failed after ${config.maxRetries} retries:`, url)
        throw error
      }

      // 判断是否需要重试
      if (!config.shouldRetry(error, attempt)) {
        console.error('[api-retry] Error not retryable:', error.message)
        throw error
      }

      // 计算延迟并等待
      const delay = calculateDelay(attempt, config.baseDelay, config.maxDelay)
      console.warn(`[api-retry] Attempt ${attempt + 1} failed, retrying in ${Math.round(delay)}ms...`)
      await new Promise(resolve => setTimeout(resolve, delay))
    }
  }

  throw lastError
}

// 为现有的 apiClient 添加重试包装
export function wrapWithRetry(apiClient: any) {
  const originalFetch = apiClient.fetch?.bind(apiClient)
  
  if (originalFetch) {
    apiClient.fetch = async function (url: string, options?: RequestInit) {
      return fetchWithRetry(url, options)
    }
  }
  
  return apiClient
}
