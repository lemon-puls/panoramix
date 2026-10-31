import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Wails 开发模式下由 wails CLI 接管端口与资源注入；
// 此配置同时保证纯浏览器 `npm run dev` 可用（后端桥接缺失时页面优雅降级）
export default defineConfig({
  plugins: [vue()],
  server: { port: 5173, strictPort: true },
  build: { outDir: 'dist', emptyOutDir: true },
})
