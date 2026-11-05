<script setup lang="ts">
import { computed, h, inject, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NDropdown, NInput, NInputNumber, NModal, NPopover, NSelect } from 'naive-ui'
import {
  DBDescribeTable,
  DBListDatabases,
  DBListForeignKeys,
  DBListIndexes,
  DBListTables,
  DBPreviewTableSQL,
  DBQuery,
  DBShowCreateTable,
  DBTableData,
  DBTableMeta,
  DBUpdateRows,
  DeleteDBConn,
  GetGridPref,
  ListDBConns,
  RedisDeleteKey,
  RedisGetValue,
  RedisScanKeys,
  RedisSetString,
  RedisSetTTL,
  SaveDBConn,
  SetGridPref,
  TestDBConn,
} from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'
import DBConnModal from './DBConnModal.vue'
import ColumnModal from './ColumnModal.vue'
import IndexModal from './IndexModal.vue'
import AIPanel from './AIPanel.vue'
import AISettingsModal from './AISettingsModal.vue'
import GridColumnsPanel from './GridColumnsPanel.vue'
import GridFilterPanel from './GridFilterPanel.vue'
import GridFilterChips from './GridFilterChips.vue'
import { buildRowUpdates, countPendingEdits, shouldBlockUnguardedSwitch, type PendingEdit } from './dbEditing'
import {
  cloneConditions,
  conditionError,
  defaultOperatorForType,
  familyOfType,
  sameCondition,
  stripLeadingWhereText,
  type FilterCondition,
  type FilterOp,
} from './gridFilters'
import {
  gridPrefKey,
  parseGridPref,
  repairHidden,
  serializeGridPref,
  visibleColumnRefs as computeVisibleColumns,
} from '../stores/gridPrefs'

// 数据库视图（HeidiSQL 风格）：
// 左：连接 → 库 → 表 树；右：SQL 工作台（编辑器 + 结果/结构）或 Redis 值查看器
const { t, te, locale } = useI18n()
const message = useMessage()
const dialog = useDialog()
// App 注入的视图切换守卫：切到终端视图前 await，防止未提交修改随组件卸载丢失
const viewGuard = inject<{ fn: (() => Promise<boolean>) | null } | null>('viewGuard', null)

// 后端错误统一为 "code: message"；按 code 映射 i18n，未知 code 原样展示。
// 不按英文 message 文本匹配（文案/数据库版本会变）。
const SERVICE_ERROR_RE = /^([a-z_]+):\s*([\s\S]*)$/
function serviceErrorText(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e)
  const m = SERVICE_ERROR_RE.exec(raw)
  if (!m) return raw
  const key = `db.err.${m[1]}`
  if (!te(key)) return raw
  return t(key, { message: m[2] })
}

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
    loadError.value = serviceErrorText(e)
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

async function askDelete(c: models.store.DBConn) {
  // 删除当前连接会清空全部标签；先保护未提交修改
  if (
    c.id === activeId.value &&
    !(await guardPendingTabs(dataTabsWithPending(), {
      switchConnection: true,
      tabsToClose: tabs.value.length,
    })).ok
  ) {
    return
  }
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
        message.error(serviceErrorText(e))
      }
    },
  })
}

// 切换连接：watch(activeId) 会清空全部标签，因此必须在这里先确认。
// 不要在其他地方直接给 activeId 赋值（删除连接/首次加载除外，那两条路径没有 pending）。
async function selectConn(c: models.store.DBConn) {
  if (c.id === activeId.value) return
  const guard = await guardPendingTabs(dataTabsWithPending(), {
    switchConnection: true,
    tabsToClose: tabs.value.length,
  })
  if (!guard.ok) return
  activeId.value = c.id
}

async function testConn(c: models.store.DBConn) {
  try {
    await TestDBConn(c.id)
    message.success(t('db.testOk'))
  } catch (e) {
    message.error(serviceErrorText(e))
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
    message.error(serviceErrorText(e))
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
        message.error(serviceErrorText(e))
        tablesByDb[db] = []
      }
    }
  }
  expandedDbs.value = next
}

// 单击表 → 打开结构标签（已存在则激活）
// 单击表：创建「表」与「数据」两个固定标签，激活「表」，数据同步加载。
// 保护在动作入口 await：只有确认后才改标签/表名，取消时界面保持原状。
async function openTableStructure(db: string, table: models.db.TableInfo) {
  const prior = currentDataTab()
  const hadPending = !!prior && prior.pending.length > 0
  // 打开/查看结构前也保护 pending（即使还是同一张表，DDL 可能让旧值失效）
  if (!(await guardPendingTabs(prior ? [prior] : [])).ok) return

  const structTab = ensureStructureTab(db, table.name)
  const dataTab = ensureDataTab(db, table.name)
  activeTabId.value = structTab.id
  // 刚应用/丢弃过修改时必须刷新同表数据，否则网格仍显示旧值
  const needLoad =
    hadPending || !(dataTab.result && dataTab.database === db && dataTab.table === table.name)
  await Promise.all([
    loadTableStructure(structTab),
    needLoad ? loadTableData(dataTab, true, 'discard') : Promise.resolve(),
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
    tab.error = serviceErrorText(e)
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
      // 切换了表：清空旧结果与旧编辑状态，等待重新加载。
      // pending 的截图已不存在，不能带着旧行索引进入新表；
      // 调用方必须在此之前完成保护确认（openTableStructure/openTableData 的入口守卫）。
      // 切表重置清单（漏一项就会串表）；filterMode 按设计在会话内保留
      tab.sortCol = ''
      tab.page = 0
      tab.result = null
      tab.pending = []
      tab.editing = null
      tab.columns = []
      tab.pkColumns = []
      tab.gridSortCol = -1
      tab.selectedRow = -1
      tab.selectedCol = -1
      tab.hiddenColumns = []
      tab.columnsPrefLoaded = false
      tab.columnsPanelOpen = false
      tab.filterPanelOpen = false
      tab.conditions = []
      tab.conditionsDraft = []
      tab.rawWhere = ''
      tab.rawWhereDraft = ''
      tab.rawPrefill = ''
      tab.conditionsSnapshot = null
      tab.filterDirty = false
      tab.filterPreviewSql = ''
      tab.filterPreviewError = ''
      tab.lastTableSQL = ''
      tab.filterError = ''
      tab.columnsStale = false
      tab.metadataError = ''
      tab.appliedSource = 'structured'
      tab.loadSeq++
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
// 双击是显式重查：即使表没变也要先确认未提交修改
async function openTableData(db: string, table: models.db.TableInfo) {
  const prior = currentDataTab()
  if (!(await guardPendingTabs(prior ? [prior] : [])).ok) return
  ensureStructureTab(db, table.name)
  const tab = ensureDataTab(db, table.name)
  activeTabId.value = tab.id
  await loadTableData(tab, true, 'discard')
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
  // 筛选（Applied：当前结果对应的谓词）
  filterMode: 'structured' | 'raw'
  appliedSource: 'structured' | 'raw'
  conditions: FilterCondition[]
  rawWhere: string
  lastTableSQL: string
  filterError: string
  // 筛选草稿 / SQL 预览
  conditionsDraft: FilterCondition[]
  rawWhereDraft: string
  filterPanelOpen: boolean
  filterPreviewSql: string
  filterPreviewLoading: boolean
  filterPreviewError: string
  filterPreviewSeq: number
  rawPrefill: string
  conditionsSnapshot: FilterCondition[] | null
  filterDirty: boolean
  // 元数据
  columnsStale: boolean
  metadataError: string
  // 查询序号：旧响应不得覆盖新状态
  loadSeq: number
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
  pending: PendingEdit[]
  editing: { row: number; col: number } | null
  gridSortCol: number
  gridSortDir: 1 | -1
  selectedRow: number
  selectedCol: number
  // 列可见性（按连接+库+表持久化到 settings 表）
  hiddenColumns: string[]
  columnsPrefLoaded: boolean
  columnsPanelOpen: boolean
  // 最近一次实际执行的 SQL（修复模式用它，避免用户报错后改了编辑器导致对象错位）
  lastRunSql: string
  // AI 助手侧栏是否展开（按查询标签保存，仅前端状态）
  aiOpen: boolean
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
    filterMode: 'structured',
    appliedSource: 'structured',
    conditions: [],
    rawWhere: '',
    lastTableSQL: '',
    filterError: '',
    conditionsDraft: [],
    rawWhereDraft: '',
    filterPanelOpen: false,
    filterPreviewSql: '',
    filterPreviewLoading: false,
    filterPreviewError: '',
    filterPreviewSeq: 0,
    rawPrefill: '',
    conditionsSnapshot: null,
    filterDirty: false,
    columnsStale: false,
    metadataError: '',
    loadSeq: 0,
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
    hiddenColumns: [],
    columnsPrefLoaded: false,
    columnsPanelOpen: false,
    lastRunSql: '',
    aiOpen: false,
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

async function closeTab(id: string) {
  const idx = tabs.value.findIndex((x) => x.id === id)
  if (idx === -1) return
  const tab = tabs.value[idx]
  if (!canCloseTab(tab)) return
  // 数据标签目前固定（pinned）不可关闭；这里仍兜底保护，避免未来放开后静默丢修改
  if (!(await guardPendingTabs([tab])).ok) return
  tabs.value.splice(idx, 1)
  if (activeTabId.value === id) activeTabId.value = tabs.value.at(-1)?.id ?? ''
}

// 数据标签查询：结构化条件与 RawWhere 由后端校验并参数化生成
function buildTableQuery(
  tab: WorkTab,
  conditions: FilterCondition[],
  rawWhere: string,
  page: number,
): models.db.TableQuery {
  return models.db.TableQuery.createFrom({
    database: tab.database,
    table: tab.table,
    conditions: conditions.map((c) => ({ column: c.column, op: c.op, value: c.value })),
    rawWhere,
    sortColumn: tab.sortCol,
    sortDesc: tab.sortDir < 0,
    page,
    pageSize: tab.pageSize,
  })
}

function errorCodeOf(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e)
  const m = SERVICE_ERROR_RE.exec(raw)
  return m ? m[1] : ''
}

// unknown_column：刷新一次元数据、移除失效条件/排序，最多重试一次
async function dropStaleConditions(tab: WorkTab): Promise<boolean> {
  let cols: models.db.ColumnInfo[]
  try {
    cols = (await DBDescribeTable(tab.connId, tab.database, tab.table)) ?? []
  } catch {
    return false
  }
  if (cols.length === 0) return false
  tab.columns = cols
  tab.pkColumns = cols.filter((c) => c.key === 'PRI').map((c) => c.field)
  const names = new Set(cols.map((c) => c.field))
  const keyOf = (c: FilterCondition) => `${c.column}\u0000${c.op}\u0000${c.value}`
  const beforeKeys = new Set([...tab.conditions, ...tab.conditionsDraft].map(keyOf))
  tab.conditions = tab.conditions.filter((c) => names.has(c.column))
  tab.conditionsDraft = tab.conditionsDraft.filter((c) => names.has(c.column))
  const afterKeys = new Set([...tab.conditions, ...tab.conditionsDraft].map(keyOf))
  const removed = [...beforeKeys].filter((k) => !afterKeys.has(k)).length
  let changed = removed > 0
  if (tab.sortCol && !names.has(tab.sortCol)) {
    tab.sortCol = ''
    changed = true
  }
  if (removed > 0) message.info(t('db.filtersRemoved', { n: removed }))
  return changed
}

async function loadTableData(
  tab: WorkTab,
  resetPage = false,
  intent: LoadIntent = 'plain',
  metadataRetried = false,
  pageRetried = false,
): Promise<boolean> {
  // 单一收口：pending > 0 时普通加载直接拒绝，调用方必须先走确认流程
  if (intent === 'plain' && tab.pending.length > 0) {
    console.warn('[db] loadTableData blocked: pending edits require explicit discard intent')
    return false
  }
  if (resetPage) tab.page = 0
  const seq = ++tab.loadSeq
  tab.loading = true
  tab.error = ''
  tab.filterError = ''
  tab.pending = []
  tab.editing = null
  try {
    const conditions = tab.appliedSource === 'structured' ? cloneConditions(tab.conditions) : []
    const rawWhere = tab.appliedSource === 'raw' ? tab.rawWhere : ''
    const resp = await DBTableData(tab.connId, buildTableQuery(tab, conditions, rawWhere, tab.page))
    if (seq !== tab.loadSeq) return false
    tab.result = resp.result
    tab.columns = resp.columns ?? []
    tab.lastTableSQL = resp.executedSql ?? ''
    tab.pkColumns = tab.columns.filter((c) => c.key === 'PRI').map((c) => c.field)
    tab.metadataError = ''
    tab.columnsStale = false
    if (resp.hadLeadingWhere) {
      // 后端词法层已去掉一个前导 WHERE；同步前端文本，避免每次查询重复提示
      message.info(t('db.rawWhereStripped'))
      tab.rawWhere = stripLeadingWhereText(tab.rawWhere)
      tab.rawWhereDraft = stripLeadingWhereText(tab.rawWhereDraft)
      tab.rawPrefill = stripLeadingWhereText(tab.rawPrefill)
    }
    await syncGridPref(tab)
    if (seq !== tab.loadSeq) return false
    tab.gridSortCol = -1
    tab.selectedRow = -1
    tab.selectedCol = -1
    return true
  } catch (e) {
    if (seq !== tab.loadSeq) return false
    // 查询失败保留上一次结果，界面用错误 banner 标注“显示的是上一次结果”
    tab.error = serviceErrorText(e)
    tab.filterError = e instanceof Error ? e.message : String(e)
    const code = errorCodeOf(e)
    if (code === 'invalid_page' && tab.page !== 0 && !pageRetried) {
      tab.page = 0
      message.info(t('db.err.invalid_page'))
      return loadTableData(tab, false, 'applied', metadataRetried, true)
    }
    if (code === 'unknown_column' && !metadataRetried && tab.appliedSource === 'structured') {
      const changed = await dropStaleConditions(tab)
      if (changed) return loadTableData(tab, false, 'applied', true, pageRetried)
    }
    if (code === 'table_metadata_unavailable') tab.metadataError = tab.error
    return false
  } finally {
    if (seq === tab.loadSeq) tab.loading = false
  }
}

// ---------- 列可见性 ----------

// 可见列引用（原始索引必须保留：编辑/排序/CSV 都用它）
function visibleColumnsOf(tab: WorkTab) {
  return computeVisibleColumns(tab.hiddenColumns, tab.result?.columns ?? [])
}

// pending 中落在隐藏列上的条数（工具栏警告）
function hiddenPendingColumns(tab: WorkTab): string[] {
  const columns = tab.result?.columns ?? []
  const hidden = new Set(tab.hiddenColumns)
  const out = new Set<string>()
  for (const p of tab.pending) {
    const name = columns[p.col]
    if (name && hidden.has(name)) out.add(name)
  }
  return [...out]
}

// 元数据加载失败时列面板仍可用：退化为只依赖 result.columns 的列名
function columnsForPanel(tab: WorkTab): models.db.ColumnInfo[] {
  if (tab.columns.length > 0) return tab.columns
  return (tab.result?.columns ?? []).map(
    (field) =>
      ({
        field,
        type: '',
        null: '',
        key: '',
        extra: '',
        comment: '',
      }) as models.db.ColumnInfo,
  )
}

async function saveGridPref(tab: WorkTab) {
  try {
    await SetGridPref(
      gridPrefKey(tab.connId, tab.database, tab.table),
      serializeGridPref(tab.hiddenColumns),
    )
  } catch (e) {
    // 偏好写入失败只影响记忆，不影响本次会话的显示
    console.warn('[db] save grid prefs failed', e)
  }
}

// 读取/修复列偏好。只在“首次加载该表”时读存储；之后只做求交修复。
async function syncGridPref(tab: WorkTab) {
  const columns = tab.result?.columns ?? []
  if (columns.length === 0) return
  if (!tab.columnsPrefLoaded) {
    tab.columnsPrefLoaded = true
    try {
      const raw = await GetGridPref(gridPrefKey(tab.connId, tab.database, tab.table))
      const { hidden, repaired } = repairHidden(parseGridPref(raw), columns)
      tab.hiddenColumns = hidden
      if (repaired) await saveGridPref(tab)
    } catch (e) {
      console.warn('[db] load grid prefs failed', e)
      tab.hiddenColumns = []
    }
    return
  }
  const { hidden, repaired } = repairHidden(tab.hiddenColumns, columns)
  tab.hiddenColumns = hidden
  if (repaired) await saveGridPref(tab)
}

async function applyColumns(tab: WorkTab, hidden: string[]) {
  const { hidden: valid } = repairHidden(hidden, tab.result?.columns ?? [])
  tab.hiddenColumns = valid
  tab.columnsPanelOpen = false
  // 隐藏列不重新查询，只影响显示与 CSV
  await saveGridPref(tab)
}

function openColumnsPanel(tab: WorkTab) {
  if (!(tab.result?.columns?.length)) {
    message.warning(t('db.colDisabled'))
    return
  }
  // 两个面板互斥
  tab.filterPanelOpen = false
  tab.columnsPanelOpen = !tab.columnsPanelOpen
}

function columnsButtonTitle(tab: WorkTab): string {
  const hidden = tab.hiddenColumns.length
  return hidden > 0 ? t('db.colButtonHidden', { n: hidden }) : t('db.colButtonHint')
}

function hiddenPendingEditCount(tab: WorkTab): number {
  const columns = tab.result?.columns ?? []
  const hidden = new Set(tab.hiddenColumns)
  return tab.pending.filter((p) => hidden.has(columns[p.col])).length
}

async function clearSort(tab: WorkTab) {
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.sortCol = ''
  })
}

function toggleSortDirection(tab: WorkTab) {
  if (tab.sortCol) void sortTableByColumn(tab, tab.sortCol)
}

// ---------- 结构化筛选 / 高级 WHERE ----------

const rawInputEl = ref<HTMLTextAreaElement | null>(null)
const sqlViewer = reactive({ show: false, title: '', sql: '' })
const previewTimers = new Map<string, ReturnType<typeof setTimeout>>()

function filterCount(tab: WorkTab): number {
  return tab.conditions.length
}

function hasAppliedPredicate(tab: WorkTab): boolean {
  return tab.appliedSource === 'raw' ? tab.rawWhere.trim() !== '' : tab.conditions.length > 0
}

async function gotoFirstPage(tab: WorkTab) {
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, false, guard, () => {
    tab.page = 0
  })
}

function focusRawInput() {
  nextTick(() => rawInputEl.value?.focus())
}

function filterButtonTitle(tab: WorkTab): string {
  if (tab.metadataError) return tab.metadataError
  if (!(tab.result?.columns?.length)) return t('db.colDisabled')
  return t('db.filter.title')
}

function openFilterPanel(tab: WorkTab, focusIndex = 0) {
  if (tab.filterMode === 'raw') {
    focusRawInput()
    return
  }
  if (!columnsForPanel(tab).length) {
    message.warning(t('db.colDisabled'))
    return
  }
  tab.columnsPanelOpen = false
  if (!tab.filterPanelOpen) {
    tab.conditionsDraft = cloneConditions(tab.conditions)
    if (tab.conditionsDraft.length === 0) {
      const first = columnsForPanel(tab)[0]
      tab.conditionsDraft = [
        { column: first.field, op: defaultOperatorForType(first.type), value: '' },
      ]
    }
  }
  tab.filterPanelOpen = true
  focusDraftRow(focusIndex)
}

function closeFilterPanel(tab: WorkTab) {
  tab.filterPanelOpen = false
}

function onDraftChange(tab: WorkTab, draft: FilterCondition[]) {
  // 只有面板内展开“预览 SQL”时，GridFilterPanel 才会 emit preview；
  // 这里更新草稿即可，不主动打后端。
  tab.conditionsDraft = draft
}

function scheduleStructuredPreview(tab: WorkTab) {
  const old = previewTimers.get(tab.id)
  if (old) clearTimeout(old)
  previewTimers.set(
    tab.id,
    setTimeout(() => {
      void runPreview(tab)
    }, 250),
  )
}

// 草稿 SQL 预览：固定 page=0（应用筛选后回到第一页），旧响应按序号丢弃。
// 只在面板展开“预览 SQL”时由 GridFilterPanel 触发，草稿编辑本身不请求后端。
async function runPreview(tab: WorkTab) {
  const seq = ++tab.filterPreviewSeq
  tab.filterPreviewLoading = true
  tab.filterPreviewError = ''
  try {
    const q = buildTableQuery(tab, tab.conditionsDraft, '', 0)
    const preview = await DBPreviewTableSQL(tab.connId, q)
    if (seq !== tab.filterPreviewSeq) return
    tab.filterPreviewSql = preview.sql
  } catch (e) {
    if (seq !== tab.filterPreviewSeq) return
    tab.filterPreviewSql = ''
    tab.filterPreviewError = serviceErrorText(e)
  } finally {
    if (seq === tab.filterPreviewSeq) tab.filterPreviewLoading = false
  }
}

async function applyStructured(tab: WorkTab) {
  if (tab.loading) return
  const draft = cloneConditions(tab.conditionsDraft)
  if (draft.some((c) => conditionError(c, columnsForPanel(tab)) !== '')) {
    message.error(t('db.filter.applyBlocked'))
    return
  }
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.conditions = draft
    tab.rawWhere = ''
    tab.appliedSource = 'structured'
    tab.filterMode = 'structured'
    tab.filterPanelOpen = false
    tab.filterDirty = false
    tab.page = 0
  })
}

async function clearFilter(tab: WorkTab) {
  if (tab.loading) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.conditions = []
    tab.conditionsDraft = []
    tab.rawWhere = ''
    tab.rawWhereDraft = ''
    tab.rawPrefill = ''
    tab.conditionsSnapshot = null
    tab.filterDirty = false
    tab.filterPanelOpen = false
    tab.appliedSource = 'structured'
    tab.filterMode = 'structured'
    tab.page = 0
  })
}

async function removeCondition(tab: WorkTab, index: number) {
  if (tab.loading) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.conditions = tab.conditions.filter((_, i) => i !== index)
    tab.appliedSource = 'structured'
    tab.rawWhere = ''
    tab.page = 0
  })
}

function editCondition(tab: WorkTab, index: number) {
  openFilterPanel(tab, index)
}

function onRawInput(tab: WorkTab, value: string) {
  tab.rawWhereDraft = value
  tab.filterDirty =
    tab.appliedSource === 'structured'
      ? value !== tab.rawPrefill
      : value.trim() !== tab.rawWhere.trim()
  // 高级输入不自动预览；只有点「预览 SQL」才请求后端
}

async function applyRaw(tab: WorkTab) {
  if (tab.loading) return
  const draft = tab.rawWhereDraft.trim()
  if (tab.appliedSource === 'raw' && draft === tab.rawWhere.trim()) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.rawWhere = draft
    tab.appliedSource = 'raw'
    tab.filterMode = 'raw'
    // Applied 来源切到 raw 后，结构化条件不再是真相，清空避免工具栏计数陈旧
    tab.conditions = []
    tab.conditionsDraft = []
    tab.conditionsSnapshot = null
    tab.filterDirty = false
    tab.page = 0
  })
}

// 结构化 → 高级：只切换编辑表面，appliedSource 仍是 structured，不重新查询
async function switchToRaw(tab: WorkTab) {
  try {
    const preview = await DBPreviewTableSQL(
      tab.connId,
      buildTableQuery(tab, tab.conditions, '', 0),
    )
    tab.rawPrefill = preview.where ?? ''
    tab.rawWhereDraft = tab.rawPrefill
    tab.conditionsSnapshot = cloneConditions(tab.conditions)
    tab.filterMode = 'raw'
    tab.filterDirty = false
    tab.filterPanelOpen = false
    tab.columnsPanelOpen = false
    focusRawInput()
  } catch (e) {
    message.error(serviceErrorText(e))
  }
}

function copySQL(sql: string) {
  if (!sql) return
  navigator.clipboard?.writeText(sql).then(
    () => message.success(t('db.sqlCopied')),
    () => {
      /* 忽略 */
    },
  )
}

function showSQL(title: string, sql: string) {
  sqlViewer.title = title
  sqlViewer.sql = sql
  sqlViewer.show = true
}

function openSQLInQueryTab(sql: string) {
  if (!sql) return
  const n = tabs.value.filter((x) => x.kind === 'query').length + 1
  const qtab = newTab('query', {
    title: t('db.queryN', { n }),
    sql: `${t('db.openInQueryComment')}\n${sql}`,
  })
  tabs.value.push(qtab)
  sortTabs()
  activeTabId.value = qtab.id
  sqlViewer.show = false
}

function showExecutedSQL(tab: WorkTab) {
  if (!tab.lastTableSQL) {
    message.info(t('db.noExecutedSql'))
    return
  }
  showSQL(t('db.executedSql'), tab.lastTableSQL)
}

async function showRawPreview(tab: WorkTab) {
  try {
    const preview = await DBPreviewTableSQL(
      tab.connId,
      buildTableQuery(tab, [], tab.rawWhereDraft, 0),
    )
    showSQL(t('db.previewSqlTitle'), preview.sql)
  } catch (e) {
    message.error(serviceErrorText(e))
  }
}

// 高级 → 结构化：不做 SQL 解析，只按是否有未应用/已应用的高级文本走确认流程
function confirmRawAction(
  title: string,
  content: string,
  positiveText: string,
  copyValue: string,
): Promise<boolean> {
  return new Promise((resolve) => {
    let done = false
    let dlg: { destroy: () => void } | null = null
    const finish = (v: boolean) => {
      if (done) return
      done = true
      dlg?.destroy()
      resolve(v)
    }
    const button = (text: string, kind: 'default' | 'primary', onClick: () => void) =>
      h(
        NButton,
        { size: 'small', type: kind === 'primary' ? 'primary' : 'default', onClick },
        { default: () => text },
      )
    dlg = dialog.create({
      type: 'warning',
      title,
      content: () => h('div', content),
      action: () =>
        h('div', { style: 'display:flex;justify-content:flex-end;gap:8px;margin-top:16px' }, [
          button(t('db.filter.copySql'), 'default', () => copySQL(copyValue)),
          button(t('common.cancel'), 'default', () => finish(false)),
          button(positiveText, 'primary', () => finish(true)),
        ]),
      closable: false,
      maskClosable: false,
      closeOnEsc: false,
      onClose: () => finish(false),
    })
  })
}

async function switchToStructured(tab: WorkTab) {
  if (tab.filterMode !== 'raw') return
  const draftChanged = tab.rawWhereDraft !== tab.rawPrefill

  if (tab.appliedSource === 'structured') {
    if (draftChanged) {
      const ok = await confirmRawAction(
        t('db.filter.discardDraftTitle'),
        t('db.filter.discardDraftContent'),
        t('db.filter.discardDraftOk'),
        tab.rawWhereDraft,
      )
      if (!ok) return
    }
    // Applied 来源本来就是结构化：恢复条件快照即可，不查询
    tab.filterMode = 'structured'
    tab.rawWhere = ''
    tab.rawWhereDraft = ''
    tab.rawPrefill = ''
    tab.conditionsSnapshot = null
    tab.filterDirty = false
    return
  }

  // appliedSource === 'raw'：清除高级 WHERE 并查一次无筛选结果，走 pending 保护
  const ok = await confirmRawAction(
    t('db.filter.clearRawTitle'),
    t('db.filter.clearRawContent'),
    t('db.filter.clearRawOk'),
    tab.rawWhereDraft,
  )
  if (!ok) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.filterMode = 'structured'
    tab.appliedSource = 'structured'
    tab.conditions = []
    tab.conditionsDraft = []
    tab.rawWhere = ''
    tab.rawWhereDraft = ''
    tab.rawPrefill = ''
    tab.conditionsSnapshot = null
    tab.filterDirty = false
    tab.page = 0
  })
}

function onDataKeydown(e: KeyboardEvent) {
  const tab = activeTab.value
  if (!tab || tab.kind !== 'data') return
  const mod = e.ctrlKey || e.metaKey
  if (mod && !e.shiftKey && e.key.toLowerCase() === 'f') {
    e.preventDefault()
    if (tab.filterMode === 'raw') focusRawInput()
    else openFilterPanel(tab)
    return
  }
  if (mod && e.shiftKey && e.key.toLowerCase() === 'l') {
    e.preventDefault()
    openColumnsPanel(tab)
    nextTick(() => {
      document.querySelector<HTMLInputElement>('.grid-columns-search input')?.focus()
    })
  }
}

// 弹层在 teleport 容器里，键盘事件不会冒泡到 .data-tab，因此 Esc 用 window 监听
function onWindowKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  const tab = activeTab.value
  if (!tab || tab.kind !== 'data') return
  if (tab.filterPanelOpen) {
    tab.filterPanelOpen = false
    e.preventDefault()
    return
  }
  if (tab.columnsPanelOpen) {
    tab.columnsPanelOpen = false
    e.preventDefault()
  }
}

// ---------- 单元格 / 列头右键快捷筛选 ----------

type ContextMenuKind = 'cell' | 'header'

const cellMenu = reactive({
  show: false,
  x: 0,
  y: 0,
  tabId: '',
  kind: 'cell' as ContextMenuKind,
  row: -1,
  col: -1,
  column: '',
  databaseValue: null as string | null,
  hasPending: false,
})

const cellMenuTab = computed(() => tabs.value.find((t) => t.id === cellMenu.tabId) ?? null)

const cellMenuColumn = computed(() => {
  const tab = cellMenuTab.value
  if (!tab) return null
  return tab.columns.find((c) => c.field === cellMenu.column) ?? null
})

function truncateValue(v: string): string {
  const runes = [...v]
  return runes.length > 24 ? runes.slice(0, 24).join('') + '…' : v
}

function copyText(text: string, okMessage?: string) {
  if (!text) return
  navigator.clipboard?.writeText(text).then(
    () => message.success(okMessage ?? t('db.cell.copied')),
    () => {
      /* 忽略 */
    },
  )
}

function openCellMenu(e: MouseEvent, tab: WorkTab, row: number, col: number) {
  e.preventDefault()
  const rows = sortedRows(tab)
  cellMenu.show = true
  cellMenu.x = e.clientX
  cellMenu.y = e.clientY
  cellMenu.tabId = tab.id
  cellMenu.kind = 'cell'
  cellMenu.row = row
  cellMenu.col = col
  cellMenu.column = tab.result?.columns?.[col] ?? ''
  cellMenu.databaseValue = rows[row]?.[col] ?? null
  cellMenu.hasPending = tab.pending.some((p) => p.row === row && p.col === col)
}

function openHeaderMenu(e: MouseEvent, tab: WorkTab, name: string) {
  e.preventDefault()
  cellMenu.show = true
  cellMenu.x = e.clientX
  cellMenu.y = e.clientY
  cellMenu.tabId = tab.id
  cellMenu.kind = 'header'
  cellMenu.row = -1
  cellMenu.col = -1
  cellMenu.column = name
  cellMenu.databaseValue = null
  cellMenu.hasPending = false
}

const cellMenuOptions = computed(() => {
  const tab = cellMenuTab.value
  if (!tab) return []
  const col = cellMenuColumn.value
  const rawMode = tab.filterMode === 'raw'
  const options: Record<string, unknown>[] = []

  if (cellMenu.kind === 'header') {
    if (rawMode) {
      options.push({ label: t('db.cell.rawDisabledShort'), key: 'hint-raw', disabled: true })
    }
    options.push({
      label: t('db.cell.filterColumn'),
      key: 'filter-column',
      disabled: rawMode,
    })
    options.push({ label: t('db.cell.sortAsc'), key: 'sort-asc' })
    options.push({ label: t('db.cell.sortDesc'), key: 'sort-desc' })
    options.push({ type: 'divider', key: 'divider-1' })
    options.push({
      label: t('db.cell.hideColumn'),
      key: 'hide-column',
      disabled: visibleColumnsOf(tab).length <= 1,
    })
    options.push({ label: t('db.cell.copyColumn'), key: 'copy-column' })
    return options
  }

  // 单元格菜单：高级模式下只保留编辑/复制
  if (rawMode) {
    options.push({ label: t('db.cell.rawDisabled'), key: 'hint-raw', disabled: true })
    const v = cellMenu.databaseValue === null ? 'NULL' : truncateValue(cellMenu.databaseValue)
    options.push({ label: t('db.cell.filterEq', { value: v }), key: 'filter-eq', disabled: true })
    options.push({ label: t('db.cell.filterContains'), key: 'filter-contains', disabled: true })
    options.push({ label: t('db.cell.filterNe', { value: v }), key: 'filter-ne', disabled: true })
  } else if (cellMenu.databaseValue === null) {
    options.push({ label: t('db.cell.filterNull'), key: 'filter-null' })
    options.push({ label: t('db.cell.filterNotNull'), key: 'filter-not-null' })
  } else {
    const value = cellMenu.databaseValue
    const shown = truncateValue(value)
    const fam = familyOfType(col?.type ?? '')
    const tooLong = [...value].length > 4096
    const db = cellMenu.hasPending
    if (fam === 'numeric' || fam === 'date' || fam === 'enum' || fam === 'text') {
      options.push({
        label: t(db ? 'db.cell.filterDbEq' : 'db.cell.filterEq', { value: shown }),
        key: 'filter-eq',
        disabled: tooLong,
      })
    }
    if (fam === 'text' || fam === 'json') {
      options.push({
        label: t(db ? 'db.cell.filterDbContains' : 'db.cell.filterContains'),
        key: 'filter-contains',
        disabled: tooLong,
      })
    }
    if (fam === 'json') {
      options.push({ label: t('db.cell.jsonWarning'), key: 'hint-json', disabled: true })
    }
    if (fam === 'numeric' || fam === 'date' || fam === 'enum' || fam === 'text') {
      options.push({
        label: t(db ? 'db.cell.filterDbNe' : 'db.cell.filterNe', { value: shown }),
        key: 'filter-ne',
        disabled: tooLong,
      })
    }
    if (tooLong) {
      options.push({ label: t('db.cell.valueTooLong'), key: 'hint-long', disabled: true })
    }
    // JSON/BLOB/未知类型只提供 NULL 语义的操作
    if (fam === 'json' || fam === 'binary' || fam === 'other') {
      options.push({ label: t('db.cell.filterNull'), key: 'filter-null' })
      options.push({ label: t('db.cell.filterNotNull'), key: 'filter-not-null' })
    }
  }
  if (cellMenu.hasPending) {
    options.push({ label: t('db.cell.pendingHint'), key: 'hint-pending', disabled: true })
  }
  options.push({ type: 'divider', key: 'divider-2' })
  options.push({ label: t('db.cell.edit'), key: 'edit' })
  options.push({ label: t('db.cell.copyValue'), key: 'copy-value' })
  return options
})

function focusDraftRow(index: number) {
  nextTick(() => {
    const row = document.querySelectorAll('.grid-filter-row')[index]
    row?.scrollIntoView({ block: 'nearest' })
    // 优先值输入；NULL 操作符没有值输入框时回退到该行第一个 input
    const valueInput = row?.querySelector<HTMLInputElement>('.grid-filter-value input')
    const fallback = row?.querySelector<HTMLInputElement>('input')
    ;(valueInput ?? fallback)?.focus()
  })
}

// 快捷筛选只追加到草稿，不自动应用
function appendQuickCondition(tab: WorkTab, column: string, op: FilterOp, value: string) {
  if (tab.filterMode === 'raw') return
  const base = tab.filterPanelOpen
    ? cloneConditions(tab.conditionsDraft)
    : cloneConditions(tab.conditions)
  const cond: FilterCondition = { column, op, value }
  const existing = base.findIndex((c) => sameCondition(c, cond))
  tab.filterMode = 'structured'
  tab.columnsPanelOpen = false
  if (existing >= 0) {
    tab.conditionsDraft = base
    tab.filterPanelOpen = true
    focusDraftRow(existing)
    return
  }
  if (base.length >= 5) {
    tab.conditionsDraft = base
    tab.filterPanelOpen = true
    message.warning(t('db.cell.maxConditions'))
    return
  }
  tab.conditionsDraft = [...base, cond]
  tab.filterPanelOpen = true
  focusDraftRow(base.length)
}

function addFilterForColumn(tab: WorkTab, column: string) {
  if (tab.filterMode === 'raw') {
    message.warning(t('db.cell.rawDisabledShort'))
    return
  }
  const base = tab.filterPanelOpen
    ? cloneConditions(tab.conditionsDraft)
    : cloneConditions(tab.conditions)
  if (base.length >= 5) {
    tab.conditionsDraft = base
    tab.filterPanelOpen = true
    message.warning(t('db.cell.maxConditions'))
    return
  }
  const col = tab.columns.find((c) => c.field === column)
  tab.filterMode = 'structured'
  tab.columnsPanelOpen = false
  tab.conditionsDraft = [
    ...base,
    { column, op: defaultOperatorForType(col?.type ?? ''), value: '' },
  ]
  tab.filterPanelOpen = true
  focusDraftRow(base.length)
}

async function setSort(tab: WorkTab, column: string, dir: 1 | -1) {
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    tab.sortCol = column
    tab.sortDir = dir
  })
}

function hideColumn(tab: WorkTab, name: string) {
  if (visibleColumnsOf(tab).length <= 1) {
    message.warning(t('db.columns.keepOne', { name }))
    return
  }
  // 活动编辑属于该列时先提交，再隐藏
  if (tab.editing) {
    const editingName = tab.result?.columns?.[tab.editing.col]
    if (editingName === name) commitEdit(tab)
  }
  if (tab.hiddenColumns.includes(name)) return
  tab.hiddenColumns = [...tab.hiddenColumns, name]
  void saveGridPref(tab)
}

function onCellMenuSelect(key: string) {
  cellMenu.show = false
  const tab = cellMenuTab.value
  if (!tab) return
  const dbValue = cellMenu.databaseValue
  switch (key) {
    case 'filter-eq':
      if (dbValue !== null) appendQuickCondition(tab, cellMenu.column, 'eq', dbValue)
      break
    case 'filter-contains':
      if (dbValue !== null) appendQuickCondition(tab, cellMenu.column, 'contains', dbValue)
      break
    case 'filter-ne':
      if (dbValue !== null) appendQuickCondition(tab, cellMenu.column, 'ne', dbValue)
      break
    case 'filter-null':
      appendQuickCondition(tab, cellMenu.column, 'is_null', '')
      break
    case 'filter-not-null':
      appendQuickCondition(tab, cellMenu.column, 'is_not_null', '')
      break
    case 'edit':
      startEdit(tab, cellMenu.row, cellMenu.col)
      break
    case 'copy-value': {
      // 复制用户看到的值（若该单元格有 pending，则复制 pending 值）
      const p = tab.pending.find((x) => x.row === cellMenu.row && x.col === cellMenu.col)
      copyText(p ? p.value : dbValue === null ? 'NULL' : dbValue)
      break
    }
    case 'copy-column':
      copyText(cellMenu.column, t('db.cell.copiedColumn'))
      break
    case 'sort-asc':
      void setSort(tab, cellMenu.column, 1)
      break
    case 'sort-desc':
      void setSort(tab, cellMenu.column, -1)
      break
    case 'hide-column':
      hideColumn(tab, cellMenu.column)
      break
    case 'filter-column':
      addFilterForColumn(tab, cellMenu.column)
      break
  }
}



// ---------- 数据单元格编辑 ----------

// 编辑态绑定的输入值
const cellInput = ref('')

function startEdit(tab: WorkTab, row: number, col: number) {
  // 写入进行中不允许新开编辑，否则成功后清空 pending 会吞掉新修改
  if (tab.loading) return
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

// ---------- 待提交编辑保护 ----------
//
// 原则：任何会清空 pending 的动作（重查/切表/切连接/关标签/切视图/DDL）都必须先确认。
// loadTableData 只接受显式 intent，普通调用在 pending > 0 时直接拒绝，
// 防止未来新增入口绕过保护而静默丢数据。

type LoadIntent = 'plain' | 'discard' | 'applied'
type PendingDecision = 'proceed' | 'cancel' | 'discard' | 'apply'

// 当前连接的数据标签（全局唯一）
function currentDataTab(): WorkTab | undefined {
  return tabs.value.find((x) => x.kind === 'data' && x.connId === activeId.value)
}

// 指定连接/库/表的数据标签（DDL 保护用）
function dataTabFor(connId: string, database: string, table: string): WorkTab | undefined {
  return tabs.value.find(
    (x) => x.kind === 'data' && x.connId === connId && x.database === database && x.table === table,
  )
}

function dataTabsWithPending(): WorkTab[] {
  return tabs.value.filter((x) => x.kind === 'data' && x.pending.length > 0)
}

// 当前连接全部数据标签的未提交修改总数
function pendingCount(): number {
  return countPendingEdits(tabs.value.filter((x) => x.kind === 'data'))
}

// 同一时间只允许一个保护弹窗：弹窗期间的重复点击直接拒绝，
// 避免叠出多个确认框或“第二个动作在用户确认前就执行”。
let pendingGuardOpen = false

interface PendingStats {
  edits: number
  rows: number
  hidden: number
  tabsWithPending: number
}

interface PendingGuardOptions {
  switchConnection?: boolean
  tabsToClose?: number
}

function pendingStats(targets: WorkTab[]): PendingStats {
  let edits = 0
  let hidden = 0
  let tabsWithPending = 0
  const rows = new Set<string>()
  for (const tab of targets) {
    if (tab.pending.length === 0) continue
    tabsWithPending++
    edits += tab.pending.length
    const hiddenSet = new Set(tab.hiddenColumns)
    const columns = tab.result?.columns ?? []
    for (const p of tab.pending) {
      rows.add(`${tab.id}:${p.row}`)
      if (hiddenSet.has(columns[p.col])) hidden++
    }
  }
  return { edits, rows: rows.size, hidden, tabsWithPending }
}

// 三选一确认框：取消 / 丢弃并继续 / 应用并继续。
// 切换连接时使用聚合文案与“丢弃并切换 / 全部应用并切换”。
// Naive UI 的 positive/negative 只有两个按钮，因此用 dialog.create 自定义 action。
function askPendingDecision(
  stats: PendingStats,
  opts: PendingGuardOptions = {},
): Promise<PendingDecision> {
  if (pendingGuardOpen) return Promise.resolve('cancel')
  pendingGuardOpen = true
  const switchMode = !!opts.switchConnection
  return new Promise((resolve) => {
    let done = false
    let dlg: { destroy: () => void } | null = null
    const finish = (decision: PendingDecision) => {
      if (done) return
      done = true
      pendingGuardOpen = false
      dlg?.destroy()
      resolve(decision)
    }
    const button = (text: string, kind: 'default' | 'primary', onClick: () => void) =>
      h(
        NButton,
        { size: 'small', type: kind === 'primary' ? 'primary' : 'default', onClick },
        { default: () => text },
      )
    const summary = switchMode
      ? t('db.pendingGuard.switchSummary', {
          tabs: opts.tabsToClose ?? 0,
          pendingTabs: stats.tabsWithPending,
          edits: stats.edits,
        })
      : t('db.pendingGuard.summary', { edits: stats.edits, rows: stats.rows })
    dlg = dialog.create({
      type: 'warning',
      title: t('db.pendingGuard.title'),
      content: () =>
        h('div', [
          h('div', summary),
          stats.hidden > 0
            ? h(
                'div',
                { style: 'color: var(--warning); margin-top: 6px' },
                t('db.pendingGuard.hidden', { hidden: stats.hidden }),
              )
            : null,
        ]),
      action: () =>
        h('div', { style: 'display:flex;justify-content:flex-end;gap:8px;margin-top:16px' }, [
          button(t('common.cancel'), 'default', () => finish('cancel')),
          button(
            switchMode ? t('db.pendingGuard.discardSwitch') : t('db.pendingGuard.discard'),
            'default',
            () => finish('discard'),
          ),
          button(
            switchMode ? t('db.pendingGuard.applySwitch') : t('db.pendingGuard.apply'),
            'primary',
            () => finish('apply'),
          ),
        ]),
      closable: false,
      maskClosable: false,
      closeOnEsc: false,
      onClose: () => finish('cancel'),
    })
  })
}

async function decidePending(
  targets: WorkTab[],
  opts: PendingGuardOptions = {},
): Promise<PendingDecision> {
  const stats = pendingStats(targets)
  if (stats.edits === 0) return 'proceed'
  return askPendingDecision(stats, opts)
}

// 丢弃 pending：只清界面状态，不碰数据库
function discardPendingTabs(targets: WorkTab[]) {
  for (const tab of targets) {
    tab.pending = []
    tab.editing = null
  }
}

// 把数据标签的修改写入数据库（不重查）。成功返回 true 并清空 pending；
// 失败时后端整批回滚，pending 保留，用户可修正后重试。
async function persistEdits(tab: WorkTab): Promise<boolean> {
  if (!tab.result || tab.pending.length === 0) return true
  // 写入进行中禁止再次应用：避免同一批 pending 被重复提交
  if (tab.loading) return false
  const columns = tab.result.columns
  if (tab.pkColumns.some((name) => !columns.includes(name))) {
    message.error(t('db.noPrimaryKey'))
    return false
  }
  const updates: models.db.RowUpdate[] = buildRowUpdates(
    tab.pending,
    sortedRows(tab),
    columns,
    tab.pkColumns,
  )
  if (updates.length === 0) return true

  tab.loading = true
  try {
    const applied = await DBUpdateRows(tab.connId, tab.database, tab.table, updates)
    tab.pending = []
    tab.editing = null
    message.success(t('db.editsApplied', { n: applied }))
    return true
  } catch (e) {
    message.error(serviceErrorText(e))
    return false
  } finally {
    tab.loading = false
  }
}

async function persistPendingTabs(targets: WorkTab[]): Promise<boolean> {
  let applied = 0
  let failed = 0
  for (const tab of targets) {
    if (tab.pending.length === 0) continue
    if (await persistEdits(tab)) applied++
    else failed++
  }
  if (failed > 0) {
    // 跨标签按标签事务：已成功的已落库并清空 pending，失败标签保留 pending
    message.error(t('db.pendingGuard.partial', { applied, failed }))
    return false
  }
  return true
}

// 统一守卫：无 pending 直接放行；否则按用户选择取消 / 丢弃 / 先应用。
// ok=false 表示原操作必须中止（用户取消，或“应用并继续”执行失败）。
interface GuardOutcome {
  ok: boolean
  applied: boolean
}

async function guardPendingTabs(
  targets: WorkTab[],
  opts: PendingGuardOptions = {},
): Promise<GuardOutcome> {
  const decision = await decidePending(targets, opts)
  if (decision === 'cancel') return { ok: false, applied: false }
  if (decision === 'apply') {
    const ok = await persistPendingTabs(targets)
    return { ok, applied: ok }
  }
  if (decision === 'discard') discardPendingTabs(targets)
  return { ok: true, applied: false }
}

// 保护通过后按目标状态重查；若刚刚应用过修改而重查失败，明确提示用户
async function reloadAfterGuard(
  tab: WorkTab,
  resetPage: boolean,
  guard: GuardOutcome,
  mutate?: () => void,
): Promise<void> {
  mutate?.()
  const ok = await loadTableData(tab, resetPage, 'discard')
  if (guard.applied && !ok) message.warning(t('db.editsAppliedReloadFailed'))
}

// 数据标签重查入口（刷新/筛选/页大小）：先确认，再加载
async function reloadTableData(tab: WorkTab, resetPage = false): Promise<void> {
  if (tab.loading) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, resetPage, guard)
}

// 应用按钮：先写库，成功后刷新
async function applyEdits(tab: WorkTab) {
  if (!(await persistEdits(tab))) return
  // 已成功提交，允许重查清空 pending
  const ok = await loadTableData(tab, false, 'applied')
  if (!ok) message.warning(t('db.editsAppliedReloadFailed'))
}

async function sortTableByColumn(tab: WorkTab, colName: string) {
  if (tab.loading) return
  // 先确认再改排序：取消时排序状态与页码都不变
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, true, guard, () => {
    if (tab.sortCol === colName) tab.sortDir = tab.sortDir === 1 ? -1 : 1
    else {
      tab.sortCol = colName
      tab.sortDir = 1
    }
  })
}

function hasNextPage(tab: WorkTab): boolean {
  return (tab.result?.rows?.length ?? 0) >= tab.pageSize
}

async function nextPage(tab: WorkTab) {
  if (tab.loading) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, false, guard, () => {
    tab.page++
  })
}

async function prevPage(tab: WorkTab) {
  if (tab.page === 0 || tab.loading) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, false, guard, () => {
    tab.page--
  })
}

// 页大小：先确认再改值；取消时下拉框仍显示原值（:value 绑定）
async function changePageSize(tab: WorkTab, size: number | null) {
  if (size == null || size === tab.pageSize || tab.loading) return
  const guard = await guardPendingTabs([tab])
  if (!guard.ok) return
  await reloadAfterGuard(tab, false, guard, () => {
    tab.pageSize = size
    tab.page = 0
  })
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
  tab.lastRunSql = text
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
    tab.error = serviceErrorText(e)
    tab.result = null
    message.error(serviceErrorText(e))
  } finally {
    tab.loading = false
  }
}

// ---------- AI 助手 ----------

const aiSettingsOpen = ref(false)
const aiPanelRef = ref<InstanceType<typeof AIPanel> | null>(null)
const sqlEditorEl = ref<HTMLTextAreaElement | null>(null)
const uiLocale = computed(() => String(locale.value))

function toggleAI(tab: WorkTab) {
  tab.aiOpen = !tab.aiOpen
}

// 插入编辑器：用 setRangeText（保留 undo 栈），再派发 input 事件同步 v-model。
// 不直接改 tab.sql，避免丢失光标位置与撤销历史。
function insertAIText(tab: WorkTab, text: string) {
  const clean = text.trim()
  if (!clean) return
  const el = sqlEditorEl.value
  if (!el) {
    tab.sql = clean
    return
  }
  el.focus()
  const start = el.selectionStart ?? el.value.length
  const end = el.selectionEnd ?? start
  // 若前后不是空白/换行，补换行，避免和已有 SQL 粘成一条无效语句
  const before = el.value.slice(0, start)
  const after = el.value.slice(end)
  const prefix = before.length > 0 && !/\s$/.test(before) ? '\n' : ''
  const suffix = after.length > 0 && !/^\s/.test(after) ? '\n' : ''
  el.setRangeText(prefix + clean + suffix, start, end, 'end')
  el.dispatchEvent(new Event('input', { bubbles: true }))
  message.success(t('ai.inserted'))
}

function onAISettingsSaved() {
  void aiPanelRef.value?.reloadConfig()
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
  // \u6570\u636E\u6807\u7B7E\u53EA\u5BFC\u51FA\u5F53\u524D\u9875\u7684\u53EF\u89C1\u5217\uFF08\u4E0E\u5C4F\u5E55\u4E00\u81F4\uFF09\uFF1B\u67E5\u8BE2\u6807\u7B7E\u4FDD\u6301\u5168\u90E8\u5217
  const columns = tab.result.columns
  const refs =
    tab.kind === 'data'
      ? visibleColumnsOf(tab)
      : columns.map((name, idx) => ({ name, idx }))
  const lines = [refs.map((r) => esc(columns[r.idx])).join(',')]
  for (const row of sortedRows(tab)) lines.push(refs.map((r) => esc(row[r.idx])).join(','))
  const blob = new Blob([`\uFEFF${lines.join('\n')}`], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${tab.table || 'query'}_${Date.now()}.csv`
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
  if (tab.kind === 'data') {
    message.success(
      t('db.csvExported', { cols: refs.length, hidden: Math.max(0, columns.length - refs.length) }),
    )
  }
}

// 查看 DDL 前保护同表数据标签的 pending：
// DDL 可能改变列定义，旧的行/列快照之后无法安全应用。
async function loadDDL(tab: WorkTab): Promise<boolean> {
  const dataTab = dataTabFor(tab.connId, tab.database, tab.table)
  if (!(await guardPendingTabs(dataTab ? [dataTab] : [])).ok) return false
  try {
    tab.ddl = await DBShowCreateTable(tab.connId, tab.database, tab.table)
    tab.showDDL = true
    return true
  } catch (e) {
    message.error(serviceErrorText(e))
    return false
  }
}

// 工具栏 / 视图切换按钮：只有 DDL 加载成功（未被用户取消）才切到 DDL 视图
async function showDDL(tab: WorkTab) {
  if (!(await loadDDL(tab))) return
  tab.structView = 'ddl'
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
async function runDDL(tab: WorkTab, sql: string) {
  // 执行 DDL 前先处理同表数据标签的 pending：列被改/删后旧修改无法应用
  const dataTab = dataTabFor(tab.connId, tab.database, tab.table)
  if (!(await guardPendingTabs(dataTab ? [dataTab] : [])).ok) return
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
        // 结构变更后列元数据过期：切到数据标签时强制重查并重新求交隐藏列
        if (dataTab) {
          dataTab.columnsStale = true
          dataTab.columnsPrefLoaded = false
        }
      } catch (e) {
        message.error(serviceErrorText(e))
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
    message.error(serviceErrorText(e))
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
    message.error(serviceErrorText(e))
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
    message.error(serviceErrorText(e))
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
    message.error(serviceErrorText(e))
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
    message.error(serviceErrorText(e))
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

// watch(activeId) 只作为防御：所有用户触发的切换必须先经过 selectConn() 的确认。
// 若未来新增了未保护的 activeId 赋值点，这里拒绝清空并回退，避免静默丢 pending。
let revertingActiveId = false
watch(activeId, (next, prev) => {
  if (revertingActiveId) {
    revertingActiveId = false
    return
  }
  if (shouldBlockUnguardedSwitch(prev ?? '', next, pendingCount())) {
    console.error('[db] blocked unguarded connection switch with pending edits')
    message.error(t('db.pendingSwitchBlocked'))
    revertingActiveId = true
    activeId.value = prev
    return
  }
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
    else {
      openQuery()
      // 仅浏览器预览调试用：?ai=1 展开 AI 侧栏
      if (typeof window !== 'undefined' && new URLSearchParams(location.search).get('ai') === '1') {
        const qtab = tabs.value.find((x) => x.kind === 'query')
        if (qtab) qtab.aiOpen = true
      }
      // 仅浏览器预览调试用：?autodata=1 自动打开第一张表的数据标签
      if (typeof window !== 'undefined' && new URLSearchParams(location.search).get('autodata') === '1') {
        void (async () => {
          await toggleConn()
          const db = databases.value[0]
          if (!db) return
          await toggleDb(db)
          const tbl = tablesByDb[db]?.[0]
          if (tbl) await openTableData(db, tbl)
        })()
      }
    }
  }
})

// 注册/注销视图切换守卫：切到终端视图会卸载本组件，必须在卸载前确认
onMounted(() => {
  window.addEventListener('keydown', onWindowKeydown)
  if (!viewGuard) return
  viewGuard.fn = async () => (await guardPendingTabs(dataTabsWithPending())).ok
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
  if (viewGuard) viewGuard.fn = null
})

// 结构标签执行 DDL 后，同表数据标签标记 columnsStale；首次激活时强制重查
watch(activeTabId, () => {
  const tab = activeTab.value
  if (tab && tab.kind === 'data' && tab.columnsStale && !tab.loading) {
    void reloadTableData(tab)
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
          <div class="conn-item" :class="{ active: c.id === activeId }" @click="selectConn(c)">
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
          <div class="data-tab" @keydown="onDataKeydown">
          <div class="toolbar">
            <n-popover
              v-model:show="activeTab.filterPanelOpen"
              trigger="manual"
              placement="bottom-start"
              :show-arrow="false"
              raw
              @clickoutside="closeFilterPanel(activeTab)"
            >
              <template #trigger>
                <n-button
                  size="small"
                  class="grid-filter-trigger"
                  :class="{ active: activeTab.filterMode === 'raw' || filterCount(activeTab) > 0 }"
                  :disabled="!!activeTab.metadataError || !(activeTab.result?.columns?.length)"
                  :title="filterButtonTitle(activeTab)"
                  @click="openFilterPanel(activeTab)"
                >
                  {{ activeTab.filterMode === 'raw'
                    ? t('db.advancedWhere')
                    : filterCount(activeTab)
                      ? t('db.filterButtonCount', { n: filterCount(activeTab) })
                      : t('db.filterButton') }}
                </n-button>
              </template>
              <GridFilterPanel
                v-if="activeTab.filterPanelOpen"
                :columns="columnsForPanel(activeTab)"
                :draft="activeTab.conditionsDraft"
                :hidden="activeTab.hiddenColumns"
                :preview-sql="activeTab.filterPreviewSql"
                :preview-loading="activeTab.filterPreviewLoading"
                :preview-error="activeTab.filterPreviewError"
                :loading="activeTab.loading"
                @update:draft="onDraftChange(activeTab, $event)"
                @apply="applyStructured(activeTab)"
                @clear="clearFilter(activeTab)"
                @cancel="closeFilterPanel(activeTab)"
                @switch-raw="switchToRaw(activeTab)"
                @preview="scheduleStructuredPreview(activeTab)"
                @copy-sql="copySQL"
                @open-sql="openSQLInQueryTab"
              />
            </n-popover>
            <n-button size="small" :loading="activeTab.loading" @click="reloadTableData(activeTab, true)">
              {{ t('db.refresh') }}
            </n-button>
            <n-popover
              v-model:show="activeTab.columnsPanelOpen"
              trigger="manual"
              placement="bottom-start"
              :show-arrow="false"
              raw
              @clickoutside="activeTab.columnsPanelOpen = false"
            >
              <template #trigger>
                <n-button
                  size="small"
                  class="grid-columns-trigger"
                  :class="{ warn: activeTab.hiddenColumns.length > 0 }"
                  :disabled="!(activeTab.result?.columns?.length)"
                  :title="columnsButtonTitle(activeTab)"
                  @click="openColumnsPanel(activeTab)"
                >
                  {{ t('db.colButton', {
                    visible: visibleColumnsOf(activeTab).length,
                    total: activeTab.result?.columns?.length ?? 0,
                  }) }}
                </n-button>
              </template>
              <GridColumnsPanel
                v-if="activeTab.columnsPanelOpen"
                :columns="columnsForPanel(activeTab)"
                :hidden="activeTab.hiddenColumns"
                @apply="applyColumns(activeTab, $event)"
                @close="activeTab.columnsPanelOpen = false"
              />
            </n-popover>
            <span
              v-if="activeTab.sortCol && activeTab.hiddenColumns.includes(activeTab.sortCol)"
              class="grid-sort-hidden"
            >
              <span
                class="grid-sort-hidden-label"
                :title="t('db.sortHidden', {
                  column: activeTab.sortCol,
                  dir: activeTab.sortDir === 1 ? '▲' : '▼',
                })"
                @click="toggleSortDirection(activeTab)"
              >
                {{ t('db.sortHidden', {
                  column: activeTab.sortCol,
                  dir: activeTab.sortDir === 1 ? '▲' : '▼',
                }) }}
              </span>
              <span class="grid-sort-hidden-x" :title="t('db.clearSort')" @click="clearSort(activeTab)">×</span>
            </span>
            <n-button size="small" @click="gotoStructure(activeTab)">{{ t('db.structureShort') }}</n-button>
            <span class="spacer" />
            <n-button size="small" :disabled="activeTab.page === 0" @click="prevPage(activeTab)">‹</n-button>
            <span class="page-num">{{ t('db.pageN', { n: activeTab.page + 1 }) }}</span>
            <n-button size="small" :disabled="!hasNextPage(activeTab)" @click="nextPage(activeTab)">›</n-button>
            <n-select
              :value="activeTab.pageSize"
              size="small"
              :options="[100, 500, 1000, 5000].map((n) => ({ label: String(n), value: n }))"
              style="width: 90px"
              @update:value="changePageSize(activeTab, $event)"
            />
            <n-button
              size="small"
              :disabled="!(activeTab.result?.columns?.length)"
              :title="t('db.csvTitle')"
              @click="exportCSV(activeTab)"
            >
              {{ t('db.csvVisible') }}
            </n-button>
            <span class="edit-actions" :class="{ active: activeTab.pending.length > 0 }">
              <span
                v-if="hiddenPendingEditCount(activeTab)"
                class="grid-hidden-pending"
                :title="hiddenPendingColumns(activeTab).join(', ')"
              >
                ⚠ {{ t('db.hiddenPending', { n: hiddenPendingEditCount(activeTab) }) }}
              </span>
              <button
                class="cell-edit-btn apply"
                :disabled="activeTab.pending.length === 0 || activeTab.loading"
                :title="t('db.applyEdits', { n: activeTab.pending.length })"
                @click="applyEdits(activeTab)"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </button>
              <button
                class="cell-edit-btn discard"
                :disabled="activeTab.pending.length === 0 || activeTab.loading"
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

          <GridFilterChips
            v-if="activeTab.filterMode === 'structured' && activeTab.conditions.length > 0"
            :conditions="activeTab.conditions"
            :loading="activeTab.loading"
            @remove="removeCondition(activeTab, $event)"
            @clear="clearFilter(activeTab)"
            @add="openFilterPanel(activeTab)"
            @edit="editCondition(activeTab, $event)"
          />

          <div v-if="activeTab.filterMode === 'raw'" class="grid-raw-wrap">
            <div class="grid-raw-row">
              <span class="grid-raw-mode">{{ t('db.advancedWhere') }}</span>
              <textarea
                ref="rawInputEl"
                class="grid-raw-input"
                :value="activeTab.rawWhereDraft"
                :placeholder="t('db.rawPlaceholder')"
                spellcheck="false"
                @input="onRawInput(activeTab, ($event.target as HTMLTextAreaElement).value)"
                @keydown.ctrl.enter.prevent="applyRaw(activeTab)"
                @keydown.meta.enter.prevent="applyRaw(activeTab)"
              />
              <span v-if="activeTab.filterDirty" class="grid-raw-dirty">{{ t('db.rawDirty') }}</span>
              <n-button size="small" :disabled="!activeTab.filterDirty" @click="applyRaw(activeTab)">
                {{ t('db.rawApply') }}
              </n-button>
              <n-button size="small" quaternary @click="showRawPreview(activeTab)">{{ t('db.rawPreview') }}</n-button>
              <n-button size="small" quaternary @click="switchToStructured(activeTab)">
                {{ t('db.backToStructured') }}
              </n-button>
            </div>
            <div class="grid-raw-hint">{{ t('db.rawHint') }}</div>
            <div v-if="activeTab.filterError" class="grid-raw-error">{{ activeTab.filterError }}</div>
          </div>

          <div class="result">
            <div v-if="activeTab.loading" class="grid-progress"><div class="grid-progress-bar" /></div>
            <div v-if="activeTab.error" class="query-error">
              {{ activeTab.error }}
              <span v-if="activeTab.result" class="hint">· {{ t('db.showingStaleResult') }}</span>
            </div>
            <template
              v-if="activeTab.result && (activeTab.result.columns?.length ?? 0) > 0 && (activeTab.result.rows?.length ?? 0) > 0"
            >
              <div class="result-meta">
                <span v-if="activeTab.loading" class="grid-loading-label">{{ t('db.filtering') }}</span>
                <span v-else>
                  {{ t('db.pageRows', { n: activeTab.result.rows?.length ?? 0 }) }} ·
                  {{ activeTab.result.durationMs }}ms
                </span>
                <span v-if="activeTab.result.truncated" class="warn">· {{ t('db.truncated') }}</span>
                <span class="spacer" />
                <n-button size="tiny" quaternary :disabled="!activeTab.lastTableSQL" @click="showExecutedSQL(activeTab)">
                  {{ t('db.executedSql') }}
                </n-button>
                <span class="hint">{{ t('db.gridHint') }}</span>
              </div>
              <div class="grid-wrap" :class="{ 'grid-stale': activeTab.loading }">
                <table class="grid">
                  <thead>
                    <tr>
                      <th class="rownum">#</th>
                      <th
                        v-for="ref in visibleColumnsOf(activeTab)"
                        :key="ref.name"
                        class="sortable"
                        :title="t('db.sortServerSide')"
                        @click="sortTableByColumn(activeTab, ref.name)"
                        @contextmenu.prevent="openHeaderMenu($event, activeTab, ref.name)"
                      >
                        {{ ref.name }}
                        <span v-if="activeTab.sortCol === ref.name" class="sort">{{ activeTab.sortDir === 1 ? '▲' : '▼' }}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(row, ri) in sortedRows(activeTab)" :key="ri">
                      <td class="rownum">{{ ri + 1 + activeTab.page * activeTab.pageSize }}</td>
                      <td
                        v-for="ref in visibleColumnsOf(activeTab)"
                        :key="ref.name"
                        :class="{
                          null: row[ref.idx] === null,
                          edited: pendingValue(activeTab, ri, ref.idx) !== null,
                          selected: activeTab.selectedRow === ri && activeTab.selectedCol === ref.idx,
                        }"
                        @click="activeTab.editing?.row === ri && activeTab.editing?.col === ref.idx ? undefined : startEdit(activeTab, ri, ref.idx)"
                        @contextmenu.prevent="openCellMenu($event, activeTab, ri, ref.idx)"
                      >
                        <input
                          v-if="activeTab.editing?.row === ri && activeTab.editing?.col === ref.idx"
                          v-model="cellInput"
                          class="cell-input"
                          autofocus
                          @keyup.enter="commitEdit(activeTab)"
                          @keyup.esc="cancelEdit(activeTab)"
                          @blur="commitEdit(activeTab)"
                          @click.stop
                        />
                        <template v-else>
                          {{ pendingValue(activeTab, ri, ref.idx) ?? (row[ref.idx] === null ? 'NULL' : row[ref.idx]) }}
                        </template>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
            <div v-else-if="!activeTab.error" class="result-empty">
              <template v-if="activeTab.loading">
                <div class="empty-glyph">◌</div>
                <div>{{ t('db.loadingData') }}</div>
              </template>
              <template v-else-if="(activeTab.result?.rows?.length ?? 0) === 0 && activeTab.page > 0">
                <div class="empty-glyph">▤</div>
                <div>{{ t('db.empty.page') }}</div>
                <div class="empty-actions">
                  <n-button size="small" @click="prevPage(activeTab)">{{ t('db.empty.prevPage') }}</n-button>
                  <n-button size="small" type="primary" secondary @click="gotoFirstPage(activeTab)">
                    {{ t('db.empty.firstPage') }}
                  </n-button>
                </div>
              </template>
              <template
                v-else-if="(activeTab.result?.rows?.length ?? 0) === 0 && hasAppliedPredicate(activeTab)"
              >
                <div class="empty-glyph">∅</div>
                <div>{{ t('db.empty.filtered') }}</div>
                <div class="empty-actions">
                  <n-button size="small" @click="openFilterPanel(activeTab)">{{ t('db.empty.adjustFilter') }}</n-button>
                  <n-button size="small" type="primary" secondary @click="clearFilter(activeTab)">
                    {{ t('db.empty.clearFilter') }}
                  </n-button>
                </div>
              </template>
              <template v-else>
                <div class="empty-glyph">▤</div>
                <div>{{ t('db.empty.table') }}</div>
                <div class="empty-actions">
                  <n-button size="small" @click="reloadTableData(activeTab, true)">{{ t('db.refresh') }}</n-button>
                </div>
              </template>
            </div>
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
            <n-button
              size="small"
              :type="activeTab.aiOpen ? 'primary' : 'default'"
              secondary
              @click="toggleAI(activeTab)"
            >
              {{ t('ai.open') }}
            </n-button>
            <n-button size="small" quaternary :title="t('ai.settings')" @click="aiSettingsOpen = true">
              ⚙
            </n-button>
          </div>

          <div class="query-body" @keydown.ctrl.k.prevent="toggleAI(activeTab)">
            <div class="query-main">
              <textarea
                ref="sqlEditorEl"
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
            </div>

            <AIPanel
              v-if="activeTab.aiOpen"
              ref="aiPanelRef"
              :conn-id="activeTab.connId"
              :default-database="activeTab.database || (active?.dbName ?? '')"
              :sql="activeTab.sql"
              :last-run-sql="activeTab.lastRunSql"
              :last-error="activeTab.error"
              :locale="uiLocale"
              @close="activeTab.aiOpen = false"
              @insert="insertAIText(activeTab, $event)"
              @configure="aiSettingsOpen = true"
            />
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
                @click="showDDL(activeTab)"
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
    <AISettingsModal v-model:show="aiSettingsOpen" @saved="onAISettingsSaved" />
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

    <n-dropdown
      placement="bottom-start"
      trigger="manual"
      :show="cellMenu.show"
      :x="cellMenu.x"
      :y="cellMenu.y"
      :options="cellMenuOptions"
      @select="onCellMenuSelect"
      @clickoutside="cellMenu.show = false"
    />

    <n-modal
      v-model:show="sqlViewer.show"
      preset="card"
      :title="sqlViewer.title"
      style="width: min(760px, calc(100vw - 32px))"
    >
      <pre class="grid-sql-viewer">{{ sqlViewer.sql }}</pre>
      <div class="grid-sql-viewer-actions">
        <span class="spacer" />
        <n-button size="small" @click="copySQL(sqlViewer.sql)">{{ t('db.copySql') }}</n-button>
        <n-button size="small" type="primary" @click="openSQLInQueryTab(sqlViewer.sql)">
          {{ t('db.openInQuery') }}
        </n-button>
      </div>
    </n-modal>
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
  flex: 0 1 420px;
  min-width: 140px;
  max-width: 520px;
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
  min-width: 0;
}.toolbar > * {
  flex-shrink: 0;
}
.grid-columns-trigger.warn {
  color: var(--warning);
  border-color: var(--warning);
}
.grid-sort-hidden {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text-secondary);
  padding: 0 6px;
  border: 1px solid var(--border-subtle);
  border-radius: 10px;
  white-space: nowrap;
}
.grid-sort-hidden-label {
  cursor: pointer;
}
.grid-sort-hidden-label:hover {
  color: var(--accent);
}
.grid-sort-hidden-x {
  cursor: pointer;
  color: var(--text-muted);
}
.grid-sort-hidden-x:hover {
  color: var(--danger);
}
.grid-hidden-pending {
  color: var(--warning);
  font-size: 11px;
  white-space: nowrap;
}
.data-tab {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.grid-filter-trigger.active {
  color: var(--accent);
  border-color: var(--accent);
}
.grid-raw-wrap {
  flex-shrink: 0;
  padding: 6px 12px 8px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border-subtle);
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.grid-raw-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.grid-raw-mode {
  color: var(--warning);
  font-size: 12px;
  line-height: 24px;
  white-space: nowrap;
}
.grid-raw-input {
  flex: 1;
  min-width: 120px;
  min-height: 48px;
  max-height: 120px;
  padding: 5px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-app);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.5;
  box-sizing: border-box;
  resize: vertical;
}
.grid-raw-input:focus {
  outline: none;
  border-color: var(--accent);
}
.grid-raw-dirty {
  color: var(--warning);
  font-size: 11px;
  line-height: 24px;
  white-space: nowrap;
}
.grid-raw-hint {
  color: var(--text-muted);
  font-size: 11px;
}
.grid-raw-error {
  color: var(--danger);
  font-size: 11px;
  line-height: 1.5;
}
.grid-progress {
  height: 2px;
  background: var(--bg-hover);
  overflow: hidden;
  flex-shrink: 0;
}
.grid-progress-bar {
  height: 100%;
  width: 30%;
  background: var(--accent);
  animation: grid-progress-slide 1.1s ease-in-out infinite;
}
@keyframes grid-progress-slide {
  0% {
    transform: translateX(-110%);
  }
  100% {
    transform: translateX(420%);
  }
}
.grid-stale {
  opacity: 0.55;
  pointer-events: none;
}
.grid-loading-label {
  color: var(--accent);
}
.grid-sql-viewer {
  margin: 0;
  padding: 10px;
  max-height: 55vh;
  overflow: auto;
  background: var(--bg-app);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}
.grid-sql-viewer-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
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
/* 查询标签：编辑器/结果 + AI 侧栏左右分栏 */
.query-body {
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}
.query-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.sql-editor {
  width: 100%;
  height: 130px;
  box-sizing: border-box;
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
  gap: 6px;
  margin-left: 4px;
  padding-left: 10px;
  border-left: 1px solid var(--border-subtle);
  flex: 0 0 auto;
  min-width: max-content;
}
.cell-edit-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 28px;
  flex: 0 0 auto;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elevated);
  cursor: pointer;
  color: var(--text-muted);
  transition: all 0.12s ease;
}
.cell-edit-btn.apply {
  color: #34d399;
  border-color: rgba(52, 211, 153, 0.35);
}
.cell-edit-btn.discard {
  color: #f87171;
  border-color: rgba(248, 113, 113, 0.35);
}
.cell-edit-btn.apply:not(:disabled):hover {
  background: rgba(52, 211, 153, 0.18);
  border-color: rgba(52, 211, 153, 0.7);
  box-shadow: 0 0 0 2px rgba(52, 211, 153, 0.15);
}
.cell-edit-btn.discard:not(:disabled):hover {
  background: rgba(248, 113, 113, 0.16);
  border-color: rgba(248, 113, 113, 0.7);
  box-shadow: 0 0 0 2px rgba(248, 113, 113, 0.12);
}
.cell-edit-btn:disabled {
  cursor: default;
  opacity: 0.3;
  background: transparent;
}
.edit-actions.active .cell-edit-btn {
  box-shadow: 0 0 0 1px currentColor inset;
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
  position: absolute;
  inset: 1px;
  width: auto;
  box-sizing: border-box;
  background: var(--bg-app);
  border: 1px solid var(--accent);
  border-radius: 3px;
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 0 4px;
  outline: none;
  z-index: 2;
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
.empty-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
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
  padding: 4px 10px;
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
  position: relative;
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
