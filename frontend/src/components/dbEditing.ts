// 数据标签编辑相关的纯函数（不依赖 Vue，便于 vitest 直接测试）。
// 这些函数只做“界面状态 → 后端参数化请求”的转换，不拼接 SQL。

export interface PendingEdit {
  row: number
  col: number
  value: string
}

export interface RowUpdatePayload {
  pk: Record<string, string | null>
  set: Record<string, string | null>
}

// 统计数据标签的待提交修改数（调用方负责只传 kind === 'data' 的标签）
export function countPendingEdits(tabs: { pending: PendingEdit[] }[]): number {
  return tabs.reduce((n, tab) => n + tab.pending.length, 0)
}

// 连接切换防御：watch(activeId) 只允许在“已保护”的切换后清空标签。
// 若 prev 非空、目标连接不同且仍有 pending，说明有赋值点绕过了 selectConn，
// 调用方必须拒绝清空并回退（不弹第二个确认框，避免重复触发）。
export function shouldBlockUnguardedSwitch(prev: string, next: string, pending: number): boolean {
  return !!prev && next !== prev && pending > 0
}

// 按行组装后端 DBUpdateRows 的载荷：
// - 一行多列合并为一条 update（后端单事务执行）；
// - 主键值取当前结果行，数据库 NULL 保持 null；
// - 输入大写的 NULL 表示置空（与既有交互一致），其余值按字面量传递；
// - 行顺序稳定，便于断言与日志排查。
export function buildRowUpdates(
  pending: PendingEdit[],
  rows: (string | null)[][],
  columns: string[],
  pkColumns: string[],
): RowUpdatePayload[] {
  const idxOf = (name: string) => columns.findIndex((c) => c === name)
  const byRow = new Map<number, PendingEdit[]>()
  for (const p of pending) {
    const list = byRow.get(p.row) ?? []
    list.push(p)
    byRow.set(p.row, list)
  }

  const updates: RowUpdatePayload[] = []
  for (const rowIndex of [...byRow.keys()].sort((a, b) => a - b)) {
    const rowData = rows[rowIndex]
    if (!rowData) continue
    const pk: Record<string, string | null> = {}
    for (const name of pkColumns) pk[name] = rowData[idxOf(name)] ?? null
    const set: Record<string, string | null> = {}
    for (const edit of byRow.get(rowIndex) ?? []) {
      const column = columns[edit.col]
      if (!column) continue
      set[column] = edit.value === 'NULL' ? null : edit.value
    }
    if (Object.keys(set).length === 0) continue
    updates.push({ pk, set })
  }
  return updates
}
