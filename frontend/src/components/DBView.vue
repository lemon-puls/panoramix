<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NInput, NInputNumber } from 'naive-ui'
import {
  DBDescribeTable,
  DBListDatabases,
  DBListTables,
  DBQuery,
  DBShowCreateTable,
  DeleteDBConn,
  ListDBConns,
  RedisDeleteKey,
  RedisGetValue,
  RedisScanKeys,
  RedisSetString,
  RedisSetTTL,
  SaveDBConn,
  TestDBConn,
} from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'
import DBConnModal from './DBConnModal.vue'

// 数据库视图（HeidiSQL 风格）：
// 左：连接 → 库 → 表 树；右：SQL 工作台（编辑器 + 结果/结构）或 Redis 值查看器
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const conns = ref<models.store.DBConn[]>([])
const activeId = ref<string>('')
const modal = ref(false)
const editing = ref<models.store.DBConn | null>(null)
const loadError = ref('')

const active = computed(() => conns.value.find((c) => c.id === activeId.value) ?? null)
const isRedis = computed(() => active.value?.kind === 'redis')

// ---------- 连接管理 ----------

async function load() {
  try {
    conns.value = (await ListDBConns()) ?? []
    if (!activeId.value && conns.value.length > 0) activeId.value = conns.value[0].id
  } catch (e) {
    loadError.value = String(e)
    console.error('[db] load connections failed', e)
  }
}

function openNew() {
  editing.value = null
  modal.value = true
}

function openEdit(c: models.store.DBConn) {
  editing.value = c
  modal.value = true
}

async function onSaved() {
  await load()
}

function askDelete(c: models.store.DBConn) {
  dialog.warning({
    title: t('tree.deleteTitle'),
    content: t('db.deleteConfirm', { name: c.name }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await DeleteDBConn(c.id)
        if (activeId.value === c.id) activeId.value = ''
        await load()
        message.success(t('common.saved'))
      } catch (e) {
        message.error(String(e))
      }
    },
  })
}

async function testConn(c: models.store.DBConn) {
  try {
    await TestDBConn(c.id)
    message.success(t('db.testOk'))
  } catch (e) {
    message.error(String(e))
  }
}

// ---------- MySQL 库/表树 ----------

const databases = ref<string[]>([])
const tablesByDb = reactive<Record<string, models.db.TableInfo[]>>({})
const expandedDbs = ref<Set<string>>(new Set())
const currentDb = ref('')
const treeLoading = ref(false)

// 当前选中的表（右侧显示结构或数据）


async function toggleConn() {
  if (!active.value || isRedis.value) return
  if (databases.value.length > 0) {
    databases.value = []
    expandedDbs.value = new Set()
    return
  }
  treeLoading.value = true
  try {
    databases.value = (await DBListDatabases(active.value.id)) ?? []
  } catch (e) {
    message.error(String(e))
  } finally {
    treeLoading.value = false
  }
}

async function toggleDb(db: string) {
  if (!active.value) return
  const next = new Set(expandedDbs.value)
  if (next.has(db)) {
    next.delete(db)
  } else {
    next.add(db)
    if (!tablesByDb[db]) {
      try {
        tablesByDb[db] = (await DBListTables(active.value.id, db)) ?? []
      } catch (e) {
        message.error(String(e))
        tablesByDb[db] = []
      }
    }
  }
  expandedDbs.value = next
}

// 单击表 → 打开结构标签（已存在则激活）
async function openTableStructure(db: string, table: models.db.TableInfo) {
  const id = activeId.value
  const exist = tabs.value.find(
    (x) => x.kind === 'structure' && x.connId === id && x.database === db && x.table === table.name,
  )
  if (exist) {
    activeTabId.value = exist.id
    return
  }
  const tab = newTab('structure', {
    title: `${table.name} · ${t('db.structureShort')}`,
    database: db,
    table: table.name,
  })
  tabs.value.push(tab)
  activeTabId.value = tab.id
  try {
    tab.columns = (await DBDescribeTable(id, db, table.name)) ?? []
  } catch (e) {
    tab.error = String(e)
  }
}

// 双击表 → 打开数据标签（默认 1000 条）
async function openTableData(db: string, table: models.db.TableInfo) {
  const id = activeId.value
  const exist = tabs.value.find(
    (x) => x.kind === 'data' && x.connId === id && x.database === db && x.table === table.name,
  )
  if (exist) {
    activeTabId.value = exist.id
    await loadTableData(exist, true)
    return
  }
  const tab = newTab('data', { title: table.name, database: db, table: table.name })
  tabs.value.push(tab)
  activeTabId.value = tab.id
  await loadTableData(tab, true)
}

// ---------- 工作标签（HeidiSQL 风格：数据 / 查询 / 结构） ----------

type TabKind = 'query' | 'data' | 'structure'

interface WorkTab {
  id: string
  kind: TabKind
  title: string
  connId: string
  database: string
  table: string
  sql: string
  filter: string
  page: number
  pageSize: number
  sortCol: string
  sortDir: 1 | -1
  result: models.db.Result | null
  error: string
  loading: boolean
  columns: models.db.ColumnInfo[]
  ddl: string
  showDDL: boolean
  gridSortCol: number
  gridSortDir: 1 | -1
  selectedRow: number
  selectedCol: number
}

let tabSeq = 0
const tabs = ref<WorkTab[]>([])
const activeTabId = ref('')
const activeTab = computed(() => tabs.value.find((x) => x.id === activeTabId.value) ?? null)

function newTab(kind: TabKind, init: Partial<WorkTab>): WorkTab {
  return {
    id: `tab-${++tabSeq}`,
    kind,
    title: '',
    connId: activeId.value,
    database: '',
    table: '',
    sql: 'SELECT 1',
    filter: '',
    page: 0,
    pageSize: 1000,
    sortCol: '',
    sortDir: 1,
    result: null,
    error: '',
    loading: false,
    columns: [],
    ddl: '',
    showDDL: false,
    gridSortCol: -1,
    gridSortDir: 1,
    selectedRow: -1,
    selectedCol: -1,
    ...init,
  }
}

function openQuery() {
  const n = tabs.value.filter((x) => x.kind === 'query').length + 1
  const tab = newTab('query', { title: t('db.queryN', { n }) })
  tabs.value.push(tab)
  activeTabId.value = tab.id
}

function closeTab(id: string) {
  const idx = tabs.value.findIndex((x) => x.id === id)
  if (idx === -1) return
  tabs.value.splice(idx, 1)
  if (activeTabId.value === id) activeTabId.value = tabs.value.at(-1)?.id ?? ''
}

// 数据标签 SQL 生成（用户筛选条件原样拼入 WHERE，与 HeidiSQL 一致）
function buildTableSQL(tab: WorkTab): string {
  const parts = [`SELECT * FROM \`${tab.database}\`.\`${tab.table}\``]
  const filter = tab.filter.trim()
  if (filter) parts.push(`WHERE ${filter}`)
  if (tab.sortCol) parts.push(`ORDER BY \`${tab.sortCol}\` ${tab.sortDir === 1 ? 'ASC' : 'DESC'}`)
  parts.push(`LIMIT ${tab.pageSize} OFFSET ${tab.page * tab.pageSize}`)
  return parts.join(' ')
}

async function loadTableData(tab: WorkTab, resetPage = false) {
  if (resetPage) tab.page = 0
  tab.loading = true
  tab.error = ''
  try {
    tab.result = await DBQuery(tab.connId, buildTableSQL(tab), tab.pageSize)
    tab.gridSortCol = -1
    tab.selectedRow = -1
    tab.selectedCol = -1
  } catch (e) {
    tab.error = String(e)
    tab.result = null
  } finally {
    tab.loading = false
  }
}

function sortTableByColumn(tab: WorkTab, colName: string) {
  if (tab.sortCol === colName) tab.sortDir = tab.sortDir === 1 ? -1 : 1
  else {
    tab.sortCol = colName
    tab.sortDir = 1
  }
  void loadTableData(tab, true)
}

function hasNextPage(tab: WorkTab): boolean {
  return (tab.result?.rows?.length ?? 0) >= tab.pageSize
}

function nextPage(tab: WorkTab) {
  tab.page++
  void loadTableData(tab)
}

function prevPage(tab: WorkTab) {
  if (tab.page === 0) return
  tab.page--
  void loadTableData(tab)
}

function gotoStructure(tab: WorkTab) {
  const info = tablesByDb[tab.database]?.find((x) => x.name === tab.table)
  if (info) void openTableStructure(tab.database, info)
}

// 查询执行
async function runQueryTab(tab: WorkTab) {
  const text = tab.sql.trim()
  if (!text) return
  tab.loading = true
  tab.error = ''
  try {
    tab.result = await DBQuery(tab.connId, text, 1000)
    tab.gridSortCol = -1
    tab.selectedRow = -1
    tab.selectedCol = -1
    pushHistory(text)
    if ((tab.result.columns?.length ?? 0) > 0 && (tab.result.rows?.length ?? 0) === 0) {
      message.info(t('db.zeroRows'))
    }
  } catch (e) {
    tab.error = String(e)
    tab.result = null
    message.error(String(e))
  } finally {
    tab.loading = false
  }
}

// 结果网格
function sortedRows(tab: WorkTab) {
  const rows = tab.result?.rows ?? []
  if (tab.gridSortCol < 0) return rows
  const col = tab.gridSortCol
  const dir = tab.gridSortDir
  return [...rows].sort((a, b) => {
    const av = a[col]
    const bv = b[col]
    if (av === null && bv === null) return 0
    if (av === null) return 1
    if (bv === null) return -1
    const na = Number(av)
    const nb = Number(bv)
    if (!Number.isNaN(na) && !Number.isNaN(nb)) return (na - nb) * dir
    return av.localeCompare(bv) * dir
  })
}

function toggleGridSort(tab: WorkTab, col: number) {
  if (tab.gridSortCol === col) tab.gridSortDir = tab.gridSortDir === 1 ? -1 : 1
  else {
    tab.gridSortCol = col
    tab.gridSortDir = 1
  }
}

function copyCell(tab: WorkTab, row: number, col: number) {
  tab.selectedRow = row
  tab.selectedCol = col
  const v = sortedRows(tab)[row]?.[col]
  if (v == null) return
  navigator.clipboard?.writeText(v).then(
    () => message.success(t('db.copied')),
    () => {
      /* 忽略 */
    },
  )
}

function exportCSV(tab: WorkTab) {
  if (!tab.result?.columns?.length) return
  const esc = (v: string | null) => {
    if (v == null) return ''
    return /[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
  }
  const lines = [tab.result.columns.map(esc).join(',')]
  for (const row of sortedRows(tab)) lines.push(row.map(esc).join(','))
  const blob = new Blob([`\uFEFF${lines.join('\n')}`], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${tab.table || 'query'}_${Date.now()}.csv`
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

async function loadDDL(tab: WorkTab) {
  try {
    tab.ddl = await DBShowCreateTable(tab.connId, tab.database, tab.table)
    tab.showDDL = true
  } catch (e) {
    message.error(String(e))
  }
}

// 查询历史
const HISTORY_KEY = 'panoramix.sqlHistory'
const history = ref<string[]>([])
function loadHistory() {
  try {
    history.value = JSON.parse(localStorage.getItem(HISTORY_KEY) ?? '[]')
  } catch {
    history.value = []
  }
}
loadHistory()

function pushHistory(sql: string) {
  const list = [sql, ...history.value.filter((h) => h !== sql)].slice(0, 50)
  history.value = list
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(list))
  } catch {
    /* 忽略 */
  }
}

// ---------- Redis ----------

const redisPattern = ref('*')
const redisKeys = ref<models.db.KeyInfo[]>([])
const redisCursor = ref(0)
const redisLoading = ref(false)
const redisSelected = ref('')
const redisValue = ref<models.db.Value | null>(null)
const ttlInput = ref<number>(0)
const editValue = ref('')
const editingValue = ref(false)

async function loadRedisFirstPage() {
  if (!active.value || !isRedis.value) return
  redisKeys.value = []
  redisCursor.value = 0
  await loadRedisMore(true)
}

async function loadRedisMore(reset = false) {
  if (!active.value || redisLoading.value) return
  if (!reset && redisCursor.value === 0 && redisKeys.value.length > 0) return
  redisLoading.value = true
  try {
    const cursor = reset ? 0 : redisCursor.value
    const page = await RedisScanKeys(active.value.id, cursor, redisPattern.value || '*', 200)
    if (reset) redisKeys.value = []
    redisKeys.value = [...redisKeys.value, ...(page.keys ?? [])]
    redisCursor.value = page.cursor
  } catch (e) {
    message.error(String(e))
  } finally {
    redisLoading.value = false
  }
}

const hasMoreKeys = computed(() => redisCursor.value !== 0)

// ---------- Redis 键树（按 ":" 分层，HeidiSQL/Another-Redis-Desktop 风格） ----------

interface KeyNode {
  name: string // 该层显示名
  fullKey: string // 仅叶子有完整键名
  isLeaf: boolean
  type: string
  ttl: number
  children: KeyNode[]
}

const treeMode = ref(true)
const collapsedNodes = ref<Set<string>>(new Set())

function buildKeyTree(keys: models.db.KeyInfo[]): KeyNode[] {
  const root: KeyNode = { name: '', fullKey: '', isLeaf: false, type: '', ttl: 0, children: [] }
  const index = new Map<string, KeyNode>([['', root]])
  for (const k of keys) {
    const parts = k.key.split(':')
    let path = ''
    let parent = root
    for (let i = 0; i < parts.length; i++) {
      path = path ? `${path}:${parts[i]}` : parts[i]
      const isLeaf = i === parts.length - 1
      let node = index.get(path)
      if (!node) {
        node = {
          name: parts[i],
          fullKey: isLeaf ? k.key : '',
          isLeaf,
          type: isLeaf ? k.type : '',
          ttl: isLeaf ? k.ttl : 0,
          children: [],
        }
        index.set(path, node)
        parent.children.push(node)
      } else if (isLeaf) {
        // 同时存在 "a" 与 "a:b"：把 "a" 标记为叶子
        node.isLeaf = true
        node.fullKey = k.key
        node.type = k.type
        node.ttl = k.ttl
      }
      parent = node
    }
  }
  const sortNodes = (nodes: KeyNode[]) => {
    nodes.sort((a, b) => {
      if (a.children.length !== b.children.length) return b.children.length - a.children.length
      return a.name.localeCompare(b.name)
    })
    nodes.forEach((n) => sortNodes(n.children))
  }
  sortNodes(root.children)
  return root.children
}

const keyTree = computed(() => buildKeyTree(redisKeys.value))

function toggleNode(node: KeyNode) {
  const next = new Set(collapsedNodes.value)
  const path = node.fullKey || node.name
  if (next.has(path)) next.delete(path)
  else next.add(path)
  collapsedNodes.value = next
}

function isCollapsed(node: KeyNode): boolean {
  return collapsedNodes.value.has(node.fullKey || node.name)
}

// 扁平化（预计算缩进），避免递归组件
const flatTree = computed(() => {
  const out: { node: KeyNode; depth: number; path: string }[] = []
  const walk = (nodes: KeyNode[], depth: number, prefix: string) => {
    for (const n of nodes) {
      const path = prefix ? `${prefix}:${n.name}` : n.name
      out.push({ node: n, depth, path })
      if (n.children.length > 0 && !collapsedNodes.value.has(path)) {
        walk(n.children, depth + 1, path)
      }
    }
  }
  walk(keyTree.value, 0, '')
  return out
})

async function selectKey(k: models.db.KeyInfo) {
  if (!active.value) return
  redisSelected.value = k.key
  try {
    redisValue.value = await RedisGetValue(active.value.id, k.key)
    ttlInput.value = redisValue.value.ttl > 0 ? redisValue.value.ttl : 0
    editValue.value = redisValue.value.str ?? ''
    editingValue.value = false
  } catch (e) {
    message.error(String(e))
    redisValue.value = null
  }
}

async function saveTTL() {
  if (!active.value || !redisValue.value) return
  try {
    await RedisSetTTL(active.value.id, redisValue.value.key, ttlInput.value)
    message.success(t('common.saved'))
    await refreshSelected()
  } catch (e) {
    message.error(String(e))
  }
}

async function deleteKey() {
  if (!active.value || !redisValue.value) return
  const key = redisValue.value.key
  try {
    await RedisDeleteKey(active.value.id, key)
    redisValue.value = null
    redisKeys.value = redisKeys.value.filter((k) => k.key !== key)
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  }
}

async function saveStringValue() {
  if (!active.value || !redisValue.value) return
  try {
    await RedisSetString(active.value.id, redisValue.value.key, editValue.value, true)
    message.success(t('common.saved'))
    editingValue.value = false
    await refreshSelected()
  } catch (e) {
    message.error(String(e))
  }
}

async function refreshSelected() {
  if (!active.value || !redisSelected.value) return
  redisValue.value = await RedisGetValue(active.value.id, redisSelected.value)
}

function fmtTTL(ttl: number): string {
  if (ttl === -2) return t('db.ttlGone')
  if (ttl < 0) return t('db.ttlNone')
  if (ttl < 60) return `${ttl}s`
  if (ttl < 3600) return `${Math.floor(ttl / 60)}m`
  return `${Math.floor(ttl / 3600)}h`
}

const typeColors: Record<string, string> = {
  string: '#63e2b7',
  hash: '#4a9ffd',
  list: '#f0a020',
  set: '#722ed1',
  zset: '#d4380d',
  stream: '#888',
}

// ---------- 连接切换时重置 ----------

watch(activeId, () => {
  databases.value = []
  Object.keys(tablesByDb).forEach((k) => delete tablesByDb[k])
  expandedDbs.value = new Set()
  tabs.value = []
  activeTabId.value = ''
  redisKeys.value = []
  redisCursor.value = 0
  redisValue.value = null
  redisSelected.value = ''
  if (active.value) {
    if (isRedis.value) void loadRedisFirstPage()
    else openQuery()
  }
})

void load()
</script>

<template>
  <div class="db-view">
    <!-- 左：连接 + 库/表树 -->
    <div class="sidebar">
      <div class="sidebar-head">
        <span>{{ t('db.connections') }}</span>
        <n-button size="tiny" type="primary" @click="openNew">{{ t('db.newConn') }}</n-button>
      </div>
      <div class="tree-scroll">
        <div v-if="loadError" class="load-error">{{ loadError }}</div>
        <div
          v-for="c in conns"
          :key="c.id"
          class="conn-block"
        >
          <div class="conn-item" :class="{ active: c.id === activeId }" @click="activeId = c.id">
            <span class="twist" @click.stop="c.id === activeId && !isRedis && toggleConn()">
              {{ c.kind === 'redis' ? '●' : databases.length > 0 && c.id === activeId ? '▾' : '▸' }}
            </span>
            <span class="kind" :class="c.kind">{{ c.kind === 'redis' ? 'R' : 'M' }}</span>
            <span class="conn-name" :title="`${c.addr}:${c.port}`">{{ c.name }}</span>
            <span class="conn-ops">
              <span class="op" :title="t('db.test')" @click.stop="testConn(c)">⚡</span>
              <span class="op" :title="t('menu.edit')" @click.stop="openEdit(c)">✎</span>
              <span class="op danger" :title="t('common.delete')" @click.stop="askDelete(c)">×</span>
            </span>
          </div>

          <!-- MySQL：库 → 表 -->
          <template v-if="c.id === activeId && !isRedis">
            <div v-for="db in databases" :key="db" class="db-block">
              <div class="db-item" @click="toggleDb(db)">
                <span class="twist">{{ expandedDbs.has(db) ? '▾' : '▸' }}</span>
                <span class="db-name">{{ db }}</span>
                <span v-if="tablesByDb[db]" class="db-count">{{ tablesByDb[db].length }}</span>
              </div>
              <div v-if="expandedDbs.has(db)" class="table-list">
                <div
                  v-for="tb in tablesByDb[db] ?? []"
                  :key="tb.name"
                  class="table-item"
                  :title="`${tb.name}${tb.comment ? ' — ' + tb.comment : ''}`"
                  @click="openTableStructure(db, tb)"
                  @dblclick="openTableData(db, tb)"
                >
                  <span class="table-icon">{{ tb.isView ? '◇' : '▤' }}</span>
                  <span class="table-name">{{ tb.name }}</span>
                  <span class="table-rows">{{ tb.rows > 0 ? tb.rows : '' }}</span>
                </div>
                <div v-if="(tablesByDb[db] ?? []).length === 0" class="tree-empty">{{ t('db.noTables') }}</div>
              </div>
            </div>
            <div v-if="databases.length === 0 && !treeLoading" class="tree-empty">{{ t('db.expandHint') }}</div>
          </template>

          <!-- Redis：键列表内嵌在侧栏 -->
          <template v-else-if="c.id === activeId && isRedis">
            <div class="redis-filter">
              <n-input
                v-model:value="redisPattern"
                size="tiny"
                :placeholder="t('db.keyPattern')"
                @keyup.enter="loadRedisFirstPage"
              />
              <n-button size="tiny" @click="loadRedisFirstPage">{{ t('db.scan') }}</n-button>
            </div>
            <div class="redis-mode">
              <span class="mode-link" @click="treeMode = !treeMode">
                {{ treeMode ? t('db.flatMode') : t('db.treeMode') }}
              </span>
            </div>
            <template v-if="treeMode">
              <div
                v-for="it in flatTree"
                :key="it.path"
                class="key-item"
                :class="{ active: it.node.fullKey && it.node.fullKey === redisSelected }"
                :style="{ paddingLeft: 5 + it.depth * 12 + 'px' }"
                :title="it.node.fullKey || it.path"
                @click="it.node.isLeaf ? selectKey({ key: it.node.fullKey, type: it.node.type, ttl: it.node.ttl }) : toggleNode(it.node)"
              >
                <span v-if="it.node.children.length" class="twist">
                  {{ collapsedNodes.has(it.path) ? '▸' : '▾' }}
                </span>
                <span v-else class="twist" />
                <span class="key-type" :style="{ color: typeColors[it.node.type] ?? (it.node.isLeaf ? '#888' : '#63e2b7') }">
                  {{ it.node.isLeaf ? it.node.type : '▾' }}
                </span>
                <span class="key-name">{{ it.node.name }}</span>
                <span v-if="it.node.isLeaf" class="key-ttl">{{ fmtTTL(it.node.ttl) }}</span>
              </div>
            </template>
            <template v-else>
              <div
                v-for="k in redisKeys"
                :key="k.key"
                class="key-item"
                :class="{ active: k.key === redisSelected }"
                @click="selectKey(k)"
              >
                <span class="key-type" :style="{ color: typeColors[k.type] ?? '#888' }">{{ k.type }}</span>
                <span class="key-name" :title="k.key">{{ k.key }}</span>
                <span class="key-ttl">{{ fmtTTL(k.ttl) }}</span>
              </div>
            </template>
            <div v-if="redisKeys.length === 0" class="tree-empty">{{ t('db.noKeys') }}</div>
            <n-button
              v-if="hasMoreKeys"
              size="tiny"
              quaternary
              :loading="redisLoading"
              class="more-btn"
              @click="loadRedisMore()"
            >
              {{ t('db.loadMore') }}
            </n-button>
          </template>
        </div>
      </div>
    </div>

    <!-- 右：工作区 -->
    <div class="workspace">
      <div v-if="!active" class="placeholder">{{ t('db.pickConn') }}</div>

      <!-- MySQL 工作台 -->
      <template v-else-if="!isRedis">
        <!-- 标签栏 -->
        <div class="tab-bar">
          <div
            v-for="tab in tabs"
            :key="tab.id"
            class="tab"
            :class="{ active: tab.id === activeTabId }"
            @click="activeTabId = tab.id"
          >
            <span class="tab-icon">{{ tab.kind === 'query' ? '▶' : tab.kind === 'data' ? '▤' : '◇' }}</span>
            <span class="tab-title">{{ tab.title }}</span>
            <span class="tab-close" @click.stop="closeTab(tab.id)">×</span>
          </div>
          <div class="tab-add" :title="t('db.newQueryTab')" @click="openQuery">＋</div>
        </div>

        <div v-if="!activeTab" class="placeholder">{{ t('db.noTab') }}</div>

        <!-- 数据浏览标签 -->
        <template v-else-if="activeTab.kind === 'data'">
          <div class="toolbar">
            <n-input
              v-model:value="activeTab.filter"
              size="small"
              :placeholder="t('db.filterPlaceholder')"
              class="filter-input"
              @keyup.enter="loadTableData(activeTab, true)"
            />
            <n-button size="small" :loading="activeTab.loading" @click="loadTableData(activeTab, true)">
              {{ t('db.refresh') }}
            </n-button>
            <n-button size="small" @click="gotoStructure(activeTab)">{{ t('db.structureShort') }}</n-button>
            <span class="spacer" />
            <n-button size="small" :disabled="activeTab.page === 0" @click="prevPage(activeTab)">‹</n-button>
            <span class="page-num">{{ t('db.pageN', { n: activeTab.page + 1 }) }}</span>
            <n-button size="small" :disabled="!hasNextPage(activeTab)" @click="nextPage(activeTab)">›</n-button>
            <n-select
              v-model:value="activeTab.pageSize"
              size="small"
              :options="[100, 500, 1000, 5000].map((n) => ({ label: String(n), value: n }))"
              style="width: 90px"
              @update:value="loadTableData(activeTab, true)"
            />
            <n-button size="small" :disabled="!(activeTab.result?.columns?.length)" @click="exportCSV(activeTab)">
              CSV
            </n-button>
          </div>

          <div class="result">
            <div v-if="activeTab.error" class="query-error">{{ activeTab.error }}</div>
            <template v-else-if="activeTab.result && (activeTab.result.columns?.length ?? 0) > 0">
              <div class="result-meta">
                {{ activeTab.result.rows?.length ?? 0 }} {{ t('db.rows') }} · {{ activeTab.result.durationMs }}ms
                <span v-if="activeTab.result.truncated" class="warn">· {{ t('db.truncated') }}</span>
                <span class="spacer" />
                <span class="hint">{{ t('db.clickSort') }}</span>
              </div>
              <div class="grid-wrap">
                <table class="grid">
                  <thead>
                    <tr>
                      <th class="rownum">#</th>
                      <th
                        v-for="c in activeTab.result.columns"
                        :key="c"
                        class="sortable"
                        :title="t('db.sortServerSide')"
                        @click="sortTableByColumn(activeTab, c)"
                      >
                        {{ c }}
                        <span v-if="activeTab.sortCol === c" class="sort">{{ activeTab.sortDir === 1 ? '▲' : '▼' }}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(row, ri) in sortedRows(activeTab)" :key="ri">
                      <td class="rownum">{{ ri + 1 + activeTab.page * activeTab.pageSize }}</td>
                      <td
                        v-for="(cell, ci) in row"
                        :key="ci"
                        :class="{
                          null: cell === null,
                          selected: activeTab.selectedRow === ri && activeTab.selectedCol === ci,
                        }"
                        @click="copyCell(activeTab, ri, ci)"
                      >
                        {{ cell === null ? 'NULL' : cell }}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
            <div v-else class="result-empty">{{ activeTab.loading ? t('db.loadingData') : t('db.noData') }}</div>
          </div>
        </template>

        <!-- 查询标签 -->
        <template v-else-if="activeTab.kind === 'query'">
          <div class="toolbar">
            <span class="ctx">{{ t('db.queryTitle') }}</span>
            <span class="spacer" />
            <n-button size="small" type="primary" :loading="activeTab.loading" @click="runQueryTab(activeTab)">
              {{ t('db.run') }}
            </n-button>
            <n-button size="small" :disabled="!(activeTab.result?.columns?.length)" @click="exportCSV(activeTab)">
              CSV
            </n-button>
          </div>

          <textarea
            v-model="activeTab.sql"
            class="sql-editor"
            spellcheck="false"
            :placeholder="t('db.sqlPlaceholder')"
            @keydown.ctrl.enter.prevent="runQueryTab(activeTab)"
            @keydown.meta.enter.prevent="runQueryTab(activeTab)"
          />

          <div class="result">
            <div v-if="activeTab.error" class="query-error">{{ activeTab.error }}</div>
            <div v-else-if="!activeTab.result" class="result-empty">{{ t('db.noResult') }}</div>
            <template v-else-if="(activeTab.result.columns?.length ?? 0) > 0">
              <div class="result-meta">
                {{ activeTab.result.rows?.length ?? 0 }} {{ t('db.rows') }} · {{ activeTab.result.durationMs }}ms
                <span v-if="activeTab.result.truncated" class="warn">· {{ t('db.truncated') }}</span>
                <span class="spacer" />
                <span class="hint">{{ t('db.clickSort') }}</span>
              </div>
              <div class="grid-wrap">
                <table class="grid">
                  <thead>
                    <tr>
                      <th class="rownum">#</th>
                      <th v-for="(c, ci) in activeTab.result.columns" :key="c" class="sortable" @click="toggleGridSort(activeTab, ci)">
                        {{ c }}
                        <span v-if="activeTab.gridSortCol === ci" class="sort">{{ activeTab.gridSortDir === 1 ? '▲' : '▼' }}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(row, ri) in sortedRows(activeTab)" :key="ri">
                      <td class="rownum">{{ ri + 1 }}</td>
                      <td
                        v-for="(cell, ci) in row"
                        :key="ci"
                        :class="{
                          null: cell === null,
                          selected: activeTab.selectedRow === ri && activeTab.selectedCol === ci,
                        }"
                        @click="copyCell(activeTab, ri, ci)"
                      >
                        {{ cell === null ? 'NULL' : cell }}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
            <div v-else class="result-meta">
              {{ t('db.affected', { n: activeTab.result.affected }) }} · {{ activeTab.result.durationMs }}ms
            </div>
          </div>

          <div v-if="history.length" class="history">
            <div class="history-head">{{ t('db.history') }}</div>
            <div class="history-list">
              <div
                v-for="(h, i) in history.slice(0, 6)"
                :key="i"
                class="history-item"
                :title="h"
                @click="activeTab.sql = h"
              >
                {{ h }}
              </div>
            </div>
          </div>
        </template>

        <!-- 结构标签 -->
        <template v-else>
          <div class="toolbar">
            <span class="ctx">{{ activeTab.database }} · {{ activeTab.table }}</span>
            <span class="spacer" />
            <n-button size="small" @click="loadDDL(activeTab)">{{ t('db.showDDL') }}</n-button>
            <n-button size="small" :disabled="!activeTab.showDDL" @click="activeTab.showDDL = false">
              {{ t('db.colsView') }}
            </n-button>
          </div>
          <div class="result">
            <div v-if="activeTab.error" class="query-error">{{ activeTab.error }}</div>
            <pre v-else-if="activeTab.showDDL" class="ddl">{{ activeTab.ddl }}</pre>
            <div v-else class="grid-wrap">
              <table class="grid">
                <thead>
                  <tr>
                    <th>{{ t('db.colField') }}</th>
                    <th>{{ t('db.colType') }}</th>
                    <th>{{ t('db.colNull') }}</th>
                    <th>{{ t('db.colKey') }}</th>
                    <th>{{ t('db.colDefault') }}</th>
                    <th>{{ t('db.colExtra') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="col in activeTab.columns" :key="col.field">
                    <td class="mono">{{ col.field }}</td>
                    <td class="mono">{{ col.type }}</td>
                    <td>{{ col.null }}</td>
                    <td>{{ col.key }}</td>
                    <td class="mono">{{ col.default ?? 'NULL' }}</td>
                    <td>{{ col.extra }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </template>
      </template>

      <!-- Redis 值查看器 -->
      <template v-else>
        <div class="value">
          <div v-if="!redisValue" class="result-empty">{{ t('db.pickKey') }}</div>
          <template v-else>
            <div class="value-head">
              <span class="value-type" :style="{ color: typeColors[redisValue.type] ?? '#888' }">
                {{ redisValue.type }}
              </span>
              <span class="value-key">{{ redisValue.key }}</span>
              <span class="spacer" />
              <span class="ttl-label">{{ t('db.ttl') }}</span>
              <n-input-number v-model:value="ttlInput" size="tiny" :min="0" style="width: 100px" />
              <n-button size="tiny" @click="saveTTL">{{ t('common.save') }}</n-button>
              <n-button size="tiny" type="error" quaternary @click="deleteKey">
                {{ t('common.delete') }}
              </n-button>
            </div>
            <div v-if="redisValue.truncated" class="warn-line">
              {{ t('db.truncatedValue', { n: redisValue.total }) }}
            </div>

            <template v-if="redisValue.type === 'string'">
              <div v-if="!editingValue" class="str-value">
                <pre>{{ redisValue.str }}</pre>
                <n-button size="tiny" class="edit-btn" @click="editingValue = true">
                  {{ t('menu.edit') }}
                </n-button>
              </div>
              <div v-else class="str-edit">
                <textarea v-model="editValue" class="str-editor" spellcheck="false" />
                <div class="str-actions">
                  <n-button size="tiny" @click="editingValue = false">{{ t('common.cancel') }}</n-button>
                  <n-button size="tiny" type="primary" @click="saveStringValue">{{ t('common.save') }}</n-button>
                </div>
              </div>
            </template>

            <div v-else-if="redisValue.type === 'hash'" class="kv-table">
              <div v-for="(v, k) in redisValue.hash" :key="k" class="kv-row">
                <span class="kv-key">{{ k }}</span>
                <span class="kv-val">{{ v }}</span>
              </div>
            </div>

            <div v-else-if="redisValue.type === 'list' || redisValue.type === 'set'" class="kv-table">
              <div v-for="(v, i) in (redisValue.list ?? redisValue.set ?? [])" :key="i" class="kv-row">
                <span class="kv-idx">{{ i }}</span>
                <span class="kv-val">{{ v }}</span>
              </div>
            </div>

            <div v-else-if="redisValue.type === 'zset'" class="kv-table">
              <div v-for="(m, i) in redisValue.zset" :key="i" class="kv-row">
                <span class="kv-idx">{{ m.score }}</span>
                <span class="kv-val">{{ m.member }}</span>
              </div>
            </div>

            <div v-else class="result-empty">{{ t('db.unsupportedType', { type: redisValue.type }) }}</div>
          </template>
        </div>
      </template>
    </div>

    <DBConnModal v-model:show="modal" :conn="editing" @saved="onSaved" />
  </div>
</template>

<style scoped>
.db-view {
  display: flex;
  height: 100%;
  gap: 8px;
}
/* ---------- 左侧栏 ---------- */
.sidebar {
  width: 250px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-right: 1px solid rgba(255, 255, 255, 0.08);
  padding-right: 8px;
}
.sidebar-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  opacity: 0.75;
}
.tree-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.conn-block {
  margin-bottom: 2px;
}
.conn-item,
.db-item,
.table-item,
.key-item {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 5px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;
}
.conn-item:hover,
.db-item:hover,
.table-item:hover,
.key-item:hover {
  background: rgba(255, 255, 255, 0.06);
}
.conn-item.active,
.table-item.active,
.key-item.active {
  background: rgba(99, 226, 183, 0.12);
}
.twist {
  width: 10px;
  flex-shrink: 0;
  font-size: 9px;
  opacity: 0.6;
}
.kind {
  width: 15px;
  height: 15px;
  border-radius: 3px;
  font-size: 9px;
  line-height: 15px;
  text-align: center;
  background: rgba(74, 159, 253, 0.2);
  color: #4a9ffd;
  flex-shrink: 0;
}
.kind.redis {
  background: rgba(212, 56, 13, 0.2);
  color: #ff7a45;
}
.conn-name,
.db-name,
.table-name,
.key-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}
.conn-ops {
  display: none;
  gap: 3px;
}
.conn-item:hover .conn-ops {
  display: inline-flex;
}
.op {
  opacity: 0.6;
  cursor: pointer;
}
.op:hover {
  opacity: 1;
}
.op.danger {
  color: #e88080;
}
.db-block {
  padding-left: 12px;
}
.db-item {
  color: rgba(255, 255, 255, 0.85);
}
.db-count {
  font-size: 10px;
  opacity: 0.4;
}
.table-list {
  padding-left: 16px;
}
.table-icon {
  font-size: 9px;
  opacity: 0.5;
  width: 10px;
}
.table-rows {
  font-size: 10px;
  opacity: 0.35;
  font-variant-numeric: tabular-nums;
}
.tree-empty {
  font-size: 11px;
  opacity: 0.4;
  padding: 4px 8px;
}
.redis-filter {
  display: flex;
  gap: 4px;
  padding: 4px 0 4px 12px;
}
.more-btn {
  width: 100%;
}
.redis-mode {
  padding: 2px 6px 2px 12px;
}
.mode-link {
  font-size: 10px;
  color: #63e2b7;
  cursor: pointer;
  opacity: 0.8;
}
.mode-link:hover {
  opacity: 1;
}
.key-item .twist {
  width: 10px;
  flex-shrink: 0;
  font-size: 9px;
  opacity: 0.6;
}
.key-type {
  font-size: 9px;
  width: 32px;
  flex-shrink: 0;
}
.key-ttl {
  font-size: 10px;
  opacity: 0.45;
}
.load-error {
  margin: 8px 0;
  padding: 8px 10px;
  border-radius: 6px;
  background: rgba(232, 128, 128, 0.12);
  color: #e88080;
  font-size: 11px;
  white-space: pre-wrap;
}

/* ---------- 右侧工作区 ---------- */
.workspace {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.placeholder {
  margin: auto;
  opacity: 0.4;
  font-size: 13px;
}
.tab-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  padding-bottom: 4px;
  overflow-x: auto;
  flex-shrink: 0;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-radius: 4px 4px 0 0;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
  opacity: 0.7;
  max-width: 200px;
}
.tab:hover {
  background: rgba(255, 255, 255, 0.06);
  opacity: 1;
}
.tab.active {
  background: rgba(99, 226, 183, 0.12);
  opacity: 1;
  color: #63e2b7;
}
.tab-icon {
  font-size: 9px;
  opacity: 0.7;
}
.tab-title {
  overflow: hidden;
  text-overflow: ellipsis;
}
.tab-close {
  opacity: 0.4;
  font-size: 13px;
  line-height: 1;
}
.tab-close:hover {
  opacity: 1;
  color: #e88080;
}
.tab-add {
  padding: 0 8px;
  font-size: 14px;
  opacity: 0.6;
  cursor: pointer;
}
.tab-add:hover {
  opacity: 1;
  color: #63e2b7;
}
.filter-input {
  flex: 1;
  min-width: 160px;
}
.page-num {
  font-size: 12px;
  opacity: 0.7;
  min-width: 52px;
  text-align: center;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.ctx {
  font-size: 12px;
  opacity: 0.75;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 50%;
}
.spacer {
  flex: 1;
}
.sql-editor {
  width: 100%;
  height: 130px;
  resize: vertical;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  color: #d4d4d4;
  font-family: Consolas, 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
  padding: 8px 10px;
  outline: none;
}
.sql-editor:focus {
  border-color: rgba(99, 226, 183, 0.4);
}
.result {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  overflow: hidden;
}
.query-error {
  margin: 12px;
  padding: 10px 12px;
  border-radius: 6px;
  background: rgba(232, 128, 128, 0.12);
  color: #e88080;
  font-size: 12px;
  white-space: pre-wrap;
}
.result-empty {
  margin: auto;
  opacity: 0.4;
  font-size: 12px;
}
.result-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  font-size: 11px;
  opacity: 0.65;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  flex-shrink: 0;
}
.warn {
  color: #f0a020;
}
.hint {
  font-size: 10px;
  opacity: 0.5;
}
.link {
  color: #63e2b7;
  cursor: pointer;
}
.grid-wrap {
  flex: 1;
  overflow: auto;
}
.grid {
  border-collapse: collapse;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.grid th,
.grid td {
  padding: 3px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  text-align: left;
  white-space: nowrap;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.grid th {
  position: sticky;
  top: 0;
  background: #202028;
  color: rgba(255, 255, 255, 0.7);
  font-weight: 500;
  z-index: 1;
}
.grid th.sortable {
  cursor: pointer;
  user-select: none;
}
.grid th.sortable:hover {
  color: #fff;
}
.sort {
  font-size: 8px;
  color: #63e2b7;
}
.grid tbody tr:hover {
  background: rgba(255, 255, 255, 0.04);
}
.grid td {
  cursor: pointer;
}
.grid td.selected {
  background: rgba(99, 226, 183, 0.18);
}
.rownum {
  color: rgba(255, 255, 255, 0.3);
  text-align: right;
  width: 40px;
}
.grid td.null {
  color: rgba(255, 255, 255, 0.3);
  font-style: italic;
}
.mono {
  font-family: Consolas, monospace;
  font-size: 11px;
}
.ddl {
  flex: 1;
  overflow: auto;
  margin: 0;
  padding: 10px;
  font-family: Consolas, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  color: #d4d4d4;
}
.history {
  flex-shrink: 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding-top: 4px;
}
.history-head {
  font-size: 10px;
  opacity: 0.5;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 2px;
}
.history-list {
  display: flex;
  flex-direction: column;
  max-height: 110px;
  overflow: auto;
}
.history-item {
  font-size: 11px;
  font-family: Consolas, monospace;
  opacity: 0.6;
  padding: 1px 4px;
  border-radius: 3px;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.history-item:hover {
  background: rgba(255, 255, 255, 0.06);
  opacity: 1;
}

/* ---------- Redis 值 ---------- */
.value {
  flex: 1;
  min-height: 0;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  padding: 8px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.value-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.value-type {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.08);
}
.value-key {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 320px;
}
.ttl-label {
  font-size: 11px;
  opacity: 0.6;
}
.warn-line {
  font-size: 11px;
  color: #f0a020;
}
.str-value {
  position: relative;
  flex: 1;
}
.str-value pre {
  margin: 0;
  padding: 8px;
  background: rgba(255, 255, 255, 0.03);
  border-radius: 4px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.edit-btn {
  position: absolute;
  top: 6px;
  right: 6px;
}
.str-editor {
  width: 100%;
  height: 220px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  color: #d4d4d4;
  font-family: Consolas, monospace;
  font-size: 12px;
  padding: 8px;
  outline: none;
  resize: vertical;
}
.str-actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 6px;
}
.kv-table {
  display: flex;
  flex-direction: column;
}
.kv-row {
  display: flex;
  gap: 8px;
  padding: 2px 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  font-size: 12px;
}
.kv-key,
.kv-idx {
  width: 140px;
  flex-shrink: 0;
  color: rgba(255, 255, 255, 0.6);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.kv-val {
  flex: 1;
  word-break: break-all;
}
</style>
