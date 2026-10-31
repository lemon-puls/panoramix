// Wails 运行时类型（手写占位；`wails dev`/`wails build` 会重新生成官方版本）
export {}

declare global {
  interface Window {
    runtime: any
    go: any
  }
}
