<script setup lang="ts">
import { computed, h, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import {
  NButton,
  NDropdown,
  NEmpty,
  NInput,
  NSpin,
  NTree,
  type DropdownOption,
  type TreeOption,
} from 'naive-ui'
import { useConnectionsStore, type TreeNode } from '../stores/connections'
import { useTerminalsStore } from '../stores/terminals'
import * as models from '../../wailsjs/go/models'
import GroupNameModal from './GroupNameModal.vue'
import HostFormModal from './HostFormModal.vue'
import ImportModal from './ImportModal.vue'
import PasswordPromptModal from './PasswordPromptModal.vue'

// 连接管理器：搜索 + 分组树 + 最近连接 + 右键菜单（CRUD/排序/克隆）
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const store = useConnectionsStore()
const terminals = useTerminalsStore()

const search = ref('')
const expandedKeys = ref<string[]>([])

// ---------- 弹窗状态 ----------

const groupModal = reactive({
  show: false,
  group: null as models.store.Group | null,
  parentId: '',
})
const hostModal = reactive({
  show: false,
  host: null as models.store.Host | null,
  groupId: '',
})
const importModal = ref(false)
const credModal = reactive({
  show: false,
  host: null as models.store.Host | null,
  kind: 'password' as 'password' | 'passphrase',
})

// ---------- 树 ----------

const treeData = computed(() => store.treeData as unknown as TreeOption[])

onMounted(() => {
  store.load().catch((e) => message.error(String(e)))
})

function findNode(key: string): TreeNode | undefined {
  const walk = (nodes: TreeNode[]): TreeNode | undefined => {
    for (const n of nodes) {
      if (n.key === key) return n
      const hit = n.children ? walk(n.children) : undefined
      if (hit) return hit
    }
    return undefined
  }
  return walk(store.treeData)
}

const nodeProps = ({ option }: { option: TreeOption }) => ({
  onDblclick: () => {
    const node = option as unknown as TreeNode
    if (node.kind === 'host') tryConnect(node)
  },
  onContextmenu: (e: MouseEvent) => {
    e.preventDefault()
    openMenu(e.clientX, e.clientY, option as unknown as TreeNode)
  },
})

// ---------- 右键菜单 ----------

const menu = reactive({ show: false, x: 0, y: 0, node: null as TreeNode | null })

function openMenu(x: number, y: number, node: TreeNode) {
  menu.x = x
  menu.y = y
  menu.node = node
  menu.show = true
}

const menuOptions = computed<DropdownOption[]>(() => {
  const node = menu.node
  if (!node) return []
  if (node.kind === 'group') {
    return [
      { label: t('menu.newHostHere'), key: 'newHostHere' },
      { label: t('menu.newSubGroup'), key: 'newSubGroup' },
      { type: 'divider', key: 'd1' },
      { label: t('menu.rename'), key: 'renameGroup' },
      { label: t('menu.moveUp'), key: 'groupUp', disabled: !canMove(node, -1) },
      { label: t('menu.moveDown'), key: 'groupDown', disabled: !canMove(node, 1) },
      { type: 'divider', key: 'd2' },
      { label: t('menu.delete'), key: 'deleteGroup', props: { style: 'color: #e88080' } },
    ]
  }
  return [
    { label: t('menu.connect'), key: 'connect' },
    { label: t('menu.connectSplit'), key: 'connectSplit' },
    { type: 'divider', key: 'd1' },
    { label: t('menu.edit'), key: 'editHost' },
    { label: t('menu.clone'), key: 'cloneHost' },
    { label: t('menu.moveUp'), key: 'hostUp', disabled: !canMove(node, -1) },
    { label: t('menu.moveDown'), key: 'hostDown', disabled: !canMove(node, 1) },
    { type: 'divider', key: 'd2' },
    { label: t('menu.delete'), key: 'deleteHost', props: { style: 'color: #e88080' } },
  ]
})

function canMove(node: TreeNode, dir: -1 | 1): boolean {
  if (!store.treeData.length) return false
  const siblings = siblingsOf(node)
  const idx = siblings.findIndex((s) => s.key === node.key)
  return idx >= 0 && idx + dir >= 0 && idx + dir < siblings.length
}

function siblingsOf(node: TreeNode): TreeNode[] {
  // 兄弟 = 树中包含该节点的同一数组（树结构即排序视图）
  const findSiblings = (nodes: TreeNode[]): TreeNode[] | null => {
    for (const n of nodes) {
      if (n.key === node.key) return nodes
      if (n.children) {
        const hit = findSiblings(n.children)
        if (hit) return hit
      }
    }
    return null
  }
  return findSiblings(store.treeData) ?? []
}

async function onMenuSelect(key: string) {
  menu.show = false
  const node = menu.node
  if (!node) return
  try {
    switch (key) {
      case 'connect':
        tryConnect(node)
        break
      case 'connectSplit':
        if (node.host) await tryConnectHost(node.host, true)
        break
      case 'newHostHere':
        Object.assign(hostModal, { show: true, host: null, groupId: node.group?.id ?? '' })
        break
      case 'newSubGroup':
        Object.assign(groupModal, { show: true, group: null, parentId: node.group?.id ?? '' })
        break
      case 'renameGroup':
        Object.assign(groupModal, { show: true, group: node.group ?? null, parentId: '' })
        break
      case 'editHost':
        Object.assign(hostModal, { show: true, host: node.host ?? null, groupId: '' })
        break
      case 'cloneHost': {
        if (!node.host) return
        const src = node.host
        await store.saveHost(
          new models.app.HostInput({
            groupId: src.groupId,
            name: `${src.name} - ${t('hostForm.cloneSuffix')}`,
            addr: src.addr,
            port: src.port,
            username: src.username,
            authType: src.authType,
            keyPath: src.keyPath,
            proxyChain: [...(src.proxyChain ?? [])],
            initCommands: [...src.initCommands],
            keepaliveSec: src.keepaliveSec,
            autoReconnect: src.autoReconnect,
          }),
        )
        // 克隆有意不复制凭据（密码只存 keychain 原条目，安全优先）
        message.success(t('tree.cloneDone'))
        break
      }
      case 'groupUp':
        if (node.group) await store.moveSibling('group', node.group.id, -1)
        break
      case 'groupDown':
        if (node.group) await store.moveSibling('group', node.group.id, 1)
        break
      case 'hostUp':
        if (node.host) await store.moveSibling('host', node.host.id, -1)
        break
      case 'hostDown':
        if (node.host) await store.moveSibling('host', node.host.id, 1)
        break
      case 'deleteGroup': {
        const g = node.group
        if (!g) return
        dialog.warning({
          title: t('tree.deleteTitle'),
          content: t('tree.deleteGroupConfirm', { name: g.name }),
          positiveText: t('common.delete'),
          negativeText: t('common.cancel'),
          onPositiveClick: async () => {
            try {
              await store.deleteGroup(g.id)
              message.success(t('common.saved'))
            } catch (e) {
              // 分组非空等后端约束错误直接展示
              message.error(String(e))
            }
          },
        })
        break
      }
      case 'deleteHost': {
        const hst = node.host
        if (!hst) return
        dialog.warning({
          title: t('tree.deleteTitle'),
          content: t('tree.deleteHostConfirm', { name: hst.name }),
          positiveText: t('common.delete'),
          negativeText: t('common.cancel'),
          onPositiveClick: async () => {
            await store.deleteHost(hst.id)
            message.success(t('common.saved'))
          },
        })
        break
      }
    }
  } catch (e) {
    message.error(String(e))
  }
}

// 真实连接流程：直连 → TOFU 未知指纹弹窗（信任后 add 重试）→
// 指纹变更红色警告（明确确认后 replace 重试）→ 其余错误直接提示
async function tryConnectHost(host: models.store.Host, split = false) {
  if (!host) return
  const res = await (split ? terminals.splitPane(host) : terminals.connect(host))
  if (res.ok) return
  if (res.errorCode === 'host_key_unknown') {
    dialog.warning({
      title: t('tofu.unknownTitle'),
      content: t('tofu.unknownContent', { name: host.name, fp: res.fingerprint }),
      positiveText: t('tofu.trust'),
      negativeText: t('common.cancel'),
      onPositiveClick: async () => {
        const r2 = await (split ? terminals.splitPane(host, 'add') : terminals.connect(host, 'add'))
        if (!r2.ok) message.error(r2.message || t('terminal.connectFailed'))
      },
    })
    return
  }
  if (res.errorCode === 'host_key_changed') {
    dialog.error({
      title: t('tofu.changedTitle'),
      content: t('tofu.changedContent', { name: host.name, fp: res.fingerprint }),
      positiveText: t('tofu.replaceKey'),
      negativeText: t('common.cancel'),
      onPositiveClick: async () => {
        const r2 = await (split ? terminals.splitPane(host, 'replace') : terminals.connect(host, 'replace'))
        if (!r2.ok) message.error(r2.message || t('terminal.connectFailed'))
      },
    })
    return
  }
  if (res.errorCode === 'auth_failed') {
    // 无凭据/凭据错误：引导补录后立即重试（Xshell 导入的主机首次连接的常规路径）
    const kind = /passphrase/i.test(res.message || '') ? 'passphrase' : 'password'
    Object.assign(credModal, { show: true, host, kind })
    return
  }
  message.error(res.message || t('terminal.connectFailed'))
}

// 凭据补录保存后自动重连
async function onCredSaved() {
  if (!credModal.host) return
  try {
    await store.load()
    const fresh = store.hosts.find((h) => h.id === credModal.host?.id) ?? credModal.host
    const res = await terminals.connect(fresh)
    if (!res.ok) message.error(res.message || t('terminal.connectFailed'))
  } catch (e) {
    message.error(String(e))
  }
}

function tryConnect(node: TreeNode) {
  if (node.kind === 'host' && node.host) void tryConnectHost(node.host)
}

// ---------- 工具栏 ----------

function newRootGroup() {
  Object.assign(groupModal, { show: true, group: null, parentId: '' })
}

function newRootHost() {
  Object.assign(hostModal, { show: true, host: null, groupId: '' })
}

const allGroupKeys = computed(() => store.groups.map((g) => `group:${g.id}`))

// 分组异步加载完成/变化后，同步展开态（保留用户手动收起的节点）
watch(allGroupKeys, (keys) => {
  expandedKeys.value = Array.from(new Set([...expandedKeys.value, ...keys]))
})

// 树节点 suffix 显示主机地址（灰色小字）
function renderSuffix({ option }: { option: TreeOption }) {
  const node = option as unknown as TreeNode
  if (node.kind !== 'host' || !node.host) return null
  return h('span', { class: 'node-addr' }, `${node.host.addr}:${node.host.port}`)
}
</script>

<template>
  <div class="conn-tree">
    <div class="toolbar">
      <n-button size="small" dashed @click="newRootHost">{{ t('tree.newHost') }}</n-button>
      <n-button size="small" dashed @click="newRootGroup">{{ t('tree.newGroup') }}</n-button>
      <n-button size="small" dashed @click="importModal = true">{{ t('import.title') }}</n-button>
    </div>
    <n-input
      v-model:value="search"
      size="small"
      clearable
      :placeholder="t('tree.searchPlaceholder')"
    />
    <n-spin :show="store.loading" size="small">
      <n-tree
        :data="treeData"
        :pattern="search"
        :show-irrelevant-nodes="false"
        block-node
        :expanded-keys="expandedKeys"
        @update:expanded-keys="(keys: string[]) => (expandedKeys = keys)"
        :node-props="nodeProps"
        :render-suffix="renderSuffix"
        class="tree"
      />
    </n-spin>

    <div class="section-title">{{ t('tree.recent') }}</div>
    <n-empty
      v-if="store.recentHosts.length === 0"
      size="small"
      :description="t('tree.noRecent')"
      class="recent-empty"
    />
    <div v-else class="recent">
      <div
        v-for="h in store.recentHosts"
        :key="h.id"
        class="recent-item"
        @dblclick="tryConnectHost(h)"
      >
        <span class="recent-name">{{ h.name }}</span>
        <span class="node-addr">{{ h.addr }}</span>
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

    <GroupNameModal v-model:show="groupModal.show" :group="groupModal.group" :parent-id="groupModal.parentId" />
    <HostFormModal v-model:show="hostModal.show" :host="hostModal.host" :group-id="hostModal.groupId" />
    <ImportModal v-model:show="importModal" @imported="() => void store.load()" />
    <PasswordPromptModal
      v-model:show="credModal.show"
      :host="credModal.host"
      :kind="credModal.kind"
      @saved="onCredSaved"
    />
  </div>
</template>

<style scoped>
.conn-tree {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  height: 100%;
}
.toolbar {
  display: flex;
  gap: 8px;
}
.tree {
  flex: 1;
  overflow: auto;
}
.section-title {
  margin-top: 8px;
  font-size: 12px;
  opacity: 0.6;
}
.recent {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.recent-item {
  display: flex;
  justify-content: space-between;
  padding: 4px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}
.recent-item:hover {
  background: rgba(255, 255, 255, 0.06);
}
.recent-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.node-addr {
  font-size: 12px;
  opacity: 0.5;
}
.recent-empty {
  margin-top: 12px;
}
</style>
