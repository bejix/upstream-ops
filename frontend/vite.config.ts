import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const BACKEND_TARGET = process.env.VITE_BACKEND_URL ?? 'http://localhost:8418'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '.'),
    },
  },
  server: {
    port: 3010,
    strictPort: true,
    proxy: {
      // 仅代理后端 API / 健康检查 / 公开转发；/gateway 是前端 SPA 页面，不要代理。
      '/api':     { target: BACKEND_TARGET, changeOrigin: true },
      '/healthz': { target: BACKEND_TARGET, changeOrigin: true },
      '/v1':      { target: BACKEND_TARGET, changeOrigin: true },
    },
  },
  // 生产构建直接写入 Go 的嵌入目录；emptyOutDir 确保旧哈希资源不会被保留。
  build: {
    outDir: path.resolve(__dirname, '../web/dist'),
    emptyOutDir: true,
  },
})
