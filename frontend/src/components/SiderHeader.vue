<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NInputNumber, NPopover, NSwitch } from 'naive-ui'
import {
  GetTransferSpeedLimit,
  IsSessionLogEnabled,
  SetSessionLogEnabled,
  SetTransferSpeedLimit,
} from '../../wailsjs/go/app/Service'

// 侧栏头部：应用标题 + 设置弹层（会话日志开关）。
// 必须渲染在 NMessageProvider 内部（useMessage 依赖注入）。
const { t } = useI18n()
const message = useMessage()

const logEnabled = ref(false)
const speedLimit = ref<number>(0)

onMounted(async () => {
  logEnabled.value = await IsSessionLogEnabled().catch(() => false)
  speedLimit.value = await GetTransferSpeedLimit().catch(() => 0)
})

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
    <span>{{ t('app.title') }}</span>
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
