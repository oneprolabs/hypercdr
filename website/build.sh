#!/usr/bin/env bash
# 构建站点静态产物（输出到 .vitepress/dist）。
#
# 整站是一个 VitePress 应用：
#   zh/            中文内容（默认语言，经 rewrites 输出到站点根路径）
#   en/            英文内容（输出到 /en/）
#
# 产物结构：
#   .vitepress/dist/index.html    中文首页（zh/index.md，layout: hcdr-home）
#   .vitepress/dist/docs/         中文文档站
#   .vitepress/dist/en/           英文站
#
# 本地预览：npm run dev / npm run preview；
# 发布打包由 scripts/release/package-release.sh 调用同一套构建。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

[[ -f package.json ]] || { echo "ERROR: package.json not found in ${SCRIPT_DIR}" >&2; exit 1; }

# 中英文文档必须一一对应：英文是独立 locale，切语言时按路径替换前缀
# （themeConfig.i18nRouting），中文有而英文没有的页面切换后就是 404。
parity_failed=0
while IFS= read -r rel; do
  if [[ ! -f "en/docs/${rel}" ]]; then
    parity_failed=$((parity_failed + 1))
    printf 'ERROR: 缺少英文页面 en/docs/%s\n' "${rel}" >&2
  fi
done < <(cd zh/docs && find . -name '*.md' | sed 's|^\./||' | sort)

if (( parity_failed > 0 )); then
  echo "ERROR: 中文文档有 ${parity_failed} 页没有对应的英文页面。" >&2
  echo "       中英文必须一一对应，否则切语言会 404。" >&2
  echo "       在 scripts/mirror-en-docs.mjs 的 TITLES 里补英文标题后运行：" >&2
  echo "         node scripts/mirror-en-docs.mjs" >&2
  exit 1
fi

if [[ ! -d node_modules ]]; then
  if [[ -f package-lock.json ]]; then
    npm ci --no-audit --no-fund
  else
    npm install --no-audit --no-fund
  fi
fi

npm run build

[[ -f .vitepress/dist/index.html ]] \
  || { echo "ERROR: build finished without .vitepress/dist/index.html (中文首页)" >&2; exit 1; }
[[ -f .vitepress/dist/docs/index.html ]] \
  || { echo "ERROR: build finished without .vitepress/dist/docs/index.html (中文文档站)" >&2; exit 1; }
[[ -f .vitepress/dist/en/index.html ]] \
  || { echo "ERROR: build finished without .vitepress/dist/en/index.html (英文站)" >&2; exit 1; }

printf 'Website built at %s\n' "${SCRIPT_DIR}/.vitepress/dist"
