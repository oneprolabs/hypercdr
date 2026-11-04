import { defineConfig } from 'vitepress'
import { zhThemeConfig } from './navigation/zh'
import { enThemeConfig } from './navigation/en'
import { siteSearch } from './navigation/search'

const ZH_DESCRIPTION =
  'HyperCDR 是面向 Kubernetes 与 OpenShift 的开源容器容灾平台，提供集群管理、保护策略、同步、恢复点、容灾演练和恢复任务。'
const EN_DESCRIPTION =
  'HyperCDR is an open source disaster recovery control plane for Kubernetes and OpenShift: cluster management, protection policies, restore points, drills and recovery tasks.'

/**
 * 整站是一个 VitePress 应用，没有 base 前缀。
 *
 * 中文是默认语言，内容放在 zh/ 下，由 rewrites 提到站点根路径：
 *   zh/index.md              → /
 *   zh/docs/product/index.md → /docs/product/
 * 英文保留语言前缀：
 *   en/index.md              → /en/
 *   en/docs/index.md         → /en/docs/
 *
 * 因此 Markdown 链接、nav、sidebar 里写的路径与最终 URL 完全一致（/docs/product/），
 * 不需要任何前缀换算。
 */
export default defineConfig({
  srcDir: '.',
  // 归档的旧实现（含历史 md）不参与构建，避免被当成内容页渲染出 /archive/...
  srcExclude: ['_archive/**'],
  lang: 'zh-Hans',
  title: 'HyperCDR',
  description: ZH_DESCRIPTION,
  cleanUrls: true,
  sitemap: { hostname: 'https://hypercdr.com' },
  rewrites: (id) => (id.startsWith('zh/') ? id.slice(3) : id),
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
    ['meta', { name: 'theme-color', content: '#335cff' }],
    ['meta', { name: 'viewport', content: 'width=device-width, initial-scale=1' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'HyperCDR' }],
    ['meta', { name: 'twitter:card', content: 'summary' }],
  ],
  markdown: {
    image: { lazyLoading: true },
    theme: { light: 'github-light', dark: 'github-dark' },
  },
  // 搜索插件读的是站点级 themeConfig，中英文文案都在 siteSearch 里
  themeConfig: { ...zhThemeConfig, search: siteSearch },
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-Hans',
      link: '/',
      title: 'HyperCDR',
      description: ZH_DESCRIPTION,
      themeConfig: zhThemeConfig,
      head: [
        ['meta', { property: 'og:title', content: 'HyperCDR — 面向 Kubernetes 与 OpenShift 的容器容灾平台' }],
        ['meta', { property: 'og:description', content: ZH_DESCRIPTION }],
      ],
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      title: 'HyperCDR',
      description: EN_DESCRIPTION,
      themeConfig: enThemeConfig,
      head: [
        ['meta', { property: 'og:title', content: 'HyperCDR — Disaster recovery control plane for Kubernetes' }],
        ['meta', { property: 'og:description', content: EN_DESCRIPTION }],
      ],
    },
  },
})
