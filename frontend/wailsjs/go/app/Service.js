// Wails 绑定调用（手写占位实现；防御式访问，纯浏览器 dev 模式下优雅降级不崩溃）
function call(method, ...args) {
  const svc = typeof window !== 'undefined' ? window.go?.app?.Service : undefined
  if (!svc || typeof svc[method] !== 'function') {
    return Promise.reject(new Error('wails bridge unavailable'))
  }
  return svc[method](...args)
}

export function Ping() {
  return call('Ping')
}
