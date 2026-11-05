<script setup lang="ts">
import { computed, inject, onMounted, ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NInputNumber, NPopover, NSelect, NSwitch } from 'naive-ui'
import { SUPPORTED_LOCALES, setLocale, type LocaleCode } from '../i18n'
import {
  GetTransferSpeedLimit,
  IsCwdSyncEnabled,
  IsSessionLogEnabled,
  SetCwdSyncEnabled,
  SetSessionLogEnabled,
  SetTransferSpeedLimit,
} from '../../wailsjs/go/app/Service'

// 顶部栏：品牌 + 视图切换 + 设置
const { t } = useI18n()
const message = useMessage()

const mainView = inject<Ref<'terminal' | 'db'>>('mainView')
const isTerminalView = computed(() => mainView?.value === 'terminal')
const isDBView = computed(() => mainView?.value === 'db')

// DBView 注册的视图切换守卫：有未提交的数据修改时先确认
const viewGuard = inject<{ fn: (() => Promise<boolean>) | null } | null>('viewGuard', null)

async function switchView(v: 'terminal' | 'db') {
  if (!mainView || mainView.value === v) return
  if (viewGuard?.fn && !(await viewGuard.fn())) return
  mainView.value = v
}

const logEnabled = ref(false)
const cwdSync = ref(false)
const speedLimit = ref<number>(0)
const locale = ref<LocaleCode>(currentLocale())
const localeOptions = SUPPORTED_LOCALES.map((l) => ({ label: l.label, value: l.value }))

function currentLocale(): LocaleCode {
  try {
    return localStorage.getItem('panoramix.locale') === 'en-US' ? 'en-US' : 'zh-CN'
  } catch {
    return 'zh-CN'
  }
}

onMounted(async () => {
  logEnabled.value = await IsSessionLogEnabled().catch(() => false)
  cwdSync.value = await IsCwdSyncEnabled().catch(() => false)
  speedLimit.value = await GetTransferSpeedLimit().catch(() => 0)
})

async function onToggleLog(on: boolean) {
  try {
    await SetSessionLogEnabled(on)
  } catch (e) {
    message.error(String(e))
  }
}

async function onCwdSyncChange(on: boolean) {
  try {
    await SetCwdSyncEnabled(on)
  } catch (e) {
    message.error(String(e))
  }
}

async function onSpeedLimitChange(v: number | null) {
  try {
    await SetTransferSpeedLimit(v ?? 0)
  } catch (e) {
    message.error(String(e))
  }
}
</script>

<template>
  <header class="app-header">
    <div class="brand">
      <span class="logo">◆</span>
      <span class="brand-name">Panoramix</span>
    </div>

    <nav class="segmented">
      <button class="seg-btn" :class="{ active: isTerminalView }" @click="switchView('terminal')">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="4 17 10 11 4 5" /><line x1="12" y1="19" x2="20" y2="19" />
        </svg>
        {{ t('view.terminal') }}
      </button>
      <button class="seg-btn" :class="{ active: isDBView }" @click="switchView('db')">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <ellipse cx="12" cy="5" rx="9" ry="3" /><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" />
          <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
        </svg>
        {{ t('view.database') }}
      </button>
    </nav>

    <div class="spacer" />

    <n-popover trigger="click" placement="bottom-end" :show-arrow="false" raw>
      <template #trigger>
        <button class="icon-btn" :title="t('settings.title')">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
        </button>
      </template>
      <div class="settings-panel">
        <div class="settings-title">{{ t('settings.title') }}</div>
        <div class="settings-row">
          <span>{{ t('settings.language') }}</span>
          <n-select
            v-model:value="locale"
            size="small"
            :options="localeOptions"
            style="width: 130px"
            @update:value="setLocale"
          />
        </div>
        <div class="settings-row">
          <span>{{ t('settings.sessionLog') }}</span>
          <n-switch v-model:value="logEnabled" size="small" @update:value="onToggleLog" />
        </div>
        <div class="settings-row">
          <span>{{ t('settings.cwdSync') }}</span>
          <n-switch v-model:value="cwdSync" size="small" @update:value="onCwdSyncChange" />
        </div>
        <div class="settings-row">
          <span>{{ t('transfers.speedLimit') }}</span>
          <n-input-number
            v-model:value="speedLimit"
            size="small"
            :min="0"
            :max="1048576"
            :step="128"
            style="width: 120px"
            @update:value="onSpeedLimitChange"
          />
        </div>
      </div>
    </n-popover>
  </header>
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  gap: var(--gap);
  height: 52px;
  padding: 0 20px;
  background: linear-gradient(180deg, #171d26 0%, #12161c 100%);
  border-bottom: 1px solid var(--border-subtle);
  flex-shrink: 0;
  -webkit-app-region: drag;
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  font-weight: 650;
  font-size: 15px;
  letter-spacing: 0.01em;
  color: var(--text-primary);
}
.logo {
  color: var(--accent);
  font-size: 14px;
  text-shadow: 0 0 12px rgba(94, 234, 212, 0.45);
}
.segmented {
  display: flex;
  gap: 2px;
  padding: 3px;
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: 8px;
  -webkit-app-region: no-drag;
}
.seg-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  font-family: inherit;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}
.seg-btn:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}
.seg-btn.active {
  background: var(--bg-elevated);
  color: var(--accent);
  box-shadow: inset 0 0 0 1px var(--border);
}
.spacer {
  flex: 1;
}
.icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  border-radius: 6px;
  cursor: pointer;
  -webkit-app-region: no-drag;
}
.icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.settings-panel {
  width: 300px;
  padding: 14px 16px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.settings-title {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
}
.settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
