// 数据标签列可见性偏好（纯函数，便于 vitest）。
// 存储后端是 settings 表，但 key 编码/解析/求交都在这里集中处理。

export const GRID_PREF_PREFIX = 'grid.columns.'
export const GRID_PREF_VERSION = 1

export interface GridPref {
  v: number
  hidden: string[]
}

// key 形如 grid.columns.<connId>.<database>.<table>
export function gridPrefKey(connId: string, database: string, table: string): string {
  return `${GRID_PREF_PREFIX}${connId}.${database}.${table}`
}

// 解析存储值；损坏 JSON / 版本不符 / 形状不对 → 视为“无隐藏”，不抛错。
export function parseGridPref(raw: string | null | undefined): string[] {
  if (!raw) return []
  let data: unknown
  try {
    data = JSON.parse(raw)
  } catch {
    return []
  }
  if (!data || typeof data !== 'object') return []
  const pref = data as Partial<GridPref>
  if (pref.v !== GRID_PREF_VERSION || !Array.isArray(pref.hidden)) return []
  const out: string[] = []
  const seen = new Set<string>()
  for (const name of pref.hidden) {
    if (typeof name !== 'string' || name === '' || seen.has(name)) continue
    seen.add(name)
    out.push(name)
  }
  return out
}

export function serializeGridPref(hidden: string[]): string {
  const seen = new Set<string>()
  const list: string[] = []
  for (const name of hidden) {
    if (typeof name !== 'string' || name === '' || seen.has(name)) continue
    seen.add(name)
    list.push(name)
  }
  return JSON.stringify({ v: GRID_PREF_VERSION, hidden: list })
}

// 与当前列求交；未知列名忽略（改名回来仍可能生效，但 UI 只用有效集合）
export function intersectHidden(hidden: string[], columns: string[]): string[] {
  const names = new Set(columns)
  return hidden.filter((name) => names.has(name))
}

// 至少保留一列：若全部隐藏，按表格顺序保留第一列。
// 返回修复后的隐藏集合与是否发生修复。
export function repairHidden(hidden: string[], columns: string[]): { hidden: string[]; repaired: boolean } {
  if (columns.length === 0) return { hidden: [], repaired: false }
  const valid = intersectHidden(hidden, columns)
  if (valid.length < columns.length) return { hidden: valid, repaired: false }
  return { hidden: columns.slice(1), repaired: true }
}

export interface VisibleColumnRef {
  name: string
  idx: number // 原始列索引：编辑/排序/筛选/CSV 都用它
}

export function visibleColumnRefs(hidden: string[], columns: string[]): VisibleColumnRef[] {
  const hiddenSet = new Set(hidden)
  const refs: VisibleColumnRef[] = []
  for (let idx = 0; idx < columns.length; idx++) {
    if (!hiddenSet.has(columns[idx])) refs.push({ name: columns[idx], idx })
  }
  // 防御：即使持久化被手工改坏，界面也至少渲染一列
  if (refs.length === 0 && columns.length > 0) refs.push({ name: columns[0], idx: 0 })
  return refs
}
