import { describe, expect, it } from 'vitest'
import {
  NUMERIC_RE,
  cloneConditions,
  conditionError,
  defaultOperatorForType,
  familyOfType,
  opNeedsValue,
  operatorsForType,
  sameCondition,
  validateConditionValue,
} from './gridFilters'

describe('familyOfType', () => {
  it('maps MySQL types to families', () => {
    expect(familyOfType('bigint unsigned')).toBe('numeric')
    expect(familyOfType('decimal(12,2)')).toBe('numeric')
    expect(familyOfType('datetime')).toBe('date')
    expect(familyOfType('varchar(200)')).toBe('text')
    expect(familyOfType("enum('new','paid')")).toBe('enum')
    expect(familyOfType("set('a','b')")).toBe('enum')
    expect(familyOfType('json')).toBe('json')
    expect(familyOfType('longblob')).toBe('binary')
    expect(familyOfType('geometry')).toBe('other')
  })
})

describe('operatorsForType', () => {
  it('matches the backend matrix', () => {
    expect(operatorsForType('int')).toEqual(['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'is_null', 'is_not_null'])
    expect(operatorsForType('varchar(10)')).toEqual(['eq', 'ne', 'contains', 'is_null', 'is_not_null'])
    expect(operatorsForType("enum('a')")).toEqual(['eq', 'ne', 'is_null', 'is_not_null'])
    expect(operatorsForType('json')).toEqual(['contains', 'is_null', 'is_not_null'])
    expect(operatorsForType('blob')).toEqual(['is_null', 'is_not_null'])
  })

  it('uses eq/contains as the default operator', () => {
    expect(defaultOperatorForType('int')).toBe('eq')
    expect(defaultOperatorForType('json')).toBe('contains')
    expect(defaultOperatorForType('blob')).toBe('is_null')
  })
})

describe('NUMERIC_RE', () => {
  it('accepts decimals, signs and scientific notation', () => {
    for (const v of ['0', '-12', '+3.14', '.5', '1e10', '9007199254740993']) {
      expect(NUMERIC_RE.test(v)).toBe(true)
    }
  })

  it('rejects empty/non-numeric values', () => {
    for (const v of ['', 'abc', '1,2', '1.2.3', '--1']) {
      expect(NUMERIC_RE.test(v)).toBe(false)
    }
  })
})

describe('validateConditionValue', () => {
  it('requires numbers for numeric comparisons', () => {
    expect(validateConditionValue({ op: 'eq', value: 'abc' }, 'int')).toBe('numeric')
    expect(validateConditionValue({ op: 'eq', value: '42' }, 'int')).toBe('')
    expect(validateConditionValue({ op: 'contains', value: 'abc' }, 'varchar(10)')).toBe('')
  })

  it('does not require a value for NULL operators', () => {
    expect(validateConditionValue({ op: 'is_null', value: 'ignored' }, 'int')).toBe('')
    expect(opNeedsValue('is_null')).toBe(false)
    expect(opNeedsValue('eq')).toBe(true)
  })

  it('rejects values over 4096 characters', () => {
    expect(validateConditionValue({ op: 'eq', value: 'x'.repeat(4097) }, 'varchar(10)')).toBe('too_long')
  })
})

describe('conditionError', () => {
  const columns = [
    { field: 'id', type: 'bigint' },
    { field: 'name', type: 'varchar(10)' },
  ]

  it('detects unknown columns and unsupported operators', () => {
    expect(conditionError({ column: 'nope', op: 'eq', value: 'x' }, columns)).toBe('unknown_column')
    expect(conditionError({ column: 'id', op: 'contains', value: 'x' }, columns)).toBe('operator_not_allowed')
    expect(conditionError({ column: 'name', op: 'contains', value: 'x' }, columns)).toBe('')
  })
})

describe('cloneConditions / sameCondition', () => {
  it('clones without sharing references', () => {
    const src = [{ column: 'name', op: 'eq' as const, value: 'x' }]
    const copy = cloneConditions(src)
    copy[0].value = 'y'
    expect(src[0].value).toBe('x')
  })

  it('compares all fields', () => {
    const a = { column: 'name', op: 'eq' as const, value: 'x' }
    expect(sameCondition(a, { ...a })).toBe(true)
    expect(sameCondition(a, { ...a, value: 'y' })).toBe(false)
  })
})
