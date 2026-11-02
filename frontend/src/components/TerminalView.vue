<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NDropdown, NInput } from 'naive-ui'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'
import { ClipboardGetText, ClipboardSetText } from '../../wailsjs/runtime/runtime'
import { useTerminalsStore, type TerminalPane } from '../stores/terminals'
import { useFilesStore } from '../stores/files'
import { useTransfersStore } from '../stores/transfers'
import * as models from '../../wailsjs/go/models'

// 单个终端视图：xterm 实例 + 事件绑定 + 快捷键 + 搜索/右键菜单
// pane 为标签内的一个会话视图（分屏时一个标签渲染多个）
const props = defineProps<{ tabId: string; pane: TerminalPane }>()
const { t } = useI18n()
const dialog = useDialog()
const message = useMessage()
const store = useTerminalsStore()
const files = useFilesStore()
const transfers = useTransfersStore()

const container = ref<HTMLDivElement>()
const searchVisible = ref(false)
const searchText = ref('')
const exited = ref('') // 非空 = 会话已结束，显示遮罩
const menu = reactive({ show: false, x: 0, y: 0 })

let term: Terminal
let fit: FitAddon
let searchAddon: SearchAddon
let fontSize = 14
let ro: ResizeObserver | undefined

function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return bytes
}

function copySelection() {
  const sel = term.getSelection()
  if (sel) void ClipboardSetText(sel)
}

async function pasteClipboard() {
  const txt = await ClipboardGetText()
  if (txt) term.paste(txt)
}

// 智能剪贴板（ARCHITECTURE M2 验收项）：
// Ctrl+C 有选中=复制、无选中=透传 SIGINT；Ctrl+V 粘贴；Ctrl+Shift+C/V 恒定
function onKey(ev: KeyboardEvent): boolean {
  if (ev.type !== 'keydown') return true
  const key = ev.key.toLowerCase()
  const ctrl = ev.ctrlKey && !ev.metaKey && !ev.altKey
  if (ctrl && ev.shiftKey && key === 'c') {
    copySelection()
    return false
  }
  if (ctrl && ev.shiftKey && key === 'v') {
    void pasteClipboard()
    return false
  }
  if (ctrl && !ev.shiftKey && key === 'c' && term.hasSelection()) {
    copySelection()
    return false
  }
  if (ctrl && !ev.shiftKey && key === 'v') {
    void pasteClipboard()
    return false
  }
  if (ctrl && !ev.shiftKey && key === 'f') {
    searchVisible.value = true
    return false
  }
  return true
}

// Ctrl+滚轮 调整字号
function onWheel(ev: WheelEvent) {
  if (!ev.ctrlKey) return
  ev.preventDefault()
  fontSize = Math.min(32, Math.max(8, fontSize + (ev.deltaY < 0 ? 1 : -1)))
  term.options.fontSize = fontSize
  fit.fit()
}

function openMenu(e: MouseEvent) {
  menu.x = e.clientX
  menu.y = e.clientY
  menu.show = true
}

const menuOptions = [
  { label: t('terminal.copy'), key: 'copy' },
  { label: t('terminal.paste'), key: 'paste' },
  { label: t('terminal.search'), key: 'search' },
  { label: t('terminal.clear'), key: 'clear' },
  { type: 'divider', key: 'd1' },
  { label: t('terminal.disconnect'), key: 'disconnect' },
]

function onMenuSelect(key: string) {
  menu.show = false
  switch (key) {
    case 'copy':
      copySelection()
      break
    case 'paste':
      void pasteClipboard()
      break
    case 'search':
      searchVisible.value = true
      break
    case 'clear':
      term.clear()
      break
    case 'disconnect':
      dialog.warning({
        title: t('terminal.disconnectTitle'),
        content: t('terminal.disconnectConfirm', { name: props.pane.hostName }),
        positiveText: t('terminal.disconnect'),
        negativeText: t('common.cancel'),
        onPositiveClick: () => store.closePane(props.tabId, props.pane.sessionId),
      })
      break
  }
}

function findNext() {
  if (searchText.value) searchAddon.findNext(searchText.value)
}
function findPrevious() {
  if (searchText.value) searchAddon.findPrevious(searchText.value)
}

onMounted(() => {
  term = new Terminal({
    fontSize,
    fontFamily: 'Consolas, "Microsoft YaHei Mono", "Courier New", monospace',
    cursorBlink: true,
    scrollback: 5000,
    theme: { background: '#18181c', foreground: '#d4d4d4' },
  })
  fit = new FitAddon()
  searchAddon = new SearchAddon()
  term.loadAddon(fit)
  term.loadAddon(searchAddon)
  try {
    term.loadAddon(new WebglAddon()) // 渲染加速；不可用时自动回退 DOM 渲染
  } catch {
    // 忽略
  }
  term.open(container.value!)

  // 必须先挂 resize 监听再做 fit：首帧 fit 触发的尺寸变化是 PTY 初始
  // 80x24 → 真实尺寸的第一次同步，注册晚了就会丢失这次 WindowChange
  term.onData((d) => handleData(d))
  term.onResize(({ cols, rows }) => void store.resize(props.pane.sessionId, cols, rows))
  fit.fit()

  ro = new ResizeObserver(() => fit.fit())
  ro.observe(container.value!)

  store.registerOutput(props.pane.sessionId, (p) => {
    if (p.dataB64) term.write(b64ToBytes(p.dataB64))
    if (p.code !== undefined) exited.value = `${p.reason ?? ''} (exit ${p.code})`
  })

  term.attachCustomKeyEventHandler(onKey)
  void transfers.ensureSeeded()
})

// ---------- rz/sz 智能接管 ----------
// 用户在终端里敲 rz/sz 时，改用内置 SFTP 传输（进度/断点续传/冲突策略齐全），
// 命令本身不发送到服务器。只有当输入行以明确命令开头时才接管，其余输入原样透传。
let inputBuf = ''

function handleData(d: string) {
  if (tryRzSz(d)) return
  void store.write(props.pane.sessionId, d)
}

function tryRzSz(d: string): boolean {
  // 含控制序列（方向键/粘贴多行等）时跟踪失准，放弃接管
  for (const ch of d) {
    if (ch === '\r') {
      const line = inputBuf.trim()
      inputBuf = ''
      if (line) {
        const handled = interceptCommand(line)
        if (handled) return true
      }
      continue
    }
    if (ch === '\x7f' || ch === '\x08') {
      inputBuf = inputBuf.slice(0, -1)
      continue
    }
    if (ch >= ' ' && ch <= '~') {
      inputBuf += ch
      continue
    }
    // 其他控制字符（含方向键/粘贴前缀）→ 停用本行跟踪
    if (ch !== '\t') inputBuf = ''
  }
  return false
}

// interceptCommand 识别并拦截 rz/sz；返回 true 表示已接管
function interceptCommand(line: string): boolean {
  const sid = props.pane.sessionId
  // sz [-opts] file1 [file2 ...]
  const szMatch = /^sz\s+((?:-\S+\s+)*)(.+)$/.exec(line)
  if (szMatch) {
    const targets = szMatch[2].trim().split(/\s+/).filter(Boolean)
    if (targets.length > 0) {
      void downloadViaSftp(sid, targets)
      return true
    }
  }
  // rz [-opts]
  if (/^rz(\s+-\S+)*\s*$/.test(line)) {
    void uploadViaSftp(sid)
    return true
  }
  return false
}

async function downloadViaSftp(sid: string, targets: string[]) {
  try {
    for (const t of targets) {
      const abs = t.startsWith('/') ? t : `${files.cwd || '.'}/${t}`
      await files.downloadToLocal(sid, {
        name: abs.split('/').pop() ?? abs,
        path: abs,
        isDir: false,
        size: 0,
        mode: '',
        mtime: null,
      } as models.sftp.RemoteEntry)
    }
    message.success(t('terminal.rzszDownload', { n: targets.length, dir: files.localCwd }))
  } catch (e) {
    message.error(String(e))
  }
}

async function uploadViaSftp(sid: string) {
  try {
    await files.uploadFiles(sid, files.cwd)
    message.success(t('terminal.rzszUpload'))
  } catch (e) {
    message.error(String(e))
  }
}

onBeforeUnmount(() => {
  ro?.disconnect()
  store.unregisterOutput(props.pane.sessionId)
  term?.dispose()
})
</script>

<template>
  <div class="term-wrap">
    <div v-show="searchVisible" class="search-bar">
      <n-input
        v-model:value="searchText"
        size="small"
        :placeholder="t('terminal.searchPlaceholder')"
        @keyup.enter="findNext"
      />
      <n-button size="small" @click="findPrevious">↑</n-button>
      <n-button size="small" @click="findNext">↓</n-button>
      <n-button size="small" quaternary @click="searchVisible = false">×</n-button>
    </div>
    <div ref="container" class="term" @wheel="onWheel" @contextmenu.prevent="openMenu" />
    <div v-if="pane.state === 'reconnecting'" class="overlay warn">
      {{ t('terminal.reconnecting', { name: pane.hostName }) }}
    </div>
    <div v-if="exited" class="overlay">
      <div>{{ t('terminal.exited', { info: exited }) }}</div>
      <n-button size="small" class="close-btn" @click="store.closePane(tabId, pane.sessionId)">
        {{ t('common.close') }}
      </n-button>
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
  </div>
</template>

<style scoped>
.term-wrap {
  position: relative;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.term {
  flex: 1;
  padding: 4px 8px;
}
.search-bar {
  position: absolute;
  top: 6px;
  right: 16px;
  z-index: 10;
  display: flex;
  gap: 4px;
  align-items: center;
  background: rgba(40, 40, 48, 0.96);
  padding: 4px;
  border-radius: 4px;
  border: 1px solid rgba(255, 255, 255, 0.12);
}
.search-bar :deep(.n-input) {
  width: 180px;
}
.overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  background: rgba(10, 10, 14, 0.72);
  font-size: 14px;
}
.overlay.warn {
  background: rgba(10, 10, 14, 0.35);
  color: #f0c674;
}
.close-btn {
  min-width: 88px;
}
</style>
