#!/usr/bin/env bash
set -euo pipefail

HARBOR_PREFIX="${HCDR_IMAGE_REGISTRY:-}"
BUNDLE_DIR="${HCDR_IMAGE_BUNDLE_DIR:-./bootstrap-images}"
MANIFEST="${HCDR_IMAGE_MANIFEST:-}"
USERNAME="${HCDR_HARBOR_USERNAME:-}"
PASSWORD="${HCDR_HARBOR_PASSWORD:-}"
EXECUTE="false"
SKIP_LOGIN="false"

usage() {
  cat <<'USAGE'
Import image bundle and push to Harbor

Usage:
  ./import-images.sh --registry HOST/hypercdr --bundle-dir PATH [options]

Options:
  --registry PREFIX       Target Harbor project prefix, for example 192.168.8.149:5001/hypercdr.
  --bundle-dir PATH       Directory containing images-manifest.json and image tar files.
  --manifest PATH         Manifest path. Defaults to <bundle-dir>/images-manifest.json.
  --username USER         Harbor username. If set with password, docker login is run.
  --password PASSWORD     Harbor password.
  --skip-login            Do not run docker login.
  --execute               Run docker load/tag/push. Without this flag, prints the plan only.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) HARBOR_PREFIX="${2:?missing value for --registry}"; shift 2 ;;
    --bundle-dir) BUNDLE_DIR="${2:?missing value for --bundle-dir}"; shift 2 ;;
    --manifest) MANIFEST="${2:?missing value for --manifest}"; shift 2 ;;
    --username) USERNAME="${2:?missing value for --username}"; shift 2 ;;
    --password) PASSWORD="${2:?missing value for --password}"; shift 2 ;;
    --skip-login) SKIP_LOGIN="true"; shift ;;
    --execute) EXECUTE="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${HARBOR_PREFIX}" ]]; then
  echo "--registry is required" >&2
  exit 2
fi
if [[ -z "${MANIFEST}" ]]; then
  MANIFEST="${BUNDLE_DIR%/}/images-manifest.json"
fi
if [[ ! -f "${MANIFEST}" ]]; then
  echo "image manifest not found: ${MANIFEST}" >&2
  exit 1
fi

HARBOR_PREFIX="${HARBOR_PREFIX%/}"
HARBOR_HOST="${HARBOR_PREFIX%%/*}"
MANIFEST_DIR="$(cd "$(dirname "${MANIFEST}")" && pwd)"

parse_manifest_images() {
  awk '
    function value(line) {
      sub(/^[^:]*:[[:space:]]*"/, "", line)
      sub(/".*$/, "", line)
      return line
    }
    /"target"[[:space:]]*:/ { target=value($0) }
    /"repository"[[:space:]]*:/ { repo=value($0) }
    /"tag"[[:space:]]*:/ { tag=value($0) }
    /"tar"[[:space:]]*:/ { tar=value($0) }
    tar != "" && repo != "" && tag != "" {
      print tar "\t" target "\t" repo "\t" tag
      target=""; repo=""; tag=""; tar=""
    }
  ' "${MANIFEST}"
}

cat <<EOF
Harbor image import plan

Target registry: ${HARBOR_PREFIX}
Manifest:        ${MANIFEST}
Docker login:    $([[ -n "${USERNAME}" && -n "${PASSWORD}" && "${SKIP_LOGIN}" != "true" ]] && echo "yes (${USERNAME}@${HARBOR_HOST})" || echo "no")
Execute changes: ${EXECUTE}
EOF

if [[ "${EXECUTE}" != "true" ]]; then
  echo
  echo "Dry-run mode. Add --execute to import images."
  exit 0
fi

if [[ -n "${USERNAME}" && -n "${PASSWORD}" && "${SKIP_LOGIN}" != "true" ]]; then
  printf '%s' "${PASSWORD}" | docker login "${HARBOR_HOST}" -u "${USERNAME}" --password-stdin
fi

while IFS=$'\t' read -r tar_rel target_from_manifest repo tag; do
  [[ -n "${tar_rel}" && -n "${repo}" && -n "${tag}" ]] || continue
  tar_path="${MANIFEST_DIR}/${tar_rel}"
  target_image="${HARBOR_PREFIX}/${repo}:${tag}"
  echo "Import: ${tar_path}"
  echo "Push:   ${target_from_manifest:-"(manifest target unset)"} -> ${target_image}"
  if [[ ! -f "${tar_path}" ]]; then
    echo "image tar not found: ${tar_path}" >&2
    exit 1
  fi
  load_output="$(docker load -i "${tar_path}")"
  echo "${load_output}"
  loaded_image="$(printf '%s\n' "${load_output}" | sed -n 's/^Loaded image: //p' | tail -1)"
  if [[ -z "${loaded_image}" ]]; then
    loaded_image="$(printf '%s\n' "${load_output}" | sed -n 's/^Loaded image ID: //p' | tail -1)"
  fi
  if [[ -z "${loaded_image}" ]]; then
    echo "failed to detect loaded image from docker load output" >&2
    exit 1
  fi
  docker tag "${loaded_image}" "${target_image}"
  docker push "${target_image}"
done < <(parse_manifest_images)

echo "Image import completed."
