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
import { useTerminalsStore } from './stores/terminals'
import ConnectionTree from './components/ConnectionTree.vue'
import SiderHeader from './components/SiderHeader.vue'
import SnippetsPanel from './components/SnippetsPanel.vue'
import TerminalTabs from './components/TerminalTabs.vue'

const { t } = useI18n()
const terminals = useTerminalsStore()
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
            <div class="sider-inner">
              <SiderHeader />
              <ConnectionTree class="tree-area" />
              <SnippetsPanel class="snippets-area" />
            </div>
          </n-layout-sider>
          <n-layout-content content-style="padding: 12px; height: 100%">
            <TerminalTabs v-if="terminals.tabs.length > 0" />
            <div v-else class="hero">
              <h1 class="brand">Panoramix</h1>
              <p class="version">{{ t('app.version') }}</p>
              <n-tag :type="bridgeOk ? 'success' : 'warning'" size="large">
                {{ bridgeOk ? t('app.bridgeOk') : t('app.bridgeFail') }}
              </n-tag>
              <p class="hint">{{ t('app.m2Hint') }}</p>
            </div>
          </n-layout-content>
        </n-layout>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.sider-inner {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 2px);
}
.tree-area {
  flex: 1;
  min-height: 0;
}
.snippets-area {
  flex-shrink: 0;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}
.hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
}
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
