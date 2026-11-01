<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { NButton, NTabs, NTabPane } from 'naive-ui'
import { useTerminalsStore } from '../stores/terminals'
import { useFilesStore } from '../stores/files'
import { useMonitorPanelStore } from '../stores/monitorPanel'
import TerminalView from './TerminalView.vue'

// 终端多标签：display-directive="show" 保证切走标签不会销毁 xterm 实例
const { t } = useI18n()
const store = useTerminalsStore()
const files = useFilesStore()
const monitor = useMonitorPanelStore()

// 右侧面板互斥开关：同一时间只展开一个
function toggleFiles() {
  files.showPanel = !files.showPanel
  if (files.showPanel) monitor.showPanel = false
}

function toggleMonitor() {
  monitor.showPanel = !monitor.showPanel
  if (monitor.showPanel) files.showPanel = false
}
</script>

<template>
  <n-tabs
    type="card"
    size="small"
    :value="store.activeId ?? undefined"
    style="height: 100%"
    @update:value="(v: string) => store.setActive(v)"
    @close="(name: string | number) => store.closeTab(String(name))"
  >
    <n-tab-pane
      v-for="tab in store.tabs"
      :key="tab.sessionId"
      :name="tab.sessionId"
      :closable="true"
      display-directive="show"
      style="padding: 0"
    >
      <template #tab>
        <span class="tab-label" :class="tab.state">{{ tab.hostName }}</span>
      </template>
      <TerminalView :tab="tab" />
    </n-tab-pane>
    <template #suffix>
      <span class="panel-toggles">
        <n-button
          size="tiny"
          :type="files.showPanel ? 'primary' : 'default'"
          @click="toggleFiles"
        >
          {{ t('files.toggle') }}
        </n-button>
        <n-button
          size="tiny"
          :type="monitor.showPanel ? 'primary' : 'default'"
          @click="toggleMonitor"
        >
          {{ t('monitor.toggle') }}
        </n-button>
      </span>
    </template>
  </n-tabs>
</template>

<style scoped>
.tab-label {
  font-size: 13px;
}
.tab-label.reconnecting,
.tab-label.closed {
  opacity: 0.45; /* 断线标签置灰（M2 验收项） */
}
.tab-label.error {
  color: #e88080;
}
.panel-toggles {
  display: inline-flex;
  gap: 4px;
  margin-right: 8px;
}
</style>
