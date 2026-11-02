import { ref } from 'vue'
import { defineStore } from 'pinia'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  FinishRemoteEdit,
  ListRemoteEdits,
  ResolveEditConflict,
  StartRemoteEdit,
} from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'

// 远程编辑会话状态：edit:state 事件驱动（uploaded / conflict / error / resolved）
export interface EditEvent {
  id: string
  sessionId: string
  remotePath: string
  name: string
  localPath: string
  state: string
  message: string
}

export const useEditsStore = defineStore('edits', () => {
  const edits = ref<models.sftp.EditInfo[]>([])
  const conflicts = ref<EditEvent[]>([])

  let busReady = false
  function ensureBus() {
    if (busReady) return
    busReady = true
    EventsOn('edit:state', (e: EditEvent) => {
      if (e.state === 'conflict') {
        if (!conflicts.value.some((c) => c.id === e.id)) conflicts.value.push(e)
      }
      if (e.state === 'resolved') {
        conflicts.value = conflicts.value.filter((c) => c.id !== e.id)
      }
    })
  }

  async function refresh() {
    try {
      edits.value = (await ListRemoteEdits()) ?? []
    } catch {
      // 桥接不可用时忽略
    }
  }

  async function edit(sessionId: string, remotePath: string) {
    ensureBus()
    const info = await StartRemoteEdit(sessionId, remotePath)
    await refresh()
    return info
  }

  async function finish(id: string) {
    await FinishRemoteEdit(id)
    conflicts.value = conflicts.value.filter((c) => c.id !== id)
    await refresh()
  }

  async function resolve(id: string, overwrite: boolean) {
    await ResolveEditConflict(id, overwrite)
    conflicts.value = conflicts.value.filter((c) => c.id !== id)
    await refresh()
  }

  return { edits, conflicts, ensureBus, refresh, edit, finish, resolve }
})
