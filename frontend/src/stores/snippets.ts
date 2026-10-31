import { ref } from 'vue'
import { defineStore } from 'pinia'
import * as models from '../../wailsjs/go/models'
import {
  DeleteSnippet,
  ListSnippets,
  SaveSnippet,
} from '../../wailsjs/go/app/Service'

// 命令片段：点击发送到当前活跃终端；{host} 占位符在发送时替换为目标主机名
export const useSnippetsStore = defineStore('snippets', () => {
  const snippets = ref<models.store.Snippet[]>([])

  async function load() {
    const list = await ListSnippets()
    snippets.value = list ?? []
  }

  async function save(sn: models.store.Snippet) {
    await SaveSnippet(sn)
    await load()
  }

  async function remove(id: string) {
    await DeleteSnippet(id)
    await load()
  }

  return { snippets, load, save, remove }
})
