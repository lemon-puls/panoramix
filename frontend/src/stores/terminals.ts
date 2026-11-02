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

// 终端 pane：一个标签内的单个会话视图（分屏即一个标签多个 pane）
export interface TerminalPane {
  sessionId: string
  hostId: string
  hostName: string
  state: string // connecting | connected | reconnecting | closed | error | paused
  exitReason: string
}

// 终端标签：至少一个 pane；panes>1 时为分屏布局
export interface TerminalTab {
  id: string
  panes: TerminalPane[]
  activePaneId: string
  direction: 'row' | 'column' // 分屏方向：row=左右，column=上下
}

// terminal:output / terminal:exit 按 sessionId 分发到对应视图；
// wails 的 EventsOff 会移除同名事件的全部回调，因此全局只注册一份监听
type OutputPayload = { dataB64?: string; code?: string; reason?: string }
type OutputHandler = (payload: OutputPayload) => void

let tabSeq = 0

export const useTerminalsStore = defineStore('terminals', () => {
  const tabs = ref<TerminalTab[]>([])
  const activeTabId = ref<string | null>(null)

  const outputHandlers = new Map<string, OutputHandler>()

  function handleExit(sessionId: string, code: string, reason: string) {
    for (const tab of tabs.value) {
      const pane = tab.panes.find((p) => p.sessionId === sessionId)
      if (pane) pane.exitReason = `${reason} (exit ${code})`
    }
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
    for (const tab of tabs.value) {
      const pane = tab.panes.find((x) => x.sessionId === p?.sessionId)
      if (pane) pane.state = String(p?.state ?? pane.state)
    }
  })

  const activeTab = computed(() => tabs.value.find((t) => t.id === activeTabId.value))

  // 当前激活 pane 对应的会话（文件/监控/片段面板都跟随它）
  const activeSessionId = computed(() => {
    const tab = activeTab.value
    if (!tab) return null
    return tab.panes.find((p) => p.sessionId === tab.activePaneId)?.sessionId ?? null
  })
  const activePane = computed(() => {
    const tab = activeTab.value
    return tab?.panes.find((p) => p.sessionId === tab.activePaneId)
  })

  // connect 连接主机并开新标签
  async function connect(
    host: models.store.Host,
    trustMode = '',
  ): Promise<models.app.ConnectHostResult> {
    const res = await ConnectHost(host.id, trustMode)
    if (res.ok && res.sessionId) {
      const pane: TerminalPane = {
        sessionId: res.sessionId,
        hostId: host.id,
        hostName: host.name,
        state: 'connected',
        exitReason: '',
      }
      const id = `tab-${++tabSeq}`
      tabs.value.push({ id, panes: [pane], activePaneId: pane.sessionId, direction: 'row' })
      activeTabId.value = id
    }
    return res
  }

  // splitPane 在当前标签内新增 pane（分屏）；targetTabId 缺省用当前标签
  async function splitPane(
    host: models.store.Host,
    trustMode = '',
    targetTabId?: string,
  ): Promise<models.app.ConnectHostResult> {
    const tab = tabs.value.find((t) => t.id === (targetTabId ?? activeTabId.value))
    if (!tab) {
      // 没有标签时退化为新标签
      return connect(host, trustMode)
    }
    if (tab.panes.length >= 4) {
      return {
        ok: false,
        sessionId: '',
        errorCode: 'connect_failed',
        fingerprint: '',
        message: 'split limit reached (max 4 panes)',
      } as models.app.ConnectHostResult
    }
    const res = await ConnectHost(host.id, trustMode)
    if (res.ok && res.sessionId) {
      tab.panes.push({
        sessionId: res.sessionId,
        hostId: host.id,
        hostName: host.name,
        state: 'connected',
        exitReason: '',
      })
      tab.activePaneId = res.sessionId
      activeTabId.value = tab.id
    }
    return res
  }

  // closePane 关闭单个 pane；标签内最后一个 pane 关闭时整个标签移除
  async function closePane(tabId: string, sessionId: string) {
    const tab = tabs.value.find((t) => t.id === tabId)
    if (!tab) return
    outputHandlers.delete(sessionId)
    try {
      await CloseSession(sessionId)
    } catch {
      // 会话可能已自行退出
    }
    tab.panes = tab.panes.filter((p) => p.sessionId !== sessionId)
    if (tab.panes.length === 0) {
      removeTab(tabId)
      return
    }
    if (tab.activePaneId === sessionId) {
      tab.activePaneId = tab.panes[tab.panes.length - 1].sessionId
    }
  }

  async function closeTab(tabId: string) {
    const tab = tabs.value.find((t) => t.id === tabId)
    if (!tab) return
    for (const pane of [...tab.panes]) {
      outputHandlers.delete(pane.sessionId)
      try {
        await CloseSession(pane.sessionId)
      } catch {
        // 忽略
      }
    }
    removeTab(tabId)
  }

  function removeTab(tabId: string) {
    const idx = tabs.value.findIndex((t) => t.id === tabId)
    if (idx === -1) return
    tabs.value.splice(idx, 1)
    if (activeTabId.value === tabId) {
      activeTabId.value = tabs.value.at(-1)?.id ?? null
    }
  }

  function setActiveTab(tabId: string) {
    activeTabId.value = tabId
  }

  function setActivePane(tabId: string, sessionId: string) {
    const tab = tabs.value.find((t) => t.id === tabId)
    if (tab) {
      tab.activePaneId = sessionId
      activeTabId.value = tabId
    }
  }

  function toggleDirection(tabId: string) {
    const tab = tabs.value.find((t) => t.id === tabId)
    if (tab) tab.direction = tab.direction === 'row' ? 'column' : 'row'
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
      // 会话已结束时静默
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
    activeTabId,
    activeTab,
    activeSessionId,
    activePane,
    connect,
    splitPane,
    closePane,
    closeTab,
    setActiveTab,
    setActivePane,
    toggleDirection,
    registerOutput,
    unregisterOutput,
    write,
    resize,
  }
})
