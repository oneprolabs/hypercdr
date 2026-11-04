import type { DefaultTheme } from 'vitepress'

/**
 * 文档总览：给文档站首页一个覆盖全部板块的侧边栏。
 *
 * sidebar 的 key 与 item.link 都是站点绝对路径（与 URL 一致，例如 /docs/product/）。
 * 中文内容源码放在 zh/ 下，由 config.mts 的 rewrites 提到根路径，因此 URL 不带 zh 前缀。
 */
const docsOverview: DefaultTheme.SidebarItem[] = [
  {
    text: '文档',
    items: [
      { text: '文档总览', link: '/docs/' },
      { text: '产品概览', link: '/docs/product/' },
      { text: '安装部署', link: '/docs/deployment/' },
      { text: '用户指南', link: '/docs/user-guide/' },
      { text: '使用手册', link: '/docs/manual/' },
      { text: '运维', link: '/docs/operations/' },
      { text: '参考', link: '/docs/reference/' },
    ],
  },
]

/** 产品概览：先讲清是什么、由哪些部分组成、有哪些核心概念。 */
const product: DefaultTheme.SidebarItem[] = [
  {
    text: '产品概览',
    items: [
      { text: 'HyperCDR 是什么', link: '/docs/product/' },
      { text: '中控架构', link: '/docs/product/architecture' },
      { text: '核心概念', link: '/docs/product/concepts' },
    ],
  },
]

/** 安装部署：从环境准备到可登录的控制台。 */
const deployment: DefaultTheme.SidebarItem[] = [
  {
    text: '安装部署',
    items: [
      { text: '部署指南', link: '/docs/deployment/' },
      { text: '安装前准备', link: '/docs/deployment/install#prepare' },
      { text: '执行安装', link: '/docs/deployment/install#run' },
      { text: '检查安装结果', link: '/docs/deployment/install#verify' },
    ],
  },
  {
    text: '安装之后',
    items: [
      { text: '配置参考', link: '/docs/deployment/configuration' },
      { text: '更改平台访问地址', link: '/docs/deployment/configuration#access-url' },
    ],
  },
]

/** 用户指南：面向第一次使用平台的人。 */
const userGuide: DefaultTheme.SidebarItem[] = [
  {
    text: '用户指南',
    items: [
      { text: '从这里开始', link: '/docs/user-guide/' },
      { text: '首次使用：注册到恢复验证', link: '/docs/user-guide/first-use' },
      { text: '用户、组织与平台权限', link: '/docs/user-guide/users-and-organizations' },
    ],
  },
]

/** 使用手册：按功能模块组织的操作说明。 */
const manual: DefaultTheme.SidebarItem[] = [
  {
    text: '使用手册',
    items: [
      { text: '手册导览', link: '/docs/manual/' },
      { text: '仪表盘', link: '/docs/manual/dashboard' },
    ],
  },
  {
    text: '资源准备',
    items: [
      { text: '集群注册与管理', link: '/docs/manual/clusters' },
      { text: '对象存储', link: '/docs/manual/storage' },
      { text: '保护策略', link: '/docs/manual/policies' },
    ],
  },
  {
    text: '容灾与生命周期',
    items: [
      { text: '应用容灾保护', link: '/docs/manual/application-dr' },
      { text: '任务与恢复点', link: '/docs/manual/tasks' },
      { text: '容灾演练', link: '/docs/manual/drill' },
      { text: '卸载集群 Agent', link: '/docs/manual/agent-uninstall' },
    ],
  },
]

/** 运维：平台自身的运行、升级与排障。 */
const operations: DefaultTheme.SidebarItem[] = [
  {
    text: '运维',
    items: [
      { text: '运维总览', link: '/docs/operations/' },
      { text: '平台运维与升级', link: '/docs/operations/platform' },
      { text: '蓝绿部署与回滚', link: '/docs/operations/blue-green' },
      { text: 'Velero 恢复缓存', link: '/docs/operations/restore-cache' },
    ],
  },
  {
    text: '维护与排障',
    items: [
      { text: '故障排查', link: '/docs/operations/troubleshooting' },
      { text: '构建与发布维护', link: '/docs/operations/release' },
    ],
  },
]

/** 参考：边界、兼容性与术语。 */
const reference: DefaultTheme.SidebarItem[] = [
  {
    text: '参考',
    items: [
      { text: '参考总览', link: '/docs/reference/' },
      { text: '术语表', link: '/docs/reference/glossary' },
      { text: '支持与适配范围', link: '/docs/reference/support-scope' },
      { text: '限制与安全建议', link: '/docs/reference/limitations' },
    ],
  },
  {
    text: '平台专项',
    items: [
      { text: 'Huawei CCE', link: '/docs/reference/cce' },
      { text: 'OpenShift 与 OADP', link: '/docs/reference/openshift' },
      { text: 'OpenShift POC 验证清单', link: '/docs/reference/openshift-checklist' },
    ],
  },
]

export const zhThemeConfig: DefaultTheme.Config = {
  logo: { src: '/logo.svg', alt: 'HyperCDR' },
  siteTitle: 'HyperCDR',
  /**
   * 切语言时停留在同一页（VitePress 默认行为，这里显式写出来当作约定）。
   *
   * 前提是 en/docs/ 与 zh/docs/ 的页面树一一对应：/docs/product/architecture
   * 切到英文就是 /en/docs/product/architecture。所以**新增中文页时必须同时补一个
   * 同路径的英文页**（没翻译就先放待翻译占位），否则这个页面切语言会 404。
   */
  i18nRouting: true,
  nav: [
    { text: '官网', link: '/' },
    // 文档站首页（/docs/）在导航里必须有入口：各板块的侧边栏只列本板块的页面，
    // 进到 /docs/xxx 之后就再也回不到总览了。命名与侧边栏首项、404 页保持一致。
    { text: '文档总览', link: '/docs/', activeMatch: '^/docs/$' },
    { text: '产品概览', link: '/docs/product/', activeMatch: '^/docs/product/' },
    { text: '安装部署', link: '/docs/deployment/', activeMatch: '^/docs/deployment/' },
    { text: '用户指南', link: '/docs/user-guide/', activeMatch: '^/docs/user-guide/' },
    { text: '使用手册', link: '/docs/manual/', activeMatch: '^/docs/manual/' },
    { text: '运维', link: '/docs/operations/', activeMatch: '^/docs/operations/' },
    { text: '参考', link: '/docs/reference/', activeMatch: '^/docs/reference/' },
  ],
  sidebar: {
    '/docs/product/': product,
    '/docs/deployment/': deployment,
    '/docs/user-guide/': userGuide,
    '/docs/manual/': manual,
    '/docs/operations/': operations,
    '/docs/reference/': reference,
    '/docs/': docsOverview,
  },
  outline: { label: '本页内容', level: [2, 3] },
  docFooter: { prev: '上一页', next: '下一页' },
  returnToTopLabel: '返回顶部',
  sidebarMenuLabel: '目录',
  langMenuLabel: '切换语言',
  skipToContentLabel: '跳到正文',
  darkModeSwitchLabel: '外观',
  lightModeSwitchTitle: '切换为浅色模式',
  darkModeSwitchTitle: '切换为深色模式',
  notFound: {
    title: '页面不存在',
    quote: '这个地址没有对应内容，可以回到文档总览重新查找。',
    linkText: '返回文档总览',
  },
  socialLinks: [{ icon: 'github', link: 'https://github.com/oneprolabs/hypercdr' }],
  footer: {
    message: '基于 Apache 2.0 许可发布。',
    copyright: '© 2026 OneProLabs',
  },
}
