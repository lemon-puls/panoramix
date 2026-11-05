import { describe, expect, it } from 'vitest'
import { buildRowUpdates, countPendingEdits, shouldBlockUnguardedSwitch } from './dbEditing'

describe('countPendingEdits', () => {
  it('sums pending edits across tabs', () => {
    expect(
      countPendingEdits([
        { pending: [{ row: 0, col: 1, value: 'a' }] },
        { pending: [] },
        { pending: [{ row: 1, col: 2, value: 'b' }, { row: 2, col: 2, value: 'c' }] },
      ]),
    ).toBe(3)
  })

  it('returns 0 for empty input', () => {
    expect(countPendingEdits([])).toBe(0)
  })
})

describe('shouldBlockUnguardedSwitch', () => {
  it('allows the initial assignment (prev is empty)', () => {
    expect(shouldBlockUnguardedSwitch('', 'mysql-1', 3)).toBe(false)
  })

  it('allows re-selecting the same connection', () => {
    expect(shouldBlockUnguardedSwitch('mysql-1', 'mysql-1', 3)).toBe(false)
  })

  it('allows switching when there are no pending edits', () => {
    expect(shouldBlockUnguardedSwitch('mysql-1', 'redis-1', 0)).toBe(false)
  })

  it('blocks a direct switch that bypassed selectConn', () => {
    expect(shouldBlockUnguardedSwitch('mysql-1', 'redis-1', 1)).toBe(true)
  })
})

describe('buildRowUpdates', () => {
  const columns = ['id', 'tenant_id', 'name', 'note']
  const pkColumns = ['id', 'tenant_id']

  it('keeps backslash and quote values verbatim and maps NULL to null', () => {
    const rows: (string | null)[][] = [['1', 't1', 'alice', 'old']]
    const updates = buildRowUpdates(
      [
        { row: 0, col: 2, value: "O'Reilly\\temp" },
        { row: 0, col: 3, value: 'NULL' },
      ],
      rows,
      columns,
      pkColumns,
    )
    expect(updates).toEqual([
      {
        pk: { id: '1', tenant_id: 't1' },
        set: { name: "O'Reilly\\temp", note: null },
      },
    ])
  })

  it('does not treat lowercase null as SQL NULL', () => {
    const updates = buildRowUpdates([{ row: 0, col: 2, value: 'null' }], [['1', 't1', 'a', 'x']], columns, pkColumns)
    expect(updates[0].set.name).toBe('null')
  })

  it('groups multiple edits per row and keeps composite PK values', () => {
    const rows: (string | null)[][] = [
      ['1', 't1', 'a', 'x'],
      ['2', 't2', 'b', 'y'],
    ]
    const updates = buildRowUpdates(
      [
        { row: 1, col: 2, value: 'b2' },
        { row: 0, col: 2, value: 'a2' },
        { row: 0, col: 3, value: 'x2' },
      ],
      rows,
      columns,
      pkColumns,
    )
    expect(updates).toEqual([
      { pk: { id: '1', tenant_id: 't1' }, set: { name: 'a2', note: 'x2' } },
      { pk: { id: '2', tenant_id: 't2' }, set: { name: 'b2' } },
    ])
  })

  it('skips edits whose row or column no longer exists', () => {
    const updates = buildRowUpdates(
      [
        { row: 9, col: 2, value: 'gone' },
        { row: 0, col: 99, value: 'bad col' },
      ],
      [['1', 't1', 'a', 'x']],
      columns,
      pkColumns,
    )
    expect(updates).toEqual([])
  })
})
