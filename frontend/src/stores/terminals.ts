import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import * as models from '../../wailsjs/go/models'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  CloseSession,
  ConnectHost,
  ResizeSession,
  WriteSession,
} from '../../wailsjs/go/app/Service'

// 终端标签（一个标签对应一个后端 SSH 会话）
export interface TerminalTab {
  sessionId: string
  hostId: string
  hostName: string
  state: string // connecting | connected | reconnecting | closed | error
  exitReason: string
}

// terminal:output / terminal:exit 按 sessionId 分发到对应视图；
// wails 的 EventsOff 会移除同名事件的全部回调，因此全局只注册一份监听
type OutputPayload = { dataB64?: string; code?: string; reason?: string }
type OutputHandler = (payload: OutputPayload) => void

export const useTerminalsStore = defineStore('terminals', () => {
  const tabs = ref<TerminalTab[]>([])
  const activeId = ref<string | null>(null)

  const outputHandlers = new Map<string, OutputHandler>()

  function handleExit(sessionId: string, code: string, reason: string) {
    // 标签状态由随后的 conn:state 事件兜底，这里只记退出原因并转发给视图
    const tab = tabs.value.find((t) => t.sessionId === sessionId)
    if (tab) tab.exitReason = `${reason} (exit ${code})`
    outputHandlers.get(sessionId)?.({ code, reason })
  }

  // 总线只初始化一次（store 在应用生命周期内单例）
  EventsOn('terminal:output', (p: any) => {
    if (p?.sessionId) outputHandlers.get(p.sessionId)?.({ dataB64: p?.dataB64 })
  })
  EventsOn('terminal:exit', (p: any) => {
    if (p?.sessionId) handleExit(p.sessionId, String(p?.code ?? '-1'), String(p?.reason ?? ''))
  })
  EventsOn('conn:state', (p: any) => {
    const tab = tabs.value.find((t) => t.sessionId === p?.sessionId)
    if (tab) tab.state = String(p?.state ?? tab.state)
  })

  const activeSessionId = computed(() => activeId.value)
  const activeTab = computed(() => tabs.value.find((t) => t.sessionId === activeId.value))

  // connect 连接主机并开标签。trustMode: '' | 'add' | 'replace'（TOFU 二次确认）
  async function connect(
    host: models.store.Host,
    trustMode = '',
  ): Promise<models.app.ConnectHostResult> {
    const res = await ConnectHost(host.id, trustMode)
    // Start 成功返回即已 connected（后端先于返回值发出 connected 事件，此处直接置态）
    if (res.ok && res.sessionId) {
      const existing = tabs.value.find((t) => t.sessionId === res.sessionId)
      if (!existing) {
        tabs.value.push({
          sessionId: res.sessionId,
          hostId: host.id,
          hostName: host.name,
          state: 'connected',
          exitReason: '',
        })
      }
      activeId.value = res.sessionId
    }
    return res
  }

  async function closeTab(sessionId: string) {
    const idx = tabs.value.findIndex((t) => t.sessionId === sessionId)
    if (idx === -1) return
    outputHandlers.delete(sessionId)
    try {
      await CloseSession(sessionId)
    } catch {
      // 会话可能已自行退出，忽略
    }
    tabs.value.splice(idx, 1)
    if (activeId.value === sessionId) {
      activeId.value = tabs.value.at(-1)?.sessionId ?? null
    }
  }

  function setActive(sessionId: string) {
    activeId.value = sessionId
  }

  function registerOutput(sessionId: string, fn: OutputHandler) {
    outputHandlers.set(sessionId, fn)
  }

  function unregisterOutput(sessionId: string) {
    outputHandlers.delete(sessionId)
  }

  async function write(sessionId: string, data: string) {
    try {
      await WriteSession(sessionId, data)
    } catch {
      // 会话已结束时静默（连击按键到达晚于退出属正常）
    }
  }

  async function resize(sessionId: string, cols: number, rows: number) {
    try {
      await ResizeSession(sessionId, cols, rows)
    } catch {
      // 同上
    }
  }

  return {
    tabs,
    activeId,
    activeSessionId,
    activeTab,
    connect,
    closeTab,
    setActive,
    registerOutput,
    unregisterOutput,
    write,
    resize,
  }
})
