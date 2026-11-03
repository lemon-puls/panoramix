<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NForm, NFormItem, NInput, NModal, NSelect, NSwitch } from 'naive-ui'
import * as models from '../../wailsjs/go/models'

// 字段编辑弹窗：新增 / 修改列定义
const props = defineProps<{
  show: boolean
  mode: 'add' | 'edit'
  column: models.db.ColumnInfo | null
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'submit', payload: { name: string; type: string; nullable: boolean; defaultValue: string; comment: string; autoIncrement: boolean }): void
}>()

const { t } = useI18n()
const message = useMessage()

const form = reactive({
  name: '',
  type: 'VARCHAR(255)',
  nullable: true,
  defaultValue: '',
  comment: '',
  autoIncrement: false,
})

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

const commonTypes = [
  'INT', 'BIGINT', 'TINYINT', 'SMALLINT', 'MEDIUMINT',
  'VARCHAR(255)', 'TEXT', 'MEDIUMTEXT', 'LONGTEXT',
  'DATETIME', 'TIMESTAMP', 'DATE', 'TIME',
  'DECIMAL(10,2)', 'FLOAT', 'DOUBLE', 'JSON', 'BLOB', 'LONGBLOB',
  'CHAR(36)', 'TINYINT(1)',
].map((v) => ({ label: v, value: v }))

watch(
  () => props.show,
  (v) => {
    if (!v) return
    const c = props.column
    form.name = c?.field ?? ''
    form.type = c?.type ?? 'VARCHAR(255)'
    form.nullable = c ? c.null === 'YES' : true
    form.defaultValue = c?.default ?? ''
    form.comment = c?.comment ?? ''
    form.autoIncrement = (c?.extra ?? '').toLowerCase().includes('auto_increment')
  },
)

function submit() {
  if (!form.name.trim() || !form.type.trim()) {
    message.warning(t('dbCol.required'))
    return
  }
  emit('submit', {
    name: form.name.trim(),
    type: form.type.trim(),
    nullable: form.nullable,
    defaultValue: form.defaultValue.trim(),
    comment: form.comment.trim(),
    autoIncrement: form.autoIncrement,
  })
  showModal.value = false
}
</script>

<template>
  <n-modal
    v-model:show="showModal"
    preset="card"
    :title="mode === 'add' ? t('dbCol.addTitle') : t('dbCol.editTitle')"
    style="width: 520px"
  >
    <n-form label-placement="left" label-width="92">
      <n-form-item :label="t('db.colField')">
        <n-input v-model:value="form.name" :placeholder="t('dbCol.namePlaceholder')" />
      </n-form-item>
      <n-form-item :label="t('db.colType')">
        <n-select
          v-model:value="form.type"
          :options="commonTypes"
          filterable
          tag
          :placeholder="t('dbCol.typePlaceholder')"
        />
      </n-form-item>
      <n-form-item :label="t('dbCol.nullable')">
        <n-switch v-model:value="form.nullable" />
      </n-form-item>
      <n-form-item :label="t('db.colDefault')">
        <n-input
          v-model:value="form.defaultValue"
          :placeholder="t('dbCol.defaultPlaceholder')"
        />
      </n-form-item>
      <n-form-item :label="t('dbCol.comment')">
        <n-input v-model:value="form.comment" />
      </n-form-item>
      <n-form-item :label="t('dbCol.extra')">
        <n-switch v-model:value="form.autoIncrement" />
        <span class="hint">{{ t('dbCol.autoIncHint') }}</span>
      </n-form-item>
      <div class="actions">
        <n-button quaternary @click="showModal = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" @click="submit">{{ t('common.save') }}</n-button>
      </div>
    </n-form>
  </n-modal>
</template>

<style scoped>
.hint {
  margin-left: 10px;
  font-size: 11px;
  color: var(--text-muted);
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
