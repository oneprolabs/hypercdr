import type { DefaultTheme } from 'vitepress'

/**
 * 英文导航与侧边栏，与 zh.ts 同构：分组、顺序、层级一一对应，只是文案换成英文。
 *
 * 英文内容目录（en/docs/）的页面树与中文（zh/docs/）完全镜像，
 * 因此 VitePress 内置的「切语言保留当前页」可以直接生效，不需要任何映射表。
 * 新增中文页时，记得在 en/docs/ 下放同路径的英文页（未翻译就先放待翻译占位）。
 */

/** 快速开始：文档站入口，替代原来的文档总览 */
const gettingStarted: DefaultTheme.SidebarItem[] = [
  {
    text: 'Quick start',
    items: [
      { text: 'Quick start', link: '/en/docs/' },
      { text: 'Install HyperCDR', link: '/en/docs/getting-started/install' },
      { text: 'First verified recovery', link: '/en/docs/user-guide/first-use' },
    ],
  },
  {
    text: 'After installation',
    items: [
      { text: 'Install in detail', link: '/en/docs/deployment/install' },
      { text: 'Configuration reference', link: '/en/docs/deployment/configuration' },
      { text: 'Troubleshooting', link: '/en/docs/operations/troubleshooting' },
    ],
  },
]

/** 产品概览 */
const product: DefaultTheme.SidebarItem[] = [
  {
    text: 'Product',
    items: [
      { text: 'Product overview', link: '/en/docs/product/' },
      { text: 'Control plane architecture', link: '/en/docs/product/architecture' },
      { text: 'Core concepts', link: '/en/docs/product/concepts' },
    ],
  },
]

/** 安装部署 */
const deployment: DefaultTheme.SidebarItem[] = [
  {
    text: 'Deployment',
    items: [
      { text: 'Deployment planning', link: '/en/docs/deployment/' },
      { text: 'Install in detail', link: '/en/docs/deployment/install' },
      { text: 'Configuration reference', link: '/en/docs/deployment/configuration' },
    ],
  },
  {
    text: 'After installation',
    items: [
      { text: 'Configuration reference', link: '/en/docs/deployment/configuration' },
      { text: 'Change the platform access URL', link: '/en/docs/deployment/configuration#access-url' },
    ],
  },
]

/** 用户指南 */
const userGuide: DefaultTheme.SidebarItem[] = [
  {
    text: 'User guide',
    items: [
      { text: 'Start here', link: '/en/docs/user-guide/' },
      { text: 'First use: registration to verified recovery', link: '/en/docs/user-guide/first-use' },
      { text: 'Users, organizations and platform permissions', link: '/en/docs/user-guide/users-and-organizations' },
    ],
  },
]

/** 使用手册 */
const manual: DefaultTheme.SidebarItem[] = [
  {
    text: 'Operation manual',
    items: [
      { text: 'Manual overview', link: '/en/docs/manual/' },
      { text: 'Dashboard', link: '/en/docs/manual/dashboard' },
    ],
  },
  {
    text: 'Prepare resources',
    items: [
      { text: 'Cluster registration and management', link: '/en/docs/manual/clusters' },
      { text: 'Object storage', link: '/en/docs/manual/storage' },
      { text: 'Protection policies', link: '/en/docs/manual/policies' },
    ],
  },
  {
    text: 'Disaster recovery and lifecycle',
    items: [
      { text: 'Application disaster recovery', link: '/en/docs/manual/application-dr' },
      { text: 'Tasks and restore points', link: '/en/docs/manual/tasks' },
      { text: 'DR drills', link: '/en/docs/manual/drill' },
      { text: 'Formal takeover', link: '/en/docs/manual/takeover' },
      { text: 'Uninstall a cluster agent', link: '/en/docs/manual/agent-uninstall' },
    ],
  },
  {
    text: 'Platform settings',
    items: [
      { text: 'Email service', link: '/en/docs/manual/email-service' },
    ],
  },
]

/** 运维 */
const operations: DefaultTheme.SidebarItem[] = [
  {
    text: 'Administration',
    items: [
      { text: 'Administration overview', link: '/en/docs/operations/' },
      { text: 'Platform operations and upgrades', link: '/en/docs/operations/platform' },
      { text: 'Blue-green deployment and rollback', link: '/en/docs/operations/blue-green' },
      { text: 'Velero restore cache', link: '/en/docs/operations/restore-cache' },
    ],
  },
  {
    text: 'Maintenance and troubleshooting',
    items: [
      { text: 'Troubleshooting', link: '/en/docs/operations/troubleshooting' },
      { text: 'Diagnostic logs', link: '/en/docs/operations/logging' },
      { text: 'Build and release maintenance', link: '/en/docs/operations/release' },
    ],
  },
]

/** 参考 */
const reference: DefaultTheme.SidebarItem[] = [
  {
    text: 'Reference',
    items: [
      { text: 'Reference overview', link: '/en/docs/reference/' },
      { text: 'FAQ', link: '/en/docs/reference/faq' },
      { text: 'Glossary', link: '/en/docs/reference/glossary' },
      { text: 'Support and compatibility scope', link: '/en/docs/reference/support-scope' },
      { text: 'Limitations and security recommendations', link: '/en/docs/reference/limitations' },
    ],
  },
  {
    text: 'Platform specifics',
    items: [
      { text: 'Huawei CCE', link: '/en/docs/reference/cce' },
      { text: 'OpenShift and OADP', link: '/en/docs/reference/openshift' },
      { text: 'OpenShift POC validation checklist', link: '/en/docs/reference/openshift-checklist' },
    ],
  },
]

export const enThemeConfig: DefaultTheme.Config = {
  logo: { src: '/logo.svg', alt: 'HyperCDR' },
  siteTitle: 'HyperCDR',
  nav: [
    { text: 'Home', link: '/en/' },
    // 与 zh.ts 同构：文档站入口就是快速开始，不再提供文档总览页。
    { text: 'Quick start', link: '/en/docs/', activeMatch: '^/en/docs/(getting-started/)?$' },
    { text: 'Product', link: '/en/docs/product/', activeMatch: '^/en/docs/product/' },
    { text: 'Deployment', link: '/en/docs/deployment/', activeMatch: '^/en/docs/deployment/' },
    { text: 'User guide', link: '/en/docs/user-guide/', activeMatch: '^/en/docs/user-guide/' },
    { text: 'Manual', link: '/en/docs/manual/', activeMatch: '^/en/docs/manual/' },
    { text: 'Administration', link: '/en/docs/operations/', activeMatch: '^/en/docs/operations/' },
    { text: 'Reference', link: '/en/docs/reference/', activeMatch: '^/en/docs/reference/' },
  ],
  sidebar: {
    '/en/docs/product/': product,
    '/en/docs/deployment/': deployment,
    '/en/docs/user-guide/': userGuide,
    '/en/docs/manual/': manual,
    '/en/docs/operations/': operations,
    '/en/docs/reference/': reference,
    '/en/docs/getting-started/': gettingStarted,
    '/en/docs/': gettingStarted,
  },
  outline: { label: 'On this page', level: [2, 3] },
  docFooter: { prev: 'Previous', next: 'Next' },
  returnToTopLabel: 'Return to top',
  sidebarMenuLabel: 'Menu',
  langMenuLabel: 'Change language',
  skipToContentLabel: 'Skip to content',
  darkModeSwitchLabel: 'Appearance',
  darkModeSwitchTitle: 'Switch to dark theme',
  lightModeSwitchTitle: 'Switch to light theme',
  notFound: {
    title: 'Page not found',
    quote: 'This address has no matching content.',
    linkText: 'Back to documentation',
  },
  socialLinks: [{ icon: 'github', link: 'https://github.com/oneprolabs/hypercdr' }],
  footer: {
    message: 'Released under the Apache 2.0 License.',
    copyright: '© 2026 OneProLabs',
  },
}
