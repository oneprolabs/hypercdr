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
AUTH_CHALLENGE="auto"
WORK_DIR=""

usage() {
  cat <<'USAGE'
HyperCDR online installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh | sudo bash -s -- --base-url https://HOST:12443 [--install-website] [options]

Options:
  --version VERSION          GitHub Release version; default: latest
  --base-url URL             Required public control-plane URL
  --public-base-url URL      Optional public URL used in generated commands
  --registry REGISTRY        Optional image registry override
  --install-dir PATH         Installation directory, default: /var/lib/hypercdr
  --auth-challenge MODE      Authentication mode: auto, turnstile, or image
  --yes                      Skip confirmation
  -h, --help                 Show this help

The script downloads the versioned installer asset from a GitHub Release and
delegates installation to install-blue-green.sh. It does not use Bootstrap.
USAGE
}

fail() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
info() { printf '[HyperCDR] %s\n' "$*"; }

read_prompt() {
  if [[ -r /dev/tty ]]; then
    read -r "$@" </dev/tty
  else
    read -r "$@"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:?missing value for --version}"; shift 2 ;;
    --base-url) BASE_URL="${2:?missing value for --base-url}"; shift 2 ;;
    --public-base-url) PUBLIC_BASE_URL="${2:?missing value for --public-base-url}"; shift 2 ;;
    --install-website) INSTALL_WEBSITE=true; shift ;;
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:?missing value for --install-dir}"; shift 2 ;;
    --auth-challenge) AUTH_CHALLENGE="${2:?missing value for --auth-challenge}"; shift 2 ;;
    --yes) ASSUME_YES="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage; fail "unknown option: $1" ;;
  esac
done

case "$AUTH_CHALLENGE" in
  auto|image|turnstile) ;;
  *) usage; fail "--auth-challenge must be auto, image, or turnstile" ;;
esac

[[ -n "$BASE_URL" ]] || { usage; fail "--base-url is required"; }
command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v docker >/dev/null 2>&1 || fail "Docker is required; install Docker before running this installer"
[[ "$(id -u)" -eq 0 ]] || fail "run this command as root, for example: curl ... | sudo bash -s -- ..."

info "Preparing the HyperCDR online installer."
if [[ -z "$VERSION" ]]; then
  info "Resolving the latest GitHub Release."
  latest_url="https://raw.githubusercontent.com/${OWNER_REPO}/main/deploy/online/latest.json"
  VERSION="$(curl -fsSL "$latest_url" | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
fi
[[ -n "$VERSION" ]] || fail "could not resolve a GitHub Release version"
case "$VERSION" in
 ce-v*) RELEASE_TAG="$VERSION" ;;
 v*) RELEASE_TAG="$VERSION"; VERSION="${VERSION#v}" ;;
 *) RELEASE_TAG="v$VERSION" ;;
esac
[[ "$VERSION" =~ ^ce-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ || "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ ]] || fail "invalid Community version"
asset="hypercdr-installer-${VERSION}.tar.gz"
asset_url="https://github.com/${OWNER_REPO}/releases/download/${RELEASE_TAG}/${asset}"
info "Using GitHub Release asset ${asset}."
WORK_DIR="$(mktemp -d /tmp/hypercdr-online.XXXXXX)"
trap 'rm -rf "${WORK_DIR}"' EXIT

printf 'HyperCDR online installer\nVersion: %s\nBase URL: %s\nInstall directory: %s\n' "$VERSION" "$BASE_URL" "$INSTALL_DIR"
if [[ "$ASSUME_YES" != "true" ]]; then
  printf 'Continue installation? [y/N] '
  read_prompt answer
  [[ "$answer" =~ ^[Yy]$ ]] || { echo 'Installation cancelled.'; exit 0; }
fi

info "Downloading the ${VERSION} installer package."
curl -fL --retry 3 --progress-bar -o "${WORK_DIR}/${asset}" "$asset_url"
info "Extracting the installer package."
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

turnstile_configured=false
if [[ -n "${HCDR_TURNSTILE_SITE_KEY:-}" && -n "${HCDR_TURNSTILE_SECRET_KEY:-}" ]]; then
  turnstile_configured=true
fi

case "$AUTH_CHALLENGE" in
  turnstile)
    [[ "$turnstile_configured" == true ]] || fail "Turnstile was requested but the release package has no complete Turnstile credentials"
    HCDR_AUTH_CHALLENGE_MODE=turnstile
    ;;
  image)
    HCDR_AUTH_CHALLENGE_MODE=image
    ;;
  auto)
    if [[ "$ASSUME_YES" == "true" ]]; then
      HCDR_AUTH_CHALLENGE_MODE=image
      info "Non-interactive installation selected image captcha by default."
    elif [[ "$turnstile_configured" == true ]]; then
      printf 'Have you added this installation host/IP to the Cloudflare Turnstile allowed hostnames list? [y/N] '
      read_prompt turnstile_allowed
      if [[ "$turnstile_allowed" =~ ^[Yy]$ ]]; then
        HCDR_AUTH_CHALLENGE_MODE=turnstile
      else
        HCDR_AUTH_CHALLENGE_MODE=image
      fi
    else
      HCDR_AUTH_CHALLENGE_MODE=image
      info "Turnstile credentials are unavailable; using image captcha."
    fi
    ;;
esac
export HCDR_AUTH_CHALLENGE_MODE
info "Authentication mode: ${HCDR_AUTH_CHALLENGE_MODE}."

info "Starting the HyperCDR control-plane installation."

domain="${BASE_URL#https://}"
domain="${domain%%/*}"
domain="${domain%%:*}"
args=("$VERSION" --base-url "$BASE_URL" --domain "$domain" --registry "$REGISTRY" --install-dir "$INSTALL_DIR" --execute)
[[ "$INSTALL_WEBSITE" == true ]] && args+=(--install-website)
exec "${package_dir}/install-blue-green.sh" "${args[@]}"
