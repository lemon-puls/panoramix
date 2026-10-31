import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'

// i18n 框架从第一天生效；v0.1 仅中文，en-US 从 v0.2 补（ROADMAP.md）
export const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages: { 'zh-CN': zhCN },
})
