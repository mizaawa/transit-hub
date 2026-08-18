import { ref, onMounted, onUnmounted } from 'vue'

/**
 * 连接监控 composable：
 * 1. 监控网络连接状态
 * 2. 定期检查后端健康状态
 * 3. 提供重连机制
 */
export function useConnectionMonitor(options = { checkInterval: 30000 }) {
  const isOnline = ref(navigator.onLine)
  const backendHealthy = ref(true)
  const lastCheckTime = ref<Date | null>(null)
  let checkIntervalId: number | null = null

  const updateOnlineStatus = () => {
    isOnline.value = navigator.onLine
  }

  const checkBackendHealth = async () => {
    try {
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), 5000)

      const response = await fetch('/api/health', {
        method: 'GET',
        signal: controller.signal,
      })

      clearTimeout(timeoutId)
      backendHealthy.value = response.ok
      lastCheckTime.value = new Date()
    } catch (error) {
      backendHealthy.value = false
      lastCheckTime.value = new Date()
      console.warn('[ConnectionMonitor] Backend health check failed:', error)
    }
  }

  const startMonitoring = () => {
    // 立即执行一次检查
    checkBackendHealth()

    // 定期检查后端健康状态
    checkIntervalId = window.setInterval(() => {
      if (isOnline.value) {
        checkBackendHealth()
      }
    }, options.checkInterval)
  }

  const stopMonitoring = () => {
    if (checkIntervalId !== null) {
      clearInterval(checkIntervalId)
      checkIntervalId = null
    }
  }

  onMounted(() => {
    // 监听网络状态变化
    window.addEventListener('online', updateOnlineStatus)
    window.addEventListener('offline', updateOnlineStatus)

    // 开始监控
    startMonitoring()
  })

  onUnmounted(() => {
    window.removeEventListener('online', updateOnlineStatus)
    window.removeEventListener('offline', updateOnlineStatus)
    stopMonitoring()
  })

  return {
    isOnline,
    backendHealthy,
    lastCheckTime,
    checkBackendHealth,
  }
}
