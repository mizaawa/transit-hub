<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const isHealthy = ref(true)
const isChecking = ref(false)
const lastError = ref<string>('')
const consecutiveFailures = ref(0)
let checkInterval: number | null = null

const checkBackendHealth = async () => {
  if (isChecking.value) return
  
  isChecking.value = true
  
  try {
    const response = await fetch('/api/health', {
      method: 'GET',
      headers: { 'Content-Type': 'application/json' },
      signal: AbortSignal.timeout(5000)
    })
    
    if (!response.ok) {
      throw new Error(`Backend returned ${response.status}`)
    }
    
    const data = await response.json()
    
    if (data.status !== 'ok') {
      throw new Error('Backend health check failed')
    }
    
    // 恢复正常
    if (!isHealthy.value) {
      console.log('[backend-status] Backend recovered')
    }
    isHealthy.value = true
    consecutiveFailures.value = 0
    lastError.value = ''
    
  } catch (error: any) {
    consecutiveFailures.value++
    lastError.value = error.message || 'Unknown error'
    
    // 连续失败 2 次才标记为不健康，避免误报
    if (consecutiveFailures.value >= 2) {
      isHealthy.value = false
      console.error('[backend-status] Backend unhealthy:', lastError.value)
    }
  } finally {
    isChecking.value = false
  }
}

onMounted(() => {
  // 首次立即检查
  checkBackendHealth()
  
  // 每 10 秒检查一次
  checkInterval = window.setInterval(checkBackendHealth, 10000)
})

onUnmounted(() => {
  if (checkInterval !== null) {
    clearInterval(checkInterval)
  }
})
</script>

<template>
  <div v-if="!isHealthy" class="backend-status-indicator">
    <div class="content">
      <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor">
        <circle cx="12" cy="12" r="10" stroke-width="2"/>
        <path d="M12 8v4M12 16h.01" stroke-width="2" stroke-linecap="round"/>
      </svg>
      <span class="message">后端服务连接异常，正在重试...</span>
    </div>
  </div>
</template>

<style scoped>
.backend-status-indicator {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 9999;
  background: linear-gradient(135deg, #dc2626 0%, #b91c1c 100%);
  color: #fff;
  padding: 0.75rem 1rem;
  box-shadow: 0 4px 12px rgba(220, 38, 38, 0.3);
  animation: slideDown 0.3s ease-out;
}

@keyframes slideDown {
  from {
    transform: translateY(-100%);
    opacity: 0;
  }
  to {
    transform: translateY(0);
    opacity: 1;
  }
}

.content {
  max-width: 1200px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.icon {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
}

.message {
  font-size: 0.875rem;
  font-weight: 500;
  letter-spacing: 0.01em;
}
</style>
