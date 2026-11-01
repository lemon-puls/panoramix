import { ref } from 'vue'
import { defineStore } from 'pinia'

// 右侧监控面板开关（与文件面板互斥，开关逻辑在 TerminalTabs）
export const useMonitorPanelStore = defineStore('monitorPanel', () => {
  const showPanel = ref(false)
  return { showPanel }
})
