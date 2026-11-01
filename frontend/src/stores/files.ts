import { ref } from 'vue'
import { defineStore } from 'pinia'
import * as models from '../../wailsjs/go/models'
import {
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

  return {
    cwd,
    entries,
    loading,
    showPanel,
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
