import { ref } from 'vue'
import { defineStore } from 'pinia'
import * as models from '../../wailsjs/go/models'
import {
  LocalHome,
  LocalListDir,
  LocalParent,
  LocalRoots,
  PickDir,
  PickSavePath,
  PickUploadFile,
  PickUploadFiles,
  SftpDownload,
  SftpHome,
  SftpListDir,
  SftpMkdir,
  SftpRemove,
  SftpRename,
  SftpUpload,
} from '../../wailsjs/go/app/Service'

// 远程文件浏览器状态：跟随当前活跃终端会话（切换标签时重置）
export const useFilesStore = defineStore('files', () => {
  const cwd = ref('')
  const entries = ref<models.sftp.RemoteEntry[]>([])
  const loading = ref(false)
  const showPanel = ref(false)

  // 本地栏（双面板）
  const localCwd = ref('')
  const localEntries = ref<models.fsx.Entry[]>([])
  const localLoading = ref(false)
  const localRoots = ref<string[]>([])

  async function load(sessionId: string, dir: string) {
    loading.value = true
    try {
      if (dir === '') {
        // 首次打开：定位到远程用户主目录
        dir = await SftpHome(sessionId)
      }
      const list = await SftpListDir(sessionId, dir)
      entries.value = list ?? []
      cwd.value = dir
    } finally {
      loading.value = false
    }
  }

  async function reload(sessionId: string) {
    await load(sessionId, cwd.value)
  }

  async function enter(sessionId: string, dir: string) {
    await load(sessionId, dir)
  }

  async function up(sessionId: string) {
    if (cwd.value === '/' || !cwd.value) return
    const idx = cwd.value.lastIndexOf('/')
    const parent = idx > 0 ? cwd.value.slice(0, idx) : '/'
    await load(sessionId, parent)
  }

  async function mkdir(sessionId: string, dir: string, name: string) {
    await SftpMkdir(sessionId, joinPath(dir, name))
    await reload(sessionId)
  }

  async function rename(sessionId: string, entry: models.sftp.RemoteEntry, newName: string) {
    await SftpRename(sessionId, entry.path, joinPath(parentOf(entry.path), newName))
    await reload(sessionId)
  }

  async function remove(sessionId: string, entry: models.sftp.RemoteEntry) {
    await SftpRemove(sessionId, entry.path)
    await reload(sessionId)
  }

  // download 单文件：另存为对话框的完整路径直达（支持自定义文件名）
  async function downloadFile(sessionId: string, entry: models.sftp.RemoteEntry) {
    const savePath = await PickSavePath(entry.name)
    if (!savePath) return
    await SftpDownload(sessionId, entry.path, savePath)
  }

  // download 目录：目录选择框
  async function downloadDir(sessionId: string, entry: models.sftp.RemoteEntry) {
    const dir = await PickDir()
    if (!dir) return
    await SftpDownload(sessionId, entry.path, dir)
  }

  async function upload(sessionId: string, dir: string) {
    const localPath = await PickUploadFile()
    if (!localPath) return
    await SftpUpload(sessionId, dir, localPath)
  }

  // uploadFiles 多选文件上传（一次对话框选多个）
  async function uploadFiles(sessionId: string, dir: string) {
    const paths = await PickUploadFiles()
    if (!paths || paths.length === 0) return
    await uploadPaths(sessionId, dir, paths)
  }

  // uploadDir 选择目录整体上传（后端递归展开）
  async function uploadDir(sessionId: string, dir: string) {
    const localPath = await PickDir()
    if (!localPath) return
    await SftpUpload(sessionId, dir, localPath)
  }

  async function uploadPaths(sessionId: string, dir: string, paths: string[]) {
    for (const p of paths) {
      await SftpUpload(sessionId, dir, p)
    }
  }

  // ---------- 本地栏 ----------

  async function loadLocal(dir: string) {
    localLoading.value = true
    if (localRoots.value.length === 0) {
      localRoots.value = (await LocalRoots().catch(() => [])) ?? []
    }
    try {
      const target = dir || (await LocalHome())
      const list = await LocalListDir(target)
      localEntries.value = list ?? []
      localCwd.value = target
    } finally {
      localLoading.value = false
    }
  }

  async function localEnter(dir: string) {
    await loadLocal(dir)
  }

  async function localUp() {
    if (!localCwd.value) return
    await loadLocal(await LocalParent(localCwd.value))
  }

  // onTerminalCwd 终端 OSC 7 上报当前目录 → 文件面板跟随（需开启目录同步）
  let cwdTimer: ReturnType<typeof setTimeout> | null = null
  function onTerminalCwd(sessionId: string, path: string) {
    if (!showPanel.value || !path) return
    if (cwd.value === path) return
    // 防抖：连续 cd 只刷新最后一次
    if (cwdTimer) clearTimeout(cwdTimer)
    cwdTimer = setTimeout(() => {
      void load(sessionId, path).catch(() => {
        // 目录不可读（权限/已删除）时保持原状
      })
    }, 250)
  }

  // downloadToLocal 远程条目下载到本地当前目录
  async function downloadToLocal(sessionId: string, entry: models.sftp.RemoteEntry) {
    if (!localCwd.value) return
    await SftpDownload(sessionId, entry.path, localCwd.value)
  }

  return {
    cwd,
    entries,
    loading,
    showPanel,
    localCwd,
    localEntries,
    localLoading,
    localRoots,
    loadLocal,
    localEnter,
    localUp,
    downloadToLocal,
    onTerminalCwd,
    load,
    reload,
    enter,
    up,
    mkdir,
    rename,
    remove,
    downloadFile,
    downloadDir,
    upload,
    uploadFiles,
    uploadDir,
    uploadPaths,
  }
})

function joinPath(dir: string, name: string): string {
  if (dir.endsWith('/')) return dir + name
  return `${dir}/${name}`
}

function parentOf(p: string): string {
  const idx = p.lastIndexOf('/')
  return idx > 0 ? p.slice(0, idx) : '/'
}
