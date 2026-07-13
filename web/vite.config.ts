// 📌 影响范围：启动 Vite 开发服务器并将 /api 代理到本机 Go 服务。
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
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
