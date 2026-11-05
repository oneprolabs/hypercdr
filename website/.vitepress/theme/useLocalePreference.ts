/**
 * 语言偏好。
 *
 * 首屏的语言分流脚本（见 config.mts 的 localeRoutingScript）会读这个 key：
 * 一旦用户自己选过语言，就不再按浏览器语言自动跳转。
 *
 * 写入时机有两处：
 *   - 官网首页的语言下拉（LanguageMenu.vue），点击即写入；
 *   - 文档站导航栏的语言切换，走的是前端路由，由 Layout.vue 监听 localeIndex 写入。
 */

export const LOCALE_PREF_KEY = 'hcdr-locale'

/** 记下用户选的语言。隐私模式下 localStorage 可能不可写，写不进去就退化成不记忆。 */
export function rememberLocale(code: string) {
  try {
    localStorage.setItem(LOCALE_PREF_KEY, code)
  } catch {
    // 忽略：不影响当前导航，只是下次访问会重新按浏览器语言分流
  }
}
