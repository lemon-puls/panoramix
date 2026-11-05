<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NCheckbox, NInput } from 'naive-ui'
import type * as models from '../../wailsjs/go/models'

// 列可见性草稿面板：修改只在“应用”时提交给父组件。
const props = defineProps<{
  columns: models.db.ColumnInfo[]
  hidden: string[]
}>()
const emit = defineEmits<{
  apply: [hidden: string[]]
  close: []
}>()

const { t } = useI18n()
const search = ref('')
const draft = ref<string[]>([...props.hidden])
const inlineHint = ref('')

const searchResults = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return props.columns
  return props.columns.filter(
    (c) => c.field.toLowerCase().includes(q) || (c.comment ?? '').toLowerCase().includes(q),
  )
})

const hiddenSet = computed(() => new Set(draft.value))
const visibleCount = computed(() => props.columns.filter((c) => !hiddenSet.value.has(c.field)).length)
const allVisible = computed(() => draft.value.length === 0)

function isVisible(c: models.db.ColumnInfo): boolean {
  return !hiddenSet.value.has(c.field)
}

function keepOneHint() {
  const first = props.columns.find((c) => !hiddenSet.value.has(c.field)) ?? props.columns[0]
  inlineHint.value = t('db.columns.keepOne', { name: first?.field ?? '' })
}

function toggle(c: models.db.ColumnInfo) {
  inlineHint.value = ''
  const next = new Set(draft.value)
  if (next.has(c.field)) {
    next.delete(c.field)
  } else {
    if (next.size + 1 >= props.columns.length) {
      keepOneHint()
      return
    }
    next.add(c.field)
  }
  draft.value = [...next]
}

// 全选/清空只作用于当前搜索结果
function selectSearch() {
  inlineHint.value = ''
  const next = new Set(draft.value)
  for (const c of searchResults.value) next.delete(c.field)
  draft.value = [...next]
}

function clearSearch() {
  inlineHint.value = ''
  const next = new Set(draft.value)
  for (const c of searchResults.value) next.add(c.field)
  // 至少保留一列：若全部被隐藏，按表格顺序保留第一列
  if (next.size >= props.columns.length && props.columns.length > 0) {
    const first = props.columns[0]
    next.delete(first.field)
    draft.value = [...next]
    inlineHint.value = t('db.columns.clearedKept', { name: first.field })
    return
  }
  draft.value = [...next]
}

function showAll() {
  inlineHint.value = ''
  draft.value = []
}

function apply() {
  // 按表格顺序输出，保证存储稳定
  emit(
    'apply',
    props.columns.filter((c) => hiddenSet.value.has(c.field)).map((c) => c.field),
  )
}
</script>

<template>
  <div class="grid-columns-panel">
    <div class="grid-columns-head">
      <span>{{ t('db.columns.title') }}</span>
      <span class="grid-columns-count">{{ visibleCount }} / {{ columns.length }}</span>
    </div>

    <n-input
      v-model:value="search"
      size="small"
      clearable
      :placeholder="t('db.columns.searchPlaceholder')"
      class="grid-columns-search"
    />

    <div class="grid-columns-bulk">
      <n-button size="tiny" quaternary @click="selectSearch">{{ t('db.columns.selectAll') }}</n-button>
      <n-button size="tiny" quaternary @click="clearSearch">{{ t('db.columns.clear') }}</n-button>
      <span class="grid-columns-bulk-hint">{{ t('db.columns.searchOnly') }}</span>
    </div>

    <div class="grid-columns-list">
      <div v-for="c in searchResults" :key="c.field" class="grid-columns-row">
        <n-checkbox :checked="isVisible(c)" @update:checked="toggle(c)" />
        <span class="grid-columns-name">{{ c.field }}</span>
        <span class="grid-columns-type">{{ c.type }}</span>
        <span v-if="c.comment" class="grid-columns-comment" :title="c.comment">{{ c.comment }}</span>
      </div>
      <div v-if="searchResults.length === 0" class="grid-columns-empty">{{ t('db.columns.noMatch') }}</div>
    </div>

    <div v-if="inlineHint" class="grid-columns-hint">{{ inlineHint }}</div>
    <div class="grid-columns-note">{{ t('db.columns.note') }}</div>
    <div class="grid-columns-footer">
      <n-button size="tiny" quaternary :disabled="allVisible" @click="showAll">
        {{ t('db.columns.showAll') }}
      </n-button>
      <span class="spacer" />
      <n-button size="small" @click="emit('close')">{{ t('common.cancel') }}</n-button>
      <n-button size="small" type="primary" @click="apply">{{ t('db.columns.apply') }}</n-button>
    </div>
  </div>
</template>

<style scoped>
.grid-columns-panel {
  width: 320px;
  max-width: calc(100vw - 32px);
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.grid-columns-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
  font-size: 13px;
}
.grid-columns-count {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.grid-columns-bulk {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}
.grid-columns-bulk-hint {
  color: var(--text-muted);
  font-size: 11px;
}
.grid-columns-list {
  max-height: 360px;
  overflow: auto;
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  padding: 4px;
}
.grid-columns-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 6px;
  border-radius: 4px;
}
.grid-columns-row:hover {
  background: var(--bg-elevated);
}
.grid-columns-name {
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.grid-columns-type {
  color: var(--text-muted);
  font-size: 11px;
  margin-left: auto;
  white-space: nowrap;
}
.grid-columns-comment {
  color: var(--text-secondary);
  font-size: 11px;
  max-width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.grid-columns-empty {
  color: var(--text-muted);
  font-size: 12px;
  padding: 12px;
  text-align: center;
}
.grid-columns-hint {
  color: var(--warning);
  font-size: 11px;
}
.grid-columns-note {
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.5;
}
.grid-columns-footer {
  display: flex;
  align-items: center;
  gap: 8px;
}
.grid-columns-footer .spacer {
  flex: 1;
}
</style>
