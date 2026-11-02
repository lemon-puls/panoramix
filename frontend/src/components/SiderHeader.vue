<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NInputNumber, NPopover, NSelect, NSwitch } from 'naive-ui'
import { SUPPORTED_LOCALES, setLocale, type LocaleCode } from '../i18n'
import { mainView } from '../stores/view'
import {
  GetTransferSpeedLimit,
  IsCwdSyncEnabled,
  IsSessionLogEnabled,
  SetCwdSyncEnabled,
  SetSessionLogEnabled,
  SetTransferSpeedLimit,
} from '../../wailsjs/go/app/Service'

// 侧栏头部：应用标题 + 设置弹层（会话日志开关）。
// 必须渲染在 NMessageProvider 内部（useMessage 依赖注入）。
// 模块级 ref 需在 setup 内引用才会在模板中解包
const view = mainView

const { t } = useI18n()
const message = useMessage()

const logEnabled = ref(false)
const speedLimit = ref<number>(0)
const cwdSync = ref(false)
const locale = ref<LocaleCode>(currentLocale())

// 语言选项（模板不接受类型断言，这里转为普通数组）
const localeOptions = SUPPORTED_LOCALES.map((l) => ({ label: l.label, value: l.value }))

function currentLocale(): LocaleCode {
  try {
    const saved = localStorage.getItem('panoramix.locale')
    return saved === 'en-US' ? 'en-US' : 'zh-CN'
  } catch {
    return 'zh-CN'
  }
}

function onLocaleChange(v: LocaleCode) {
  setLocale(v)
}

onMounted(async () => {
  logEnabled.value = await IsSessionLogEnabled().catch(() => false)
  speedLimit.value = await GetTransferSpeedLimit().catch(() => 0)
  cwdSync.value = await IsCwdSyncEnabled().catch(() => false)
})

async function onCwdSyncChange(v: boolean) {
  try {
    await SetCwdSyncEnabled(v)
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  }
}

async function onSpeedLimitChange(v: number | null) {
  const kbps = v ?? 0
  try {
    await SetTransferSpeedLimit(kbps)
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  }
}

async function onToggleLog(on: boolean) {
  try {
    await SetSessionLogEnabled(on)
    message.success(t('common.saved'))
  } catch (e) {
    message.error(String(e))
  }
}
</script>

<template>
  <div class="sider-title">
    <span class="brand">{{ t('app.title') }}</span>
    <span class="view-switch">
      <button
        class="view-btn"
        :class="{ active: view === 'terminal' }"
        :title="t('view.terminal')"
        @click="view = 'terminal'"
      >⌨</button>
      <button
        class="view-btn"
        :class="{ active: view === 'db' }"
        :title="t('view.database')"
        @click="view = 'db'"
      >▤</button>
    </span>
    <n-popover trigger="click" placement="bottom-start">
      <template #trigger>
        <n-button quaternary size="tiny">⚙</n-button>
      </template>
      <div class="settings">
        <div class="settings-row">
          <span>{{ t('settings.sessionLog') }}</span>
          <n-switch v-model:value="logEnabled" size="small" @update:value="onToggleLog" />
        </div>
        <div class="settings-row">
          <span>{{ t('settings.language') }}</span>
          <n-select
            v-model:value="locale"
            size="small"
            :options="localeOptions"
            :consistent-menu-width="false"
            style="width: 120px"
            @update:value="onLocaleChange"
          />
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
  </div>
</template>

<style scoped>
.sider-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 12px 4px;
  font-weight: 600;
  font-size: 15px;
}
.brand {
  flex-shrink: 0;
}
.view-switch {
  display: inline-flex;
  gap: 2px;
  margin-left: auto;
  margin-right: 4px;
}
.view-btn {
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  font-size: 13px;
  padding: 1px 6px;
}
.view-btn:hover {
  color: #fff;
}
.view-btn.active {
  color: #63e2b7;
  border-color: rgba(99, 226, 183, 0.4);
  background: rgba(99, 226, 183, 0.08);
}
.settings {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 260px;
}
.settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  font-size: 13px;
}
</style>
