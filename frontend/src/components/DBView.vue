<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import { NButton, NInput } from 'naive-ui'
import {
  DBQuery,
  DeleteDBConn,
  ListDBConns,
  RedisDeleteKey,
  RedisGetValue,
  RedisScanKeys,
  RedisSetString,
  RedisSetTTL,
  SaveDBConn,
  TestDBConn,
} from '../../wailsjs/go/app/Service'
import * as models from '../../wailsjs/go/models'
import DBConnModal from './DBConnModal.vue'

// 数据库视图：左侧连接列表，右侧按类型渲染 SQL 编辑器（MySQL）或键浏览器（Redis）
const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

const conns = ref<models.store.DBConn[]>([])
const activeId = ref<string>('')
const modal = ref(false)
const editing = ref<models.store.DBConn | null>(null)

const active = computed(() => conns.value.find((c) => c.id === activeId.value) ?? null)
const isRedis = computed(() => active.value?.kind === 'redis')

async function load() {
  conns.value = (await ListDBConns()) ?? []
  if (!activeId.value && conns.value.length > 0) activeId.value = conns.value[0].id
}
void load()

watch(activeId, () => {
  result.value = null
  redisKeys.value = []
  redisCursor.value = 0
  redisValue.value = null
  redisSelected.value = ''
  sqlText.value = ''
  if (active.value) void loadRedisFirstPage()
})

function openNew() {
  editing.value = null
  modal.value = true
}

function openEdit(c: models.store.DBConn) {
  editing.value = c
  modal.value = true
}

async function onSaved() {
  await load()
}

function askDelete(c: models.store.DBConn) {
  dialog.warning({
    title: t('tree.deleteTitle'),
    content: t('db.deleteConfirm', { name: c.name }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await DeleteDBConn(c.id)
        if (activeId.value === c.id) activeId.value = ''
        await load()
        message.success(t('common.saved'))
      } catch (e) {
        message.error(String(e))
      }
    },
  })
}

async function testConn(c: models.store.DBConn) {
  try {
    await TestDBConn(c.id)
    message.success(t('db.testOk'))
  } catch (e) {
    message.error(String(e))
  }
}

// ---------- MySQL ----------

const sqlText = ref('SELECT 1')
const result = ref<models.db.Result | null>(null)
const running = ref(false)

async function runQuery() {
  if (!active.value) return
  const text = sqlText.value.trim()
  if (!text) return
  running.value = true
  try {
    result.value = await DBQuery(active.value.id, text, 1000)
  } catch (e) {
    message.error(String(e))
    result.value = null
  } finally {
    running.value = false
  }
}

// ---------- Redis ----------

const redisPattern = ref('*')
const redisKeys = ref<models.db.KeyInfo[]>([])
const redisCursor = ref(0)
const redisLoading = ref(false)
const redisSelected = ref('')
const redisValue = ref<models.db.Value | null>(null)
const ttlInput = ref<number>(0)
const editValue = ref('')
const editingValue = ref(false)

async function loadRedisFirstPage() {
  if (!active.value || !isRedis.value) return
  redisKeys.value = []
  redisCursor.value = 0
  await loadRedisMore(true)
}

async function loadRedisMore(reset = false) {
  if (!active.value) return
  if (redisLoading.value) return
  if (!reset && redisCursor.value === 0 && redisKeys.value.length > 0) return
  redisLoading.value = true
  try {
    const cursor = reset ? 0 : redisCursor.value
    const page = await RedisScanKeys(active.value.id, cursor, redisPattern.value || '*', 200)
    if (reset) redisKeys.value = []
    redisKeys.value = [...redisKeys.value, ...(page.keys ?? [])]
    redisCursor.value = page.cursor
  } catch (e) {
    message.error(String(e))
  } finally {
    redisLoading.value = false
  }
}

const hasMoreKeys = computed(() => redisCursor.value !== 0)

async function selectKey(k: models.db.KeyInfo) {
  if (!active.value) return
  redisSelected.value = k.key
  try {
    redisValue.value = await RedisGetValue(active.value.id, k.key)
    ttlInput.value = redisValue.value.ttl > 0 ? redisValue.value.ttl : 0
    editValue.value = redisValue.value.str ?? ''
    editingValue.value = false
  } catch (e) {
    message.error(String(e))
    redisValue.value = null
  }
}

async function saveTTL() {
  if (!active.value || !redisValue.value) return
  try {
    await RedisSetTTL(active.value.id, redisValue.value.key, ttlInput.value)
    message.success(t('common.saved'))
    await refreshSelected()
  } catch (e) {
    message.error(String(e))
  }
}

async function deleteKey() {
  if (!active.value || !redisValue.value) return
  const key = redisValue.value.key
  try {
    await RedisDeleteKey(active.value.id, key)
    redisValue.value = null
    redisKeys.value = redisKeys.value.filter((k) => k.key !== key)
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  }
}

async function saveStringValue() {
  if (!active.value || !redisValue.value) return
  try {
    await RedisSetString(active.value.id, redisValue.value.key, editValue.value, true)
    message.success(t('common.saved'))
    editingValue.value = false
    await refreshSelected()
  } catch (e) {
    message.error(String(e))
  }
}

async function refreshSelected() {
  if (!active.value || !redisSelected.value) return
  redisValue.value = await RedisGetValue(active.value.id, redisSelected.value)
}

function fmtTTL(ttl: number): string {
  if (ttl === -2) return t('db.ttlGone')
  if (ttl < 0) return t('db.ttlNone')
  if (ttl < 60) return `${ttl}s`
  if (ttl < 3600) return `${Math.floor(ttl / 60)}m`
  return `${Math.floor(ttl / 3600)}h`
}

const typeColors: Record<string, string> = {
  string: '#63e2b7',
  hash: '#4a9ffd',
  list: '#f0a020',
  set: '#722ed1',
  zset: '#d4380d',
  stream: '#888',
}
</script>

<template>
  <div class="db-view">
    <!-- 左：连接列表 -->
    <div class="conns">
      <div class="conns-head">
        <span>{{ t('db.connections') }}</span>
        <n-button size="tiny" type="primary" @click="openNew">{{ t('db.newConn') }}</n-button>
      </div>
      <div class="conn-list">
        <div
          v-for="c in conns"
          :key="c.id"
          class="conn-item"
          :class="{ active: c.id === activeId }"
          @click="activeId = c.id"
        >
          <span class="kind" :class="c.kind">{{ c.kind === 'redis' ? 'R' : 'M' }}</span>
          <span class="conn-name" :title="`${c.addr}:${c.port}`">{{ c.name }}</span>
          <span class="conn-ops">
            <span class="op" :title="t('db.test')" @click.stop="testConn(c)">⚡</span>
            <span class="op" :title="t('menu.edit')" @click.stop="openEdit(c)">✎</span>
            <span class="op danger" :title="t('common.delete')" @click.stop="askDelete(c)">×</span>
          </span>
        </div>
        <div v-if="conns.length === 0" class="conn-empty">{{ t('db.noConns') }}</div>
      </div>
    </div>

    <!-- 右：工作区 -->
    <div class="workspace">
      <div v-if="!active" class="placeholder">{{ t('db.pickConn') }}</div>

      <!-- MySQL -->
      <template v-else-if="!isRedis">
        <div class="sql-bar">
          <span class="ws-title">{{ active.name }} · {{ active.addr }}:{{ active.port }}</span>
          <span class="spacer" />
          <n-button size="small" type="primary" :loading="running" @click="runQuery">
            {{ t('db.run') }} (Ctrl+Enter)
          </n-button>
        </div>
        <textarea
          v-model="sqlText"
          class="sql-editor"
          spellcheck="false"
          :placeholder="t('db.sqlPlaceholder')"
          @keydown.ctrl.enter.prevent="runQuery"
          @keydown.meta.enter.prevent="runQuery"
        />
        <div class="result">
          <div v-if="!result" class="result-empty">{{ t('db.noResult') }}</div>
          <template v-else-if="result.columns && result.columns.length > 0">
            <div class="result-meta">
              {{ result.rows?.length ?? 0 }} {{ t('db.rows') }} · {{ result.durationMs }}ms
              <span v-if="result.truncated" class="warn">· {{ t('db.truncated') }}</span>
            </div>
            <div class="grid-wrap">
              <table class="grid">
                <thead>
                  <tr>
                    <th class="rownum">#</th>
                    <th v-for="c in result.columns" :key="c">{{ c }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, ri) in result.rows" :key="ri">
                    <td class="rownum">{{ ri + 1 }}</td>
                    <td v-for="(cell, ci) in row" :key="ci" :class="{ null: cell === null }">
                      {{ cell === null ? 'NULL' : cell }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </template>
          <div v-else class="result-meta">
            {{ t('db.affected', { n: result.affected }) }} · {{ result.durationMs }}ms
          </div>
        </div>
      </template>

      <!-- Redis -->
      <template v-else>
        <div class="sql-bar">
          <span class="ws-title">{{ active.name }} · db{{ active.dbName || 0 }}</span>
          <n-input
            v-model:value="redisPattern"
            size="small"
            :placeholder="t('db.keyPattern')"
            style="width: 220px"
            @keyup.enter="loadRedisFirstPage"
          />
          <n-button size="small" @click="loadRedisFirstPage">{{ t('db.scan') }}</n-button>
        </div>
        <div class="redis-body">
          <div class="keys">
            <div
              v-for="k in redisKeys"
              :key="k.key"
              class="key-item"
              :class="{ active: k.key === redisSelected }"
              @click="selectKey(k)"
            >
              <span class="key-type" :style="{ color: typeColors[k.type] ?? '#888' }">{{ k.type }}</span>
              <span class="key-name" :title="k.key">{{ k.key }}</span>
              <span class="key-ttl">{{ fmtTTL(k.ttl) }}</span>
            </div>
            <div v-if="redisKeys.length === 0" class="conn-empty">{{ t('db.noKeys') }}</div>
            <n-button
              v-if="hasMoreKeys"
              size="tiny"
              quaternary
              :loading="redisLoading"
              class="more-btn"
              @click="loadRedisMore()"
            >
              {{ t('db.loadMore') }}
            </n-button>
          </div>

          <div class="value">
            <div v-if="!redisValue" class="result-empty">{{ t('db.pickKey') }}</div>
            <template v-else>
              <div class="value-head">
                <span class="value-type" :style="{ color: typeColors[redisValue.type] ?? '#888' }">
                  {{ redisValue.type }}
                </span>
                <span class="value-key">{{ redisValue.key }}</span>
                <span class="spacer" />
                <span class="ttl-label">{{ t('db.ttl') }}</span>
                <n-input-number v-model:value="ttlInput" size="tiny" :min="0" style="width: 100px" />
                <n-button size="tiny" @click="saveTTL">{{ t('common.save') }}</n-button>
                <n-button size="tiny" type="error" quaternary @click="deleteKey">
                  {{ t('common.delete') }}
                </n-button>
              </div>
              <div v-if="redisValue.truncated" class="warn-line">{{ t('db.truncatedValue', { n: redisValue.total }) }}</div>

              <!-- string -->
              <template v-if="redisValue.type === 'string'">
                <div v-if="!editingValue" class="str-value">
                  <pre>{{ redisValue.str }}</pre>
                  <n-button size="tiny" class="edit-btn" @click="editingValue = true">
                    {{ t('menu.edit') }}
                  </n-button>
                </div>
                <div v-else class="str-edit">
                  <textarea v-model="editValue" class="str-editor" spellcheck="false" />
                  <div class="str-actions">
                    <n-button size="tiny" @click="editingValue = false">{{ t('common.cancel') }}</n-button>
                    <n-button size="tiny" type="primary" @click="saveStringValue">{{ t('common.save') }}</n-button>
                  </div>
                </div>
              </template>

              <!-- hash -->
              <div v-else-if="redisValue.type === 'hash'" class="kv-table">
                <div v-for="(v, k) in redisValue.hash" :key="k" class="kv-row">
                  <span class="kv-key">{{ k }}</span>
                  <span class="kv-val">{{ v }}</span>
                </div>
              </div>

              <!-- list / set -->
              <div v-else-if="redisValue.type === 'list' || redisValue.type === 'set'" class="kv-table">
                <div v-for="(v, i) in (redisValue.list ?? redisValue.set ?? [])" :key="i" class="kv-row">
                  <span class="kv-idx">{{ i }}</span>
                  <span class="kv-val">{{ v }}</span>
                </div>
              </div>

              <!-- zset -->
              <div v-else-if="redisValue.type === 'zset'" class="kv-table">
                <div v-for="(m, i) in redisValue.zset" :key="i" class="kv-row">
                  <span class="kv-idx">{{ m.score }}</span>
                  <span class="kv-val">{{ m.member }}</span>
                </div>
              </div>

              <div v-else class="result-empty">{{ t('db.unsupportedType', { type: redisValue.type }) }}</div>
            </template>
          </div>
        </div>
      </template>
    </div>

    <DBConnModal v-model:show="modal" :conn="editing" @saved="onSaved" />
  </div>
</template>

<style scoped>
.db-view {
  display: flex;
  height: 100%;
  gap: 8px;
}
.conns {
  width: 190px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-right: 1px solid rgba(255, 255, 255, 0.08);
  padding-right: 8px;
}
.conns-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  opacity: 0.7;
}
.conn-list {
  flex: 1;
  overflow: auto;
}
.conn-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 6px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
}
.conn-item:hover {
  background: rgba(255, 255, 255, 0.06);
}
.conn-item.active {
  background: rgba(99, 226, 183, 0.12);
}
.kind {
  width: 16px;
  height: 16px;
  border-radius: 3px;
  font-size: 10px;
  line-height: 16px;
  text-align: center;
  background: rgba(74, 159, 253, 0.2);
  color: #4a9ffd;
}
.kind.redis {
  background: rgba(212, 56, 13, 0.2);
  color: #ff7a45;
}
.conn-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conn-ops {
  display: none;
  gap: 4px;
}
.conn-item:hover .conn-ops {
  display: inline-flex;
}
.op {
  opacity: 0.6;
  cursor: pointer;
}
.op:hover {
  opacity: 1;
}
.op.danger {
  color: #e88080;
}
.conn-empty {
  font-size: 11px;
  opacity: 0.4;
  padding: 8px 6px;
}
.workspace {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.placeholder {
  margin: auto;
  opacity: 0.4;
  font-size: 13px;
}
.sql-bar {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ws-title {
  font-size: 12px;
  opacity: 0.75;
}
.spacer {
  flex: 1;
}
.sql-editor {
  width: 100%;
  height: 140px;
  resize: vertical;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  color: #d4d4d4;
  font-family: Consolas, 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
  padding: 8px 10px;
  outline: none;
}
.sql-editor:focus {
  border-color: rgba(99, 226, 183, 0.4);
}
.result {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  overflow: hidden;
}
.result-empty {
  margin: auto;
  opacity: 0.4;
  font-size: 12px;
}
.result-meta {
  padding: 6px 10px;
  font-size: 11px;
  opacity: 0.6;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.warn {
  color: #f0a020;
}
.grid-wrap {
  flex: 1;
  overflow: auto;
}
.grid {
  border-collapse: collapse;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.grid th,
.grid td {
  padding: 3px 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  text-align: left;
  white-space: nowrap;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.grid th {
  position: sticky;
  top: 0;
  background: #202028;
  color: rgba(255, 255, 255, 0.7);
  font-weight: 500;
  z-index: 1;
}
.grid tbody tr:hover {
  background: rgba(255, 255, 255, 0.04);
}
.rownum {
  color: rgba(255, 255, 255, 0.3);
  text-align: right;
  width: 40px;
}
.grid td.null {
  color: rgba(255, 255, 255, 0.3);
  font-style: italic;
}
.redis-body {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 8px;
}
.keys {
  width: 300px;
  flex-shrink: 0;
  overflow: auto;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  padding: 4px;
}
.key-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 6px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
}
.key-item:hover {
  background: rgba(255, 255, 255, 0.06);
}
.key-item.active {
  background: rgba(99, 226, 183, 0.12);
}
.key-type {
  font-size: 9px;
  width: 34px;
  flex-shrink: 0;
}
.key-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.key-ttl {
  font-size: 10px;
  opacity: 0.45;
}
.more-btn {
  width: 100%;
}
.value {
  flex: 1;
  min-width: 0;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  padding: 8px;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.value-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.value-type {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.08);
}
.value-key {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 320px;
}
.ttl-label {
  font-size: 11px;
  opacity: 0.6;
}
.warn-line {
  font-size: 11px;
  color: #f0a020;
}
.str-value {
  position: relative;
  flex: 1;
}
.str-value pre {
  margin: 0;
  padding: 8px;
  background: rgba(255, 255, 255, 0.03);
  border-radius: 4px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.edit-btn {
  position: absolute;
  top: 6px;
  right: 6px;
}
.str-editor {
  width: 100%;
  height: 220px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  color: #d4d4d4;
  font-family: Consolas, monospace;
  font-size: 12px;
  padding: 8px;
  outline: none;
  resize: vertical;
}
.str-actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 6px;
}
.kv-table {
  display: flex;
  flex-direction: column;
}
.kv-row {
  display: flex;
  gap: 8px;
  padding: 2px 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  font-size: 12px;
}
.kv-key,
.kv-idx {
  width: 140px;
  flex-shrink: 0;
  color: rgba(255, 255, 255, 0.6);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.kv-val {
  flex: 1;
  word-break: break-all;
}
</style>
