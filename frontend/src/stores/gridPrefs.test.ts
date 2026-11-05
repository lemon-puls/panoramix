import { describe, expect, it } from 'vitest'
import {
  GRID_PREF_VERSION,
  gridPrefKey,
  intersectHidden,
  parseGridPref,
  repairHidden,
  serializeGridPref,
  visibleColumnRefs,
} from './gridPrefs'

describe('gridPrefKey', () => {
  it('encodes connection, database and table', () => {
    expect(gridPrefKey('conn-1', 'demo', 'users')).toBe('grid.columns.conn-1.demo.users')
  })
})

describe('parseGridPref', () => {
  it('parses a valid pref', () => {
    expect(parseGridPref(JSON.stringify({ v: GRID_PREF_VERSION, hidden: ['avatar', 'note'] }))).toEqual([
      'avatar',
      'note',
    ])
  })

  it('returns [] for empty/corrupted/version-mismatched payloads', () => {
    expect(parseGridPref(null)).toEqual([])
    expect(parseGridPref('')).toEqual([])
    expect(parseGridPref('{not json')).toEqual([])
    expect(parseGridPref(JSON.stringify({ v: 99, hidden: ['a'] }))).toEqual([])
    expect(parseGridPref(JSON.stringify({ v: 1, hidden: 'a' }))).toEqual([])
  })

  it('drops non-string and duplicate names', () => {
    expect(parseGridPref(JSON.stringify({ v: 1, hidden: ['a', 1, null, 'a', ''] }))).toEqual(['a'])
  })
})

describe('serializeGridPref', () => {
  it('round-trips and de-duplicates', () => {
    const raw = serializeGridPref(['a', 'a', 'b'])
    expect(JSON.parse(raw)).toEqual({ v: 1, hidden: ['a', 'b'] })
    expect(parseGridPref(raw)).toEqual(['a', 'b'])
  })
})

describe('hidden intersection and repair', () => {
  it('keeps only names that still exist', () => {
    expect(intersectHidden(['avatar', 'gone'], ['id', 'avatar', 'name'])).toEqual(['avatar'])
  })

  it('repairs an all-hidden set by keeping the first column', () => {
    expect(repairHidden(['id', 'avatar', 'name'], ['id', 'avatar', 'name'])).toEqual({
      hidden: ['avatar', 'name'],
      repaired: true,
    })
  })

  it('does not repair a normal hidden set', () => {
    expect(repairHidden(['avatar'], ['id', 'avatar', 'name'])).toEqual({
      hidden: ['avatar'],
      repaired: false,
    })
  })

  it('handles an empty column list', () => {
    expect(repairHidden(['a'], [])).toEqual({ hidden: [], repaired: false })
  })
})

describe('visibleColumnRefs', () => {
  it('keeps original indices for hidden-column-safe editing', () => {
    expect(visibleColumnRefs(['avatar'], ['id', 'avatar', 'email'])).toEqual([
      { name: 'id', idx: 0 },
      { name: 'email', idx: 2 },
    ])
  })

  it('always renders at least one column', () => {
    expect(visibleColumnRefs(['id', 'email'], ['id', 'email'])).toEqual([{ name: 'id', idx: 0 }])
  })
})
