<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  darkTheme,
  dateZhCN,
  NConfigProvider,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NTag,
  zhCN,
} from 'naive-ui'
import { Ping } from '../wailsjs/go/app/Service'

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
    <n-layout has-sider style="height: 100%">
      <n-layout-sider bordered :width="264">
        <div class="sider-title">{{ t('app.title') }}</div>
        <div class="placeholder">{{ t('app.placeholder') }}</div>
      </n-layout-sider>
      <n-layout-content content-style="padding: 24px;">
        <h1 class="brand">Panoramix</h1>
        <p class="version">{{ t('app.version') }}</p>
        <n-tag :type="bridgeOk ? 'success' : 'warning'" size="large">
          {{ bridgeOk ? t('app.bridgeOk') : t('app.bridgeFail') }}
        </n-tag>
      </n-layout-content>
    </n-layout>
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
</style>
