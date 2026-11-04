<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NModal, NSelect } from 'naive-ui'
import {
  AICancel,
  AIGenerate,
  AIGetConfig,
  AIPrepare,
  DBListDatabases,
  DBListTables,
} from '../../wailsjs/go/app/Service'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import * as models from '../../wailsjs/go/models'

// AI 助手侧栏。
// 交互契约：
//   1) 每次生成都先 AIPrepare，后端缓存完整 payload 并返回预览；
//   2) 本机 endpoint 首次发送前需要一次确认，之后直接发送（发送的仍是同一份缓存）；
//   3) 非本机 endpoint 由后端按 endpoint 记录授权，首次需要显式确认；
//   4) 结果只能插入编辑器，永不自动执行。
const props = defineProps<{
  connId: string
  defaultDatabase: string
  sql: string
  lastRunSql: string
  lastError: string
  locale: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'insert', text: string): void
  (e: 'configure'): void
}>()

const { t } = useI18n()
const message = useMessage()

type AIMode = 'generate' | 'explain' | 'fix'
type Scope = 'local' | 'lan' | 'cloud'

const config = ref<models.ai.ConfigView | null>(null)
const configLoaded = ref(false)
const configError = ref('')

const mode = ref<AIMode>('generate')
const database = ref('')
const databases = ref<string[]>([])
const tables = ref<models.db.TableInfo[]>([])
const selectedTables = ref<string[]>([])
const question = ref('')
const sqlInput = ref('')
const errorInput = ref('')

const busy = ref(false)
const phase = ref('')
const elapsedMs = ref(0)
const streamingText = ref('')
const prepared = ref<models.ai.Prepared | null>(null)
const result = ref<models.ai.Result | null>(null)
const errorCode = ref('')
const errorDetail = ref('')
const showPayload = ref(false)
const pendingSend = ref(false) // 本机 endpoint 首次发送确认
const consentOpen = ref(false)
const writeConfirmOpen = ref(false)

let requestId = ''
let timer: number | undefined
let offStatus: (() => void) | undefined
let offDelta: (() => void) | undefined
let autoConfigured = false
// 取消 prepare/请求后递增；旧异步结果回来时据此丢弃，避免“停止后又发送”
let runToken = 0

const PREVIEW_SEEN_KEY = 'panoramix.ai.previewSeen'

function loadPreviewSeen(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem(PREVIEW_SEEN_KEY) ?? '{}') as Record<string, boolean>
  } catch {
    return {}
  }
}

const previewSeen = ref<Record<string, boolean>>(loadPreviewSeen())

function markPreviewSeen(endpoint: string) {
  previewSeen.value = { ...previewSeen.value, [endpoint]: true }
  try {
    localStorage.setItem(PREVIEW_SEEN_KEY, JSON.stringify(previewSeen.value))
  } catch {
    /* 忽略 */
  }
}

const configReady = computed(() => !!config.value?.enabled && !!config.value?.model)
const scope = computed<Scope>(() => (config.value?.scope as Scope) || 'local')
const scopeLabel = computed(() => {
  if (scope.value === 'lan') return t('ai.scopeLan')
  if (scope.value === 'cloud') return t('ai.scopeCloud')
  return t('ai.scopeLocal')
})
const providerTitle = computed(() => `${config.value?.endpoint ?? ''} · ${config.value?.model ?? ''}`)

const modeOptions = computed(() => [
  { value: 'generate' as AIMode, label: t('ai.mode.generate') },
  { value: 'explain' as AIMode, label: t('ai.mode.explain') },
  { value: 'fix' as AIMode, label: t('ai.mode.fix') },
])

const databaseOptions = computed(() =>
  databases.value.map((d) => ({ label: d, value: d })),
)

const tableOptions = computed(() =>
  tables.value.map((tbl) => ({
    label: tbl.isView ? `${tbl.name} (view)` : tbl.name,
    value: tbl.name,
    disabled: tbl.isView,
  })),
)

const canRun = computed(() => {
  if (!configReady.value || busy.value || !database.value) return false
  if (mode.value === 'generate') return question.value.trim().length > 0
  return sqlInput.value.trim().length > 0
})

const runLabel = computed(() => {
  if (mode.value === 'explain') return t('ai.runExplain')
  if (mode.value === 'fix') return t('ai.runFix')
  return t('ai.run')
})

const phaseText = computed(() => {
  if (!phase.value) return ''
  const key = `ai.phase.${phase.value}`
  const translated = t(key)
  return translated === key ? phase.value : translated
})

const elapsedText = computed(() => t('ai.elapsed', { n: (elapsedMs.value / 1000).toFixed(1) }))

const streamingCursor = computed(() => (busy.value && phase.value === 'request' ? '▍' : ''))

const activeWarnings = computed(() => result.value?.warnings ?? [])

// 疑似敏感内容：只展示“字段 + 类型”，不展示命中值
const sensitiveText = computed(() => {
  const hints = prepared.value?.sensitiveHints ?? []
  return hints
    .map((hint) => {
      const kindKey = `ai.sensitiveKind.${hint.kind}`
      const kind = t(kindKey)
      const fieldKey = `ai.sensitiveField.${hint.field}`
      const field = t(fieldKey)
      return `${field === fieldKey ? hint.field : field}: ${kind === kindKey ? hint.kind : kind}`
    })
    .join(', ')
})

async function reloadConfig() {
  try {
    const v = await AIGetConfig()
    config.value = v ?? null
    configError.value = ''
    configLoaded.value = true
    if (configReady.value && databases.value.length === 0) await loadDatabases()
    // 未配置时点击 AI：直接打开设置弹窗（已配置但被禁用则不打扰；每次打开面板最多一次）
    if (v && !v.enabled && !v.model && !autoConfigured) {
      autoConfigured = true
      emit('configure')
    }
  } catch (e) {
    configError.value = errorText(e)
    configLoaded.value = true
  }
}

async function loadDatabases() {
  try {
    const list = await DBListDatabases(props.connId)
    databases.value = list ?? []
    if (databases.value.length === 0) {
      database.value = ''
      tables.value = []
      return
    }
    if (!database.value || !databases.value.includes(database.value)) {
      // 连接没有默认库时必须让用户显式选择（评审要求），不要默认落到 information_schema
      const preferred = props.defaultDatabase
      database.value = preferred && databases.value.includes(preferred) ? preferred : ''
    }
    await loadTables()
  } catch (e) {
    message.error(String(e))
  }
}

async function loadTables() {
  if (!database.value) {
    tables.value = []
    selectedTables.value = []
    return
  }
  try {
    tables.value = (await DBListTables(props.connId, database.value)) ?? []
    // 保留仍存在的选择；视图不进入上下文
    const valid = new Set(tables.value.filter((x) => !x.isView).map((x) => x.name))
    selectedTables.value = selectedTables.value.filter((name) => valid.has(name))
  } catch (e) {
    message.error(String(e))
  }
}

watch(database, () => {
  selectedTables.value = []
  void loadTables()
})

watch(mode, (m) => {
  if (m === 'explain' || m === 'fix') {
    // 修复模式优先使用“上次实际执行”的 SQL：用户报错后可能已改过编辑器内容
    if (!sqlInput.value.trim()) {
      sqlInput.value = m === 'fix' ? props.lastRunSql || props.sql : props.sql
    }
    if (m === 'fix' && !errorInput.value.trim()) errorInput.value = props.lastError
  }
})

watch(
  () => props.defaultDatabase,
  () => {
    if (!database.value) void loadDatabases()
  },
)

function onTablesChange(value: string[]) {
  if (value.length > 8) {
    message.warning(t('ai.warning.too_many_tables'))
    selectedTables.value = value.slice(0, 8)
    return
  }
  selectedTables.value = value
}

function useEditorSQL() {
  sqlInput.value = props.sql
  if (mode.value === 'fix' && !errorInput.value.trim()) errorInput.value = props.lastError
}

function newRequestID(): string {
  // Wails 的 wails:// 在部分 WebView 不是 secure context，crypto.randomUUID 可能不存在
  const uuid = (globalThis.crypto as Crypto | undefined)?.randomUUID?.()
  if (typeof uuid === 'string' && uuid) return uuid
  return `ai-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`
}

function startTimer() {
  stopTimer()
  elapsedMs.value = 0
  const startedAt = Date.now()
  timer = window.setInterval(() => {
    elapsedMs.value = Date.now() - startedAt
  }, 200)
}

function stopTimer() {
  if (timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

function resetOutput() {
  streamingText.value = ''
  result.value = null
  errorCode.value = ''
  errorDetail.value = ''
  prepared.value = null
  showPayload.value = false
  pendingSend.value = false
  consentOpen.value = false
}

async function run() {
  if (!configReady.value) {
    emit('configure')
    return
  }
  if (busy.value) return
  if (mode.value === 'generate' && !question.value.trim()) {
    message.warning(t('ai.error.invalid_request'))
    return
  }
  if (mode.value !== 'generate' && !sqlInput.value.trim()) {
    message.warning(t('ai.error.invalid_request'))
    return
  }
  resetOutput()
  requestId = newRequestID()
  const token = ++runToken
  busy.value = true
  phase.value = 'context'
  startTimer()
  try {
    const req = new models.ai.Request({
      requestId,
      connId: props.connId,
      database: database.value,
      mode: mode.value,
      question: question.value,
      tables: [...selectedTables.value],
      sql: sqlInput.value,
      error: errorInput.value,
      locale: props.locale,
    })
    const p = await AIPrepare(req)
    if (token !== runToken) return // 已被停止/取消
    prepared.value = p
    // 用户没有手选表时，把后端自动匹配的表回显出来，避免“暗中发送”错觉
    if (selectedTables.value.length === 0) {
      selectedTables.value = p.tables.filter((x) => !x.excluded).map((x) => x.name)
    }
    showPayload.value = false
    if (p.needsConsent) {
      phase.value = 'preview'
      showPayload.value = true
      consentOpen.value = true
      return
    }
    if (!previewSeen.value[p.endpoint]) {
      phase.value = 'preview'
      showPayload.value = true
      pendingSend.value = true
      return
    }
    await send(false)
  } catch (e) {
    handleError(e)
  }
}

async function send(consent: boolean) {
  const p = prepared.value
  if (!p) return
  const token = runToken
  pendingSend.value = false
  consentOpen.value = false
  busy.value = true
  phase.value = 'request'
  streamingText.value = ''
  errorCode.value = ''
  errorDetail.value = ''
  try {
    const res = await AIGenerate(p.prepareId, consent)
    if (token !== runToken) return // 已停止
    result.value = res
    phase.value = 'done'
    markPreviewSeen(p.endpoint)
  } catch (e) {
    if (token !== runToken) return
    handleError(e)
  } finally {
    if (token === runToken) {
      busy.value = false
      stopTimer()
    }
  }
}

function cancelPending() {
  runToken++
  pendingSend.value = false
  consentOpen.value = false
  prepared.value = null
  phase.value = ''
  busy.value = false
  stopTimer()
}

async function stop() {
  // 等待确认/收集 schema 阶段没有可取消的模型请求：直接清理本地状态
  if (pendingSend.value || consentOpen.value || phase.value === 'context' || phase.value === 'preview') {
    cancelPending()
    message.info(t('ai.stopped'))
    return
  }
  if (!requestId) {
    cancelPending()
    return
  }
  try {
    await AICancel(requestId)
    message.info(t('ai.stopped'))
  } catch (e) {
    message.error(errorText(e))
  }
}

function handleError(e: unknown) {
  const raw = String(e ?? '')
  const m = raw.match(/^([a-z_]+):\s*(.*)$/s)
  errorCode.value = m ? m[1] : 'generic'
  errorDetail.value = m ? m[2] : raw
  phase.value = 'error'
  busy.value = false
  stopTimer()
  // 用户主动“停止”不是错误：stop() 已给出提示，不再弹红色错误
  if (errorCode.value === 'canceled') {
    phase.value = ''
    errorCode.value = ''
    errorDetail.value = ''
    return
  }
  message.error(errorText(e))
}

function errorText(e: unknown): string {
  const raw = String(e ?? '')
  const m = raw.match(/^([a-z_]+):\s*(.*)$/s)
  if (m) {
    const key = `ai.error.${m[1]}`
    const translated = t(key)
    const head = translated !== key ? translated : m[2] || t('ai.error.generic')
    // 保留后端 detail（例如 context_too_large 的逐表字符数）
    return m[2] && !head.includes(m[2]) ? `${head} · ${m[2]}` : head
  }
  return raw || t('ai.error.generic')
}

function warningText(w: models.ai.Warning): string {
  const key = `ai.warning.${w.code}`
  const translated = t(key, { detail: w.detail ?? '' })
  return translated === key ? `${w.code}${w.detail ? ': ' + w.detail : ''}` : translated
}

function blockText(reason?: string): string {
  if (!reason) return ''
  const key = `ai.block.${reason}`
  const translated = t(key)
  return translated === key ? reason : translated
}

const insertAllowed = computed(() => !!result.value?.insertAllowed && !!result.value?.sql)

function requestInsert() {
  if (!result.value?.sql) return
  if (!result.value.insertAllowed) {
    message.warning(blockText(result.value.insertBlockReason) || t('ai.insertDangerous'))
    return
  }
  if (result.value.isWrite) {
    writeConfirmOpen.value = true
    return
  }
  doInsert()
}

function doInsert() {
  const text = result.value?.sql
  writeConfirmOpen.value = false
  if (!text) return
  emit('insert', text)
}

async function copyResult() {
  const text = result.value?.sql || result.value?.explanation || ''
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('ai.copied'))
  } catch {
    /* 忽略 */
  }
}

function regenerate() {
  void run()
}

function onKeydown(ev: KeyboardEvent) {
  // AI 输入框内 Ctrl+Enter 生成；Ctrl+Alt+I 插入结果
  if (ev.ctrlKey && ev.altKey && ev.key.toLowerCase() === 'i') {
    ev.preventDefault()
    if (insertAllowed.value) requestInsert()
  }
}

onMounted(() => {
  offStatus = EventsOn('ai:status', (...args: unknown[]) => {
    const d = args[0] as { requestId?: string; phase?: string } | undefined
    if (!d || d.requestId !== requestId) return
    phase.value = d.phase ?? ''
  })
  offDelta = EventsOn('ai:delta', (...args: unknown[]) => {
    const d = args[0] as { requestId?: string; text?: string } | undefined
    if (!d || d.requestId !== requestId) return
    streamingText.value += d.text ?? ''
  })
  void reloadConfig()
})

onBeforeUnmount(() => {
  offStatus?.()
  offDelta?.()
  stopTimer()
  if (requestId) void AICancel(requestId)
})

defineExpose({ reloadConfig })

function scopeClass(): string {
  return `ai-scope-${scope.value}`
}
</script>

<template>
  <aside class="ai-panel" @keydown="onKeydown">
    <div class="ai-head">
      <span class="ai-title">{{ t('ai.title') }}</span>
      <span class="ai-badge" :class="scopeClass()" :title="providerTitle">{{ scopeLabel }}</span>
      <span class="ai-model" :title="providerTitle">{{ config?.model || '—' }}</span>
      <button class="ai-close" :title="t('common.close')" @click="emit('close')">×</button>
    </div>

    <div v-if="!configLoaded" class="ai-empty">{{ t('ai.loadingConfig') }}</div>
    <div v-else-if="configError" class="ai-error">{{ configError }}</div>

    <div v-else-if="!configReady" class="ai-setup">
      <div class="ai-setup-title">{{ t('ai.setupTitle') }}</div>
      <div class="ai-setup-hint">{{ t('ai.setupHint') }}</div>
      <n-button type="primary" size="small" @click="emit('configure')">{{ t('ai.openSettings') }}</n-button>
    </div>

    <div v-else class="ai-body">
      <div class="ai-modes">
        <button
          v-for="m in modeOptions"
          :key="m.value"
          class="ai-mode-btn"
          :class="{ active: mode === m.value }"
          @click="mode = m.value"
        >
          {{ m.label }}
        </button>
      </div>

      <div v-for="(w, i) in config?.warnings ?? []" :key="'cw-' + i" class="ai-warning">
        {{ warningText(w) }}
      </div>

      <div class="ai-field">
        <span class="ai-label">{{ t('ai.database') }}</span>
        <n-select
          v-model:value="database"
          size="small"
          :options="databaseOptions"
          :placeholder="t('ai.pickDatabase')"
        />
      </div>

      <div class="ai-field">
        <span class="ai-label">{{ t('ai.tables') }}</span>
        <n-select
          :value="selectedTables"
          size="small"
          multiple
          filterable
          :options="tableOptions"
          :placeholder="t('ai.pickTables')"
          :max-tag-count="2"
          @update:value="onTablesChange"
        />
        <span class="ai-hint">{{ t('ai.tablesHint') }}</span>
      </div>

      <div v-if="mode === 'generate'" class="ai-field">
        <span class="ai-label">{{ t('ai.question') }}</span>
        <textarea
          v-model="question"
          class="ai-textarea"
          rows="3"
          spellcheck="false"
          :placeholder="t('ai.questionPlaceholder')"
          @keydown.ctrl.enter.prevent="run"
          @keydown.meta.enter.prevent="run"
        />
      </div>

      <template v-else>
        <div class="ai-field">
          <span class="ai-label">
            {{ t('ai.sqlInput') }}
            <button class="ai-link" @click="useEditorSQL">{{ t('ai.useEditorSQL') }}</button>
          </span>
          <textarea
            v-model="sqlInput"
            class="ai-textarea"
            rows="4"
            spellcheck="false"
            :placeholder="t('ai.sqlPlaceholder')"
          />
        </div>
        <div v-if="mode === 'fix'" class="ai-field">
          <span class="ai-label">{{ t('ai.errorInput') }}</span>
          <textarea
            v-model="errorInput"
            class="ai-textarea"
            rows="2"
            spellcheck="false"
            :placeholder="t('ai.errorPlaceholder')"
          />
        </div>
      </template>

      <div class="ai-actions">
        <n-button v-if="!busy" type="primary" size="small" :disabled="!canRun" @click="run">
          {{ runLabel }}
        </n-button>
        <n-button v-else size="small" type="error" secondary @click="stop">{{ t('ai.stop') }}</n-button>
        <span v-if="phase" class="ai-phase">{{ phaseText }} · {{ elapsedText }}</span>
      </div>

      <!-- 首次向本机 endpoint 发送前的确认条 -->
      <div v-if="pendingSend && prepared" class="ai-confirm">
        <div class="ai-confirm-title">{{ t('ai.firstSendTitle') }}</div>
        <div class="ai-confirm-body">{{ t('ai.firstSendBody', { endpoint: prepared.endpoint }) }}</div>
        <div class="ai-confirm-actions">
          <n-button size="tiny" quaternary @click="cancelPending">{{ t('ai.cancel') }}</n-button>
          <n-button size="tiny" type="primary" @click="send(false)">{{ t('ai.send') }}</n-button>
        </div>
      </div>

      <!-- payload 预览：每次 prepare 后都展示，首次/云端确认前必须展开可见 -->
      <div v-if="prepared" class="ai-preview">
        <div class="ai-preview-head">
          <span class="ai-label">{{ t('ai.previewTitle') }}</span>
          <button class="ai-link" @click="showPayload = !showPayload">
            {{ showPayload ? t('ai.collapsePayload') : t('ai.expandPayload') }}
          </button>
        </div>
        <div class="ai-preview-meta">
          <span>{{ t('ai.payloadChars', { n: prepared.promptChars }) }}</span>
          <span>{{ t('ai.schemaChars', { n: prepared.schemaChars }) }}</span>
          <span class="mono">{{ t('ai.payloadHash') }} {{ prepared.payloadHash }}</span>
        </div>
        <div class="ai-chips">
          <span v-for="tbl in prepared.tables" :key="tbl.name" class="ai-chip" :class="{ excluded: tbl.excluded }">
            {{ tbl.name }}<span v-if="tbl.chars"> · {{ tbl.chars }}</span>
          </span>
        </div>
        <div v-if="prepared.sensitiveHints?.length" class="ai-sensitive">
          {{ t('ai.sensitiveWarn', { items: sensitiveText }) }}
        </div>
        <div v-for="(w, i) in prepared.warnings" :key="'pw-' + i" class="ai-warning">{{ warningText(w) }}</div>
        <pre v-if="showPayload" class="ai-payload">{{ prepared.payload }}</pre>
      </div>

      <div v-if="streamingText && busy" class="ai-stream">
        <div class="ai-label">{{ t('ai.result') }}</div>
        <pre class="ai-code">{{ streamingText }}{{ streamingCursor }}</pre>
      </div>

      <div v-if="errorCode" class="ai-error">
        <div>{{ errorText(`${errorCode}: ${errorDetail}`) }}</div>
        <div v-if="errorDetail" class="ai-error-detail">{{ errorDetail }}</div>
      </div>

      <div v-if="result" class="ai-result">
        <div v-if="result.sql" class="ai-result-block">
          <div class="ai-label">{{ t('ai.result') }}</div>
          <pre class="ai-code">{{ result.sql }}</pre>
        </div>
        <div v-if="result.explanation" class="ai-result-block">
          <div class="ai-label">{{ t('ai.explanation') }}</div>
          <div class="ai-explanation">{{ result.explanation }}</div>
        </div>
        <div v-if="activeWarnings.length" class="ai-result-block">
          <div class="ai-label">{{ t('ai.warnings') }}</div>
          <div v-for="(w, i) in activeWarnings" :key="'rw-' + i" class="ai-warning">{{ warningText(w) }}</div>
        </div>
        <div v-if="result.sql && !result.insertAllowed" class="ai-blocked">
          {{ blockText(result.insertBlockReason) }}
        </div>
        <div class="ai-result-actions">
          <n-button
            v-if="mode !== 'explain' && result.sql"
            size="small"
            type="primary"
            :disabled="!insertAllowed"
            @click="requestInsert"
          >
            {{ t('ai.insert') }}
          </n-button>
          <n-button size="small" @click="copyResult">{{ t('ai.copy') }}</n-button>
          <n-button size="small" quaternary @click="regenerate">{{ t('ai.regenerate') }}</n-button>
        </div>
      </div>

      <div v-if="!prepared && !result && !busy && !errorCode" class="ai-empty">
        {{ t('ai.noResult') }}
      </div>
    </div>

    <n-modal v-model:show="consentOpen" preset="card" :title="t('ai.consentTitle')" style="width: 520px">
      <div class="ai-consent-body">
        {{ t('ai.consentBody', { endpoint: prepared?.endpoint ?? '' }) }}
      </div>
      <div class="ai-preview-meta ai-consent-meta">
        <span class="mono">{{ t('ai.payloadHash') }} {{ prepared?.payloadHash }}</span>
        <span>{{ t('ai.payloadChars', { n: prepared?.promptChars ?? 0 }) }}</span>
      </div>
      <div v-if="prepared?.sensitiveHints?.length" class="ai-sensitive">
        {{ t('ai.sensitiveWarn', { items: sensitiveText }) }}
      </div>
      <pre v-if="showPayload" class="ai-payload">{{ prepared?.payload }}</pre>
      <button class="ai-link" @click="showPayload = !showPayload">
        {{ showPayload ? t('ai.collapsePayload') : t('ai.expandPayload') }}
      </button>
      <div class="ai-result-actions">
        <n-button size="small" quaternary @click="cancelPending">{{ t('ai.cancel') }}</n-button>
        <n-button size="small" type="primary" @click="send(true)">{{ t('ai.consentConfirm') }}</n-button>
      </div>
    </n-modal>

    <n-modal v-model:show="writeConfirmOpen" preset="card" :title="t('ai.warnings')" style="width: 480px">
      <div class="ai-consent-body">{{ t('ai.insertWriteConfirm') }}</div>
      <pre class="ai-code">{{ result?.sql }}</pre>
      <div class="ai-result-actions">
        <n-button size="small" quaternary @click="writeConfirmOpen = false">{{ t('ai.cancel') }}</n-button>
        <n-button size="small" type="primary" @click="doInsert">{{ t('ai.insert') }}</n-button>
      </div>
    </n-modal>
  </aside>
</template>

<style scoped>
.ai-panel {
  flex: 0 0 320px;
  min-width: 300px;
  max-width: 380px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--bg-panel);
  border-left: 1px solid var(--border-subtle);
  overflow: hidden;
}
.ai-head {
  display: flex;
  align-items: center;
  gap: var(--gap-sm);
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-subtle);
  flex-shrink: 0;
}
.ai-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
}
.ai-badge {
  padding: 1px 7px;
  border-radius: 10px;
  border: 1px solid var(--border);
  font-size: 10px;
  white-space: nowrap;
}
.ai-scope-local {
  color: var(--accent);
  border-color: var(--accent);
}
.ai-scope-lan {
  color: var(--warning);
  border-color: var(--warning);
}
.ai-scope-cloud {
  color: var(--danger);
  border-color: var(--danger);
}
.ai-model {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 10px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.ai-close {
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
  padding: 0 2px;
}
.ai-close:hover {
  color: var(--danger);
}
.ai-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.ai-modes {
  display: flex;
  gap: 2px;
  padding: 2px;
  background: var(--bg-app);
  border: 1px solid var(--border-subtle);
  border-radius: 7px;
}
.ai-mode-btn {
  flex: 1;
  padding: 4px 6px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 11px;
  font-family: inherit;
  cursor: pointer;
}
.ai-mode-btn:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}
.ai-mode-btn.active {
  background: var(--bg-elevated);
  color: var(--accent);
}
.ai-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ai-label {
  font-size: 11px;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}
.ai-hint {
  font-size: 10px;
  color: var(--text-muted);
}
.ai-textarea {
  width: 100%;
  resize: vertical;
  min-height: 56px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.5;
  padding: 6px 8px;
  outline: none;
  box-sizing: border-box;
}
.ai-textarea:focus {
  border-color: rgba(94, 234, 212, 0.45);
}
.ai-link {
  border: none;
  background: transparent;
  color: var(--info);
  font-size: 10px;
  cursor: pointer;
  padding: 0;
  font-family: inherit;
}
.ai-link:hover {
  text-decoration: underline;
}
.ai-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.ai-phase {
  font-size: 10px;
  color: var(--text-muted);
}
.ai-empty,
.ai-setup {
  padding: 16px 12px;
  font-size: 12px;
  color: var(--text-muted);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ai-setup-title {
  color: var(--text-primary);
  font-size: 13px;
}
.ai-setup-hint {
  line-height: 1.6;
}
.ai-error {
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: rgba(248, 113, 113, 0.1);
  color: var(--danger);
  font-size: 11px;
  white-space: pre-wrap;
}
.ai-error-detail {
  margin-top: 4px;
  color: var(--text-secondary);
  font-size: 10px;
}
.ai-confirm {
  padding: 8px 10px;
  border: 1px solid var(--warning);
  border-radius: var(--radius-sm);
  background: rgba(251, 191, 36, 0.08);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ai-confirm-title {
  font-size: 11px;
  color: var(--warning);
  font-weight: 600;
}
.ai-confirm-body {
  font-size: 10px;
  color: var(--text-secondary);
  line-height: 1.5;
  word-break: break-all;
}
.ai-confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
}
.ai-preview {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ai-preview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}
.ai-preview-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 10px;
  color: var(--text-muted);
}
.ai-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.ai-chip {
  padding: 1px 6px;
  border-radius: 9px;
  border: 1px solid var(--border);
  font-size: 10px;
  color: var(--text-secondary);
}
.ai-chip.excluded {
  color: var(--text-muted);
  text-decoration: line-through;
}
.ai-payload {
  max-height: 220px;
  overflow: auto;
  margin: 0;
  padding: 8px;
  background: var(--bg-app);
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: 10px;
  line-height: 1.5;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}
.ai-warning {
  font-size: 10px;
  color: var(--warning);
  line-height: 1.5;
}
/* 疑似敏感内容提示条：只提示，不自动删改 */
.ai-sensitive {
  padding: 6px 8px;
  border: 1px solid var(--warning);
  border-radius: var(--radius-sm);
  color: var(--warning);
  background: var(--bg-elevated);
  font-size: 10px;
  line-height: 1.5;
}
.ai-stream,
.ai-result {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ai-result-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ai-code {
  margin: 0;
  padding: 8px;
  background: var(--bg-app);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.5;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 260px;
  overflow: auto;
}
.ai-explanation {
  font-size: 11px;
  line-height: 1.6;
  color: var(--text-secondary);
  white-space: pre-wrap;
}
.ai-blocked {
  font-size: 10px;
  color: var(--danger);
}
.ai-result-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.ai-consent-body {
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-secondary);
  word-break: break-all;
}
.ai-consent-meta {
  margin: 8px 0;
}
.mono {
  font-family: var(--font-mono);
}
</style>
