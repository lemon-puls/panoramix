<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton } from 'naive-ui'
import { opNeedsValue, type FilterCondition } from './gridFilters'

// 已应用条件的 chip 回显；× 立即移除并重查（由父组件走保护）。
const props = defineProps<{
  conditions: FilterCondition[]
}>()
const emit = defineEmits<{
  remove: [number]
  clear: []
  add: []
  edit: [number]
}>()

const { t } = useI18n()

function truncate(v: string): string {
  const runes = [...v]
  return runes.length > 24 ? runes.slice(0, 24).join('') + '…' : v
}

function label(cond: FilterCondition): string {
  const op = t(`db.filter.op.${cond.op}`)
  if (!opNeedsValue(cond.op)) return `${cond.column} ${op}`
  return `${cond.column} ${op} ${truncate(cond.value)}`
}

function fullLabel(cond: FilterCondition): string {
  const op = t(`db.filter.op.${cond.op}`)
  return opNeedsValue(cond.op) ? `${cond.column} ${op} ${cond.value}` : `${cond.column} ${op}`
}

const visible = computed(() => props.conditions.length > 0)
</script>

<template>
  <div v-if="visible" class="grid-filter-chips">
    <button
      v-for="(cond, i) in conditions"
      :key="i"
      class="grid-filter-chip"
      :title="fullLabel(cond)"
      @click="emit('edit', i)"
    >
      <span class="grid-filter-chip-text">{{ label(cond) }}</span>
      <span
        class="grid-filter-chip-x"
        role="button"
        :aria-label="t('db.filter.remove')"
        @click.stop="emit('remove', i)"
      >×</span>
    </button>
    <n-button size="tiny" quaternary class="grid-filter-add-chip" @click="emit('add')">
      {{ t('db.filter.add') }}
    </n-button>
    <span class="spacer" />
    <n-button size="tiny" quaternary @click="emit('clear')">{{ t('db.filter.clearAll') }}</n-button>
  </div>
</template>

<style scoped>
.grid-filter-chips {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  padding: 6px 12px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border-subtle);
}
.grid-filter-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 24px;
  padding: 0 8px;
  border-radius: 12px;
  border: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
  max-width: 320px;
}
.grid-filter-chip:hover {
  border-color: var(--accent);
  color: var(--text-primary);
}
.grid-filter-chip-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.grid-filter-chip-x {
  color: var(--text-muted);
  padding: 0 2px;
}
.grid-filter-chip-x:hover {
  color: var(--danger);
}
.spacer {
  flex: 1;
}
</style>
