import { ref } from 'vue'

// 主视图切换：终端 / 数据库（全局单例 ref，组件间共享）
export type MainView = 'terminal' | 'db'

export const mainView = ref<MainView>('terminal')
