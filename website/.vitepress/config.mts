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
 * 默认语言是中文，内容放在 zh/ 下，由 rewrites 提到站点根路径：
 *   zh/index.md              → /
 *   zh/docs/product/index.md → /docs/product/
 * 其余语言保留语言前缀：
 *   en/index.md              → /en/
 *   en/docs/index.md         → /en/docs/
 *
 * 因此 Markdown 链接、nav、sidebar 里写的路径与最终 URL 完全一致（/docs/product/），
 * 不需要任何前缀换算。
 *
 * 首次访问会按浏览器语言跳到对应语言版本，实现见 localeRoutingScript。
 */

/** 与 theme/index.ts 里的常量保持一致：用户主动选过语言就不再按浏览器语言跳转 */
const LOCALE_PREF_KEY = 'hcdr-locale'
const LOCALE_ROUTED_KEY = 'hcdr-locale-routed'

/**
 * 浏览器语言主标记 → 站点 URL 前缀。
 * 空字符串表示默认语言（无前缀）。新增语言时在这里补一行。
 */
const BROWSER_LOCALE_PREFIX: Record<string, string> = {
  zh: '',
  en: '/en',
}

/** 浏览器语言都匹配不上时落到英文 */
const LOCALE_FALLBACK_PREFIX = '/en'

/**
 * 首屏渲染前执行的语言分流脚本。
 *
 * 放在 head 里而不是组件里，是为了避免中文页面先闪一下再跳到英文。
 * 三个前置条件任一成立就不跳：
 *   1. 用户主动选过语言（localStorage 有记录）；
 *   2. 本次会话已经分流过，不再覆盖用户后续的手动操作；
 *   3. 当前是 404 页面。
 */
const localeRoutingScript = `(function(){try{
var PREF='${LOCALE_PREF_KEY}',ROUTED='${LOCALE_ROUTED_KEY}';
if(localStorage.getItem(PREF)||sessionStorage.getItem(ROUTED))return;
sessionStorage.setItem(ROUTED,'1');
var path=location.pathname;
if(/404(\\.html)?$/.test(path))return;
var map=${JSON.stringify(BROWSER_LOCALE_PREFIX)},fallback=${JSON.stringify(LOCALE_FALLBACK_PREFIX)};
var langs=(navigator.languages&&navigator.languages.length)?navigator.languages:[navigator.language||''];
var target=null;
for(var i=0;i<langs.length&&target===null;i++){
  var p=String(langs[i]||'').toLowerCase().split('-')[0];
  if(Object.prototype.hasOwnProperty.call(map,p))target=map[p];
}
if(target===null)target=fallback;
var prefixes=[];
for(var k in map){if(map[k])prefixes.push(map[k]);}
prefixes.sort(function(a,b){return b.length-a.length;});
var current='';
for(var j=0;j<prefixes.length;j++){
  var pf=prefixes[j];
  if(path===pf||path.indexOf(pf+'/')===0){current=pf;break;}
}
if(current===target)return;
var rest=current?(path.slice(current.length)||'/'):path;
location.replace(target+(rest==='/'?'/':rest)+location.search+location.hash);
}catch(e){}})();`

export default defineConfig({
  srcDir: '.',
  // 归档的旧实现（含历史 md）不参与构建，避免被当成内容页渲染出 /archive/...
  // README*.md 是给维护者看的源码说明（中英两份），不属于文档内容，不能出现在线上。
  srcExclude: ['_archive/**', 'README.md', 'README.zh-CN.md'],
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
    // 首屏前按浏览器语言分流，见上方 localeRoutingScript
    ['script', {}, localeRoutingScript],
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
