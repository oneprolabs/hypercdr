import type { DefaultTheme } from 'vitepress'

/**
 * 本地搜索（provider: 'local'）的多语言文案。
 *
 * 两点容易踩：
 * 1. 搜索插件读的是**站点级** themeConfig 里的 `search`（config.mts 顶层那个），
 *    不是各 locale 自己的 themeConfig，所以中英文文案必须在这里按 locale key 一起给出，
 *    否则英文页的搜索框会显示中文占位符。
 * 2. `provider: 'local'` 不能省——VitePress 靠它在构建时决定是否把本地搜索打包进来，
 *    写成纯 options 对象会导致搜索框整个消失。
 *
 * key 与 config.mts 的 locales 一致：root（默认中文）、en。
 */
export const siteSearch: NonNullable<DefaultTheme.Config['search']> = {
  provider: 'local',
  options: {
    detailedView: false,
    locales: {
      root: {
        translations: {
          button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
          modal: {
            displayDetails: '显示详细列表',
            backButtonTitle: '关闭搜索',
            noResultsText: '未找到相关内容',
            resetButtonTitle: '清除查询',
            footer: {
              selectText: '选择',
              selectKeyAriaLabel: '打开结果',
              navigateText: '切换',
              navigateUpKeyAriaLabel: '上一个结果',
              navigateDownKeyAriaLabel: '下一个结果',
              closeText: '关闭',
              closeKeyAriaLabel: '关闭搜索',
            },
          },
        },
      },
      en: {
        translations: {
          button: { buttonText: 'Search', buttonAriaLabel: 'Search documentation' },
          modal: {
            displayDetails: 'Display detailed list',
            backButtonTitle: 'Close search',
            noResultsText: 'No results found for',
            resetButtonTitle: 'Reset search',
            footer: {
              selectText: 'to select',
              selectKeyAriaLabel: 'Enter key',
              navigateText: 'to navigate',
              navigateUpKeyAriaLabel: 'Arrow up',
              navigateDownKeyAriaLabel: 'Arrow down',
              closeText: 'to close',
              closeKeyAriaLabel: 'Escape key',
            },
          },
        },
      },
    },
  },
}
