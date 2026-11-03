<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NForm, NFormItem, NInput, NModal, NSelect, NSwitch } from 'naive-ui'

// 索引 / 外键编辑弹窗（共用）
const props = defineProps<{
  show: boolean
  kind: 'index' | 'foreignKey'
  columns: string[] // 当前表的列（供选择）
  table: string // 当前表名（外键引用表的候选）
  tables: string[] // 同库表列表（外键引用候选）
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (
    e: 'submit',
    payload: {
      name: string
      columns: string[]
      unique?: boolean
      refTable?: string
      refColumns?: string
      onUpdate?: string
      onDelete?: string
    },
  ): void
}>()

const { t } = useI18n()
const message = useMessage()

const form = reactive({
  name: '',
  columns: [] as string[],
  unique: false,
  refTable: '',
  refColumns: '',
  onUpdate: 'RESTRICT',
  onDelete: 'RESTRICT',
})

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

const columnOptions = computed(() => props.columns.map((c) => ({ label: c, value: c })))
const tableOptions = computed(() => props.tables.map((c) => ({ label: c, value: c })))
const ruleOptions = ['RESTRICT', 'CASCADE', 'SET NULL', 'NO ACTION'].map((v) => ({ label: v, value: v }))

watch(
  () => props.show,
  (v) => {
    if (!v) return
    form.name = ''
    form.columns = []
    form.unique = false
    form.refTable = ''
    form.refColumns = ''
    form.onUpdate = 'RESTRICT'
    form.onDelete = 'RESTRICT'
  },
)

function submit() {
  if (!form.name.trim() || form.columns.length === 0) {
    message.warning(t('dbIndex.required'))
    return
  }
  if (props.kind === 'foreignKey' && (!form.refTable || !form.refColumns.trim())) {
    message.warning(t('dbIndex.fkRequired'))
    return
  }
  emit('submit', {
    name: form.name.trim(),
    columns: [...form.columns],
    unique: form.unique,
    refTable: form.refTable,
    refColumns: form.refColumns.trim(),
    onUpdate: form.onUpdate,
    onDelete: form.onDelete,
  })
  showModal.value = false
}
</script>

<template>
  <n-modal
    v-model:show="showModal"
    preset="card"
    :title="kind === 'index' ? t('dbIndex.addTitle') : t('dbIndex.fkAddTitle')"
    style="width: 540px"
  >
    <n-form label-placement="left" label-width="100">
      <n-form-item :label="t('dbIndex.name')">
        <n-input
          v-model:value="form.name"
          :placeholder="kind === 'index' ? 'idx_name' : 'fk_name'"
        />
      </n-form-item>
      <n-form-item :label="t('dbIndex.columns')">
        <n-select v-model:value="form.columns" multiple :options="columnOptions" filterable />
      </n-form-item>
      <n-form-item v-if="kind === 'index'" :label="t('dbIndex.unique')">
        <n-switch v-model:value="form.unique" />
      </n-form-item>

      <template v-else>
        <n-form-item :label="t('dbIndex.refTable')">
          <n-select v-model:value="form.refTable" :options="tableOptions" filterable />
        </n-form-item>
        <n-form-item :label="t('dbIndex.refColumns')">
          <n-input v-model:value="form.refColumns" placeholder="id" />
        </n-form-item>
        <n-form-item :label="t('dbIndex.onUpdate')">
          <n-select v-model:value="form.onUpdate" :options="ruleOptions" />
        </n-form-item>
        <n-form-item :label="t('dbIndex.onDelete')">
          <n-select v-model:value="form.onDelete" :options="ruleOptions" />
        </n-form-item>
      </template>

      <div class="actions">
        <n-button quaternary @click="showModal = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" @click="submit">{{ t('common.save') }}</n-button>
      </div>
    </n-form>
  </n-modal>
</template>

<style scoped>
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
