<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  darkTheme,
  dateZhCN,
  NConfigProvider,
  NDialogProvider,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NMessageProvider,
  NTag,
  zhCN,
} from 'naive-ui'
import { Ping } from '../wailsjs/go/app/Service'
import ConnectionTree from './components/ConnectionTree.vue'

const { t } = useI18n()
const bridgeOk = ref(false)

// M1 验收项：桥接自检，验证 Go ↔ WebView 通道
onMounted(async () => {
  try {
    bridgeOk.value = (await Ping()) === 'pong'
  } catch {
    bridgeOk.value = false
  }
})
</script>

<template>
  <n-config-provider :theme="darkTheme" :locale="zhCN" :date-locale="dateZhCN" style="height: 100vh">
    <n-message-provider>
      <n-dialog-provider>
        <n-layout has-sider style="height: 100%">
          <n-layout-sider bordered :width="264">
            <div class="sider-title">{{ t('app.title') }}</div>
            <ConnectionTree />
          </n-layout-sider>
          <n-layout-content content-style="padding: 24px;">
            <h1 class="brand">Panoramix</h1>
            <p class="version">{{ t('app.version') }}</p>
            <n-tag :type="bridgeOk ? 'success' : 'warning'" size="large">
              {{ bridgeOk ? t('app.bridgeOk') : t('app.bridgeFail') }}
            </n-tag>
            <p class="hint">{{ t('app.m1Hint') }}</p>
          </n-layout-content>
        </n-layout>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.brand {
  margin: 0 0 4px;
  font-size: 28px;
}
.version {
  margin: 0 0 16px;
  color: rgba(255, 255, 255, 0.5);
}
.hint {
  margin-top: 24px;
  color: rgba(255, 255, 255, 0.45);
  font-size: 13px;
}
</style>
