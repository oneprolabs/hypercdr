/**
 * 官网首页文案。zh 为默认语言（站点根路径），en 在 /en/。
 *
 * 新增语言时：在这里补一份文案表，并在 config.mts 的 locales 里加一项。
 */

export type HomeLocale = 'zh' | 'en'

export function isHomeLocale(value: string): value is HomeLocale {
  return value === 'zh' || value === 'en'
}

export interface NavItem {
  label: string
  href: string
  external?: boolean
}

export interface CardCopy {
  icon?: string
  index?: string
  title: string
  description: string
}

export interface StepCopy {
  title: string
  description: string
}

export interface HomeCopy {
  nav: NavItem[]
  header: {
    brandAria: string
    navAria: string
    menuLabel: string
    openMenu: string
    closeMenu: string
    githubLabel: string
    githubAria: string
    cta: string
    /** 语言切换器的无障碍名称（语言列表本身来自 VitePress 的 locales 配置） */
    langAria: string
  }
  hero: {
    eyebrow: string
    eyebrowHref: string
    titleLine1: string
    titleLine2: string
    lede: string
    primaryLabel: string
    primaryNote: string
    primaryHref: string
    secondaryLabel: string
    secondaryHref: string
    quality: string[]
  }
  preview: {
    ariaLabel: string
    title: string
    status: string
    heading: string
    updated: string
    metrics: { label: string; value: string; note: string }[]
    flowTitle: string
    flowAction: string
    flowSteps: { label: string; value: string }[]
  }
  capabilities: {
    kicker: string
    title: string
    description: string
    items: CardCopy[]
  }
  useCases: {
    kicker: string
    title: string
    items: CardCopy[]
  }
  howItWorks: {
    kicker: string
    title: string
    description: string
    steps: StepCopy[]
  }
  openSource: {
    kicker: string
    title: string
    description: string
    actions: NavItem[]
    install: {
      badge: string
      note: string
      copy: string
      copied: string
      command: string
    }
  }
  contact: {
    kicker: string
    title: string
    items: { title: string; description: string; linkLabel: string; href: string }[]
  }
  footer: {
    description: string
    columns: { title: string; links: NavItem[] }[]
    copyright: string
  }
  /** 文档入口：首页 nav / 安装区块 / 页脚都靠它跳转 */
  docs: {
    home: string
    install: string
  }
}

const GITHUB = 'https://github.com/oneprolabs/hypercdr'
const RELEASES = 'https://github.com/oneprolabs/hypercdr/releases'
const ISSUES = 'https://github.com/oneprolabs/hypercdr/issues'
const LOGIN = 'https://app.hypercdr.com/login'
const INSTALL_CMD_TAIL = `curl -fL --progress-bar https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh | sudo bash -s -- --base-url https://your-host:12443 --install-dir /var/lib/hypercdr --yes`
const INSTALL_COMMAND_ZH = `# 安装最新版本\n$ ${INSTALL_CMD_TAIL}`
const INSTALL_COMMAND_EN = `# Install the latest release\n$ ${INSTALL_CMD_TAIL}`

const zh: HomeCopy = {
  nav: [
    { label: '产品能力', href: '#capabilities' },
    { label: '应用场景', href: '#use-cases' },
    { label: '工作原理', href: '#how-it-works' },
    { label: '文档', href: '/docs/' },
    { label: '开源', href: '#open-source' },
    { label: '联系我们', href: '#contact' },
    { label: 'GitHub', href: GITHUB, external: true },
  ],
  header: {
    brandAria: 'HyperCDR 首页',
    navAria: '主导航',
    menuLabel: '菜单',
    openMenu: '打开菜单',
    closeMenu: '关闭菜单',
    githubLabel: 'GitHub',
    githubAria: '在 GitHub 上查看 HyperCDR',
    cta: '免费试用',
    langAria: '切换语言',
  },
  hero: {
    eyebrow: 'OneProLabs · Apache 2.0',
    eyebrowHref: GITHUB,
    titleLine1: '保护你的集群。',
    titleLine2: '让恢复有把握。',
    lede: 'HyperCDR 是面向 Kubernetes 与 OpenShift 的开源容器容灾平台。可以保护工作负载、验证恢复结果，并在同一个控制台里管理所有集群。',
    primaryLabel: '免费试用 HyperCDR',
    primaryNote: '',
    primaryHref: LOGIN,
    secondaryLabel: '在 GitHub 查看',
    secondaryHref: GITHUB,
    quality: ['✓ Kubernetes 与 OpenShift', '✓ 经过验证的恢复', '✓ 无厂商锁定'],
  },
  preview: {
    ariaLabel: 'HyperCDR 控制台预览',
    title: 'HyperCDR / 概览',
    status: '● 系统运行正常',
    heading: '概览',
    updated: '刚刚更新',
    metrics: [
      { label: '已保护集群', value: '12', note: '↑ 本月新增 3' },
      { label: '恢复就绪度', value: '98.4%', note: '● 已验证' },
      { label: '生效策略', value: '28', note: '覆盖 4 个站点' },
    ],
    flowTitle: '保护链路',
    flowAction: '查看拓扑 →',
    flowSteps: [
      { label: '生产', value: '12 个集群' },
      { label: '已保护', value: '428.7 TB' },
      { label: '可恢复', value: '全部已验证' },
    ],
  },
  capabilities: {
    kicker: '产品能力',
    title: '备份只是起点。',
    description: '从保护到恢复的每一步，都在同一个平台里完成。',
    items: [
      {
        icon: '◈',
        title: '集群保护',
        description: '用一套一致的流程注册和管理 Kubernetes 与 OpenShift 集群，不必为每种发行版单独维护工具链。',
      },
      {
        icon: '⌁',
        title: '策略化备份',
        description: '定义计划、资源范围、保留规则与目标存储，让保护动作可重复、可审计。',
      },
      {
        icon: '⇄',
        title: '跨集群容灾',
        description: '建模源端与目标端的对应关系，从保护策略直接走到恢复执行。',
      },
      {
        icon: '◌',
        title: '恢复验证',
        description: '不只看到任务成功，还要确认备份可用、恢复后的工作负载通过校验。',
      },
      {
        icon: '▣',
        title: '恢复工作流',
        description: '引导式操作完成资源映射、进度跟踪和结果核对，减少临场判断。',
      },
      {
        icon: '⌘',
        title: '统一管理',
        description: '集群、策略、任务、存储和平台升级都在同一处查看，不必来回切换。',
      },
    ],
  },
  useCases: {
    kicker: '应用场景',
    title: '常见的使用场景',
    items: [
      {
        index: '01',
        title: '保护生产集群',
        description: '让平台团队在一个地方用可重复的策略保护生产、测试和边缘集群。',
      },
      {
        index: '02',
        title: '为灾难做好准备',
        description: '为迁移、故障和勒索软件恢复维护清晰的源到目标计划。',
      },
      {
        index: '03',
        title: '确认恢复可用',
        description: '用容灾演练确认恢复可用，不必等故障发生才知道结果。',
      },
    ],
  },
  howItWorks: {
    kicker: '工作原理',
    title: '工作负载留在你的集群里，控制台负责调度和记录。',
    description: 'HyperCDR 协调保护策略与任务执行，Agent 在你掌控的集群内完成实际动作，备份写入你指定的对象存储。',
    steps: [
      { title: '生产集群', description: '工作负载与保护策略' },
      { title: 'HyperCDR Agent', description: '出站连接，执行任务' },
      { title: '对象存储', description: 'S3 兼容的备份数据' },
      { title: '目标集群', description: '经过验证的恢复' },
    ],
  },
  openSource: {
    kicker: '社区',
    title: '开源版可以直接用于生产',
    description:
      '用 Docker 在自己的 Linux 主机上运行 HyperCDR。在线安装器会下载并部署已发布的版本。',
    actions: [
      { label: '在 GitHub 查看', href: GITHUB, external: true },
      { label: '安装指南', href: '/docs/getting-started/install' },
      { label: '查看发布版本', href: RELEASES, external: true },
    ],
    install: {
      badge: '最新版本',
      note: '在线安装器',
      copy: '复制命令',
      copied: '已复制',
      command: INSTALL_COMMAND_ZH,
    },
  },
  contact: {
    kicker: '联系我们',
    title: '需要私有部署或技术支持？',
    items: [
      {
        title: '企业部署',
        description: '正在规划组织内的私有部署？我们可以一起确定平台范围。',
        linkLabel: '发邮件给我们 ↗',
        href: 'mailto:oneprolabs@oneprocloud.com',
      },
      {
        title: '技术支持',
        description: '发现问题或需要帮助？在 GitHub 上与社区一起讨论。',
        linkLabel: '提交 issue ↗',
        href: ISSUES,
      },
    ],
  },
  footer: {
    description: 'OneProLabs 出品的开源容器容灾平台。',
    columns: [
      {
        title: '产品',
        links: [
          { label: '产品能力', href: '#capabilities' },
          { label: '工作原理', href: '#how-it-works' },
          { label: '文档中心', href: '/docs/' },
          { label: '免费试用', href: LOGIN, external: true },
        ],
      },
      {
        title: '开源',
        links: [
          { label: 'GitHub', href: GITHUB, external: true },
          { label: '发布版本', href: RELEASES, external: true },
          { label: '支持', href: ISSUES, external: true },
        ],
      },
    ],
    copyright: '© 2026 OneProLabs · 开源共建',
  },
  docs: {
    home: '/docs/',
    install: '/docs/getting-started/install',
  },
}

const en: HomeCopy = {
  nav: [
    { label: 'Capabilities', href: '#capabilities' },
    { label: 'Use cases', href: '#use-cases' },
    { label: 'How it works', href: '#how-it-works' },
    { label: 'Docs', href: '/en/docs/' },
    { label: 'Open source', href: '#open-source' },
    { label: 'Contact', href: '#contact' },
    { label: 'GitHub', href: GITHUB, external: true },
  ],
  header: {
    brandAria: 'HyperCDR home',
    navAria: 'Main navigation',
    menuLabel: 'Menu',
    openMenu: 'Open menu',
    closeMenu: 'Close menu',
    githubLabel: 'GitHub',
    githubAria: 'View HyperCDR on GitHub',
    cta: 'Try free',
    langAria: 'Switch language',
  },
  hero: {
    eyebrow: 'OneProLabs · Apache 2.0',
    eyebrowHref: GITHUB,
    titleLine1: 'Protect your clusters.',
    titleLine2: 'Recover with confidence.',
    lede: 'HyperCDR is an open source container disaster recovery platform for Kubernetes and OpenShift. Protect workloads, verify recovery, and manage every cluster from one console.',
    primaryLabel: 'Try HyperCDR',
    primaryNote: 'Free',
    primaryHref: LOGIN,
    secondaryLabel: 'View on GitHub',
    secondaryHref: GITHUB,
    quality: ['✓ Kubernetes & OpenShift', '✓ Verified recovery', '✓ No vendor lock-in'],
  },
  preview: {
    ariaLabel: 'HyperCDR control plane preview',
    title: 'HyperCDR / Overview',
    status: '● All systems healthy',
    heading: 'Overview',
    updated: 'Last updated just now',
    metrics: [
      { label: 'Protected clusters', value: '12', note: '↑ 3 this month' },
      { label: 'Recovery readiness', value: '98.4%', note: '● Verified' },
      { label: 'Active policies', value: '28', note: 'Across 4 sites' },
    ],
    flowTitle: 'Protection pipeline',
    flowAction: 'View topology →',
    flowSteps: [
      { label: 'Production', value: '12 clusters' },
      { label: 'Protected', value: '428.7 TB' },
      { label: 'Recovery ready', value: 'All verified' },
    ],
  },
  capabilities: {
    kicker: 'Capabilities',
    title: 'Backup is only the beginning.',
    description: 'Every step from protection to recovery happens in one platform.',
    items: [
      {
        icon: '◈',
        title: 'Cluster protection',
        description: 'Register and manage Kubernetes and OpenShift clusters from one consistent workflow.',
      },
      {
        icon: '⌁',
        title: 'Policy-based backup',
        description: 'Define schedules, resource scope, retention, and storage without stitching tools together.',
      },
      {
        icon: '⇄',
        title: 'Cross-cluster DR',
        description: 'Model source and target relationships and move from policy to recovery with confidence.',
      },
      {
        icon: '◌',
        title: 'Recovery verification',
        description: 'See whether backups are usable and whether recovered workloads actually pass validation.',
      },
      {
        icon: '▣',
        title: 'Recovery workflows',
        description: 'Use guided operations to map resources, track progress, and understand every result.',
      },
      {
        icon: '⌘',
        title: 'Unified management',
        description: 'See clusters, policies, tasks, storage, and platform upgrades in one place instead of switching between tools.',
      },
    ],
  },
  useCases: {
    kicker: 'Use cases',
    title: 'Common use cases',
    items: [
      {
        index: '01',
        title: 'Protect production clusters',
        description:
          'Give platform teams one place to protect production, test, and edge clusters with repeatable policies.',
      },
      {
        index: '02',
        title: 'Prepare for disaster',
        description: 'Maintain a clear source-to-target plan for migrations, outages, and ransomware recovery.',
      },
      {
        index: '03',
        title: 'Confirm recovery works',
        description:
          'Use DR drills to confirm recovery works, instead of finding out during an incident.',
      },
    ],
  },
  howItWorks: {
    kicker: 'How it works',
    title: 'Workloads stay in your clusters; the control plane schedules and records.',
    description:
      'HyperCDR coordinates policy and execution while agents operate inside the clusters you control.',
    steps: [
      { title: 'Production cluster', description: 'Workloads and policies' },
      { title: 'HyperCDR Agent', description: 'Secure execution' },
      { title: 'Object storage', description: 'S3-compatible backups' },
      { title: 'Target cluster', description: 'Verified recovery' },
    ],
  },
  openSource: {
    kicker: 'Community',
    title: 'The open source edition is ready for production',
    description:
      'Run HyperCDR on your own Linux host with Docker. The online installer downloads and deploys a verified release.',
    actions: [
      { label: 'View on GitHub', href: GITHUB, external: true },
      { label: 'Installation guide', href: '/en/docs/getting-started/install' },
      { label: 'View releases', href: RELEASES, external: true },
    ],
    install: {
      badge: 'Latest release',
      note: 'Online installer',
      copy: 'Copy command',
      copied: 'Copied',
      command: INSTALL_COMMAND_EN,
    },
  },
  contact: {
    kicker: 'Contact',
    title: 'Need a private deployment or support?',
    items: [
      {
        title: 'Enterprise deployment',
        description: 'Planning a private deployment for your organization? We can help scope the platform.',
        linkLabel: 'Email us ↗',
        href: 'mailto:oneprolabs@oneprocloud.com',
      },
      {
        title: 'Technical support',
        description: 'Found an issue or need help? Open a discussion with the community on GitHub.',
        linkLabel: 'Open an issue ↗',
        href: ISSUES,
      },
    ],
  },
  footer: {
    description: 'Open source container disaster recovery platform by OneProLabs.',
    columns: [
      {
        title: 'Product',
        links: [
          { label: 'Capabilities', href: '#capabilities' },
          { label: 'How it works', href: '#how-it-works' },
          { label: 'Documentation', href: '/en/docs/' },
          { label: 'Try free', href: LOGIN, external: true },
        ],
      },
      {
        title: 'Open source',
        links: [
          { label: 'GitHub', href: GITHUB, external: true },
          { label: 'Releases', href: RELEASES, external: true },
          { label: 'Support', href: ISSUES, external: true },
        ],
      },
    ],
    copyright: '© 2026 OneProLabs · Built in the open',
  },
  docs: {
    home: '/en/docs/',
    install: '/en/docs/getting-started/install',
  },
}

export const homeCopy: Record<HomeLocale, HomeCopy> = { zh, en }
