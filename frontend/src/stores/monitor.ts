// 监控数据总线：monitor:sample 事件 → 按会话的环形缓冲（最近 1 小时）
// 断开即弃、不落盘（ROADMAP v0.2 决策）。uPlot 走 setData 命令式更新，
// 因此数据本身不必做成 Vue 响应式——用 version 计数通知组件刷新。
import { ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'

// 右侧面板开关（文件/监控互斥）
export const showMonitorPanel = ref(false)

export interface MonitorSample {
  hostId: string
  sessionId: string
  ts: number
  cpu: { percent: number; cores: number }
  loadAvg: [number, number, number]
  mem: { total: number; used: number; available: number; swapTotal: number; swapUsed: number }
  disks: { mount: string; total: number; used: number; percent: number }[]
  diskIo: { readBps: number; writeBps: number }
  net: { recvBps: number; sentBps: number }
  processes: { pid: number; name: string; cpu: number; mem: number }[]
  gpu?: { index: number; name: string; util: number; memUsedMB: number; memTotMB: number; tempC: number }[]
}

// 每会话环形缓冲（300 点 = 默认 2s 采样 × 10 分钟；超出后头部丢弃）
const CAP = 300
const buffers = new Map<string, MonitorSample[]>()
const sessionState = new Map<string, string>() // running | stopped
let busReady = false

// 组件监听该计数，在自己的渲染节奏上 setData
const listeners = new Set<(sessionId: string) => void>()

export function onMonitorUpdate(fn: (sessionId: string) => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function getSessionSamples(sessionId: string): MonitorSample[] {
  return buffers.get(sessionId) ?? []
}

export function getSessionState(sessionId: string): string {
  return sessionState.get(sessionId) ?? 'running'
}

function pushSample(s: MonitorSample) {
  let arr = buffers.get(s.sessionId)
  if (!arr) {
    arr = []
    buffers.set(s.sessionId, arr)
  }
  arr.push(s)
  if (arr.length > CAP) arr.splice(0, arr.length - CAP)
  for (const fn of listeners) fn(s.sessionId)
}

if (typeof window !== 'undefined') {
  EventsOn('monitor:sample', (m: MonitorSample) => {
    if (m?.sessionId) {
      sessionState.set(m.sessionId, 'running')
      pushSample(m)
    }
  })
  EventsOn('monitor:state', (p: { sessionId?: string; state?: string }) => {
    if (p?.sessionId) {
      sessionState.set(p.sessionId, p.state ?? 'running')
      for (const fn of listeners) fn(p.sessionId!)
    }
  })
}

// 会话关闭时清理缓冲（断开即弃）
export function dropSession(sessionId: string) {
  buffers.delete(sessionId)
  sessionState.delete(sessionId)
}
