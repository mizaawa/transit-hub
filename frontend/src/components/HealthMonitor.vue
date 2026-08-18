<script setup lang="ts">
import { useConnectionMonitor } from '@/composables/useConnectionMonitor'
import { computed } from 'vue'

const { isOnline, backendHealthy, lastCheckTime } = useConnectionMonitor()

const statusMessage = computed(() => {
  if (!isOnline.value) {
    return '网络连接已断开'
  }
  if (!backendHealthy.value) {
    return '后端服务不可用'
  }
  return '连接正常'
})

const showWarning = computed(() => !isOnline.value || !backendHealthy.value)

const formatTime = (date: Date | null) => {
  if (!date) return ''
  return date.toLocaleTimeString('zh-CN')
}
</script>

<template>
  <div v-if="showWarning" class="health-monitor">
    <div class="warning-banner">
      <span class="warning-icon">⚠️</span>
      <div class="warning-content">
        <span class="warning-message">{{ statusMessage }}</span>
        <span v-if="lastCheckTime" class="last-check">
          最后检查: {{ formatTime(lastCheckTime) }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.health-monitor {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 9999;
}

.warning-banner {
  background: linear-gradient(135deg, #E9A568 0%, #D97706 100%);
  color: #0A0D12;
  padding: 0.75rem 1rem;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  animation: slideDown 0.3s ease-out;
}

@keyframes slideDown {
  from {
    transform: translateY(-100%);
  }
  to {
    transform: translateY(0);
  }
}

.warning-icon {
  font-size: 1.25rem;
  flex-shrink: 0;
}

.warning-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.warning-message {
  font-weight: 600;
  font-size: 0.875rem;
}

.last-check {
  font-size: 0.75rem;
  opacity: 0.8;
}

@media (min-width: 640px) {
  .warning-content {
    flex-direction: row;
    align-items: center;
    gap: 1rem;
  }
}
</style>
