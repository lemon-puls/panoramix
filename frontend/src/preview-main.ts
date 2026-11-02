// 临时视觉预览入口（仅本地调试用，验证后删除）
// mock Wails runtime → 注入假样本 → 渲染 MonitorPanel
import { createApp, h } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { NConfigProvider, NMessageProvider, darkTheme } from 'naive-ui'
import { zhCN, dateZhCN } from 'naive-ui'

type Handler = (...args: any[]) => void
const handlers = new Map<string, Handler[]>()

// 必须在任何模块 import monitor.ts 之前挂载 mock
;(window as any).runtime = {
  EventsOn(name: string, cb: Handler) {
    const list = handlers.get(name) ?? []
    list.push(cb)
    handlers.set(name, list)
    return () => {}
  },
  EventsOnMultiple(name: string, cb: Handler) {
    return (window as any).runtime.EventsOn(name, cb)
  },
  EventsOff() {},
  EventsEmit() {},
  LogInfo() {},
  LogWarning() {},
  LogError() {},
  OnFileDrop() {},
  OnFileDropOff() {},
  ClipboardGetText() { return '' },
  ClipboardSetText() { return true },
}

function emit(name: string, payload: unknown) {
  for (const cb of handlers.get(name) ?? []) cb(payload)
}

async function main() {
  const { useTerminalsStore } = await import('./stores/terminals')
  const { default: MonitorPanel } = await import('./components/MonitorPanel.vue')
  const { i18n, setLocale } = await import('./i18n')
  const lang = new URLSearchParams(location.search).get('locale')
  if (lang === 'en-US') setLocale('en-US')

  const pinia = createPinia()
  setActivePinia(pinia)
  const terminals = useTerminalsStore()
  terminals.tabs.push({
    sessionId: 'demo',
    hostId: 'h1',
    hostName: '阿里云',
    state: 'connected',
    exitReason: '',
  })
  terminals.activeId = 'demo'

  const app = createApp({
    render: () =>
      h(NConfigProvider, { theme: darkTheme, locale: zhCN, dateLocale: dateZhCN }, {
        default: () =>
          h(NMessageProvider, null, {
            default: () =>
              h('div', { style: 'width:380px;height:900px;background:#18181c' }, [
                h(MonitorPanel),
              ]),
          }),
      }),
  })
  app.use(pinia)
  app.use(i18n)
  app.mount('#app')

  // 生成 300 个点的模拟数据（CPU 有起伏、内存缓慢上升、网络尖峰）
  const now = Date.now()
  const push = (i: number) => {
    const t = now - (300 - i) * 2000
    const cpu = 25 + 18 * Math.sin(i / 11) + 8 * Math.sin(i / 3.3) + Math.random() * 4
    const memUsed = (0.42 + i * 0.0006) * 16384 * 1024 * 1024
    emit('monitor:sample', {
      hostId: 'h1',
      sessionId: 'demo',
      ts: t,
      cpu: { percent: Math.max(1, cpu), cores: 2 },
      loadAvg: [0.31, 0.28, 0.21],
      mem: {
        total: 16384 * 1024 * 1024,
        used: memUsed,
        available: 16384 * 1024 * 1024 - memUsed,
        swapTotal: 0,
        swapUsed: 0,
      },
      disks: [
        { mount: '/', total: 39 * 1024 ** 3, used: 7.8 * 1024 ** 3, percent: 20 },
        { mount: '/boot/efi', total: 197 * 1024 ** 2, used: 6.1 * 1024 ** 2, percent: 3 },
        { mount: '/data', total: 500 * 1024 ** 3, used: 430 * 1024 ** 3, percent: 86 },
      ],
      diskIo: {
        readBps: 200000 + 900000 * Math.abs(Math.sin(i / 7)),
        writeBps: 300000 + 1500000 * Math.abs(Math.sin(i / 5)),
      },
      net: {
        recvBps: 1200000 + 3000000 * Math.abs(Math.sin(i / 9)) + Math.random() * 100000,
        sentBps: 90000 + 400000 * Math.abs(Math.sin(i / 4)),
      },
      processes: [
        { pid: 12539, name: 'argusagent', cpu: 12.5, mem: 3.2 },
        { pid: 206039, name: 'AliYunDunMonito', cpu: 8.4, mem: 5.1 },
        { pid: 16808, name: 'loongcollector', cpu: 6.1, mem: 2.8 },
        { pid: 279708, name: 'sshd', cpu: 3.2, mem: 0.5 },
        { pid: 267449, name: 'aliyun-service', cpu: 1.8, mem: 1.2 },
        { pid: 234256, name: 'tuned', cpu: 0.6, mem: 1.7 },
        { pid: 55813, name: 'AliYunDunUp...', cpu: 0.3, mem: 0.4 },
        { pid: 1, name: 'systemd', cpu: 0.1, mem: 0.8 },
      ],
      gpu: [
        { index: 0, name: 'NVIDIA A100-SXM4-40GB', util: 62, memUsedMB: 8192, memTotMB: 40960, tempC: 68 },
      ],
    })
  }
  for (let i = 0; i < 300; i++) push(i)
}

main().catch((e) => {
  document.body.innerHTML = `<pre style="color:#e88080;padding:20px;white-space:pre-wrap">${String(
    e && e.stack ? e.stack : e,
  )}</pre>`
})
