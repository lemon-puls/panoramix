// 结构化筛选的纯逻辑：类型族、操作符矩阵、数值正则、草稿校验。
// 与后端 internal/db/table_query.go 的矩阵保持一致；后端仍是最终校验方。

export type FilterOp =
  | 'eq'
  | 'ne'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'contains'
  | 'is_null'
  | 'is_not_null'

export interface FilterCondition {
  column: string
  op: FilterOp
  value: string
}

export type FilterFamily = 'numeric' | 'date' | 'text' | 'enum' | 'json' | 'binary' | 'other'

export const NUMERIC_RE = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/
export const MAX_CONDITION_VALUE = 4096
export const MAX_CONDITIONS = 5
export const MAX_RAW_WHERE = 4000

const NUMERIC_PREFIXES = [
  'tinyint', 'smallint', 'mediumint', 'int', 'integer', 'bigint',
  'decimal', 'numeric', 'float', 'double', 'real', 'bit',
]
const DATE_PREFIXES = ['date', 'datetime', 'timestamp', 'time', 'year']
const TEXT_PREFIXES = ['char', 'varchar', 'tinytext', 'text', 'mediumtext', 'longtext']
const ENUM_PREFIXES = ['enum', 'set']
const JSON_PREFIXES = ['json']
const BINARY_PREFIXES = ['binary', 'varbinary', 'tinyblob', 'blob', 'mediumblob', 'longblob']

export function familyOfType(typeName: string): FilterFamily {
  const t = (typeName ?? '').trim().toLowerCase()
  const has = (prefixes: string[]) => prefixes.some((p) => t.startsWith(p))
  if (has(NUMERIC_PREFIXES)) return 'numeric'
  if (has(DATE_PREFIXES)) return 'date'
  if (has(TEXT_PREFIXES)) return 'text'
  if (has(ENUM_PREFIXES)) return 'enum'
  if (has(JSON_PREFIXES)) return 'json'
  if (has(BINARY_PREFIXES)) return 'binary'
  return 'other'
}

const FAMILY_OPS: Record<FilterFamily, FilterOp[]> = {
  numeric: ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'is_null', 'is_not_null'],
  date: ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'is_null', 'is_not_null'],
  text: ['eq', 'ne', 'contains', 'is_null', 'is_not_null'],
  enum: ['eq', 'ne', 'is_null', 'is_not_null'],
  json: ['contains', 'is_null', 'is_not_null'],
  binary: ['is_null', 'is_not_null'],
  other: ['is_null', 'is_not_null'],
}

export function operatorsForType(typeName: string): FilterOp[] {
  return FAMILY_OPS[familyOfType(typeName)]
}

export function defaultOperatorForType(typeName: string): FilterOp {
  return operatorsForType(typeName)[0] ?? 'is_null'
}

export function opNeedsValue(op: FilterOp): boolean {
  return op !== 'is_null' && op !== 'is_not_null'
}

export type ConditionValueError = '' | 'numeric' | 'too_long'

export function validateConditionValue(
  cond: Pick<FilterCondition, 'op' | 'value'>,
  typeName: string,
): ConditionValueError {
  if (!opNeedsValue(cond.op)) return ''
  if ([...cond.value].length > MAX_CONDITION_VALUE) return 'too_long'
  if (familyOfType(typeName) === 'numeric' && !NUMERIC_RE.test(cond.value)) return 'numeric'
  return ''
}

// 草稿校验：列存在、操作符允许、值合法
export function conditionError(
  cond: FilterCondition,
  columns: { field: string; type: string }[],
): ConditionValueError | 'unknown_column' | 'operator_not_allowed' {
  const col = columns.find((c) => c.field === cond.column)
  if (!col) return 'unknown_column'
  if (!operatorsForType(col.type).includes(cond.op)) return 'operator_not_allowed'
  return validateConditionValue(cond, col.type)
}

export function sameCondition(a: FilterCondition, b: FilterCondition): boolean {
  return a.column === b.column && a.op === b.op && a.value === b.value
}

export function cloneConditions(list: FilterCondition[]): FilterCondition[] {
  return list.map((c) => ({ column: c.column, op: c.op, value: c.value }))
}
