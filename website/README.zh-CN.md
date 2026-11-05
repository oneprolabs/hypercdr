# HyperCDR Docs Site

中文 | [English](README.md)

HyperCDR 的文档站，内容覆盖产品概览、安装部署、用户指南、使用手册、运维和参考文档。

站点构建为静态文件，推送到 `main` 后由 GitHub Actions 自动发布。

> 本文件只放在源码里，已加入 `srcExclude`，不会被构建成页面，也不会出现在线上。

## 目录

- [技术栈](#技术栈)
- [目录结构](#目录结构)
- [架构](#架构)
- [本地开发](#本地开发)
- [添加内容](#添加内容)
- [修改现有内容](#修改现有内容)
- [写作与术语约定](#写作与术语约定)
- [构建与发布](#构建与发布)
- [常见问题](#常见问题)

## 技术栈

| 项 | 版本 / 说明 |
| --- | --- |
| VitePress | 1.6.4 |
| Vue | 3.5 |
| Node.js | 22（CI 使用同版本） |
| 包管理 | npm（`package-lock.json` 入库） |

没有 UI 框架、没有构建后处理。所有样式是手写 CSS，页面交互量很小，只用了 Vue 单文件组件。

## 目录结构

```text
website/
├── README.md                英文说明（源码可见）
├── README.zh-CN.md          本文件，仅源码可见
├── build.sh                 构建入口：校验 + 构建 + 产物断言
├── package.json             dev / build / preview 三个脚本
├── deploy/nginx.conf        线上容器挂载的 nginx 配置
├── public/                  原样拷贝到产物根目录的静态资源
│   ├── favicon.svg
│   ├── logo.svg
│   └── images/              文档配图
├── scripts/
│   └── mirror-en-docs.mjs   为缺失的英文页生成待翻译占位页
├── zh/                      中文（默认语言，输出到站点根路径）
│   ├── index.md             官网首页
│   └── docs/                文档内容
├── en/                      英文（输出到 /en/ 前缀下）
│   ├── index.md             英文首页
│   └── docs/                文档内容，与 zh/ 的页面树一一对应
├── .vitepress/
│   ├── config.mts           站点配置：语言、路由、sitemap、locales
│   ├── navigation/
│   │   ├── zh.ts            中文导航与侧边栏
│   │   ├── en.ts            英文导航与侧边栏
│   │   └── search.ts        本地搜索的文案，每个语言一份
│   └── theme/
│       ├── index.ts         主题入口，注册布局与全局样式
│       ├── Layout.vue       按 frontmatter 分流到首页或文档布局
│       ├── HomeLanding.vue  官网首页整页
│       ├── SiteHeader.vue   官网顶部导航（吸顶）
│       ├── SiteFooter.vue   官网页脚
│       ├── BackToTop.vue    全站返回顶部按钮
│       ├── LanguageMenu.vue 语言切换
│       ├── useSiteLocale.ts 当前语言判定
│       ├── useSiteLanguages.ts 语言列表
│       ├── homeCopy.ts      首页全部文案，每个语言一份
│       ├── custom.css       设计 token 与全局基础样式
│       ├── docs.css         文档站版式
│       └── home.css         官网版式
└── _archive/                历史实现，不参与构建
```

## 架构

### 一套应用，承载多个语言版本

整站只有一个 VitePress 应用（`srcDir: '.'`），每种语言是它的一个 locale，不是各自独立的工程：

| 源文件 | 输出 URL |
| --- | --- |
| `zh/index.md` | `/` |
| `zh/docs/product/index.md` | `/docs/product/` |
| `en/index.md` | `/en/` |
| `en/docs/product/index.md` | `/en/docs/product/` |

默认语言的目录（当前是 `zh/`）由 `rewrites` 去掉前缀，落在根路径；其余语言带自己的前缀，例如英文在 `/en/` 下。因此 Markdown 链接、`nav`、`sidebar` 里写的路径和最终 URL 完全一致，不需要任何前缀换算。

新增语言只需两步：在 `config.mts` 的 `locales` 里加一项，再建一个同名前缀的内容目录（如 `ja/`），导航与切换器会自动跟着变。

站点开启 `cleanUrls`，产物是 `foo.html` 而不是 `foo/index.html`，nginx 侧用 `$uri.html` 兜底（见 `deploy/nginx.conf`）。

### 首次访问按浏览器语言分流

根路径放的是默认语言，但不会一律把访客送到中文：`config.mts` 往每个页面的 `<head>` 注入一段脚本，在首屏渲染前按 `navigator.languages` 决定去向。

| 浏览器语言 | 落到 |
| --- | --- |
| `zh`、`zh-CN`、`zh-TW` … | 根路径（默认语言） |
| 其他任何语言 | `/en/` |

规则：

- 只在首次访问时分流，同一会话内不再干预，用户手动切到哪一版就停在哪一版；
- 用户在切换器里选过语言会写进 `localStorage` 的 `hcdr-locale`，之后不再自动跳转；
- 404 页面不分流；
- 浏览器语言与前缀的对应表在 `config.mts` 的 `BROWSER_LOCALE_PREFIX`，新增语言时补一行，并确认 `LOCALE_FALLBACK_PREFIX` 指向期望的兜底语言。

脚本放在 `<head>` 而不是组件里，是为了避免默认语言页面先渲染再跳走。

### 各语言页面树必须一一对应

`themeConfig.i18nRouting` 切语言时按路径替换前缀，所以同一个页面在每个语言目录下都要存在：`zh/docs/a.md` 有，`en/docs/a.md` 也要有，否则切换后 404。

`build.sh` 会逐页比对，缺一页就 `exit 1` 并打印缺失路径。补占位页用：

```bash
node scripts/mirror-en-docs.mjs
```

脚本只补缺失页，不覆盖已有内容。新页面记得先在脚本的 `TITLES` 里补一行标题。

### 三种布局

`Layout.vue` 按 frontmatter 分流：

| `layout` 值 | 用途 |
| --- | --- |
| 不写 | VitePress 默认文档布局（侧栏 + 正文 + 目录） |
| `hcdr-home` | 官网首页整页，自带 header/footer，不复用文档站导航 |
| — | 404 页面由 VitePress 默认处理 |

首页所有文案集中在 `homeCopy.ts`，每个语言一个对象，类型 `HomeCopy` 约束各份结构的 key 必须一致，漏字段会编译报错。

### 样式分层

| 文件 | 负责 |
| --- | --- |
| `custom.css` | 设计 token（`--hcdr-*`、覆盖 `--vp-*`）、深色模式、全局基础排版 |
| `docs.css` | 文档站专属版式：正文、表格、卡片栅格 |
| `home.css` | 官网专属版式：hero、能力卡片、安装卡片 |

文档站用 VitePress 的 CSS 变量，跟随深色模式；官网首页是固定浅色版式，不响应 `.dark`。

## 本地开发

```bash
cd website
npm ci
npm run dev
```

打开 <http://localhost:5174>，修改 Markdown 与主题文件都会热更新。

| 命令 | 作用 |
| --- | --- |
| `npm run dev` | 开发服务器，端口 5174 |
| `npm run build` | 只跑 `vitepress build` |
| `npm run preview` | 预览已构建产物，端口 4147 |
| `./build.sh` | **推荐**：各语言页面 parity 校验 → 构建 → 断言三个入口页存在 |

提交前请跑 `./build.sh`，`npm run build` 不含 parity 校验。

## 添加内容

新增一个文档页面，按顺序做三步。

### 1. 建页面文件

每个语言目录下建同一路径的文件，内容用该语言书写：

```text
zh/docs/operations/blue-green.md
en/docs/operations/blue-green.md
```

frontmatter 至少要有 `title` 和 `description`：

```markdown
---
title: 蓝绿部署与回滚
description: 蓝绿切换的触发条件、执行步骤、回滚边界和数据库迁移的注意事项。
---

# 蓝绿部署与回滚

正文……
```

`title` 同时用于浏览器标题、搜索结果和侧边栏回退值。暂时缺某个语言的版本时，可以跑 `node scripts/mirror-en-docs.mjs` 生成占位页，但**不要长期留着占位页**：`build.sh` 只保证路径存在，不保证内容已写。

### 2. 注册侧边栏

在 `.vitepress/navigation/` 下每个语言对应的文件里，找到同板块的 `SidebarItem` 各加一项：

```ts
// zh.ts
{ text: '蓝绿部署与回滚', link: '/docs/operations/blue-green' },

// en.ts
{ text: 'Blue-green deployment and rollback', link: '/en/docs/operations/blue-green' },
```

`text` 与该语言页面的 `title` 保持一致，否则用户会看到「点进去标题变了」。`link` 写站点绝对路径，与最终 URL 相同。

### 3. 跑一次构建

```bash
cd website && ./build.sh
```

校验通过、产物里三个入口页都存在，才算完成。

## 修改现有内容

| 想改什么 | 改哪里 |
| --- | --- |
| 正文 | 各语言目录下的同一路径文件 |
| 页面标题 | 页面的 frontmatter `title` + H1 + 各语言 `navigation/*.ts` 里的 `text` |
| 侧边栏顺序或文案 | `.vitepress/navigation/` 下每个语言一个文件，需同步 |
| 顶部导航 | 同上，`themeConfig.nav` 部分 |
| 官网首页文案 | `.vitepress/theme/homeCopy.ts`，每个语言一份，key 必须对齐 |
| 文档站配色、字体 | `.vitepress/theme/custom.css` |
| 首页版式 | `.vitepress/theme/home.css` + `HomeLanding.vue` |
| 图片 | 放 `public/images/`，用绝对路径引用 `![](/images/xxx.png)` |
| 站点标题、描述、sitemap | `.vitepress/config.mts` |

几个容易踩的点：

- **改标题会改锚点**。自动锚点由标题文本生成，改名会让指向旧标题的链接失效。
- **需要固定锚点时显式声明**：`## 更改平台访问地址 {#access-url}`，这样改标题也不会破坏外部链接。
- **frontmatter 里 `description` 含冒号**（如 `A: B`）必须整体加双引号，否则 YAML 解析失败，表现为 `incomplete explicit mapping pair`。
- **图片用绝对路径**。相对路径在 `cleanUrls` 下容易算错基准目录。

## 写作与术语约定

术语以仓库 `docs/` 下的文档为准，不自行发明译名。

| 约定 | 说明 |
| --- | --- |
| 中文平台称谓 | **控制台**（不使用"中控""控制面"） |
| 英文平台称谓 | `control plane`，与 `docs/` 英文原文一致 |
| `Namespace` | 正文写成首字母大写的 `Namespace`；命令行参数（`--namespace`）、清单键（`namespace:`）、占位符（`<host/namespace>`）保持小写 |
| 标题 | 名词短语。"怎么读这些数字" → "指标解读"，不用疑问句、比喻、口号 |
| 语气 | 陈述事实与边界。不写"极致""全方位""真正可靠"这类营销词 |
| 各语言一致性 | 一个语言版本改了事实，其他语言版本必须同步，否则会互相矛盾 |

`docs/` 原文使用中文标点与全角括号；代码块、路径、参数名保持原样，不要翻译。

## 构建与发布

```text
./build.sh
  ├─ 校验各语言目录页面一一对应
  ├─ vitepress build  →  .vitepress/dist
  └─ 断言 dist/index.html、dist/docs/index.html、dist/en/index.html

push 到 main 且改动涉及 website/**
  └─ GitHub Actions: Publish Docs Site
       ├─ 打包 dist + deploy/nginx.conf
       ├─ 上传到服务器 /tmp，再整体替换站点目录
       └─ 以独立容器 hypercdr-docs 提供服务（默认 12445，由 nginx 反代 hypercdr.com）
```

这个流程与产品的 release 部署**零交集**：站点目录、nginx 配置、容器都是独立的，release 覆盖 `/var/lib/hypercdr/website` 不会影响线上文档。唯一共用的是 TLS 证书。

仓库变量可以覆盖默认值：`DOCS_SITE_DIR`、`DOCS_CONF`、`DOCS_CONTAINER`、`DOCS_HTTP_PORT`。

## 常见问题

**构建报 `ERROR: 缺少英文页面 en/docs/...`**
各语言目录的页面树不齐。补上缺失语言的同路径页面，或先跑 `node scripts/mirror-en-docs.mjs`。

**侧边栏文案和页面标题对不上**
侧边栏 `text` 优先于页面 `title` 显示。改标题时三处一起改：frontmatter、H1、`navigation/*.ts`。

**构建报 `incomplete explicit mapping pair`**
frontmatter 的 `description` 里有冒号没加引号，把整个值用双引号包起来。

**页面能打开，但切语言后 404**
对端语言缺同路径页面，`./build.sh` 会提前拦住这种情况。

**本地改了没生效**
`npm run dev` 有缓存，重启一次；或者确认改的是 `zh/` 而不是 `_archive/`（归档目录被 `srcExclude` 排除，不参与构建）。

## 相关

- 仓库根目录 `README.md`：产品介绍与安装
- `docs/`：面向客户的原始文档，是文档站术语与事实的来源
- `.github/workflows/docs.yml`：文档站发布流程
