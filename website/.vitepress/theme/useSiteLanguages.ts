import { computed } from 'vue'
import { useData } from 'vitepress'

/**
 * 首页语言切换的数据源。
 *
 * 直接读 VitePress 自己的 `site.locales`（即 config.mts 里的 locales 配置），
 * 而不是在文案里再手写一份语言清单——以后加语言只要在 config.mts 里加一项，
 * 首页头部的切换器会自动跟着变，不会漏改。
 *
 * 文档站的切换器由主题内置的 VPNavBarTranslations 渲染，用的是同一份数据。
 */
export interface SiteLanguageLink {
  /** locale 的 key，root 表示默认语言 */
  code: string
  /** 段控里显示的短标签 */
  label: string
  href: string
  /** 给 <a hreflang> 用 */
  hreflang: string
  /** hover 提示与读屏文本，用 locale 的完整 label */
  title: string
  current: boolean
}

/** 短标签：中文用「中」，其余用语言主标记的大写（en-US → EN）。 */
const SHORT_LABELS: Record<string, string> = { zh: '中' }
const FALLBACK_CODE = 'EN'

export function useSiteLanguages() {
  const { site, localeIndex } = useData()

  return computed<SiteLanguageLink[]>(() =>
    Object.entries(site.value.locales).map(([key, locale]) => {
      const primary = (locale.lang ?? key).split('-')[0].toLowerCase()
      return {
        code: key,
        label: SHORT_LABELS[primary] ?? primary.toUpperCase() ?? FALLBACK_CODE,
        href: locale.link ?? (key === 'root' ? '/' : `/${key}/`),
        hreflang: locale.lang ?? key,
        title: locale.label ?? key,
        current: key === localeIndex.value,
      }
    }),
  )
}
