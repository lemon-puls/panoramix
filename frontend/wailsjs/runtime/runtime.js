// Wails 运行时桥（手写占位实现，接口与 CLI 生成版本一致；
// 首次 `wails dev` / `wails build` 会重新生成为官方版本）
function rt() {
  return typeof window !== 'undefined' ? window.runtime : undefined
}

export function EventsOn(name, callback) {
  const r = rt()
  return r && r.EventsOn ? r.EventsOn(name, callback) : () => {}
}

export function EventsOff(name, ...additional) {
  rt()?.EventsOff?.(name, ...additional)
}

export function EventsEmit(name, ...data) {
  rt()?.EventsEmit?.(name, ...data)
}

export function LogInfo(message) {
  rt()?.LogInfo?.(message)
}

export function LogWarning(message) {
  rt()?.LogWarning?.(message)
}

export function LogError(message) {
  rt()?.LogError?.(message)
}
