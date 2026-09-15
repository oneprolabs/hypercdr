#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
edition=community
version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) echo "Usage: $0 --edition community --version VERSION"; exit 0 ;;
    --edition) edition="${2:?missing edition}"; shift 2 ;;
    --version) version="${2:?missing version}"; shift 2 ;;
    *) echo "Usage: $0 --edition community --version VERSION" >&2; exit 2 ;;
  esac
done
[[ "$edition" == community ]] || { echo "Enterprise packages must be built by the Enterprise repository." >&2; exit 2; }
[[ -n "$version" ]] || { echo "--version is required" >&2; exit 2; }
exec bash "${SCRIPT_DIR}/package-release.sh" "$version"
