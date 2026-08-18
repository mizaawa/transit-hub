/**
 * 请求重试工具：针对网络抖动和临时故障提供容错能力。
 * 只对网络错误重试，不对业务错误（4xx/5xx）重试。
 */

export interface RetryOptions {
  maxRetries?: number
  retryDelay?: number
  shouldRetry?: (error: unknown) => boolean
}

const defaultOptions: Required<RetryOptions> = {
  maxRetries: 3,
  retryDelay: 1000,
  shouldRetry: (error: unknown) => {
    // 只对网络错误重试，不对业务错误重试
    if (error instanceof Error) {
      return error.message.includes('network') || error.message === 'Failed to fetch'
    }
    return false
  },
}

/**
 * 带指数退避的延迟函数
 */
const delay = (ms: number, attempt: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms * Math.pow(2, attempt - 1)))

/**
 * 包装异步函数，添加重试逻辑
 */
export async function withRetry<T>(
  fn: () => Promise<T>,
  options: RetryOptions = {},
): Promise<T> {
  const opts = { ...defaultOptions, ...options }
  let lastError: unknown

  for (let attempt = 1; attempt <= opts.maxRetries; attempt++) {
    try {
      return await fn()
    } catch (error) {
      lastError = error
      
      if (attempt === opts.maxRetries || !opts.shouldRetry(error)) {
        throw error
      }

      // 等待后重试
      await delay(opts.retryDelay, attempt)
    }
  }

  throw lastError
}
