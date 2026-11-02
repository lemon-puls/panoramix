<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NForm, NFormItem, NInput, NInputNumber, NModal, NSelect } from 'naive-ui'
import { SaveDBConn } from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'

// 数据库连接表单（MySQL / Redis）
const props = defineProps<{ show: boolean; conn: models.store.DBConn | null }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'saved'): void }>()

const { t } = useI18n()
const message = useMessage()
const saving = ref(false)

const form = reactive({
  name: '',
  kind: 'mysql' as string,
  addr: '127.0.0.1',
  port: 3306,
  username: 'root',
  dbName: '',
  password: '',
})

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

const kindOptions = computed(() => [
  { label: 'MySQL', value: 'mysql' },
  { label: 'Redis', value: 'redis' },
])

watch(
  () => props.show,
  (v) => {
    if (!v) return
    const c = props.conn
    form.name = c?.name ?? ''
    form.kind = c?.kind ?? 'mysql'
    form.addr = c?.addr ?? '127.0.0.1'
    form.port = c?.port ?? (form.kind === 'redis' ? 6379 : 3306)
    form.username = c?.username ?? 'root'
    form.dbName = c?.dbName ?? ''
    form.password = '' // 凭据永不回填
  },
)

// 切换类型时给默认端口
watch(
  () => form.kind,
  (k) => {
    if (!props.conn) {
      form.port = k === 'redis' ? 6379 : 3306
      form.username = k === 'redis' ? '' : 'root'
    }
  },
)

async function save() {
  if (!form.name.trim() || !form.addr.trim()) {
    message.warning(t('db.requiredTip'))
    return
  }
  saving.value = true
  try {
    const input = new models.app.DBConnInput({
      id: props.conn?.id ?? '',
      name: form.name.trim(),
      kind: form.kind,
      addr: form.addr.trim(),
      port: form.port,
      username: form.username.trim(),
      dbName: form.dbName.trim(),
      password: form.password,
    })
    await SaveDBConn(input)
    message.success(t('common.saved'))
    emit('saved')
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
    :title="conn ? t('db.editConn') : t('db.newConn')"
    style="width: 480px"
  >
    <n-form label-placement="left" label-width="96">
      <n-form-item :label="t('db.name')">
        <n-input v-model:value="form.name" :placeholder="t('db.namePlaceholder')" />
      </n-form-item>
      <n-form-item :label="t('db.kind')">
        <n-select v-model:value="form.kind" :options="kindOptions" :disabled="!!conn" />
      </n-form-item>
      <n-form-item :label="t('hostForm.addr')">
        <n-input v-model:value="form.addr" class="grow" />
        <n-input-number v-model:value="form.port" :min="1" :max="65535" class="port" />
      </n-form-item>
      <n-form-item :label="t('hostForm.username')">
        <n-input v-model:value="form.username" :placeholder="form.kind === 'redis' ? t('db.userOptional') : ''" />
      </n-form-item>
      <n-form-item :label="form.kind === 'redis' ? t('db.redisDb') : t('db.database')">
        <n-input
          v-model:value="form.dbName"
          :placeholder="form.kind === 'redis' ? '0' : 'testdb'"
        />
      </n-form-item>
      <n-form-item :label="t('hostForm.password')">
        <n-input
          v-model:value="form.password"
          type="password"
          show-password-on="click"
          :placeholder="conn?.hasSecret ? t('hostForm.passwordKeep') : t('hostForm.passwordNew')"
        />
      </n-form-item>
      <div class="actions">
        <n-button quaternary @click="showModal = false">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</n-button>
      </div>
    </n-form>
  </n-modal>
</template>

<style scoped>
.grow {
  flex: 1;
}
.port {
  width: 110px;
  margin-left: 8px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
