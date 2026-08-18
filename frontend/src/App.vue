<script setup lang="ts">
import ErrorBoundary from '@/components/ErrorBoundary.vue'
import OfflineIndicator from '@/components/OfflineIndicator.vue'
import BackendStatusIndicator from '@/components/BackendStatusIndicator.vue'
</script>

<template>
  <ErrorBoundary>
    <BackendStatusIndicator />
    <OfflineIndicator />
    <router-view />
  </ErrorBoundary>
</template>
