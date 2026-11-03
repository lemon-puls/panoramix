<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { NButton, NTabs, NTabPane } from 'naive-ui'
import { useTerminalsStore } from '../stores/terminals'
import { useFilesStore } from '../stores/files'
import { useMonitorPanelStore } from '../stores/monitorPanel'
import TerminalView from './TerminalView.vue'

// 终端多标签 + 分屏：display-directive="show" 保证切走标签不销毁 xterm 实例
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

function tabLabel(tabId: string): string {
  const tab = store.tabs.find((x) => x.id === tabId)
  if (!tab) return ''
  return tab.panes.map((p) => p.hostName).join(' | ')
}
</script>

<template>
  <n-tabs
    type="card"
    size="small"
    :value="store.activeTabId ?? undefined"
    style="height: 100%"
    @update:value="(v: string) => store.setActiveTab(v)"
    @close="(name: string | number) => store.closeTab(String(name))"
  >
    <n-tab-pane
      v-for="tab in store.tabs"
      :key="tab.id"
      :name="tab.id"
      :closable="true"
      display-directive="show"
      style="padding: 0"
    >
      <template #tab>
        <span class="tab-label" :class="tab.panes.every((p) => p.state === 'closed') ? 'closed' : ''">
          {{ tabLabel(tab.id) }}
        </span>
      </template>

      <div class="panes" :class="tab.direction">
        <div
          v-for="pane in tab.panes"
          :key="pane.sessionId"
          class="pane"
          :class="{ active: pane.sessionId === tab.activePaneId && tab.panes.length > 1 }"
          @mousedown="store.setActivePane(tab.id, pane.sessionId)"
        >
          <div v-if="tab.panes.length > 1" class="pane-bar">
            <span class="pane-name">{{ pane.hostName }}</span>
            <span class="pane-actions">
              <button class="mini" :title="t('terminal.split')" @click="store.toggleDirection(tab.id)">
                ⇅/⇄
              </button>
              <button class="mini" :title="t('terminal.closePane')" @click="store.closePane(tab.id, pane.sessionId)">
                ×
              </button>
            </span>
          </div>
          <TerminalView :tab-id="tab.id" :pane="pane" />
        </div>
      </div>
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
/* n-tabs 内容区撑满，供分屏 flex 布局使用 */
:deep(.n-tabs-pane-wrapper),
:deep(.n-tab-pane) {
  height: 100%;
}
:deep(.n-tabs) {
  height: 100%;
}
.tab-label {
  font-size: 12px;
}
.tab-label.closed {
  opacity: 0.45; /* 断线标签置灰（M2 验收项） */
}
.panel-toggles {
  display: inline-flex;
  gap: 4px;
  margin-right: 8px;
}

.panes {
  display: flex;
  height: 100%;
  min-height: 0;
}
.panes.row {
  flex-direction: row;
}
.panes.column {
  flex-direction: column;
}
.pane {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  position: relative;
}
.panes.row .pane + .pane {
  border-left: 1px solid var(--border);
}
.panes.column .pane + .pane {
  border-top: 1px solid var(--border);
}
.pane.active::after {
  content: '';
  position: absolute;
  inset: 0;
  border: 1px solid rgba(94, 234, 212, 0.4);
  pointer-events: none;
}
.pane-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  font-size: 11px;
  background: rgba(255, 255, 255, 0.04);
  color: rgba(255, 255, 255, 0.6);
}
.pane-actions {
  display: inline-flex;
  gap: 2px;
}
.mini {
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.6);
  cursor: pointer;
  font-size: 11px;
  padding: 0 4px;
}
.mini:hover {
  color: #fff;
}
</style>
