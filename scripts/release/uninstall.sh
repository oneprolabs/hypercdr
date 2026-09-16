#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd -P)"
target="${script_dir}/uninstall-platform.sh"
if [[ ! -x "${target}" && -x "${script_dir}/../uninstall-platform.sh" ]]; then
  target="${script_dir}/../uninstall-platform.sh"
fi
[[ -x "${target}" ]] || { echo "uninstall-platform.sh is missing from ${script_dir}" >&2; exit 1; }

exec "${target}" "$@"
