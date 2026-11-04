<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import {
  NAlert, NButton, NForm, NFormItem, NInput, NInputNumber, NModal, NSelect, NSwitch,
} from 'naive-ui'
import {
  AIClearConsents,
  AIDeleteKey,
  AIGetConfig,
  AISaveConfig,
  AITestConnection,
} from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'

// AI 服务配置弹窗。
// 凭据语义与主机/数据库一致：API Key 留空表示不修改，只写入系统 keychain。
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void; (e: 'saved'): void }>()

const { t } = useI18n()
const message = useMessage()

const showModal = computed({
  get: () => props.show,
  set: (v) => emit('update:show', v),
})

type ProviderKind = 'ollama' | 'lmstudio' | 'openai-compatible'

const form = reactive({
  enabled: false,
  provider: 'ollama' as ProviderKind,
  baseUrl: 'http://127.0.0.1:11434/v1',
  model: '',
  temperature: 0.1,
  timeoutSec: 120,
  allowWrite: false,
  allowInsecureTls: false,
  concurrency: 1,
})
const apiKey = ref('')
const clearKey = ref(false)
const hasKey = ref(false)
const endpoint = ref('')
const scope = ref<'local' | 'lan' | 'cloud'>('local')
const cloudConsent = ref(false)
const warnings = ref<models.ai.Warning[]>([])
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)

const providerOptions = computed(() => [
  { label: t('ai.providerOllama'), value: 'ollama' },
  { label: t('ai.providerLMStudio'), value: 'lmstudio' },
  { label: t('ai.providerOpenAI'), value: 'openai-compatible' },
])
const concurrencyOptions = [
  { label: '1', value: 1 },
  { label: '2', value: 2 },
]

function presetFor(p: ProviderKind): string {
  if (p === 'lmstudio') return 'http://127.0.0.1:1234/v1'
  if (p === 'openai-compatible') return ''
  return 'http://127.0.0.1:11434/v1'
}

function applyView(v: models.ai.ConfigView) {
  form.enabled = !!v.enabled
  form.provider = (v.provider as ProviderKind) || 'ollama'
  form.baseUrl = v.baseUrl || ''
  form.model = v.model || ''
  form.temperature = typeof v.temperature === 'number' ? v.temperature : 0.1
  form.timeoutSec = v.timeoutSec || 120
  form.allowWrite = !!v.allowWrite
  form.allowInsecureTls = !!v.allowInsecureTls
  form.concurrency = v.concurrency || 1
  hasKey.value = !!v.hasKey
  endpoint.value = v.endpoint || ''
  scope.value = (v.scope as 'local' | 'lan' | 'cloud') || 'local'
  cloudConsent.value = !!v.cloudConsent
  warnings.value = v.warnings ?? []
  apiKey.value = ''
  clearKey.value = false
}

async function reload() {
  loading.value = true
  try {
    const v = await AIGetConfig()
    if (v) applyView(v)
  } catch (e) {
    message.error(String(e))
  } finally {
    loading.value = false
  }
}

watch(
  () => props.show,
  (v) => {
    if (v) void reload()
  },
)

// 切换 provider 时填充预设地址（OpenAI 兼容保留用户输入）
watch(
  () => form.provider,
  (p, old) => {
    if (!props.show || p === old) return
    if (p === 'openai-compatible') return
    form.baseUrl = presetFor(p)
  },
)

function buildConfig(): models.ai.Config {
  return new models.ai.Config({
    enabled: form.enabled,
    provider: form.provider,
    baseUrl: form.baseUrl.trim(),
    model: form.model.trim(),
    temperature: form.temperature,
    timeoutSec: form.timeoutSec,
    allowWrite: form.allowWrite,
    allowInsecureTls: form.allowInsecureTls,
    concurrency: form.concurrency,
  })
}

async function testConnection() {
  if (!form.model.trim()) {
    message.warning(t('ai.error.model_required'))
    return
  }
  testing.value = true
  try {
    await AITestConnection(buildConfig(), apiKey.value)
    message.success(t('ai.testOk'))
  } catch (e) {
    message.error(errorText(e))
  } finally {
    testing.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await AISaveConfig(buildConfig(), apiKey.value, clearKey.value)
    message.success(t('ai.saved'))
    emit('saved')
    await reload()
    showModal.value = false
  } catch (e) {
    message.error(errorText(e))
  } finally {
    saving.value = false
  }
}

async function removeKey() {
  try {
    await AIDeleteKey()
    message.success(t('ai.keyCleared'))
    hasKey.value = false
    clearKey.value = false
  } catch (e) {
    message.error(errorText(e))
  }
}

async function clearConsents() {
  try {
    await AIClearConsents()
    message.success(t('ai.consentsCleared'))
    await reload()
  } catch (e) {
    message.error(errorText(e))
  }
}

// 绑定错误形如 "code: message"；优先用 i18n 文案，未知 code 回退通用提示
function errorText(e: unknown): string {
  const raw = String(e ?? '')
  const m = raw.match(/^([a-z_]+):\s*(.*)$/s)
  if (m) {
    const key = `ai.error.${m[1]}`
    const translated = t(key)
    const head = translated !== key ? translated : m[2] || t('ai.error.generic')
    return m[2] && !head.includes(m[2]) ? `${head} · ${m[2]}` : head
  }
  return raw || t('ai.error.generic')
}

function scopeLabel(s: 'local' | 'lan' | 'cloud'): string {
  if (s === 'lan') return t('ai.scopeLan')
  if (s === 'cloud') return t('ai.scopeCloud')
  return t('ai.scopeLocal')
}

function warningText(w: models.ai.Warning): string {
  const key = `ai.warning.${w.code}`
  const translated = t(key, { detail: w.detail ?? '' })
  return translated === key ? `${w.code}${w.detail ? ': ' + w.detail : ''}` : translated
}
</script>

<template>
  <n-modal v-model:show="showModal" preset="card" :title="t('ai.settingsTitle')" style="width: 560px">
    <n-form label-placement="left" label-width="120" :disabled="loading">
      <n-form-item :label="t('ai.enabled')">
        <n-switch v-model:value="form.enabled" />
      </n-form-item>
      <n-form-item :label="t('ai.provider')">
        <n-select v-model:value="form.provider" :options="providerOptions" />
      </n-form-item>
      <n-form-item :label="t('ai.baseUrl')">
        <n-input v-model:value="form.baseUrl" :placeholder="t('ai.baseUrlPlaceholder')" />
      </n-form-item>
      <n-form-item :label="t('ai.model')">
        <n-input v-model:value="form.model" :placeholder="t('ai.modelPlaceholder')" />
      </n-form-item>
      <n-form-item :label="t('ai.apiKey')">
        <div class="ai-settings-key">
          <n-input
            v-model:value="apiKey"
            type="password"
            show-password-on="click"
            :placeholder="hasKey ? t('ai.apiKeyStored') : t('ai.apiKeyPlaceholder')"
          />
          <n-button v-if="hasKey" size="small" quaternary type="error" @click="removeKey">
            {{ t('ai.clearKey') }}
          </n-button>
        </div>
      </n-form-item>
      <div class="ai-settings-note">{{ t('ai.keychainNote') }}</div>

      <div class="ai-settings-grid">
        <n-form-item :label="t('ai.temperature')">
          <n-input-number v-model:value="form.temperature" :min="0" :max="2" :step="0.1" />
        </n-form-item>
        <n-form-item :label="t('ai.timeout')">
          <n-input-number v-model:value="form.timeoutSec" :min="15" :max="180" />
        </n-form-item>
        <n-form-item :label="t('ai.concurrency')">
          <n-select v-model:value="form.concurrency" :options="concurrencyOptions" />
        </n-form-item>
      </div>
      <div class="ai-settings-note">{{ t('ai.timeoutHint') }} · {{ t('ai.concurrencyHint') }}</div>

      <n-form-item :label="t('ai.allowWrite')">
        <div class="ai-settings-col">
          <n-switch v-model:value="form.allowWrite" />
          <span class="ai-settings-hint">{{ t('ai.allowWriteHint') }}</span>
        </div>
      </n-form-item>
      <n-form-item :label="t('ai.allowInsecureTLS')">
        <div class="ai-settings-col">
          <n-switch v-model:value="form.allowInsecureTls" />
          <span class="ai-settings-hint">{{ t('ai.allowInsecureTLSHint') }}</span>
        </div>
      </n-form-item>

      <div v-if="endpoint" class="ai-settings-endpoint">
        <span class="ai-settings-scope" :class="`scope-${scope}`">{{ scopeLabel(scope) }}</span>
        <span class="mono">{{ endpoint }}</span>
        <span class="ai-settings-hint">
          {{ scope === 'cloud' && cloudConsent ? t('ai.consentGranted') : '' }}
          {{ scope === 'cloud' && !cloudConsent ? t('ai.consentNone') : '' }}
        </span>
        <n-button v-if="scope === 'cloud' && cloudConsent" size="tiny" quaternary @click="clearConsents">
          {{ t('ai.clearConsents') }}
        </n-button>
      </div>

      <n-alert v-for="(w, i) in warnings" :key="i" type="warning" :show-icon="false" class="ai-settings-warning">
        {{ warningText(w) }}
      </n-alert>
    </n-form>

    <div class="ai-settings-actions">
      <n-button :loading="testing" @click="testConnection">{{ testing ? t('ai.testing') : t('ai.test') }}</n-button>
      <span class="spacer" />
      <n-button quaternary @click="showModal = false">{{ t('ai.cancel') }}</n-button>
      <n-button type="primary" :loading="saving" @click="save">{{ t('ai.save') }}</n-button>
    </div>
  </n-modal>
</template>

<style scoped>
.ai-settings-key {
  display: flex;
  gap: 8px;
  width: 100%;
  align-items: center;
}
.ai-settings-key > :first-child {
  flex: 1;
}
.ai-settings-note {
  margin: -6px 0 10px 120px;
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1.5;
}
.ai-settings-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
.ai-settings-col {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: flex-start;
}
.ai-settings-hint {
  font-size: 11px;
  color: var(--text-muted);
}
.ai-settings-endpoint {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  margin: 4px 0 8px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  font-size: 11px;
  color: var(--text-secondary);
  flex-wrap: wrap;
}
.ai-settings-scope {
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 11px;
  border: 1px solid var(--border);
}
.ai-settings-scope.scope-local {
  color: var(--accent);
  border-color: var(--accent);
}
.ai-settings-scope.scope-lan {
  color: var(--warning);
  border-color: var(--warning);
}
.ai-settings-scope.scope-cloud {
  color: var(--danger);
  border-color: var(--danger);
}
.ai-settings-warning {
  margin-bottom: 8px;
}
.ai-settings-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}
.spacer {
  flex: 1;
}
.mono {
  font-family: var(--font-mono);
}
</style>
