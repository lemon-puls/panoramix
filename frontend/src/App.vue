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
  NMessageProvider,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NTag,
  zhCN,
} from 'naive-ui'
import { i18n } from './i18n'
import { Ping } from '../wailsjs/go/app/Service'
import { useTerminalsStore } from './stores/terminals'
import { useFilesStore } from './stores/files'
import { useMonitorPanelStore } from './stores/monitorPanel'
import AppHeader from './components/AppHeader.vue'
import ConnectionTree from './components/ConnectionTree.vue'
import SnippetsPanel from './components/SnippetsPanel.vue'
import TerminalTabs from './components/TerminalTabs.vue'
import FilePanel from './components/FilePanel.vue'
import MonitorPanel from './components/MonitorPanel.vue'
import DBView from './components/DBView.vue'

const { t } = useI18n()
const terminals = useTerminalsStore()
const files = useFilesStore()
const monitor = useMonitorPanelStore()
const bridgeOk = ref(false)

// ?view=db 仅在浏览器预览时使用（Wails 环境中无此参数）
const initialView =
  typeof window !== 'undefined' && new URLSearchParams(location.search).get('view') === 'db'
    ? 'db'
    : 'terminal'
const mainView = ref<'terminal' | 'db'>(initialView)
provide('mainView', mainView)

const isDBView = computed(() => mainView.value === 'db')

const renderError = ref('')
onErrorCaptured((err) => {
  renderError.value = err instanceof Error ? `${err.name}: ${err.message}` : String(err)
  console.error('[app] render error captured', err)
  return false
})

// Naive UI 主题覆盖：与设计变量对齐，统一控件观感
const themeOverrides = {
  common: {
    primaryColor: '#5eead4',
    primaryColorHover: '#2dd4bf',
    primaryColorPressed: '#14b8a6',
    primaryColorSuppl: '#2dd4bf',
    bodyColor: '#10141a',
    cardColor: '#171c24',
    modalColor: '#171c24',
    popoverColor: '#1e242e',
    tableColor: '#171c24',
    inputColor: '#10141a',
    borderColor: 'rgba(255,255,255,0.12)',
    dividerColor: 'rgba(255,255,255,0.08)',
    textColorBase: '#e9edf3',
    textColor1: '#e9edf3',
    textColor2: '#a9b3c1',
    textColor3: '#74808f',
    borderRadius: '8px',
    fontSize: '13px',
  },
  Button: {
    colorPrimary: '#5eead4',
    colorHoverPrimary: '#2dd4bf',
    colorPressedPrimary: '#14b8a6',
    textColorPrimary: '#0b1014',
  },
  Input: {
    color: '#10141a',
    colorFocus: '#10141a',
    border: '1px solid rgba(255,255,255,0.12)',
    borderFocus: '1px solid rgba(94,234,212,0.5)',
  },
  Tabs: {
    tabColor: 'transparent',
  },
}

const isEn = computed(() => i18n.global.locale.value === 'en-US')
const naiveLocale = computed(() => (isEn.value ? enUS : zhCN))
const naiveDateLocale = computed(() => (isEn.value ? dateEnUS : dateZhCN))

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
    :theme-overrides="themeOverrides"
    style="height: 100vh"
  >
    <n-message-provider>
      <n-dialog-provider>
        <div class="app-shell">
          <AppHeader />

          <div class="app-body">
            <div v-if="renderError" class="render-error">{{ renderError }}</div>

            <!-- 数据库：全屏（不显示主机侧栏） -->
            <DBView v-else-if="isDBView" class="db-full" />

            <!-- 终端模式 -->
            <template v-else>
              <n-layout has-sider class="terminal-shell">
                <n-layout-sider bordered :width="264" class="side">
                  <div class="side-inner">
                    <ConnectionTree class="tree-area" />
                    <SnippetsPanel class="snippets-area" />
                  </div>
                </n-layout-sider>
                <n-layout-content class="main">
                  <div class="content-row">
                    <div class="term-area">
                      <TerminalTabs v-if="terminals.tabs.length > 0" />
                      <div v-else class="empty-state">
                        <div class="empty-icon">◆</div>
                        <h1 class="empty-title">Panoramix</h1>
                        <p class="empty-sub">{{ t('app.m2Hint') }}</p>
                        <n-tag :type="bridgeOk ? 'success' : 'warning'" size="small" :bordered="false">
                          {{ bridgeOk ? t('app.bridgeOk') : t('app.bridgeFail') }}
                        </n-tag>
                      </div>
                    </div>
                    <FilePanel v-if="files.showPanel && terminals.tabs.length > 0" class="side-panel dual" />
                    <MonitorPanel
                      v-else-if="monitor.showPanel && terminals.tabs.length > 0"
                      class="side-panel"
                    />
                  </div>
                </n-layout-content>
              </n-layout>
            </template>
          </div>
        </div>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.app-shell {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--bg-app);
}
.app-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.render-error {
  margin: var(--gap-lg);
  padding: 12px 14px;
  border-radius: var(--radius);
  background: rgba(248, 113, 113, 0.1);
  border: 1px solid rgba(248, 113, 113, 0.25);
  color: var(--danger);
  font-size: 12px;
  white-space: pre-wrap;
}
.db-full {
  flex: 1;
  min-height: 0;
  padding: var(--gap);
}
.terminal-shell {
  flex: 1;
  min-height: 0;
  height: 100%;
}
.side-inner {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-panel);
}
.tree-area {
  flex: 1;
  min-height: 0;
}
.snippets-area {
  flex-shrink: 0;
  border-top: 1px solid var(--border-subtle);
}
.main {
  background: var(--bg-app);
  padding: var(--gap);
  height: 100%;
}
.content-row {
  display: flex;
  gap: var(--gap);
  height: 100%;
  min-height: 0;
}
.term-area {
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: 100%;
}
.side-panel {
  width: 380px;
  flex-shrink: 0;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius);
  background: var(--bg-panel);
  overflow: hidden;
}
.side-panel.dual {
  width: 640px;
}
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 10px;
}
.empty-icon {
  font-size: 40px;
  color: var(--accent);
  opacity: 0.25;
}
.empty-title {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
  letter-spacing: 0.04em;
}
.empty-sub {
  margin: 0 0 6px;
  color: var(--text-muted);
  font-size: 12px;
}
</style>
