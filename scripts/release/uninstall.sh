#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="${script_dir}/uninstall-platform.sh"
if [[ ! -x "${target}" && -x "${script_dir}/../uninstall-platform.sh" ]]; then
  target="${script_dir}/../uninstall-platform.sh"
fi
[[ -x "${target}" ]] || { echo "uninstall-platform.sh is missing from ${script_dir}" >&2; exit 1; }

# A deployed installation keeps this wrapper beside docker-compose.yaml. In
# that case, an argument-free invocation should operate on the installation
# that the user is standing in, not silently fall back to /var/lib/hypercdr.
# Package archives contain compose.yaml instead, so explicit options remain
# unchanged.
has_install_dir=false
has_compose_file=false
for arg in "$@"; do
  case "${arg}" in
    --install-dir) has_install_dir=true ;;
    --compose-file) has_compose_file=true ;;
  esac
done
if [[ "${has_install_dir}" == false && "${has_compose_file}" == false && -f "${script_dir}/docker-compose.yaml" ]]; then
  set -- --install-dir "${script_dir}" "$@"
fi
exec "${target}" "$@"
