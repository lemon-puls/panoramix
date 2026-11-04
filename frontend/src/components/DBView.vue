<script setup lang="ts">
import { computed, h, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NInput, NInputNumber } from 'naive-ui'
import {
  DBDescribeTable,
  DBListDatabases,
  DBListForeignKeys,
  DBListIndexes,
  DBListTables,
  DBQuery,
  DBShowCreateTable,
  DBTableMeta,
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
import ColumnModal from './ColumnModal.vue'
import IndexModal from './IndexModal.vue'

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
// 单击表：创建「表」与「数据」两个固定标签，激活「表」，数据同步加载
async function openTableStructure(db: string, table: models.db.TableInfo) {
  const structTab = ensureStructureTab(db, table.name)
  const dataTab = ensureDataTab(db, table.name)
  activeTabId.value = structTab.id
  await Promise.all([
    loadTableStructure(structTab),
    dataTab.result && dataTab.table === table.name
      ? Promise.resolve()
      : loadTableData(dataTab, true),
  ])
}

function ensureStructureTab(db: string, tableName: string): WorkTab {
  const id = activeId.value
  let tab = tabs.value.find((x) => x.kind === 'structure' && x.connId === id)
  if (tab) {
    tab.database = db
    tab.table = tableName
    tab.title = t('db.tabTable')
    tab.showDDL = false
    tab.ddl = ''
    tab.error = ''
    tab.structView = 'basic'
    return tab
  }
  tab = newTab('structure', {
    title: t('db.tabTable'),
    database: db,
    table: tableName,
    pinned: true,
  })
  tabs.value.push(tab)
  sortTabs()
  return tab
}

// 加载表结构（列/基本信息/索引/外键）
async function loadTableStructure(tab: WorkTab) {
  tab.loading = true
  tab.error = ''
  try {
    const [cols, meta, idx, fks] = await Promise.all([
      DBDescribeTable(tab.connId, tab.database, tab.table),
      DBTableMeta(tab.connId, tab.database, tab.table).catch(() => null),
      DBListIndexes(tab.connId, tab.database, tab.table).catch(() => []),
      DBListForeignKeys(tab.connId, tab.database, tab.table).catch(() => []),
    ])
    tab.columns = cols ?? []
    tab.meta = meta
    tab.indexes = idx ?? []
    tab.foreignKeys = fks ?? []
  } catch (e) {
    tab.error = String(e)
  } finally {
    tab.loading = false
  }
}

// 「数据」标签：全局唯一且固定，跟随当前表
function ensureDataTab(db: string, tableName: string): WorkTab {
  const id = activeId.value
  let tab = tabs.value.find((x) => x.kind === 'data' && x.connId === id)
  if (tab) {
    const changed = tab.database !== db || tab.table !== tableName
    tab.database = db
    tab.table = tableName
    tab.title = t('db.tabData')
    tab.error = ''
    if (changed) {
      // 切换了表：清空旧结果，等待重新加载
      tab.filter = ''
      tab.sortCol = ''
      tab.page = 0
      tab.result = null
    }
    return tab
  }
  tab = newTab('data', {
    title: t('db.tabData'),
    database: db,
    table: tableName,
    pinned: true,
  })
  tabs.value.push(tab)
  sortTabs()
  return tab
}

// 双击表 → 激活固定的「数据」标签并加载
async function openTableData(db: string, table: models.db.TableInfo) {
  ensureStructureTab(db, table.name)
  const tab = ensureDataTab(db, table.name)
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
  meta: models.db.TableMeta | null
  indexes: models.db.IndexInfo[]
  foreignKeys: models.db.ForeignKeyInfo[]
  structView: 'basic' | 'indexes' | 'fks' | 'ddl'
  ddl: string
  showDDL: boolean
  pinned: boolean
  // 数据编辑
  pkColumns: string[]
  pending: { row: number; col: number; value: string }[]
  editing: { row: number; col: number } | null
  gridSortCol: number
  gridSortDir: 1 | -1
  selectedRow: number
  selectedCol: number
}

let tabSeq = 0
const tabs = ref<WorkTab[]>([])
const activeTabId = ref('')
const activeTab = computed(() => tabs.value.find((x) => x.id === activeTabId.value) ?? null)

// 必须返回 reactive 对象：ref 数组中的元素若被外部原始引用修改，
// 不会触发 Vue 更新（loading 永远停在 true 的根因）
function newTab(kind: TabKind, init: Partial<WorkTab>): WorkTab {
  return reactive({
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
    meta: null,
    indexes: [],
    foreignKeys: [],
    structView: 'basic',
    ddl: '',
    showDDL: false,
    pinned: false,
    pkColumns: [],
    pending: [],
    editing: null,
    gridSortCol: -1,
    gridSortDir: 1,
    selectedRow: -1,
    selectedCol: -1,
    ...init,
  })
}

function openQuery() {
  const n = tabs.value.filter((x) => x.kind === 'query').length + 1
  const tab = newTab('query', { title: t('db.queryN', { n }) })
  tabs.value.push(tab)
  sortTabs()
  activeTabId.value = tab.id
}

const canCloseTab = (tab: WorkTab): boolean => {
  if (tab.pinned) return false
  // 查询标签至少保留一个
  if (tab.kind === 'query') {
    return tabs.value.filter((x) => x.kind === 'query').length > 1
  }
  return true
}

// 固定顺序：表 → 数据 → 查询（同组内保持创建顺序）
function sortTabs() {
  const order: Record<TabKind, number> = { structure: 0, data: 1, query: 2 }
  tabs.value.sort((a, b) => order[a.kind] - order[b.kind])
}

function closeTab(id: string) {
  const idx = tabs.value.findIndex((x) => x.id === id)
  if (idx === -1) return
  const tab = tabs.value[idx]
  if (!canCloseTab(tab)) return
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
  tab.pending = []
  tab.editing = null
  try {
    const [res, cols] = await Promise.all([
      DBQuery(tab.connId, buildTableSQL(tab), tab.pageSize),
      tab.pkColumns.length === 0
        ? DBDescribeTable(tab.connId, tab.database, tab.table).catch(() => [] as models.db.ColumnInfo[])
        : Promise.resolve([] as models.db.ColumnInfo[]),
    ])
    tab.result = res
    if (cols.length > 0) {
      tab.pkColumns = cols.filter((c) => c.key === 'PRI').map((c) => c.field)
    }
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

// ---------- 数据单元格编辑 ----------

// 编辑态绑定的输入值
const cellInput = ref('')

function startEdit(tab: WorkTab, row: number, col: number) {
  if (!tab.pkColumns.length) {
    message.warning(t('db.noPrimaryKey'))
    return
  }
  const idx = tab.pending.findIndex((p) => p.row === row && p.col === col)
  const current = idx >= 0 ? tab.pending[idx].value : (tab.result?.rows?.[row]?.[col] ?? '')
  cellInput.value = current
  tab.editing = { row, col }
}

function commitEdit(tab: WorkTab) {
  if (!tab.editing) return
  const { row, col } = tab.editing
  const original = tab.result?.rows?.[row]?.[col] ?? null
  const value = cellInput.value
  tab.editing = null
  // 与原始值相同则移除待提交项
  const unchanged =
    (original === null && value === 'NULL') || (original !== null && original === value)
  const idx = tab.pending.findIndex((p) => p.row === row && p.col === col)
  if (unchanged) {
    if (idx >= 0) tab.pending.splice(idx, 1)
    return
  }
  if (idx >= 0) tab.pending[idx].value = value
  else tab.pending.push({ row, col, value })
}

function cancelEdit(tab: WorkTab) {
  tab.editing = null
}

function discardEdits(tab: WorkTab) {
  tab.pending = []
  tab.editing = null
}

function pendingValue(tab: WorkTab, row: number, col: number): string | null {
  const p = tab.pending.find((x) => x.row === row && x.col === col)
  return p ? p.value : null
}

// 生成 UPDATE 并执行（按主键定位行；NULL 用字面量 NULL）
function sqlLiteral(v: string): string {
  return v === 'NULL' ? 'NULL' : `'${v.replace(/'/g, "''")}'`
}

async function applyEdits(tab: WorkTab) {
  if (!tab.result || tab.pending.length === 0) return
  const columns = tab.result.columns
  // 使用与界面一致的视图（排序后），确保行索引对应
  const rows = sortedRows(tab)
  const idxOf = (name: string) => columns.findIndex((c) => c === name)
  const pkIdx = tab.pkColumns.map(idxOf)
  if (pkIdx.some((i) => i < 0)) {
    message.error(t('db.noPrimaryKey'))
    return
  }

  // 按行分组生成 UPDATE
  const byRow = new Map<number, { row: number; col: number; value: string }[]>()
  for (const p of tab.pending) {
    const list = byRow.get(p.row) ?? []
    list.push(p)
    byRow.set(p.row, list)
  }
  const table = `${quoteIdent(tab.database)}.${quoteIdent(tab.table)}`
  const statements: string[] = []
  for (const [rowIdx, edits] of byRow) {
    const rowData = rows[rowIdx]
    if (!rowData) continue
    const setClause = edits
      .map((e) => `${quoteIdent(columns[e.col])} = ${sqlLiteral(e.value)}`)
      .join(', ')
    const whereClause = tab.pkColumns
      .map((name) => {
        const v = rowData[idxOf(name)]
        return `${quoteIdent(name)} = ${v === null ? 'NULL' : `'${v.replace(/'/g, "''")}'`}`
      })
      .join(' AND ')
    statements.push(`UPDATE ${table} SET ${setClause} WHERE ${whereClause} LIMIT 1;`)
  }

  tab.loading = true
  try {
    for (const sql of statements) {
      await DBQuery(tab.connId, sql, 1)
    }
    message.success(t('db.editsApplied', { n: statements.length }))
    await loadTableData(tab)
  } catch (e) {
    message.error(String(e))
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

// ---------- 表结构编辑（生成 ALTER 并预览确认） ----------

const colModal = reactive({
  show: false,
  mode: 'add' as 'add' | 'edit',
  column: null as models.db.ColumnInfo | null,
})
const idxModal = reactive({ show: false, kind: 'index' as 'index' | 'foreignKey' })

function quoteIdent(n: string): string {
  return '`' + n.replace(/`/g, '``') + '`'
}

function alterPrefix(tab: WorkTab): string {
  return `ALTER TABLE ${quoteIdent(tab.database)}.${quoteIdent(tab.table)}`
}

function buildColumnSQL(
  tab: WorkTab,
  mode: 'add' | 'edit',
  p: { name: string; type: string; nullable: boolean; defaultValue: string; comment: string; autoIncrement: boolean },
  original: models.db.ColumnInfo | null,
): string {
  const parts = [`${quoteIdent(p.name)} ${p.type}`, p.nullable ? 'NULL' : 'NOT NULL']
  const dv = p.defaultValue
  if (dv !== '') {
    const isExpr =
      /^(CURRENT_TIMESTAMP(\(\))?|NULL|NOW\(\))$/i.test(dv) || /^-?\d+(\.\d+)?$/.test(dv)
    parts.push(`DEFAULT ${isExpr ? dv : `'${dv.replace(/'/g, "''")}'`}`)
  }
  if (p.autoIncrement) parts.push('AUTO_INCREMENT')
  if (p.comment) parts.push(`COMMENT '${p.comment.replace(/'/g, "''")}'`)
  const def = parts.join(' ')
  if (mode === 'add') return `${alterPrefix(tab)} ADD COLUMN ${def};`
  if (original && original.field !== p.name) {
    return `${alterPrefix(tab)} CHANGE COLUMN ${quoteIdent(original.field)} ${def};`
  }
  return `${alterPrefix(tab)} MODIFY COLUMN ${def};`
}

// 执行 DDL：先预览确认，再执行并刷新结构
function runDDL(tab: WorkTab, sql: string) {
  dialog.warning({
    title: t('db.confirmDDL'),
    content: () => h('pre', { class: 'ddl-preview' }, sql),
    positiveText: t('db.executeDDL'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      tab.loading = true
      try {
        await DBQuery(tab.connId, sql, 1)
        message.success(t('db.ddlDone'))
        await loadTableStructure(tab)
        // 数据标签与表列表也可能受影响，刷新表列表计数
        if (active.value) {
          const list = await DBListTables(active.value.id, tab.database).catch(() => null)
          if (list) tablesByDb[tab.database] = list
        }
      } catch (e) {
        message.error(String(e))
      } finally {
        tab.loading = false
      }
    },
  })
}

function submitColumn(payload: { name: string; type: string; nullable: boolean; defaultValue: string; comment: string; autoIncrement: boolean }) {
  const tab = activeTab.value
  if (!tab) return
  const sql = buildColumnSQL(tab, colModal.mode, payload, colModal.column)
  runDDL(tab, sql)
}

function openAddColumn() {
  colModal.mode = 'add'
  colModal.column = null
  colModal.show = true
}

function openEditColumn(col: models.db.ColumnInfo) {
  colModal.mode = 'edit'
  colModal.column = col
  colModal.show = true
}

function openAddIndex(kind: 'index' | 'foreignKey') {
  idxModal.kind = kind
  idxModal.show = true
}

// 当前表的同库表列表（外键引用候选）
const currentDbTables = computed(() => {
  const db = activeTab.value?.database ?? ''
  return (tablesByDb[db] ?? []).map((x) => x.name)
})

function dropColumn(col: models.db.ColumnInfo) {
  const tab = activeTab.value
  if (!tab) return
  runDDL(tab, `${alterPrefix(tab)} DROP COLUMN ${quoteIdent(col.field)};`)
}

function submitIndex(payload: { name: string; columns: string[]; unique?: boolean; refTable?: string; refColumns?: string; onUpdate?: string; onDelete?: string }) {
  const tab = activeTab.value
  if (!tab) return
  const cols = payload.columns.map(quoteIdent).join(', ')
  let sql: string
  if (idxModal.kind === 'index') {
    const kind = payload.unique ? 'UNIQUE INDEX' : 'INDEX'
    sql = `${alterPrefix(tab)} ADD ${kind} ${quoteIdent(payload.name)} (${cols});`
  } else {
    const refCols = (payload.refColumns ?? '')
      .split(',')
      .map((x) => quoteIdent(x.trim()))
      .join(', ')
    sql = `${alterPrefix(tab)} ADD CONSTRAINT ${quoteIdent(payload.name)} FOREIGN KEY (${cols}) REFERENCES ${quoteIdent(payload.refTable ?? '')} (${refCols}) ON UPDATE ${payload.onUpdate} ON DELETE ${payload.onDelete};`
  }
  runDDL(tab, sql)
}

function dropIndex(idx: models.db.IndexInfo) {
  const tab = activeTab.value
  if (!tab) return
  runDDL(tab, `${alterPrefix(tab)} DROP INDEX ${quoteIdent(idx.name)};`)
}

function dropForeignKey(fk: models.db.ForeignKeyInfo) {
  const tab = activeTab.value
  if (!tab) return
  runDDL(tab, `${alterPrefix(tab)} DROP FOREIGN KEY ${quoteIdent(fk.name)};`)
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

function formatBytes(n: number): string {
  if (!n) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`
}

function fmtTTL(ttl: number): string {
  if (ttl === -2) return t('db.ttlGone')
  if (ttl < 0) return t('db.ttlNone')
  if (ttl < 60) return `${ttl}s`
  if (ttl < 3600) return `${Math.floor(ttl / 60)}m`
  return `${Math.floor(ttl / 3600)}h`
}

const typeColors: Record<string, string> = {
  string: 'var(--accent)',
  hash: '#4a9ffd',
  list: 'var(--warning)',
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
                <span class="key-type" :style="{ color: typeColors[it.node.type] ?? (it.node.isLeaf ? '#888' : 'var(--accent)') }">
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
            <span v-if="canCloseTab(tab)" class="tab-close" @click.stop="closeTab(tab.id)">×</span>
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
            <span class="edit-actions" :class="{ active: activeTab.pending.length > 0 }">
              <button
                class="edit-btn apply"
                :disabled="activeTab.pending.length === 0"
                :title="t('db.applyEdits', { n: activeTab.pending.length })"
                @click="applyEdits(activeTab)"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </button>
              <button
                class="edit-btn discard"
                :disabled="activeTab.pending.length === 0"
                :title="t('db.discardEdits')"
                @click="discardEdits(activeTab)"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
              <span v-if="activeTab.pending.length" class="pending-count">{{ activeTab.pending.length }}</span>
            </span>
          </div>

          <div class="result">
            <div v-if="activeTab.error" class="query-error">{{ activeTab.error }}</div>
            <template v-else-if="activeTab.result && (activeTab.result.columns?.length ?? 0) > 0">
              <div class="result-meta">
                {{ activeTab.result.rows?.length ?? 0 }} {{ t('db.rows') }} · {{ activeTab.result.durationMs }}ms
                <span v-if="activeTab.result.truncated" class="warn">· {{ t('db.truncated') }}</span>
                <span class="spacer" />
                <span class="hint">{{ t('db.clickSort') }} · {{ t('db.clickEdit') }}</span>
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
                          edited: pendingValue(activeTab, ri, ci) !== null,
                          selected: activeTab.selectedRow === ri && activeTab.selectedCol === ci,
                        }"
                        @click="activeTab.editing?.row === ri && activeTab.editing?.col === ci ? undefined : startEdit(activeTab, ri, ci)"
                      >
                        <input
                          v-if="activeTab.editing?.row === ri && activeTab.editing?.col === ci"
                          v-model="cellInput"
                          class="cell-input"
                          autofocus
                          @keyup.enter="commitEdit(activeTab)"
                          @keyup.esc="cancelEdit(activeTab)"
                          @blur="commitEdit(activeTab)"
                          @click.stop
                        />
                        <template v-else>
                          {{ pendingValue(activeTab, ri, ci) ?? (cell === null ? 'NULL' : cell) }}
                        </template>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
            <div v-else class="result-empty">
              <div class="empty-glyph">{{ activeTab.loading ? '◌' : '▤' }}</div>
              <div>{{ activeTab.loading ? t('db.loadingData') : t('db.noData') }}</div>
            </div>
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
            <div v-else-if="!activeTab.result" class="result-empty">
              <div class="empty-glyph">⌘</div>
              <div>{{ t('db.noResult') }}</div>
              <div class="empty-hint">{{ t('db.runHint') }}</div>
            </div>
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

        <!-- 结构标签：基本 / 索引 / 外键 / DDL -->
        <template v-else>
          <div class="toolbar struct-toolbar">
            <span class="ctx">{{ activeTab.database }}.{{ activeTab.table }}</span>
            <div class="sub-tabs">
              <button
                class="sub-tab"
                :class="{ active: activeTab.structView === 'basic' }"
                @click="activeTab.structView = 'basic'"
              >
                {{ t('db.tabBasic') }}
              </button>
              <button
                class="sub-tab"
                :class="{ active: activeTab.structView === 'indexes' }"
                @click="activeTab.structView = 'indexes'"
              >
                {{ t('db.tabIndexes') }} ({{ activeTab.indexes.length }})
              </button>
              <button
                class="sub-tab"
                :class="{ active: activeTab.structView === 'fks' }"
                @click="activeTab.structView = 'fks'"
              >
                {{ t('db.tabForeignKeys') }} ({{ activeTab.foreignKeys.length }})
              </button>
              <button
                class="sub-tab"
                :class="{ active: activeTab.structView === 'ddl' }"
                @click="loadDDL(activeTab); activeTab.structView = 'ddl'"
              >
                DDL
              </button>
            </div>
            <span class="spacer" />
            <n-button size="small" :loading="activeTab.loading" @click="loadTableStructure(activeTab)">
              {{ t('db.refresh') }}
            </n-button>
          </div>

          <div class="result">
            <div v-if="activeTab.error" class="query-error">{{ activeTab.error }}</div>

            <!-- 基本 -->
            <div v-else-if="activeTab.structView === 'basic'" class="struct-body">
              <div class="meta-grid">
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaName') }}</span>
                  <span class="meta-v mono">{{ activeTab.table }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaEngine') }}</span>
                  <span class="meta-v">{{ activeTab.meta?.engine || '—' }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaCollation') }}</span>
                  <span class="meta-v">{{ activeTab.meta?.collation || '—' }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaRows') }}</span>
                  <span class="meta-v">{{ activeTab.meta?.rows ?? '—' }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaSize') }}</span>
                  <span class="meta-v">{{ formatBytes((activeTab.meta?.dataLength ?? 0) + (activeTab.meta?.indexLength ?? 0)) }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaAutoInc') }}</span>
                  <span class="meta-v">{{ activeTab.meta?.autoIncrement || '—' }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-k">{{ t('db.metaCreated') }}</span>
                  <span class="meta-v">{{ activeTab.meta?.createTime || '—' }}</span>
                </div>
                <div class="meta-row wide">
                  <span class="meta-k">{{ t('db.metaComment') }}</span>
                  <span class="meta-v">{{ activeTab.meta?.comment || '—' }}</span>
                </div>
              </div>

              <div class="section-label">
                {{ t('db.columnsSection', { n: activeTab.columns.length }) }}
                <a class="link add-link" @click="openAddColumn">＋ {{ t('db.addColumn') }}</a>
              </div>
              <div class="grid-wrap">
                <table class="grid">
                  <thead>
                    <tr>
                      <th class="rownum">#</th>
                      <th>{{ t('db.colField') }}</th>
                      <th>{{ t('db.colType') }}</th>
                      <th>{{ t('db.colNull') }}</th>
                      <th>{{ t('db.colKey') }}</th>
                      <th>{{ t('db.colDefault') }}</th>
                      <th>{{ t('db.colExtra') }}</th>
                      <th class="ops-col">{{ t('db.actions') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(col, i) in activeTab.columns" :key="col.field">
                      <td class="rownum">{{ i + 1 }}</td>
                      <td class="mono strong">{{ col.field }}</td>
                      <td class="mono type">{{ col.type }}</td>
                      <td>{{ col.null }}</td>
                      <td>{{ col.key }}</td>
                      <td class="mono">{{ col.default ?? 'NULL' }}</td>
                      <td>{{ col.extra }}</td>
                      <td class="ops-col">
                        <button class="row-op" :title="t('menu.edit')" @click="openEditColumn(col)">✎</button>
                        <button class="row-op danger" :title="t('common.delete')" @click="dropColumn(col)">×</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <!-- 索引 -->
            <div v-else-if="activeTab.structView === 'indexes'" class="struct-body">
              <div class="section-label">
                {{ t('db.tabIndexes') }}
                <a class="link add-link" @click="openAddIndex('index')">＋ {{ t('db.addIndex') }}</a>
              </div>
              <div v-if="activeTab.indexes.length === 0" class="result-empty">
                <div class="empty-glyph">◈</div>
                <div>{{ t('db.noIndexes') }}</div>
              </div>
              <div v-else class="grid-wrap">
                <table class="grid">
                  <thead>
                    <tr>
                      <th>{{ t('db.idxName') }}</th>
                      <th>{{ t('db.idxType') }}</th>
                      <th>{{ t('db.idxUnique') }}</th>
                      <th>{{ t('db.idxColumns') }}</th>
                      <th class="ops-col">{{ t('db.actions') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="idx in activeTab.indexes" :key="idx.name">
                      <td class="mono strong">{{ idx.name }}</td>
                      <td>{{ idx.type }}</td>
                      <td>
                        <span class="pill" :class="idx.unique ? 'pill-accent' : ''">
                          {{ idx.unique ? t('db.yes') : t('db.no') }}
                        </span>
                      </td>
                      <td class="mono">{{ idx.columns.join(', ') }}</td>
                      <td class="ops-col">
                        <button
                          v-if="idx.name !== 'PRIMARY'"
                          class="row-op danger"
                          :title="t('common.delete')"
                          @click="dropIndex(idx)"
                        >×</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <!-- 外键 -->
            <div v-else-if="activeTab.structView === 'fks'" class="struct-body">
              <div class="section-label">
                {{ t('db.tabForeignKeys') }}
                <a class="link add-link" @click="openAddIndex('foreignKey')">＋ {{ t('db.addForeignKey') }}</a>
              </div>
              <div v-if="activeTab.foreignKeys.length === 0" class="result-empty">
                <div class="empty-glyph">⇄</div>
                <div>{{ t('db.noForeignKeys') }}</div>
              </div>
              <div v-else class="grid-wrap">
                <table class="grid">
                  <thead>
                    <tr>
                      <th>{{ t('db.fkName') }}</th>
                      <th>{{ t('db.fkColumn') }}</th>
                      <th>{{ t('db.fkRefTable') }}</th>
                      <th>{{ t('db.fkRefColumn') }}</th>
                      <th>{{ t('db.fkOnUpdate') }}</th>
                      <th>{{ t('db.fkOnDelete') }}</th>
                      <th class="ops-col">{{ t('db.actions') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="fk in activeTab.foreignKeys" :key="fk.name + fk.column">
                      <td class="mono strong">{{ fk.name }}</td>
                      <td class="mono">{{ fk.column }}</td>
                      <td class="mono">{{ fk.refTable }}</td>
                      <td class="mono">{{ fk.refColumn }}</td>
                      <td>{{ fk.onUpdate || '—' }}</td>
                      <td>{{ fk.onDelete || '—' }}</td>
                      <td class="ops-col">
                        <button class="row-op danger" :title="t('common.delete')" @click="dropForeignKey(fk)">×</button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <!-- DDL -->
            <pre v-else class="ddl">{{ activeTab.ddl || t('db.loadingData') }}</pre>
          </div>
        </template>
      </template>
    </div>

    <DBConnModal v-model:show="modal" :conn="editing" @saved="onSaved" />
    <ColumnModal
      v-model:show="colModal.show"
      :mode="colModal.mode"
      :column="colModal.column"
      @submit="submitColumn"
    />
    <IndexModal
      v-model:show="idxModal.show"
      :kind="idxModal.kind"
      :columns="activeTab?.columns.map((c) => c.field) ?? []"
      :table="activeTab?.table ?? ''"
      :tables="currentDbTables"
      @submit="submitIndex"
    />
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
  width: 264px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: var(--gap-sm);
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius);
  padding: 10px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.35);
}
.sidebar-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
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
  gap: 6px;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
  transition: background 0.12s ease, color 0.12s ease;
}
.conn-item:hover,
.db-item:hover,
.table-item:hover,
.key-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.conn-item.active,
.table-item.active,
.key-item.active {
  background: var(--accent-soft);
}
.twist {
  width: 10px;
  flex-shrink: 0;
  font-size: 9px;
  color: var(--text-muted);
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
  color: var(--danger);
}
.db-block {
  padding-left: 12px;
}
.db-item {
  color: rgba(255, 255, 255, 0.85);
}
.db-count {
  font-size: 10px;
  color: var(--text-muted);
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
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.tree-empty {
  font-size: 11px;
  color: var(--text-muted);
  padding: 6px 10px;
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
  color: var(--accent);
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
  color: var(--text-muted);
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
  background: rgba(248, 113, 113, 0.1);
  color: var(--danger);
  font-size: 11px;
  white-space: pre-wrap;
}

/* ---------- 右侧工作区 ---------- */
.workspace {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 0;
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius);
  overflow: hidden;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.35);
}
.placeholder {
  margin: auto;
  color: var(--text-muted);
  font-size: 13px;
}
.tab-bar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 6px 0;
  background: var(--bg-app);
  border-bottom: 1px solid var(--border-subtle);
  overflow-x: auto;
  flex-shrink: 0;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  border-radius: 6px 6px 0 0;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
  color: var(--text-secondary);
  max-width: 200px;
  position: relative;
  transition: background 0.12s ease, color 0.12s ease;
}
.tab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.tab.active {
  background: var(--bg-elevated);
  color: var(--text-primary);
}
.tab.active::after {
  content: '';
  position: absolute;
  left: 10px;
  right: 10px;
  bottom: 0;
  height: 2px;
  border-radius: 2px 2px 0 0;
  background: var(--accent);
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
  color: var(--danger);
}
.tab-add {
  padding: 0 8px;
  font-size: 14px;
  opacity: 0.6;
  cursor: pointer;
}
.tab-add:hover {
  opacity: 1;
  color: var(--accent);
}
.filter-input {
  flex: 1;
  min-width: 160px;
}
.page-num {
  font-size: 12px;
  color: var(--text-secondary);
  min-width: 52px;
  text-align: center;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: var(--gap-sm);
  flex-shrink: 0;
  padding: 8px 12px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border-subtle);
}
.ctx {
  font-size: 12px;
  color: var(--text-secondary);
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
  border: 1px solid var(--border);
  border-radius: 6px;
  color: #d4d4d4;
  font-family: Consolas, 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
  padding: 8px 10px;
  outline: none;
}
.sql-editor:focus {
  border-color: rgba(94, 234, 212, 0.45);
}
.result {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-panel);
  overflow: hidden;
}
.query-error {
  margin: 12px;
  padding: 10px 12px;
  border-radius: 6px;
  background: rgba(248, 113, 113, 0.1);
  color: var(--danger);
  font-size: 12px;
  white-space: pre-wrap;
}
.struct-toolbar {
  gap: var(--gap-sm);
}
.sub-tabs {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  background: var(--bg-app);
  border: 1px solid var(--border-subtle);
  border-radius: 7px;
}
.sub-tab {
  padding: 3px 10px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  font-family: inherit;
  border-radius: 5px;
  cursor: pointer;
  transition: all 0.12s ease;
}
.sub-tab:hover {
  color: var(--text-primary);
}
.sub-tab.active {
  background: var(--bg-elevated);
  color: var(--accent);
}
.struct-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 12px;
}
.meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 1px;
  background: var(--border-subtle);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius);
  overflow: hidden;
  margin-bottom: 16px;
}
.meta-row {
  display: flex;
  gap: 10px;
  padding: 8px 12px;
  background: var(--bg-panel);
  font-size: 12px;
}
.meta-row.wide {
  grid-column: 1 / -1;
}
.meta-k {
  color: var(--text-muted);
  min-width: 76px;
}
.meta-v {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
}
.section-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
  margin-bottom: 8px;
}
.grid td.strong {
  color: var(--text-primary);
  font-weight: 500;
}
.grid td.type {
  color: var(--info);
}
.pill {
  display: inline-block;
  padding: 0 7px;
  border-radius: 9px;
  font-size: 10px;
  background: var(--bg-app);
  color: var(--text-muted);
}
.add-link {
  margin-left: 12px;
  font-size: 11px;
}
.ops-col {
  width: 70px;
  text-align: right;
  white-space: nowrap;
}
.row-op {
  width: 22px;
  height: 22px;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  margin-left: 2px;
}
.row-op:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: var(--border-subtle);
}
.row-op.danger:hover {
  color: var(--danger);
}
.edit-actions {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px;
  border-radius: 6px;
  opacity: 0.45;
  transition: opacity 0.15s ease;
}
.edit-actions.active {
  opacity: 1;
}
.edit-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  color: var(--text-muted);
  transition: all 0.12s ease;
}
.edit-btn:disabled {
  cursor: default;
  opacity: 0.4;
}
.edit-btn.apply:not(:disabled) {
  color: #34d399;
  border-color: rgba(52, 211, 153, 0.4);
  background: rgba(52, 211, 153, 0.12);
}
.edit-btn.discard:not(:disabled) {
  color: var(--danger);
  border-color: rgba(248, 113, 113, 0.4);
  background: rgba(248, 113, 113, 0.1);
}
.pending-count {
  font-size: 10px;
  color: var(--warning);
  min-width: 12px;
  text-align: center;
}
.grid td.edited {
  background: rgba(251, 191, 36, 0.12);
  box-shadow: inset 0 0 0 1px rgba(251, 191, 36, 0.45);
  color: var(--text-primary);
}
.cell-input {
  width: 100%;
  min-width: 80px;
  background: var(--bg-app);
  border: 1px solid var(--accent);
  border-radius: 3px;
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 1px 4px;
  outline: none;
}
.pill-accent {
  background: var(--accent-soft);
  color: var(--accent);
}
.result-empty {
  margin: auto;
  color: var(--text-muted);
  font-size: 12px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
}
.empty-glyph {
  font-size: 30px;
  opacity: 0.35;
  line-height: 1;
}
.empty-hint {
  font-size: 11px;
  color: var(--text-muted);
  opacity: 0.8;
}
.result-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  font-size: 11px;
  opacity: 0.65;
  border-bottom: 1px solid var(--bg-hover);
  flex-shrink: 0;
}
.warn {
  color: var(--warning);
}
.hint {
  font-size: 10px;
  color: var(--text-muted);
}
.link {
  color: var(--accent);
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
  padding: 6px 12px;
  border-bottom: 1px solid var(--border-subtle);
  text-align: left;
  white-space: nowrap;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.5;
}
.grid th {
  position: sticky;
  top: 0;
  background: var(--bg-elevated);
  color: var(--text-secondary);
  font-size: 11px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  z-index: 1;
  border-bottom: 1px solid var(--border);
}
.grid tbody tr:nth-child(even) {
  background: rgba(255, 255, 255, 0.015);
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
  color: var(--accent);
}
.grid tbody tr:hover {
  background: rgba(255, 255, 255, 0.04);
}
.grid td {
  cursor: pointer;
}
.grid td.selected {
  background: rgba(94, 234, 212, 0.22);
}
.rownum {
  color: var(--text-muted);
  text-align: right;
  width: 40px;
}
.grid td.null {
  color: var(--text-muted);
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
  border-top: 1px solid var(--border-subtle);
  padding-top: 4px;
}
.history-head {
  font-size: 10px;
  color: var(--text-muted);
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
  font-family: var(--font-mono);
  color: var(--text-secondary);
  padding: 1px 4px;
  border-radius: 3px;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.history-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* ---------- Redis 值 ---------- */
.value {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--border-subtle);
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
  background: var(--border-subtle);
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
  color: var(--warning);
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
  border: 1px solid var(--border);
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
