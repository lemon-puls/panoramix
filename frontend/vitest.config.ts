import { defineConfig } from 'vitest/config'

// 前端纯逻辑单测：目前只覆盖数据标签的 pending 计算与更新载荷构造。
// 组件渲染/交互仍由预览页 + CDP 验证。
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
