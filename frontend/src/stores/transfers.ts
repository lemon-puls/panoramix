import { ref } from 'vue'
import { defineStore } from 'pinia'
import * as models from '../../wailsjs/go/models'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  ListTransfers,
  SetTransferPolicy,
  TransferCancel,
  TransferRetry,
} from '../../wailsjs/go/app/Service'

export interface TransferTask {
  transferId: string
  batchId: string
  sessionId: string
  direction: string // upload | download
  name: string
  remotePath: string
  localPath: string
  size: number
  transferred: number
  state: string // pending|running|done|skipped|canceled|failed|awaiting_policy
  speedBps: number
  resumed?: boolean
  err?: string
}

export interface ConflictInfo {
  transferId: string
  batchId: string
  sessionId: string
  direction: string
  name: string
  path: string
  size: number
}

export const useTransfersStore = defineStore('transfers', () => {
  const tasks = ref<TransferTask[]>([])
  const conflicts = ref<ConflictInfo[]>([])

  function upsert(payload: Partial<TransferTask>) {
    if (!payload.transferId) return
    const idx = tasks.value.findIndex((t) => t.transferId === payload.transferId)
    if (idx === -1) {
      tasks.value.unshift({
        transferId: payload.transferId,
        batchId: payload.batchId ?? '',
        sessionId: payload.sessionId ?? '',
        direction: payload.direction ?? '',
        name: payload.name ?? '',
        remotePath: payload.remotePath ?? '',
        localPath: payload.localPath ?? '',
        size: payload.size ?? 0,
        transferred: payload.transferred ?? 0,
        state: payload.state ?? 'pending',
        speedBps: payload.speedBps ?? 0,
        err: payload.err,
      })
    } else {
      // 原地合并（保持列表顺序，避免每次进度事件重排）
      Object.assign(tasks.value[idx], payload)
    }
  }

  // 总线只初始化一次；ListTransfers 填充打开面板前已存在的任务
  let seeded = false
  async function ensureSeeded() {
    if (seeded) return
    seeded = true
    EventsOn('transfer:progress', (p: Partial<TransferTask>) => upsert(p))
    try {
      const list = await ListTransfers()
      for (const t of list ?? []) upsert(t as unknown as Partial<TransferTask>)
    } catch {
      // 桥接不可用（纯浏览器预览）时忽略
    }
  }

  function onConflict(payload: ConflictInfo) {
    conflicts.value.push(payload)
  }

  async function setPolicy(batchId: string, mode: string, applyAll: boolean) {
    await SetTransferPolicy(batchId, mode, applyAll)
  }

  async function cancel(taskId: string) {
    await TransferCancel(taskId)
  }

  async function retry(taskId: string) {
    await TransferRetry(taskId)
  }

  return {
    tasks,
    conflicts,
    upsert,
    onConflict,
    ensureSeeded,
    setPolicy,
    cancel,
    retry,
  }
})

// 速度格式化（纯函数供组件复用）
export function formatSpeed(bps: number): string {
  if (!bps || bps <= 0) return ''
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let v = bps
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[u]}`
}

export function formatSize(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB']
  let v = bytes
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[u]}`
}

export function newRemoteEntry(partial: Partial<models.sftp.RemoteEntry>): models.sftp.RemoteEntry {
  return new models.sftp.RemoteEntry(partial)
}
