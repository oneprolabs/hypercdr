#!/usr/bin/env bash

registry_config_die() { echo "error: $*" >&2; return 1; }

# ACR/Docker Hub profiles name a repository; Harbor profiles name a project.
image_ref() {
  local registry="${1%/}" name="$2" version="$3"
  case "$registry" in
    *.aliyuncs.com/*/*|docker.io/*/*) printf '%s:%s-%s\n' "$registry" "$name" "$version" ;;
    *) printf '%s/%s:%s\n' "$registry" "$name" "$version" ;;
  esac
}

image_digest() {
  local image="$1" repo="${1%:*}"
  docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image" 2>/dev/null |
    awk -F@ -v repo="$repo" '
      BEGIN { sub(/^docker[.]io\//, "", repo) }
      { actual=$1; sub(/^docker[.]io\//, "", actual); if (actual == repo && $2 ~ /^sha256:[0-9a-f]{64}$/) { print $2; exit } }
    '
}

load_registry_profile() {
  local config_file="$1" requested_profile="${2:-}" profile upper field variable value
  local postgres_source_override="${HCDR_POSTGRES_SOURCE_IMAGE_OVERRIDE-}"
  local velero_plugin_source_override="${HCDR_VELERO_PLUGIN_SOURCE_REGISTRY_OVERRIDE-}"
  [[ -r "${config_file}" ]] || registry_config_die "registry config is not readable: ${config_file}" || return 1
  # shellcheck disable=SC1090
  source "${config_file}"
  if [[ -n "${postgres_source_override}" ]]; then
    HCDR_POSTGRES_SOURCE_IMAGE="${postgres_source_override}"
  fi
  if [[ -n "${velero_plugin_source_override}" ]]; then
    HCDR_VELERO_PLUGIN_SOURCE_REGISTRY="${velero_plugin_source_override}"
  fi
  profile="${requested_profile:-${HCDR_ACTIVE_REGISTRY:-}}"
  [[ -n "${profile}" ]] || registry_config_die "HCDR_ACTIVE_REGISTRY is not configured" || return 1
  [[ "${profile}" =~ ^[a-z0-9_]+$ ]] || registry_config_die "invalid registry profile: ${profile}" || return 1
  case " ${HCDR_REGISTRY_PROFILES:-} " in *" ${profile} "*) ;; *) registry_config_die "unknown registry profile '${profile}'; available: ${HCDR_REGISTRY_PROFILES:-none}" || return 1 ;; esac
  upper="${profile^^}"
  for field in PROVIDER SERVER PREFIX VISIBILITY TRUST CA_FILE; do
    variable="HCDR_REGISTRY_${upper}_${field}"
    value="${!variable-}"
    printf -v "HCDR_SELECTED_REGISTRY_${field}" '%s' "${value}"
    export "HCDR_SELECTED_REGISTRY_${field}"
  done
  [[ -n "${HCDR_SELECTED_REGISTRY_SERVER}" ]] || registry_config_die "profile ${profile} has no SERVER" || return 1
  [[ -n "${HCDR_SELECTED_REGISTRY_PREFIX}" ]] || registry_config_die "profile ${profile} has no PREFIX" || return 1
  HCDR_SELECTED_REGISTRY="${profile}"
  HCDR_IMAGE_REGISTRY="${HCDR_SELECTED_REGISTRY_PREFIX%/}"
  HCDR_REGISTRY_SERVER="${HCDR_SELECTED_REGISTRY_SERVER}"
  HCDR_REGISTRY_TRUST="${HCDR_SELECTED_REGISTRY_TRUST:-system}"
  HCDR_REGISTRY_CA_FILE="${HCDR_SELECTED_REGISTRY_CA_FILE}"
  export HCDR_SELECTED_REGISTRY HCDR_IMAGE_REGISTRY HCDR_REGISTRY_SERVER HCDR_REGISTRY_TRUST HCDR_REGISTRY_CA_FILE
  export HCDR_POSTGRES_SOURCE_IMAGE HCDR_VELERO_PLUGIN_SOURCE_REGISTRY
}
