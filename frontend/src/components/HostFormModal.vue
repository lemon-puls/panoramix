<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import {
  NButton,
  NDynamicTags,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSwitch,
} from 'naive-ui'
import { useConnectionsStore } from '../stores/connections'
import * as models from '../../wailsjs/go/models'

// 主机新建/编辑弹窗。
// 凭据语义与后端一致：密码/passphrase 输入框留空 = 不修改已存凭据；
// 新建时留空 = 不存凭据（连接时走键盘交互）。
const props = defineProps<{
  show: boolean
  host: models.store.Host | null // null = 新建
  groupId: string // 新建时的目标分组
}>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void }>()

const { t } = useI18n()
const message = useMessage()
const store = useConnectionsStore()

const saving = ref(false)

const form = reactive({
  name: '',
  addr: '',
  port: 22,
  username: 'root',
  authType: 'password' as string,
  keyPath: '',
  password: '',
  passphrase: '',
  keepaliveSec: 30,
  autoReconnect: true,
  initCommands: [] as string[],
})

watch(
  () => props.show,
  (v) => {
    if (!v) return
    const h = props.host
    form.name = h?.name ?? ''
    form.addr = h?.addr ?? ''
    form.port = h?.port ?? 22
    form.username = h?.username || 'root'
    form.authType = h?.authType ?? 'password'
    form.keyPath = h?.keyPath ?? ''
    form.password = '' // 凭据字段永不回填
    form.passphrase = ''
    form.keepaliveSec = h?.keepaliveSec ?? 30
    form.autoReconnect = h?.autoReconnect ?? true
    form.initCommands = [...(h?.initCommands ?? [])]
  },
)

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

const authOptions = computed(() => [
  { label: t('hostForm.authPassword'), value: 'password' },
  { label: t('hostForm.authKey'), value: 'key' },
  { label: t('hostForm.authKbd'), value: 'keyboard_interactive' },
])

async function save() {
  if (!form.name.trim() || !form.addr.trim()) {
    message.warning(t('hostForm.requiredTip'))
    return
  }
  saving.value = true
  try {
    const input = new models.app.HostInput({
      id: props.host?.id ?? '',
      groupId: props.host?.groupId ?? props.groupId,
      name: form.name.trim(),
      addr: form.addr.trim(),
      port: form.port > 0 ? form.port : 22,
      username: form.username.trim(),
      authType: form.authType,
      keyPath: form.keyPath.trim(),
      initCommands: [...form.initCommands],
      keepaliveSec: form.keepaliveSec > 0 ? form.keepaliveSec : 30,
      autoReconnect: form.autoReconnect,
      password: form.password,
      passphrase: form.passphrase,
    })
    await store.saveHost(input)
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
    :title="host ? t('hostForm.editTitle') : t('hostForm.createTitle')"
    style="width: 560px"
  >
    <n-form label-placement="left" label-width="112">
      <n-form-item :label="t('hostForm.name')">
        <n-input v-model:value="form.name" :placeholder="t('hostForm.namePlaceholder')" />
      </n-form-item>
      <n-form-item :label="t('hostForm.addr')">
        <n-input
          v-model:value="form.addr"
          :placeholder="t('hostForm.addrPlaceholder')"
          class="addr-input"
        />
        <n-input-number v-model:value="form.port" :min="1" :max="65535" class="port-input" />
      </n-form-item>
      <n-form-item :label="t('hostForm.username')">
        <n-input v-model:value="form.username" />
      </n-form-item>
      <n-form-item :label="t('hostForm.authType')">
        <n-select v-model:value="form.authType" :options="authOptions" />
      </n-form-item>
      <n-form-item v-if="form.authType === 'password'" :label="t('hostForm.password')">
        <n-input
          v-model:value="form.password"
          type="password"
          show-password-on="click"
          :placeholder="
            host?.hasSecret ? t('hostForm.passwordKeep') : t('hostForm.passwordNew')
          "
        />
      </n-form-item>
      <template v-if="form.authType === 'key'">
        <n-form-item :label="t('hostForm.keyPath')">
          <n-input v-model:value="form.keyPath" :placeholder="t('hostForm.keyPathTip')" />
        </n-form-item>
        <n-form-item :label="t('hostForm.passphrase')">
          <n-input
            v-model:value="form.passphrase"
            type="password"
            show-password-on="click"
            :placeholder="
              host?.hasSecret ? t('hostForm.passphraseKeep') : t('hostForm.passphraseNew')
            "
          />
        </n-form-item>
      </template>
      <n-form-item :label="t('hostForm.keepalive')">
        <n-input-number v-model:value="form.keepaliveSec" :min="0" :max="3600" />
      </n-form-item>
      <n-form-item :label="t('hostForm.autoReconnect')">
        <n-switch v-model:value="form.autoReconnect" />
      </n-form-item>
      <n-form-item :label="t('hostForm.initCommands')">
        <n-dynamic-tags v-model:value="form.initCommands" />
      </n-form-item>
      <p class="tip">{{ t('hostForm.initCommandsTip') }}</p>
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
.addr-input {
  flex: 1;
}
.port-input {
  width: 110px;
  margin-left: 8px;
}
.tip {
  margin: -8px 0 0 112px;
  font-size: 12px;
  opacity: 0.55;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
