<script setup lang="ts">
import { useOnlineStatus } from '@/composables/useOnlineStatus'

const { isOnline } = useOnlineStatus()
</script>

<template>
  <Transition name="slide-down">
    <div v-if="!isOnline" class="offline-indicator">
      <span class="offline-icon">⚠️</span>
      <span class="offline-text">网络连接已断开，请检查网络设置</span>
    </div>
  </Transition>
</template>

<style scoped>
.offline-indicator {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 9999;
  background: #E9A568;
  color: #0A0D12;
  padding: 0.75rem 1rem;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  font-weight: 600;
  font-size: 0.875rem;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

.offline-icon {
  font-size: 1rem;
}

.offline-text {
  letter-spacing: 0.01em;
}

.slide-down-enter-active,
.slide-down-leave-active {
  transition: transform 0.3s ease, opacity 0.3s ease;
}

.slide-down-enter-from,
.slide-down-leave-to {
  transform: translateY(-100%);
  opacity: 0;
}
</style>
