import { computed, type ComputedRef } from 'vue'
import { useData } from 'vitepress'
import { isHomeLocale, type HomeLocale } from './homeCopy'

/**
 * VitePress 把默认语言命名为 root。
 * 本站默认语言是中文，所以 root 对应文案表里的 zh。
 */
export function useSiteLocale(): ComputedRef<HomeLocale> {
  const { localeIndex } = useData()
  return computed(() => {
    const index = localeIndex.value === 'root' ? 'zh' : localeIndex.value
    return isHomeLocale(index) ? index : 'zh'
  })
}
