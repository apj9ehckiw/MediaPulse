import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// dev 模式代理到 Go 后端；build 产物拷贝到 backend/web/dist 由 Go embed
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
  build: {
    outDir: '../backend/web/dist',
    emptyOutDir: true,
  },
})
