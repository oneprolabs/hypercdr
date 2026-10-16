#!/usr/bin/env bash
set -Eeuo pipefail
VERSION=""; BASE_URL=""; INSTALL_DIR=/var/lib/hypercdr-enterprise; YES=false
usage() {
  echo 'Enterprise online installer: --base-url https://HOST:12447 [--version TAG] [--install-dir PATH] [--yes]'
  echo 'Requires Docker Compose, Python 3, curl, tar, sha256sum and public HTTPS access to GitHub and the image registry.'
}
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="${2:?missing version}"; shift 2;;
    --base-url) BASE_URL="${2:?missing URL}"; shift 2;;
    --install-dir) INSTALL_DIR="${2:?missing directory}"; shift 2;;
    --yes) YES=true; shift;;
    -h|--help) usage; exit 0;;
    *) usage; exit 2;;
  esac
done
[[ $(id -u) == 0 ]] || { echo 'Run as root' >&2; exit 1; }
for tool in docker python3 curl tar sha256sum openssl; do command -v "$tool" >/dev/null; done
docker compose version >/dev/null
PORT=$(python3 - "$BASE_URL" <<'PY'
import sys
from urllib.parse import urlsplit
u=urlsplit(sys.argv[1])
assert u.scheme=='https' and u.hostname and not u.username and not u.password and u.path in ('','/') and not u.query and not u.fragment, 'Use an HTTPS origin'
p=u.port or 443
assert p not in (3002,12443,12445), 'Community ports are reserved'
print(p)
PY
)
[[ "$INSTALL_DIR" == /* && "$INSTALL_DIR" != / && "$INSTALL_DIR" != /var/lib/hypercdr && ! -e "$INSTALL_DIR/.env" ]] || { echo 'Unsafe or existing installation directory' >&2; exit 1; }
if [[ -z "$VERSION" ]]; then
  # Enterprise releases share a public repository with Community; never use /latest.
  WORK_INDEX=$(mktemp -d)
  trap 'rm -rf "$WORK_INDEX"' EXIT
  VERSION=""
  for page in {1..20}; do
    curl -q --fail --silent --show-error --location --retry 3 --proto '=https' --proto-redir '=https' \
      "https://api.github.com/repos/oneprolabs/hypercdr/releases?per_page=100&page=$page" > "$WORK_INDEX/releases.json"
    VERSION=$(python3 - "$WORK_INDEX/releases.json" <<'PYTHON'
import json,re,sys
releases=json.load(open(sys.argv[1])); assert isinstance(releases,list)
for r in releases:
 t=r['tag_name']
 if r.get('draft') or r.get('prerelease') or not re.fullmatch(r'(?:ee-v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)|v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}-ent\.[0-9]+)',t): continue
 names={a['name'] for a in r.get('assets',[])}
 if {f'hypercdr-enterprise-installer-{t}.tar.gz',f'hypercdr-enterprise-installer-{t}.sha256'} <= names:
  print(t); break
PYTHON
)
    [[ -z "$VERSION" ]] || break
    [[ $(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))))' "$WORK_INDEX/releases.json") == 100 ]] || break
  done
  rm -rf "$WORK_INDEX"
  trap - EXIT
  [[ -n "$VERSION" ]] || { echo 'No public Enterprise installer release found' >&2; exit 1; }
fi
[[ "$VERSION" =~ ^ee-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ || "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}-ent\.[0-9]+$ ]] || { echo 'Invalid Enterprise release tag' >&2; exit 1; }
echo "Install Enterprise $VERSION at $BASE_URL in $INSTALL_DIR"
if [[ "$YES" != true ]]; then
  read -r -p 'Continue? [y/N] ' answer </dev/tty
  [[ "$answer" == y || "$answer" == Y ]] || exit 1
fi
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT
archive="hypercdr-enterprise-installer-$VERSION.tar.gz"
checksum="${archive%.tar.gz}.sha256"
for asset in "$archive" "$checksum"; do
  curl -q --fail --silent --show-error --location --retry 3 --proto '=https' --proto-redir '=https' \
    "https://github.com/oneprolabs/hypercdr/releases/download/$VERSION/$asset" -o "$WORK_DIR/$asset"
done
cd "$WORK_DIR"
python3 - "$checksum" "$archive" <<'PY'
from pathlib import Path
import re,sys
s=Path(sys.argv[1]).read_text().strip()
assert re.fullmatch(r'[0-9a-f]{64}  '+re.escape(sys.argv[2]),s), 'Invalid checksum file'
PY
sha256sum -c "$checksum"
mkdir package
python3 - "$archive" <<'PY'
import tarfile,sys
from pathlib import PurePosixPath
with tarfile.open(sys.argv[1]) as t:
 for m in t.getmembers():
  p=PurePosixPath(m.name)
  assert not p.is_absolute() and '..' not in p.parts and (m.isfile() or m.isdir()), 'Unsafe archive entry'
 t.extractall('package')
PY
python3 - "$VERSION" <<'PY'
import json,sys
m=json.load(open('package/release-manifest.json'))
assert m['edition']=='enterprise' and m['version']==sys.argv[1], 'Release identity mismatch'
PY
HCDR_ENTERPRISE_INSTALL_DIR="$INSTALL_DIR" HCDR_ENTERPRISE_PUBLIC_URL="$BASE_URL" HCDR_ENTERPRISE_PORT="$PORT" bash package/install.sh </dev/null
printf 'Enterprise installed: %s/login\n' "${BASE_URL%/}"
