import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'
import enUS from './en-US'

// 语言偏好存 localStorage（纯 UI 偏好，不涉及凭据）
const STORAGE_KEY = 'panoramix.locale'

export const SUPPORTED_LOCALES = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en-US', label: 'English' },
] as const

export type LocaleCode = (typeof SUPPORTED_LOCALES)[number]['value']

function initialLocale(): LocaleCode {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'zh-CN' || saved === 'en-US') return saved
    // 首次启动跟随系统语言（中文环境用中文，其余英文）
    if (typeof navigator !== 'undefined' && navigator.language.toLowerCase().startsWith('zh')) {
      return 'zh-CN'
    }
  } catch {
    // localStorage 不可用时回退中文
  }
  return 'zh-CN'
}

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale(),
  fallbackLocale: 'zh-CN',
  messages: { 'zh-CN': zhCN, 'en-US': enUS },
})

// setLocale 切换语言并持久化
export function setLocale(code: LocaleCode) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(i18n.global.locale as any).value = code
  try {
    localStorage.setItem(STORAGE_KEY, code)
  } catch {
    // 忽略持久化失败
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = code
  }
}
