<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NForm, NFormItem, NInput, NModal } from 'naive-ui'
import { useConnectionsStore } from '../stores/connections'
import * as models from '../../wailsjs/go/models'

// 分组新建/重命名弹窗（group 为 null 表示新建，parentId 指定挂载父级）
const props = defineProps<{
  show: boolean
  group: models.store.Group | null
  parentId: string
}>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void }>()

const { t } = useI18n()
const message = useMessage()
const store = useConnectionsStore()

const name = ref('')
const saving = ref(false)

watch(
  () => props.show,
  (v) => {
    if (v) name.value = props.group?.name ?? ''
  },
)

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

async function save() {
  const trimmed = name.value.trim()
  if (!trimmed) {
    message.warning(t('groupForm.nameRequired'))
    return
  }
  saving.value = true
  try {
    const g = props.group
      ? new models.store.Group({
          id: props.group.id,
          parentId: props.group.parentId,
          sort: props.group.sort,
          name: trimmed,
        })
      : new models.store.Group({ name: trimmed, parentId: props.parentId })
    await store.saveGroup(g)
    message.success(t('common.saved'))
    showModal.value = false
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <n-modal
    v-model:show="showModal"
    preset="card"
    :title="group ? t('groupForm.editTitle') : t('groupForm.createTitle')"
    style="width: 420px"
  >
    <n-form label-placement="top" @keyup.enter="save">
      <n-form-item :label="t('groupForm.name')">
        <n-input v-model:value="name" :placeholder="t('groupForm.nameRequired')" autofocus />
      </n-form-item>
      <div class="actions">
        <n-button quaternary @click="showModal = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="saving" @click="save">
          {{ t('common.save') }}
        </n-button>
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
