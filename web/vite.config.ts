import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 构建标识：注入构建时间，显示在侧栏底部。
// 用于排查"页面还是旧版本"这类问题（浏览器缓存旧 HTML/JS 时一眼可见）。
const buildId = new Date().toISOString().slice(5, 16).replace('T', ' ')

export default defineConfig({
  plugins: [react()],
  define: {
    __BUILD_ID__: JSON.stringify(buildId),
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        ws: true,
      },
    },
  },
})
