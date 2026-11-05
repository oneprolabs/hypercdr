# HyperCDR Docs Site

[中文](README.zh-CN.md) | English

The HyperCDR documentation site, covering the product overview, installation and deployment, the user guide, the operation manual, administration and the reference.

The site builds to static files and is published by GitHub Actions on every push to `main`.

> This file stays in the source tree only. It is listed in `srcExclude`, so it is never built into a page and never appears on the published site.

## Contents

- [Stack](#stack)
- [Layout](#layout)
- [Architecture](#architecture)
- [Local development](#local-development)
- [Adding content](#adding-content)
- [Editing existing content](#editing-existing-content)
- [Writing and terminology](#writing-and-terminology)
- [Build and publish](#build-and-publish)
- [Troubleshooting](#troubleshooting)

## Stack

| Item | Version / notes |
| --- | --- |
| VitePress | 1.6.4 |
| Vue | 3.5 |
| Node.js | 22 (CI uses the same) |
| Package manager | npm (`package-lock.json` is committed) |

No UI framework and no post-processing. All styling is hand-written CSS, and the only components are Vue single-file components for the few interactive parts.

## Layout

```text
website/
├── README.md                English readme (source only)
├── README.zh-CN.md          Chinese readme (source only)
├── build.sh                 Build entry: validation + build + output assertions
├── package.json             dev / build / preview scripts
├── deploy/nginx.conf        nginx config mounted by the production container
├── public/                  Static assets copied verbatim to the output root
│   ├── favicon.svg
│   ├── logo.svg
│   └── images/              Documentation images
├── scripts/
│   └── mirror-en-docs.mjs   Generates placeholder English pages for missing ones
├── zh/                      Chinese (default locale, served from the site root)
│   ├── index.md             Homepage
│   └── docs/                Documentation content
├── en/                      English (served under /en/)
│   ├── index.md             English homepage
│   └── docs/                Content, mirrors the zh/ page tree one to one
├── .vitepress/
│   ├── config.mts           Site config: locales, routing, sitemap
│   ├── navigation/
│   │   ├── zh.ts            Chinese nav and sidebar
│   │   ├── en.ts            English nav and sidebar
│   │   └── search.ts        Local search labels, one set per language
│   └── theme/
│       ├── index.ts         Theme entry, registers layout and global styles
│       ├── Layout.vue       Routes to the homepage or the docs layout
│       ├── HomeLanding.vue  Full homepage
│       ├── SiteHeader.vue   Homepage header (sticky)
│       ├── SiteFooter.vue   Homepage footer
│       ├── BackToTop.vue    Back-to-top button, site-wide
│       ├── LanguageMenu.vue Language switcher
│       ├── useSiteLocale.ts Active locale
│       ├── useSiteLanguages.ts Language list
│       ├── homeCopy.ts      All homepage copy, one object per language
│       ├── custom.css       Design tokens and global base styles
│       ├── docs.css         Documentation layout
│       └── home.css         Homepage layout
└── _archive/                Historical implementation, not built
```

## Architecture

### One app, many language versions

There is a single VitePress app (`srcDir: '.'`), and each language is a locale of it rather than a separate project:

| Source file | Output URL |
| --- | --- |
| `zh/index.md` | `/` |
| `zh/docs/product/index.md` | `/docs/product/` |
| `en/index.md` | `/en/` |
| `en/docs/product/index.md` | `/en/docs/product/` |

The default locale's directory (currently `zh/`) has its prefix stripped by `rewrites` and lands at the site root; every other locale keeps its own prefix, English under `/en/`. As a result, paths in Markdown links, `nav` and `sidebar` match the final URL exactly — no prefix juggling anywhere.

Adding a language takes two steps: add an entry to `locales` in `config.mts`, then create a content directory with the same prefix (such as `ja/`). Navigation and the language switcher pick it up automatically.

The site enables `cleanUrls`, so output files are `foo.html` rather than `foo/index.html`. nginx falls back to `$uri.html` (see `deploy/nginx.conf`).

### First visit follows the browser language

The site root hosts the default locale, but visitors are not all sent there: `config.mts` injects a small script into every page's `<head>` that picks a destination from `navigator.languages` before the first paint.

| Browser language | Destination |
| --- | --- |
| `zh`, `zh-CN`, `zh-TW` … | Site root (default locale) |
| Anything else | `/en/` |

Rules:

- Routing happens only on the first visit of a session; manual switches afterwards are left alone;
- Choosing a language in the switcher writes `hcdr-locale` to `localStorage`, and no automatic routing happens after that;
- The 404 page is never redirected;
- The browser language to URL prefix map lives in `BROWSER_LOCALE_PREFIX` in `config.mts`. Add a line there for a new language, and check that `LOCALE_FALLBACK_PREFIX` points at the fallback you want.

The script sits in `<head>` rather than in a component so the default locale never renders before the redirect.

### Every language tree must stay in sync

`themeConfig.i18nRouting` swaps language by replacing the path prefix, so a page must exist in every language directory: if `zh/docs/a.md` exists, `en/docs/a.md` has to exist too, otherwise switching language lands on a 404.

`build.sh` compares the trees page by page and exits with status 1 on the first missing file, printing the path. To fill the gap:

```bash
node scripts/mirror-en-docs.mjs
```

The script only creates missing pages and never overwrites existing content. Add a title to its `TITLES` map before running it for a new page.

### Three layouts

`Layout.vue` dispatches on frontmatter:

| `layout` value | Purpose |
| --- | --- |
| absent | VitePress default docs layout (sidebar, content, outline) |
| `hcdr-home` | Full homepage with its own header and footer, not the docs chrome |
| — | The 404 page is handled by VitePress |

All homepage copy lives in `homeCopy.ts`, one object per language. The `HomeCopy` type forces every object to expose the same keys, so a missing field fails compilation.

### Style layers

| File | Responsibility |
| --- | --- |
| `custom.css` | Design tokens (`--hcdr-*`, overrides for `--vp-*`), dark mode, global base typography |
| `docs.css` | Documentation layout: prose, tables, card grid |
| `home.css` | Homepage layout: hero, capability cards, install card |

Documentation pages use the VitePress CSS variables and follow dark mode. The homepage is a fixed light design and does not respond to `.dark`.

## Local development

```bash
cd website
npm ci
npm run dev
```

Open <http://localhost:5174>. Markdown and theme edits hot-reload.

| Command | What it does |
| --- | --- |
| `npm run dev` | Dev server on port 5174 |
| `npm run build` | Runs `vitepress build` only |
| `npm run preview` | Serves the built output on port 4147 |
| `./build.sh` | **Preferred**: page parity check across languages → build → assert the three entry pages |

Run `./build.sh` before committing; `npm run build` does not include the parity check.

## Adding content

Three steps for a new documentation page.

### 1. Create the page files

Create the same path under each language directory, written in that language:

```text
zh/docs/operations/blue-green.md
en/docs/operations/blue-green.md
```

The frontmatter needs at least `title` and `description`:

```markdown
---
title: Blue-green deployment and rollback
description: Trigger conditions for a blue-green switch, the steps, rollback boundaries and what happens to database migrations.
---

# Blue-green deployment and rollback

Body…
```

`title` feeds the browser title, search results and the sidebar fallback. If one language is not ready, run `node scripts/mirror-en-docs.mjs` to create a placeholder — but do not leave placeholders around: `build.sh` only checks that the path exists, not that the page is written.

### 2. Register the sidebar entry

In each language file under `.vitepress/navigation/`, add an entry to the matching `SidebarItem`:

```ts
// zh.ts
{ text: '蓝绿部署与回滚', link: '/docs/operations/blue-green' },

// en.ts
{ text: 'Blue-green deployment and rollback', link: '/en/docs/operations/blue-green' },
```

Keep `text` identical to that language's page `title`, otherwise the sidebar says one thing and the heading says another. `link` is a site-absolute path that matches the final URL.

### 3. Build once

```bash
cd website && ./build.sh
```

Done when the check passes and all three entry pages are present in the output.

## Editing existing content

| Goal | Where to change it |
| --- | --- |
| Body text | The same path under each language directory |
| Page title | The page's frontmatter `title` + H1 + `text` in each language's `navigation/*.ts` |
| Sidebar order or labels | One file per language under `.vitepress/navigation/`; keep them in step |
| Top navigation | Same files, the `themeConfig.nav` section |
| Homepage copy | `.vitepress/theme/homeCopy.ts`, one object per language with identical keys |
| Docs colors and fonts | `.vitepress/theme/custom.css` |
| Homepage layout | `.vitepress/theme/home.css` with `HomeLanding.vue` |
| Images | Drop them in `public/images/` and reference with an absolute path, `![](/images/xxx.png)` |
| Site title, description, sitemap | `.vitepress/config.mts` |

Things that bite:

- **Renaming a heading changes its anchor.** Anchors are generated from heading text, so a rename breaks every link pointing at the old one.
- **Pin anchors when they matter**: `## Change the platform access address {#access-url}` survives heading edits.
- **A colon in `description`** (as in `A: B`) must be quoted, or YAML parsing fails with `incomplete explicit mapping pair`.
- **Use absolute image paths.** Relative paths resolve against the wrong directory under `cleanUrls`.

## Writing and terminology

Terminology follows the documents under `docs/`. Do not invent translations.

| Rule | Detail |
| --- | --- |
| Chinese name for the platform | **控制台** (not 中控, not 控制面) |
| English name for the platform | `control plane`, matching the English sources under `docs/` |
| `Namespace` | Capitalize it in prose; keep it lowercase in CLI flags (`--namespace`), manifest keys (`namespace:`) and placeholders (`<host/namespace>`) |
| Headings | Noun phrases. "It reads these numbers how?" becomes "Metric interpretation" — no questions, metaphors or slogans |
| Tone | State facts and limits. No "extreme", "all-round", "truly reliable" marketing language |
| Consistency across languages | When a fact changes in one language, change it in the others, or they will contradict each other |

The Chinese sources under `docs/` use Chinese punctuation and full-width brackets. Code blocks, paths and parameter names stay as they are and are never translated.

## Build and publish

```text
./build.sh
  ├─ verify the language directories have matching pages
  ├─ vitepress build  →  .vitepress/dist
  └─ assert dist/index.html, dist/docs/index.html, dist/en/index.html

push to main touching website/**
  └─ GitHub Actions: Publish Docs Site
       ├─ package dist + deploy/nginx.conf
       ├─ upload to /tmp, then replace the site directory wholesale
       └─ serve from the dedicated container hypercdr-docs (default port 12445,
          reverse-proxied by nginx for hypercdr.com)
```

This pipeline has **zero overlap** with the product release deployment: the site directory, nginx config and container are all separate, so a release overwriting `/var/lib/hypercdr/website` does not touch the published docs. The only shared piece is the TLS certificate.

Repository variables can override the defaults: `DOCS_SITE_DIR`, `DOCS_CONF`, `DOCS_CONTAINER`, `DOCS_HTTP_PORT`.

## Troubleshooting

**Build fails with `ERROR: missing en/docs/...`**
The language directories are out of sync. Add the missing page, or run `node scripts/mirror-en-docs.mjs` first.

**Sidebar label does not match the page title**
The sidebar `text` wins over the page `title`. When renaming, change all three: frontmatter, H1, and `navigation/*.ts`.

**Build fails with `incomplete explicit mapping pair`**
`description` in the frontmatter contains a colon and is not quoted. Wrap the whole value in double quotes.

**A page loads, but switching language gives a 404**
The other locale lacks the page at the same path. `./build.sh` catches this before it ships.

**Local edits do not show up**
Restart `npm run dev` to clear its cache, and make sure you edited `zh/` rather than `_archive/` (the archive directory is excluded from the build by `srcExclude`).

## Related

- `README.md` at the repository root: product overview and installation
- `docs/`: the source documents for customers, and the origin of terminology and facts used here
- `.github/workflows/docs.yml`: the publishing pipeline
