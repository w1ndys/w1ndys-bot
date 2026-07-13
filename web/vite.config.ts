// 📌 影响范围：启动 Vite 开发服务器、拆分生产依赖包并将 /api 代理到本机 Go 服务。
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          framework: ['vue', 'vue-router'],
          ui: ['naive-ui'],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:18800',
        changeOrigin: true,
      },
    },
  },
})
