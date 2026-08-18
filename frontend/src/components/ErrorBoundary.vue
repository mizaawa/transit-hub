<script setup lang="ts">
import { ref, onErrorCaptured } from 'vue'

const error = ref<Error | null>(null)
const errorDetails = ref<string>('')

onErrorCaptured((err) => {
  console.error('[ErrorBoundary]', err)
  error.value = err instanceof Error ? err : new Error(String(err))

  // 提取更详细的错误信息以帮助调试
  if (err instanceof Error) {
    errorDetails.value = err.stack || ''

    // 识别常见错误类型并提供友好提示
    if (err.message.includes('Failed to fetch') || err.message.includes('network')) {
      error.value.message = '网络连接失败，请检查网络设置或稍后重试'
    } else if (err.message.includes('JSON') || err.message.includes('Unexpected token')) {
      error.value.message = 'API 配置错误，请检查后端服务是否正常运行'
    } else if (err.message.includes('module') || err.message.includes('import')) {
      error.value.message = '模块加载失败，请清除浏览器缓存后重试'
    }
  }

  return false
})

const reload = () => {
  error.value = null
  errorDetails.value = ''
  window.location.reload()
}

const clearCache = () => {
  // 清除localStorage和sessionStorage
  localStorage.clear()
  sessionStorage.clear()

  // 清除Service Worker缓存（如果存在）
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.getRegistrations().then((registrations) => {
      registrations.forEach((registration) => registration.unregister())
    })
  }

  // 清除缓存API
  if ('caches' in window) {
    caches.keys().then((names) => {
      names.forEach((name) => caches.delete(name))
    })
  }

  window.location.reload()
}

const showDetails = ref(false)
</script>

<template>
  <div v-if="error" class="error-boundary">
    <div class="error-card">
      <div class="error-icon">⚠️</div>
      <h2>应用遇到错误</h2>
      <p class="error-message">{{ error.message }}</p>

      <div class="error-actions">
        <button @click="reload" class="primary-button">刷新页面</button>
        <button @click="clearCache" class="secondary-button">清除缓存并刷新</button>
      </div>

      <button
        v-if="errorDetails"
        @click="showDetails = !showDetails"
        class="details-toggle"
      >
        {{ showDetails ? '隐藏' : '显示' }}错误详情
      </button>

      <div v-if="showDetails && errorDetails" class="error-details">
        <pre>{{ errorDetails }}</pre>
      </div>

      <p class="error-hint">
        如果问题持续出现，请尝试：<br>
        1. 检查网络连接<br>
        2. 确保后端服务正常运行<br>
        3. 清除浏览器缓存（Ctrl+Shift+Del）
      </p>
    </div>
  </div>
  <slot v-else />
</template>

<style scoped>
.error-boundary {
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: #0A0D12;
  padding: var(--space-4, 1rem);
}

.error-card {
  background: #0F131C;
  border: 1px solid #1E2636;
  border-radius: 12px;
  padding: var(--space-6, 1.5rem);
  max-width: 560px;
  width: 100%;
  text-align: center;
}

.error-icon {
  font-size: 3rem;
  margin-bottom: var(--space-3, 0.75rem);
}

.error-card h2 {
  color: #E9A568;
  margin: 0 0 var(--space-4, 1rem);
  font-size: clamp(1.25rem, 3vw, 1.5rem);
}

.error-message {
  color: #94A3B8;
  margin: 0 0 var(--space-6, 1.5rem);
  font-size: 0.9375rem;
  line-height: 1.6;
}

.error-actions {
  display: flex;
  gap: var(--space-3, 0.75rem);
  justify-content: center;
  margin-bottom: var(--space-4, 1rem);
  flex-wrap: wrap;
}

.primary-button {
  background: #38BDF8;
  color: #0A0D12;
  border: none;
  border-radius: 999px;
  padding: 0.75rem 1.5rem;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
  font-size: 0.875rem;
}

.primary-button:hover {
  opacity: 0.9;
}

.secondary-button {
  background: transparent;
  color: #38BDF8;
  border: 1px solid #38BDF8;
  border-radius: 999px;
  padding: 0.75rem 1.5rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 0.875rem;
}

.secondary-button:hover {
  background: rgba(56, 189, 248, 0.1);
}

.details-toggle {
  background: transparent;
  color: #94A3B8;
  border: none;
  cursor: pointer;
  font-size: 0.8125rem;
  padding: 0.5rem;
  margin-top: var(--space-2, 0.5rem);
  text-decoration: underline;
  transition: color 0.2s;
}

.details-toggle:hover {
  color: #CBD5E1;
}

.error-details {
  margin-top: var(--space-4, 1rem);
  background: #05070C;
  border: 1px solid #1E2636;
  border-radius: 8px;
  padding: var(--space-4, 1rem);
  max-height: 200px;
  overflow: auto;
  text-align: left;
}

.error-details pre {
  margin: 0;
  color: #E9A568;
  font-size: 0.75rem;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-word;
}

.error-hint {
  margin-top: var(--space-6, 1.5rem);
  padding-top: var(--space-4, 1rem);
  border-top: 1px solid #1E2636;
  color: #64748B;
  font-size: 0.8125rem;
  line-height: 1.6;
}
</style>
