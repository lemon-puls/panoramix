import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Wails 开发模式下由 wails CLI 接管端口与资源注入；
// 此配置同时保证纯浏览器 `npm run dev` 可用（后端桥接缺失时页面优雅降级）
export default defineConfig({
  plugins: [vue()],
  // 5173 是 vite 默认端口，极易被其他项目的 dev server 占用
  // （且 WSL2 localhost 转发会跨系统占口）；Panoramix 固定用 5240
  // usePolling：WSL 访问 /mnt/d（Windows 盘）时 inotify 不可靠，轮询保证
  // 文件变更能被 Vite 感知（代价很小；对 Windows 原生开发同样无害）
  server: {
    port: 5240,
    strictPort: true,
    watch: { usePolling: true, interval: 300 },
  },
  build: { outDir: 'dist', emptyOutDir: true },
})
