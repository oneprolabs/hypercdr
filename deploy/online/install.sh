#!/usr/bin/env bash
set -euo pipefail

OWNER_REPO="${HCDR_GITHUB_REPOSITORY:-oneprolabs/hypercdr}"
VERSION=""
BASE_URL=""
INSTALL_WEBSITE=false
PUBLIC_BASE_URL=""
REGISTRY="registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr"
INSTALL_DIR="/var/lib/hypercdr"
ASSUME_YES="false"
WORK_DIR=""

usage() {
  cat <<'USAGE'
HyperCDR online installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh | sudo bash -s -- --base-url https://HOST:12443 [options]

Options:
  --version VERSION          GitHub Release version; default: latest
  --base-url URL             Required public control-plane URL
  --public-base-url URL      Optional public URL used in generated commands
  --registry REGISTRY        Optional image registry override
  --install-dir PATH         Installation directory, default: /var/lib/hypercdr
  --yes                      Skip confirmation
  -h, --help                 Show this help

The script downloads the versioned installer asset from a GitHub Release and
delegates installation to install-blue-green.sh. It does not use Bootstrap.
USAGE
}

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:?missing value for --version}"; shift 2 ;;
    --base-url) BASE_URL="${2:?missing value for --base-url}"; shift 2 ;;
    --public-base-url) PUBLIC_BASE_URL="${2:?missing value for --public-base-url}"; shift 2 ;;
    --install-website) INSTALL_WEBSITE=true; shift ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:?missing value for --install-dir}"; shift 2 ;;
    --yes) ASSUME_YES="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage; fail "unknown option: $1" ;;
  esac
done

[[ -n "$BASE_URL" ]] || { usage; fail "--base-url is required"; }
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v docker >/dev/null 2>&1 || fail "Docker is required; install Docker before running this installer"
[[ "$(id -u)" -eq 0 ]] || fail "run this command as root, for example: curl ... | sudo bash -s -- ..."

api_url="https://api.github.com/repos/${OWNER_REPO}/releases"
if [[ -z "$VERSION" ]]; then
  VERSION="$(curl -fsSL "${api_url}/latest" | sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' | head -1)"
fi
[[ -n "$VERSION" ]] || fail "could not resolve a GitHub Release version"
VERSION="${VERSION#v}"
asset="hypercdr-installer-${VERSION}.tar.gz"
release_json="$(curl -fsSL "${api_url}/tags/v${VERSION}")" || fail "could not read GitHub Release metadata"
asset_api_url="$(printf '%s' "${release_json}" | jq -er --arg name "${asset}" '.assets[] | select(.name == $name) | .url' | head -1 || true)"
if [[ -z "${asset_api_url}" ]]; then
  assets_url="$(printf '%s' "${release_json}" | jq -er '.assets_url')" || fail "GitHub Release metadata has no assets endpoint"
  asset_api_url="$(curl -fsSL -H 'Accept: application/vnd.github+json' "${assets_url}" | jq -er --arg name "${asset}" '.[] | select(.name == $name) | .url' | head -1 || true)"
fi
[[ -n "${asset_api_url}" ]] || fail "GitHub Release does not contain ${asset}"
WORK_DIR="$(mktemp -d /tmp/hypercdr-online.XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

printf 'HyperCDR online installer\nVersion: %s\nBase URL: %s\nInstall directory: %s\n' "$VERSION" "$BASE_URL" "$INSTALL_DIR"
if [[ "$ASSUME_YES" != "true" ]]; then
  read -r -p 'Continue installation? [y/N] ' answer
  [[ "$answer" =~ ^[Yy]$ ]] || { echo 'Installation cancelled.'; exit 0; }
fi

curl -fL --retry 3 -H 'Accept: application/octet-stream' -o "${WORK_DIR}/${asset}" "$asset_api_url"
tar -xzf "${WORK_DIR}/${asset}" -C "$WORK_DIR"
package_dir="${WORK_DIR}/hypercdr-installer-${VERSION}"
[[ -x "${package_dir}/install-blue-green.sh" ]] || fail "downloaded package is missing install-blue-green.sh"

# The release package carries non-registry runtime settings injected by the
# publish workflow (for example the Turnstile site and secret keys). Load
# those settings before delegating to the blue/green installer so an online
# install has the same authentication configuration as a local install.
if [[ -r "${package_dir}/install-config.sh" ]]; then
  # shellcheck disable=SC1090
  source "${package_dir}/install-config.sh"
  export HCDR_AUTH_CHALLENGE_MODE HCDR_TURNSTILE_SITE_KEY
  export HCDR_TURNSTILE_SECRET_KEY HCDR_TURNSTILE_VERIFY_URL
fi

domain="${BASE_URL#https://}"
domain="${domain%%/*}"
domain="${domain%%:*}"
args=("$VERSION" --base-url "$BASE_URL" --domain "$domain" --registry "$REGISTRY" --install-dir "$INSTALL_DIR" --execute)
[[ "$INSTALL_WEBSITE" == true ]] && args+=(--install-website)
exec "${package_dir}/install-blue-green.sh" "${args[@]}"
