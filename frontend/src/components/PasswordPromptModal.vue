<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NForm, NFormItem, NInput, NModal } from 'naive-ui'
import { SetHostSecret } from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'

// 凭据补录弹窗：Xshell 导入的主机（或漏存密码的主机）首次连接失败时引导补录，
// 保存后立即重试连接。
const props = defineProps<{
  show: boolean
  host: models.store.Host | null
  kind: 'password' | 'passphrase' // 需要补录的凭据类型
}>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'saved'): void }>()

const { t } = useI18n()
const message = useMessage()

const value = ref('')
const saving = ref(false)

watch(
  () => props.show,
  (v) => {
    if (v) value.value = ''
  },
)

async function save() {
  if (!value.value || !props.host) {
    message.warning(t('passwordModal.required'))
    return
  }
  saving.value = true
  try {
    await SetHostSecret(props.host.id, props.kind, value.value)
    message.success(t('common.saved'))
    emit('saved')
    emit('update:show', false)
  } catch (e) {
    message.error(String(e))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <n-modal
    v-model:show="props.show"
    preset="card"
    :title="t('passwordModal.title', { name: host?.name ?? '' })"
    style="width: 420px"
  >
    <p class="tip">{{ t('passwordModal.tip') }}</p>
    <n-form label-placement="top" @keyup.enter="save">
      <n-form-item :label="kind === 'passphrase' ? t('hostForm.passphrase') : t('hostForm.password')">
        <n-input
          v-model:value="value"
          type="password"
          show-password-on="click"
          :placeholder="kind === 'passphrase' ? t('hostForm.passphraseNew') : t('hostForm.passwordNew')"
          autofocus
        />
      </n-form-item>
      <div class="actions">
        <n-button quaternary @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</n-button>
      </div>
    </n-form>
  </n-modal>
</template>

<style scoped>
.tip {
  margin: 0 0 12px;
  font-size: 12px;
  opacity: 0.6;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
