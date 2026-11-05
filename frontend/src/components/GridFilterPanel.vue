<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NInput, NSelect } from 'naive-ui'
import type * as models from '../../wailsjs/go/models'
import {
  conditionError,
  defaultOperatorForType,
  opNeedsValue,
  operatorsForType,
  type FilterCondition,
  type FilterOp,
} from './gridFilters'

// 结构化筛选草稿面板：所有修改通过 update:draft 回传，应用/清除由父组件执行查询。
const props = defineProps<{
  columns: models.db.ColumnInfo[]
  draft: FilterCondition[]
  hidden: string[]
  previewSql: string
  previewLoading: boolean
  previewError: string
}>()
const emit = defineEmits<{
  'update:draft': [FilterCondition[]]
  apply: []
  clear: []
  cancel: []
  'switch-raw': []
  preview: [FilterCondition[]]
  'copy-sql': [string]
  'open-sql': [string]
}>()

const { t } = useI18n()
const previewOpen = ref(false)

const columnOptions = computed(() =>
  props.columns.map((c) => ({
    label: c.field + (props.hidden.includes(c.field) ? ` · ${t('db.columns.hiddenTag')}` : ''),
    value: c.field,
  })),
)

function optionsFor(column: string): { label: string; value: FilterOp }[] {
  const col = props.columns.find((c) => c.field === column)
  const ops = operatorsForType(col?.type ?? '')
  return ops.map((op) => ({ label: t(`db.filter.op.${op}`), value: op }))
}

function rowError(cond: FilterCondition): string {
  const err = conditionError(cond, props.columns)
  switch (err) {
    case '':
      return ''
    case 'numeric':
      return t('db.filter.errNumeric')
    case 'too_long':
      return t('db.filter.errTooLong')
    case 'unknown_column':
      return t('db.filter.errUnknownColumn')
    case 'operator_not_allowed':
      return t('db.filter.errOperator')
    default:
      return ''
  }
}

const hasError = computed(() => props.draft.some((c) => rowError(c) !== ''))
const canAdd = computed(() => props.draft.length < 5 && props.columns.length > 0)

function update(next: FilterCondition[]) {
  emit('update:draft', next)
  if (previewOpen.value) emit('preview', next)
}

function addRow() {
  if (!canAdd.value) return
  const first = props.columns[0]
  update([...props.draft, { column: first.field, op: defaultOperatorForType(first.type), value: '' }])
}

function removeRow(index: number) {
  update(props.draft.filter((_, i) => i !== index))
}

function patchRow(index: number, patch: Partial<FilterCondition>) {
  const next = props.draft.map((c, i) => (i === index ? { ...c, ...patch } : c))
  update(next)
}

function changeColumn(index: number, column: string) {
  const col = props.columns.find((c) => c.field === column)
  const type = col?.type ?? ''
  const row = props.draft[index]
  const allowed = operatorsForType(type)
  const op = allowed.includes(row.op) ? row.op : defaultOperatorForType(type)
  patchRow(index, { column, op })
}

function changeOp(index: number, op: FilterOp) {
  patchRow(index, { op })
}

function togglePreview() {
  previewOpen.value = !previewOpen.value
  if (previewOpen.value) emit('preview', props.draft)
}

function valuePlaceholder(index: number): string {
  const col = props.columns.find((c) => c.field === props.draft[index]?.column)
  if (col && ['date', 'datetime', 'timestamp'].includes((col.type ?? '').toLowerCase().split('(')[0])) {
    return '2026-10-01 12:00:00'
  }
  return t('db.filter.valuePlaceholder')
}
</script>

<template>
  <div class="grid-filter-panel">
    <div class="grid-filter-head">
      <span>{{ t('db.filter.title') }}</span>
      <span class="grid-filter-hint">{{ t('db.filter.andHint') }} · {{ t('db.filter.maxHint') }}</span>
    </div>

    <div class="grid-filter-rows">
      <div v-for="(cond, i) in draft" :key="i" class="grid-filter-row">
        <n-select
          size="small"
          class="grid-filter-col"
          :value="cond.column"
          :options="columnOptions"
          @update:value="changeColumn(i, $event)"
        />
        <n-select
          size="small"
          class="grid-filter-op"
          :value="cond.op"
          :options="optionsFor(cond.column)"
          @update:value="changeOp(i, $event)"
        />
        <n-input
          v-if="opNeedsValue(cond.op)"
          size="small"
          class="grid-filter-value"
          :value="cond.value"
          :placeholder="valuePlaceholder(i)"
          @update:value="patchRow(i, { value: $event })"
        />
        <span v-else class="grid-filter-novalue">—</span>
        <n-button size="tiny" quaternary class="grid-filter-remove" :title="t('db.filter.remove')" @click="removeRow(i)">
          ×
        </n-button>
        <div v-if="rowError(cond)" class="grid-filter-row-error">{{ rowError(cond) }}</div>
      </div>
      <div v-if="draft.length === 0" class="grid-filter-empty">{{ t('db.filter.emptyHint') }}</div>
    </div>

    <div class="grid-filter-tools">
      <n-button size="tiny" quaternary :disabled="!canAdd" @click="addRow">
        {{ t('db.filter.add') }}
      </n-button>
      <span v-if="!canAdd && columns.length" class="grid-filter-hint">{{ t('db.filter.maxHint') }}</span>
      <span class="spacer" />
      <n-button size="tiny" quaternary @click="togglePreview">
        {{ previewOpen ? '▾' : '▸' }} {{ t('db.filter.previewSql') }}
      </n-button>
    </div>

    <div v-if="previewOpen" class="grid-filter-preview">
      <div v-if="previewLoading" class="grid-filter-hint">{{ t('db.filter.previewLoading') }}</div>
      <div v-else-if="previewError" class="grid-filter-row-error">{{ previewError }}</div>
      <template v-else-if="previewSql">
        <pre class="grid-filter-sql">{{ previewSql }}</pre>
        <div class="grid-filter-preview-actions">
          <span class="grid-filter-hint">{{ t('db.filter.previewNote') }}</span>
          <span class="spacer" />
          <n-button size="tiny" quaternary @click="emit('copy-sql', previewSql)">{{ t('db.filter.copySql') }}</n-button>
          <n-button size="tiny" quaternary @click="emit('open-sql', previewSql)">
            {{ t('db.filter.openInQuery') }}
          </n-button>
        </div>
      </template>
    </div>

    <div class="grid-filter-footer">
      <n-button size="tiny" quaternary @click="emit('switch-raw')">{{ t('db.filter.useRaw') }}</n-button>
      <span class="spacer" />
      <n-button size="small" quaternary type="error" @click="emit('clear')">{{ t('db.filter.clear') }}</n-button>
      <n-button size="small" @click="emit('cancel')">{{ t('common.cancel') }}</n-button>
      <n-button size="small" type="primary" :disabled="hasError || columns.length === 0" @click="emit('apply')">
        {{ t('db.filter.apply') }}
      </n-button>
    </div>
  </div>
</template>

<style scoped>
.grid-filter-panel {
  width: 560px;
  max-width: calc(100vw - 32px);
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.grid-filter-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
}
.grid-filter-hint {
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 400;
}
.grid-filter-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 320px;
  overflow: auto;
}
.grid-filter-row {
  display: grid;
  grid-template-columns: 1.1fr 0.9fr 1.4fr auto;
  gap: 8px;
  align-items: center;
}
.grid-filter-row-error {
  grid-column: 1 / -1;
  color: var(--danger);
  font-size: 11px;
}
.grid-filter-novalue {
  color: var(--text-muted);
  text-align: center;
}
.grid-filter-remove {
  width: 24px;
  min-width: 24px;
}
.grid-filter-empty {
  color: var(--text-muted);
  font-size: 12px;
  padding: 8px 0;
}
.grid-filter-tools {
  display: flex;
  align-items: center;
  gap: 8px;
}
.grid-filter-preview {
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.grid-filter-sql {
  margin: 0;
  padding: 8px;
  background: var(--bg-app);
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}
.grid-filter-preview-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.grid-filter-footer {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.spacer {
  flex: 1;
}
@media (max-width: 960px) {
  .grid-filter-row {
    grid-template-columns: 1fr 1fr;
  }
  .grid-filter-value {
    grid-column: 1 / -1;
  }
}
</style>
