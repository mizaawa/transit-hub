<template>
  <div class="not-found">
    <div class="content">
      <h1>404</h1>
      <p>{{ t('error.pageNotFound') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<style scoped>
.not-found {
  display: grid;
  place-items: center;
  min-height: 100vh;
  background: linear-gradient(135deg, #0A0D12 0%, #161D2B 100%);
}

.content {
  text-align: center;
  color: #fff;
}

h1 {
  font-size: clamp(4rem, 8vw, 8rem);
  font-weight: 700;
  margin: 0;
  background: linear-gradient(135deg, #38BDF8 0%, #6EE7B7 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

p {
  font-size: clamp(1rem, 2vw, 1.25rem);
  margin-top: 1rem;
  opacity: 0.7;
}
</style>
