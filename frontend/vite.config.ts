import path from 'node:path'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5444,
    proxy: {
      // 必须与后端默认端口（config.defaultPort = 5478）一致。
      // 之前这里写的是 5555，而后端从未监听该端口，于是 /api 代理静默失败、
      // Vite 回退返回 index.html，前端再对 HTML 做 JSON.parse，
      // 报出 `Unexpected token '<', "<script sr"... is not valid JSON`。
      '/api': {
        target: 'http://127.0.0.1:5478',
        changeOrigin: true,
      },
    },
  },
})
