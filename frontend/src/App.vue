<script setup lang="ts">
import { computed, onErrorCaptured, onMounted, provide, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  darkTheme,
  dateEnUS,
  dateZhCN,
  enUS,
  NConfigProvider,
  NDialogProvider,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NMessageProvider,
  NTag,
  zhCN,
} from 'naive-ui'
import { i18n } from './i18n'
import { Ping } from '../wailsjs/go/app/Service'
import { useTerminalsStore } from './stores/terminals'
import { useFilesStore } from './stores/files'
import { useMonitorPanelStore } from './stores/monitorPanel'
import ConnectionTree from './components/ConnectionTree.vue'
import SiderHeader from './components/SiderHeader.vue'
import SnippetsPanel from './components/SnippetsPanel.vue'
import TerminalTabs from './components/TerminalTabs.vue'
import FilePanel from './components/FilePanel.vue'
import MonitorPanel from './components/MonitorPanel.vue'
import DBView from './components/DBView.vue'

// 主视图状态由 App 自己持有并通过 provide 下发给侧栏——
// 不依赖跨模块共享的 ref（HMR 下可能产生多个实例）
// ?view=db 仅在浏览器预览时使用（Wails 环境中无此参数）
const initialView =
  typeof window !== 'undefined' && new URLSearchParams(location.search).get('view') === 'db'
    ? 'db'
    : 'terminal'
const mainView = ref<'terminal' | 'db'>(initialView)
provide('mainView', mainView)

const isDBView = computed(() => mainView.value === 'db')

// 子组件渲染异常时把错误显示出来（否则整块区域静默空白，难以排查）
const renderError = ref('')
onErrorCaptured((err) => {
  renderError.value = err instanceof Error ? `${err.name}: ${err.message}` : String(err)
  console.error('[app] render error captured', err)
  return false
})

const { t } = useI18n()
const terminals = useTerminalsStore()
const files = useFilesStore()
const monitor = useMonitorPanelStore()
const bridgeOk = ref(false)

// Naive UI 组件语言跟随 vue-i18n 当前 locale
const isEn = computed(() => i18n.global.locale.value === 'en-US')
const naiveLocale = computed(() => (isEn.value ? enUS : zhCN))
const naiveDateLocale = computed(() => (isEn.value ? dateEnUS : dateZhCN))

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
  <n-config-provider
    :theme="darkTheme"
    :locale="naiveLocale"
    :date-locale="naiveDateLocale"
    style="height: 100vh"
  >
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
            <div class="content-row">
              <div class="term-area">
                <div v-if="renderError" class="render-error">{{ renderError }}</div>
                <DBView v-else-if="isDBView" class="db-area" />
                <TerminalTabs v-else-if="terminals.tabs.length > 0" />
                <div v-else class="hero">
                  <h1 class="brand">Panoramix</h1>
                  <p class="version">{{ t('app.version') }}</p>
                  <n-tag :type="bridgeOk ? 'success' : 'warning'" size="large">
                    {{ bridgeOk ? t('app.bridgeOk') : t('app.bridgeFail') }}
                  </n-tag>
                  <p class="hint">{{ t('app.m2Hint') }}</p>
                </div>
              </div>
              <template v-if="!isDBView">
                <FilePanel v-if="files.showPanel && terminals.tabs.length > 0" class="files-area dual" />
                <MonitorPanel v-else-if="monitor.showPanel && terminals.tabs.length > 0" class="files-area" />
              </template>
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
.content-row {
  display: flex;
  gap: 8px;
  height: 100%;
  min-height: 0;
}
.term-area {
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: 100%;
}
.db-area {
  height: 100%;
}
.render-error {
  margin: 20px;
  padding: 10px 12px;
  border-radius: 6px;
  background: rgba(232, 128, 128, 0.12);
  color: #e88080;
  font-size: 12px;
  white-space: pre-wrap;
}
.files-area {
  width: 380px;
  flex-shrink: 0;
  transition: width 0.2s;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
}
.files-area.dual {
  width: 640px;
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
