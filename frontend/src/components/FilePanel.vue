<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NDropdown, NEmpty, NInput, NModal, NSpin } from 'naive-ui'
import { OnFileDrop, OnFileDropOff } from '../../wailsjs/runtime/runtime'
import { useFilesStore } from '../stores/files'
import { useTerminalsStore } from '../stores/terminals'
import { useEditsStore, type EditEvent } from '../stores/edits'
import { useTransfersStore, formatSize, type ConflictInfo } from '../stores/transfers'
import { PauseTransfer, ResumeTransfer } from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'

// 远程文件面板：路径栏 + 条目列表 + 右键操作 + 拖拽上传 + 冲突决策
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const files = useFilesStore()
const terminals = useTerminalsStore()
const transfers = useTransfersStore()
const edits = useEditsStore()

const sessionId = computed(() => terminals.activeSessionId ?? '')
const connected = computed(() => terminals.activePane?.state === 'connected')

onMounted(() => {
  edits.ensureBus()
  void edits.refresh()
  void transfers.ensureSeeded()
  // 拖拽文件到面板 → 上传到当前目录（Wails 全局文件拖放）
  OnFileDrop((_x, _y, paths) => {
    if (!sessionId.value || !connected.value || !paths.length) return
    void files.uploadPaths(sessionId.value, files.cwd, paths)
    message.success(t('files.dropQueued', { n: paths.length }))
  }, false)
})
onBeforeUnmount(() => OnFileDropOff())

// 会话切换 / 首次展开面板 → 加载主目录
const loadedFor = ref('')
watch([sessionId, connected, () => files.showPanel], async () => {
  const sid = sessionId.value
  if (!sid || !connected.value || !files.showPanel) return
  if (loadedFor.value === sid) return
  try {
    await files.load(sid, '')
    loadedFor.value = sid
  } catch (e) {
    message.error(String(e))
  }
})

// ---------- 面包屑 ----------

const crumbs = computed(() => {
  const cwd = files.cwd || '/'
  const parts = cwd.split('/').filter(Boolean)
  const out: { name: string; path: string }[] = [{ name: '/', path: '/' }]
  let acc = ''
  for (const p of parts) {
    acc += '/' + p
    out.push({ name: p, path: acc })
  }
  return out
})

// ---------- 条目右键菜单 ----------

const menu = reactive({ show: false, x: 0, y: 0, entry: null as models.sftp.RemoteEntry | null })

function openMenu(e: MouseEvent, entry: models.sftp.RemoteEntry) {
  e.preventDefault()
  menu.x = e.clientX
  menu.y = e.clientY
  menu.entry = entry
  menu.show = true
}

const menuOptions = computed(() => {
  if (!menu.entry) return []
  if (menu.entry.isDir) {
    return [
      { label: t('files.open'), key: 'open' },
      { label: t('files.download'), key: 'download' },
      { label: t('menu.rename'), key: 'rename' },
      { label: t('menu.delete'), key: 'remove', props: { style: 'color: #e88080' } },
    ]
  }
  return [
    { label: t('files.edit'), key: 'edit' },
    { label: t('files.download'), key: 'download' },
    { label: t('menu.rename'), key: 'rename' },
    { label: t('menu.delete'), key: 'remove', props: { style: 'color: #e88080' } },
  ]
})

async function onMenuSelect(key: string) {
  menu.show = false
  const entry = menu.entry
  if (!entry || !sessionId.value) return
  try {
    switch (key) {
      case 'open':
        await files.enter(sessionId.value, entry.path)
        break
      case 'edit':
        if (!entry.isDir && sessionId.value) {
          await edits.edit(sessionId.value, entry.path)
          message.success(t('files.editStarted', { name: entry.name }))
        }
        break
      case 'download':
        if (entry.isDir) {
          await files.downloadDir(sessionId.value, entry)
        } else {
          await files.downloadFile(sessionId.value, entry)
        }
        break
      case 'rename':
        renameModal.entry = entry
        renameModal.name = entry.name
        renameModal.show = true
        break
      case 'remove':
        dialog.warning({
          title: t('tree.deleteTitle'),
          content: entry.isDir
            ? t('files.deleteDirConfirm', { name: entry.name })
            : t('files.deleteFileConfirm', { name: entry.name }),
          positiveText: t('common.delete'),
          negativeText: t('common.cancel'),
          onPositiveClick: async () => {
            try {
              await files.remove(sessionId.value, entry)
            } catch (e) {
              message.error(String(e))
            }
          },
        })
        break
    }
  } catch (e) {
    message.error(String(e))
  }
}

// ---------- 新建目录 / 重命名弹窗 ----------

const mkdirModal = reactive({ show: false, name: '' })
const renameModal = reactive({ show: false, name: '', entry: null as models.sftp.RemoteEntry | null })
const saving = ref(false)

async function doMkdir() {
  if (!mkdirModal.name.trim() || !sessionId.value) return
  saving.value = true
  try {
    await files.mkdir(sessionId.value, files.cwd, mkdirModal.name.trim())
    mkdirModal.show = false
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}

async function doRename() {
  if (!renameModal.name.trim() || !renameModal.entry || !sessionId.value) return
  saving.value = true
  try {
    await files.rename(sessionId.value, renameModal.entry, renameModal.name.trim())
    renameModal.show = false
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}

// ---------- 冲突决策弹窗 ----------

const conflict = reactive({ show: false, info: null as ConflictInfo | null, applyAll: false })
const handledConflicts = new Set<string>()

watch(
  () => transfers.conflicts.length,
  (n, old) => {
    if (n > (old ?? 0) && !conflict.show) {
      const next = transfers.conflicts.find((c) => !handledConflicts.has(c.transferId))
      if (next) {
        conflict.info = next
        conflict.applyAll = false
        conflict.show = true
      }
    }
  },
)

async function resolveConflict(mode: string) {
  const info = conflict.info
  if (!info) return
  handledConflicts.add(info.transferId)
  conflict.show = false
  try {
    await transfers.setPolicy(info.batchId, mode, conflict.applyAll)
  } catch (e) {
    message.error(String(e))
  }
  // 队列里还有挂起冲突则继续弹
  const next = transfers.conflicts.find((c) => !handledConflicts.has(c.transferId))
  if (next) {
    conflict.info = next
    conflict.applyAll = conflict.applyAll // 沿用"全部应用"选择
    conflict.show = true
  }
}

// ---------- 编辑冲突 ----------

const editConflict = reactive({ show: false, ev: null as EditEvent | null })
const editDismissed = new Set<string>()

watch(
  () => edits.conflicts.length,
  () => {
    if (editConflict.show) return
    const next = edits.conflicts.find((c) => !editDismissed.has(c.id))
    if (next) {
      editConflict.ev = next
      editConflict.show = true
    }
  },
)

async function resolveEdit(overwrite: boolean) {
  const ev = editConflict.ev
  if (!ev) return
  editDismissed.add(ev.id)
  editConflict.show = false
  try {
    await edits.resolve(ev.id, overwrite)
    message.success(overwrite ? t('files.editOverwritten') : t('files.editReloaded'))
  } catch (e) {
    message.error(String(e))
  }
}

// ---------- 传输列表（活跃会话优先） ----------

const sessionTasks = computed(() => {
  const sid = sessionId.value
  const list = transfers.tasks.filter((tk) => !sid || tk.sessionId === sid)
  return list.length > 0 ? list : transfers.tasks
})

function dblclickEntry(entry: models.sftp.RemoteEntry) {
  if (!sessionId.value) return
  if (entry.isDir) {
    void files.enter(sessionId.value, entry.path)
  } else {
    void onMenuSelectQuick('download', entry)
  }
}

async function onMenuSelectQuick(key: string, entry: models.sftp.RemoteEntry) {
  menu.entry = entry
  await onMenuSelect(key)
}

function fmtMtime(v: unknown): string {
  if (typeof v !== 'string') return ''
  return v.slice(0, 16).replace('T', ' ')
}
</script>

<template>
  <div
    class="file-panel"
    @dragover.prevent
  >
    <div class="panel-head">
      <div class="crumbs">
        <span
          v-for="(c, i) in crumbs"
          :key="c.path"
          class="crumb"
          @click="sessionId && files.enter(sessionId, c.path)"
        >
          {{ c.name }}<span v-if="i < crumbs.length - 1" class="sep">/</span>
        </span>
      </div>
      <div class="toolbar">
        <n-button size="tiny" :disabled="!connected" @click="sessionId && files.up(sessionId)">
          ↑
        </n-button>
        <n-button
          size="tiny"
          :disabled="!connected"
          @click="sessionId && files.reload(sessionId)"
        >
          {{ t('files.refresh') }}
        </n-button>
        <n-button size="tiny" :disabled="!connected" @click="mkdirModal.show = true">
          {{ t('files.mkdir') }}
        </n-button>
        <n-button
          size="tiny"
          type="primary"
          :disabled="!connected"
          @click="sessionId && files.uploadFiles(sessionId, files.cwd)"
        >
          {{ t('files.upload') }}
        </n-button>
        <n-button
          size="tiny"
          type="primary"
          :disabled="!connected"
          @click="sessionId && files.uploadDir(sessionId, files.cwd)"
        >
          {{ t('files.uploadDir') }}
        </n-button>
      </div>
    </div>

    <n-spin :show="files.loading" size="small">
      <n-empty
        v-if="files.entries.length === 0"
        size="small"
        :description="connected ? t('files.empty') : t('files.notConnected')"
        class="empty"
      />
      <div v-else class="list">
        <div
          v-for="e in files.entries"
          :key="e.path"
          class="row"
          @dblclick="dblclickEntry(e)"
          @contextmenu="openMenu($event, e)"
        >
          <span class="icon">{{ e.isDir ? '📁' : '📄' }}</span>
          <span class="name" :class="{ dir: e.isDir }">{{ e.name }}</span>
          <span class="size">{{ e.isDir ? '' : formatSize(e.size) }}</span>
          <span class="mtime">{{ fmtMtime(e.mtime) }}</span>
        </div>
      </div>
    </n-spin>

    <div class="transfers">
      <div class="transfers-head">{{ t('transfers.title') }} ({{ sessionTasks.length }})</div>
      <div class="transfers-list">
        <div v-for="tk in sessionTasks.slice(0, 20)" :key="tk.transferId" class="trow">
          <div class="tline">
            <span class="tdir">{{ tk.direction === 'upload' ? '↑' : '↓' }}</span>
            <span class="tname" :title="tk.remotePath">
              {{ tk.name }}<span v-if="tk.resumed" class="tresume">{{ t('transfers.resumed') }}</span>
            </span>
            <span class="tsize">{{ formatSize(tk.transferred) }}/{{ formatSize(tk.size) }}</span>
            <span class="tspeed">{{ tk.state === 'running' ? formatSize(Math.round(tk.speedBps)) + '/s' : '' }}</span>
            <span class="tstate" :class="tk.state">{{ tk.state }}</span>
            <span class="tops">
              <span
                v-if="tk.state === 'running'"
                class="op"
                :title="t('transfers.pause')"
                @click="PauseTransfer(tk.transferId)"
              >⏸</span>
              <span
                v-if="tk.state === 'paused'"
                class="op"
                :title="t('transfers.resume')"
                @click="ResumeTransfer(tk.transferId)"
              >▶</span>
              <span
                v-if="tk.state === 'running' || tk.state === 'paused' || tk.state === 'pending' || tk.state === 'awaiting_policy'"
                class="op"
                @click="transfers.cancel(tk.transferId)"
              >×</span>
              <span
                v-if="tk.state === 'failed' || tk.state === 'canceled'"
                class="op"
                @click="transfers.retry(tk.transferId)"
              >↻</span>
            </span>
          </div>
          <div class="bar">
            <div
              class="bar-fill"
              :class="tk.state"
              :style="{ width: tk.size > 0 ? (tk.transferred / tk.size) * 100 + '%' : '0%' }"
            />
          </div>
          <div v-if="tk.err" class="terr">{{ tk.err }}</div>
        </div>
        <n-empty v-if="sessionTasks.length === 0" size="small" :description="t('transfers.empty')" />
      </div>
    </div>

    <n-dropdown
      placement="bottom-start"
      trigger="manual"
      :show="menu.show"
      :x="menu.x"
      :y="menu.y"
      :options="menuOptions"
      @select="onMenuSelect"
      @clickoutside="menu.show = false"
    />

    <n-modal v-model:show="mkdirModal.show" preset="card" :title="t('files.mkdir')" style="width: 380px">
      <n-input v-model:value="mkdirModal.name" :placeholder="t('files.dirName')" autofocus @keyup.enter="doMkdir" />
      <div class="modal-actions">
        <n-button quaternary @click="mkdirModal.show = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="saving" @click="doMkdir">{{ t('common.save') }}</n-button>
      </div>
    </n-modal>

    <n-modal v-model:show="renameModal.show" preset="card" :title="t('menu.rename')" style="width: 380px">
      <n-input v-model:value="renameModal.name" autofocus @keyup.enter="doRename" />
      <div class="modal-actions">
        <n-button quaternary @click="renameModal.show = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="saving" @click="doRename">{{ t('common.save') }}</n-button>
      </div>
    </n-modal>

    <n-modal v-model:show="editConflict.show" preset="card" :title="t('files.editConflictTitle')" style="width: 460px">
      <p class="conflict-path">{{ editConflict.ev?.remotePath }}</p>
      <p class="conflict-tip">{{ t('files.editConflictTip') }}</p>
      <div class="conflict-actions">
        <n-button type="warning" @click="resolveEdit(true)">{{ t('files.editOverwrite') }}</n-button>
        <n-button quaternary @click="resolveEdit(false)">{{ t('files.editReload') }}</n-button>
      </div>
    </n-modal>

    <n-modal v-model:show="conflict.show" preset="card" :title="t('files.conflictTitle')" style="width: 460px">
      <p v-if="conflict.info" class="conflict-path">{{ conflict.info.path }}</p>
      <p class="conflict-tip">{{ t('files.conflictTip') }}</p>
      <div class="conflict-actions">
        <n-button type="warning" @click="resolveConflict('overwrite')">{{ t('files.overwrite') }}</n-button>
        <n-button quaternary @click="resolveConflict('skip')">{{ t('files.skip') }}</n-button>
        <n-button quaternary @click="resolveConflict('rename')">{{ t('files.renameAuto') }}</n-button>
      </div>
      <label class="apply-all">
        <input v-model="conflict.applyAll" type="checkbox" />
        {{ t('files.applyAll') }}
      </label>
    </n-modal>
  </div>
</template>

<style scoped>
.file-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 6px;
  padding: 10px;
}
.panel-head {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.crumbs {
  font-size: 12px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.crumb {
  cursor: pointer;
}
.crumb:hover {
  color: #63e2b7;
}
.sep {
  margin: 0 4px;
  opacity: 0.4;
}
.toolbar {
  display: flex;
  gap: 6px;
}
.list {
  max-height: calc(100vh - 430px);
  overflow: auto;
}
.row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 6px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}
.row:hover {
  background: rgba(255, 255, 255, 0.06);
}
.icon {
  width: 18px;
  text-align: center;
}
.name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.name.dir {
  color: #63e2b7;
}
.size {
  width: 70px;
  text-align: right;
  font-size: 11px;
  opacity: 0.55;
}
.mtime {
  width: 92px;
  font-size: 11px;
  opacity: 0.55;
}
.empty {
  margin-top: 40px;
}
.transfers {
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding-top: 6px;
}
.transfers-head {
  font-size: 12px;
  opacity: 0.6;
  margin-bottom: 4px;
}
.transfers-list {
  max-height: 150px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.trow {
  font-size: 12px;
}
.tline {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tdir {
  width: 14px;
}
.tname {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tsize,
.tspeed {
  font-size: 11px;
  opacity: 0.6;
}
.tstate {
  font-size: 11px;
}
.tstate.done {
  color: #63e2b7;
}
.tstate.failed {
  color: #e88080;
}
.tstate.canceled,
.tstate.skipped {
  opacity: 0.5;
}
.tstate.paused {
  color: #f0c674;
}
.tresume {
  margin-left: 4px;
  font-size: 10px;
  color: #f0c674;
}
.tops {
  width: 16px;
}
.op {
  cursor: pointer;
  opacity: 0.7;
}
.op:hover {
  opacity: 1;
}
.bar {
  height: 3px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 2px;
  margin-top: 2px;
}
.bar-fill {
  height: 100%;
  background: #63e2b7;
  border-radius: 2px;
  transition: width 0.2s;
}
.bar-fill.failed {
  background: #e88080;
}
.terr {
  font-size: 11px;
  color: #e88080;
  opacity: 0.8;
}
.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}
.conflict-path {
  word-break: break-all;
  font-size: 13px;
}
.conflict-tip {
  font-size: 12px;
  opacity: 0.6;
}
.conflict-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.apply-all {
  display: block;
  margin-top: 12px;
  font-size: 12px;
  opacity: 0.8;
}
</style>
