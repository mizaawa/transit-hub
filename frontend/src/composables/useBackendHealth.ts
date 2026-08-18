import { ref, onMounted, onUnmounted } from 'vue'
import { endpoint } from '@/lib/apiClient'

export interface HealthStatus {
  isHealthy: boolean
  isChecking: boolean
  lastCheck: Date | null
  error: string | null
  dependencies?: {
    database?: string
    redis?: string
    database_connections?: string
  }
}

/**
 * 后端健康检查 composable
 * 定期探测后端服务状态，在连接失败时提供重连能力
 */
export function useBackendHealth(options: {
  checkInterval?: number
  autoStart?: boolean
  onHealthChange?: (isHealthy: boolean) => void
} = {}) {
  const {
    checkInterval = 30000, // 默认 30 秒检查一次
    autoStart = true,
    onHealthChange,
  } = options

  const status = ref<HealthStatus>({
    isHealthy: true,
    isChecking: false,
    lastCheck: null,
    error: null,
  })

  let intervalId: number | null = null
  let retryCount = 0
  const maxRetries = 3

  /**
   * 检查后端健康状态
   */
  const checkHealth = async (): Promise<boolean> => {
    if (status.value.isChecking) return status.value.isHealthy

    status.value.isChecking = true

    try {
      const response = await fetch(endpoint('/health/detailed'), {
        method: 'GET',
        headers: { Accept: 'application/json' },
        signal: AbortSignal.timeout(5000), // 5 秒超时
      })

      const data = await response.json()
      const wasHealthy = status.value.isHealthy
      const isHealthy = response.ok && data.status === 'healthy'

      status.value = {
        isHealthy,
        isChecking: false,
        lastCheck: new Date(),
        error: isHealthy ? null : data.status || 'Backend is unhealthy',
        dependencies: data.dependencies,
      }

      retryCount = 0 // 成功后重置重试计数

      if (wasHealthy !== isHealthy && onHealthChange) {
        onHealthChange(isHealthy)
      }

      return isHealthy
    } catch (error) {
      const wasHealthy = status.value.isHealthy

      status.value = {
        isHealthy: false,
        isChecking: false,
        lastCheck: new Date(),
        error: error instanceof Error ? error.message : 'Backend connection failed',
      }

      retryCount++

      if (wasHealthy && onHealthChange) {
        onHealthChange(false)
      }

      return false
    }
  }

  /**
   * 启动定期检查
   */
  const startMonitoring = () => {
    if (intervalId !== null) return

    // 立即检查一次
    checkHealth()

    intervalId = window.setInterval(() => {
      checkHealth()
    }, checkInterval)
  }

  /**
   * 停止定期检查
   */
  const stopMonitoring = () => {
    if (intervalId !== null) {
      clearInterval(intervalId)
      intervalId = null
    }
  }

  /**
   * 手动触发健康检查并重置重试计数
   */
  const refresh = async () => {
    retryCount = 0
    return await checkHealth()
  }

  onMounted(() => {
    if (autoStart) {
      startMonitoring()
    }
  })

  onUnmounted(() => {
    stopMonitoring()
  })

  return {
    status,
    checkHealth,
    startMonitoring,
    stopMonitoring,
    refresh,
    retryCount: () => retryCount,
    maxRetries,
  }
}
