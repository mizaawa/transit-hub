import { ref, onMounted, onUnmounted } from 'vue'

/**
 * 监听浏览器在线/离线状态，用于在断网时显示提示。
 */
export function useOnlineStatus() {
  const isOnline = ref(navigator.onLine)

  const handleOnline = () => {
    isOnline.value = true
  }

  const handleOffline = () => {
    isOnline.value = false
  }

  onMounted(() => {
    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)
  })

  onUnmounted(() => {
    window.removeEventListener('online', handleOnline)
    window.removeEventListener('offline', handleOffline)
  })

  return { isOnline }
}
