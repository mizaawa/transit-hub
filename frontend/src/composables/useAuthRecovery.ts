import { ref, onMounted } from 'vue'
import { getAccessToken, handleAuthExpired } from '@/modules/auth/api/auth'

/**
 * 认证恢复 composable：
 * 1. 检测页面加载时的登录状态
 * 2. 提供会话恢复提示
 * 3. 处理token过期场景
 */
export function useAuthRecovery() {
  const hasValidToken = ref(false)
  const isChecking = ref(true)

  const checkAuthStatus = () => {
    const token = getAccessToken()
    hasValidToken.value = !!token

    if (!token && requiresAuth()) {
      // 如果需要认证但没有token，重定向到登录页
      handleAuthExpired()
    }

    isChecking.value = false
  }

  const requiresAuth = (): boolean => {
    // 检查当前路由是否需要认证
    const publicRoutes = ['/login', '/register', '/']
    return !publicRoutes.includes(window.location.pathname)
  }

  onMounted(() => {
    checkAuthStatus()
  })

  return {
    hasValidToken,
    isChecking,
    checkAuthStatus,
  }
}
