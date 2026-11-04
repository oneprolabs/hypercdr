#!/usr/bin/env node
/**
 * 为缺失英文页的中文页面生成「待翻译」占位页，保证 en/docs 与 zh/docs 一一对应。
 *
 * 为什么要一一对应：英文是独立的 VitePress locale，切语言时（themeConfig.i18nRouting
 * 默认开启）会把当前页路径拼到目标语言前缀后面。中文有而英文没有的页面，切换后就是 404。
 *
 * 用法（在 website/ 下）：
 *   node scripts/mirror-en-docs.mjs
 *
 * 行为：
 *   - 已存在的英文页一律跳过，不会覆盖译文；
 *   - 新页面正文只放「Not translated yet」提示和中文原文链接；
 *   - 新增中文页后请先在 TITLES 里补一行英文标题，再运行本脚本。
 */
import { readFileSync, writeFileSync, mkdirSync, existsSync, readdirSync } from 'node:fs'
import { dirname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = dirname(dirname(fileURLToPath(import.meta.url)))
const ZH_DOCS = join(ROOT, 'zh/docs')
const EN_DOCS = join(ROOT, 'en/docs')

/** 中文相对路径 → 英文标题（frontmatter title 与 H1 都用它） */
const TITLES = {
  'deployment/index.md': 'Deployment guide',
  'deployment/install.md': 'Install the Community edition',
  'deployment/configuration.md': 'Configuration reference',
  'product/index.md': 'What is HyperCDR',
  'product/architecture.md': 'Control plane architecture',
  'product/concepts.md': 'Core concepts',
  'user-guide/index.md': 'User guide',
  'user-guide/first-use.md': 'First use: from cluster registration to recovery verification',
  'user-guide/users-and-organizations.md': 'Users, organizations and platform permissions',
  'manual/index.md': 'Operation manual',
  'manual/dashboard.md': 'Dashboard',
  'manual/clusters.md': 'Cluster registration and management',
  'manual/storage.md': 'Object storage',
  'manual/policies.md': 'Protection policies',
  'manual/application-dr.md': 'Application disaster recovery',
  'manual/tasks.md': 'Tasks and restore points',
  'manual/drill.md': 'DR drills',
  'manual/agent-uninstall.md': 'Uninstall a cluster agent',
  'operations/index.md': 'Administration',
  'operations/platform.md': 'Platform operations and upgrades',
  'operations/blue-green.md': 'Blue-green deployment and rollback',
  'operations/restore-cache.md': 'Velero restore cache',
  'operations/troubleshooting.md': 'Troubleshooting',
  'operations/release.md': 'Build and release maintenance',
  'reference/index.md': 'Reference',
  'reference/glossary.md': 'Glossary',
  'reference/support-scope.md': 'Support and compatibility scope',
  'reference/limitations.md': 'Limitations and security recommendations',
  'reference/cce.md': 'Huawei CCE',
  'reference/openshift.md': 'OpenShift and OADP',
  'reference/openshift-checklist.md': 'OpenShift POC validation checklist',
}

/** 递归收集 .md，返回相对 zh/docs 的 posix 路径 */
function listMarkdown(dir, base = dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) return listMarkdown(full, base)
    if (!entry.name.endsWith('.md')) return []
    return [relative(base, full).split(sep).join('/')]
  })
}

/** 从中文页 frontmatter 取 title */
function zhTitle(file, fallback) {
  const text = readFileSync(file, 'utf8')
  const block = text.match(/^---\r?\n([\s\S]*?)\r?\n---/)
  const matched = block?.[1].match(/^title:\s*(.+)$/m)
  return matched ? matched[1].trim().replace(/^["']|["']$/g, '') : fallback
}

const zhFiles = listMarkdown(ZH_DOCS).sort()

// 已经有英文页的（例如 en/docs/index.md 有真实正文）不要求配标题
const missingTitles = zhFiles.filter(
  (rel) => !TITLES[rel] && !existsSync(join(EN_DOCS, rel)),
)
if (missingTitles.length) {
  console.error('以下中文页还没有配英文标题，请先在 TITLES 里补上：')
  for (const rel of missingTitles) console.error('  ' + rel)
  process.exit(1)
}

let created = 0
let skipped = 0
for (const rel of zhFiles) {
  const enPath = join(EN_DOCS, rel)
  if (existsSync(enPath)) {
    skipped++
    continue
  }

  const title = TITLES[rel]
  const original = zhTitle(join(ZH_DOCS, rel), title)
  const url = '/docs/' + rel.replace(/(^|\/)index\.md$/, '$1').replace(/\.md$/, '')

  mkdirSync(dirname(enPath), { recursive: true })
  writeFileSync(
    enPath,
    `---
title: ${JSON.stringify(title)}
outline: false
---

# ${title}

::: warning Not translated yet
This page exists so that the English documentation mirrors the Chinese one page for page.
The full text is currently available in Chinese: [${original}](${url}).
:::

The English translation of this section is still in progress. In the meantime you can:

- read the [Chinese original](${url})
- go back to the [documentation overview](/en/docs/)
`,
  )
  created++
}

console.log(`中文页 ${zhFiles.length} 个；新建英文占位页 ${created} 个，已存在跳过 ${skipped} 个`)
