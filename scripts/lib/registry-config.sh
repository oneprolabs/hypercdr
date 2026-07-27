#!/usr/bin/env bash

registry_config_die() { echo "error: $*" >&2; return 1; }

load_registry_profile() {
  local config_file="$1" requested_profile="${2:-}" profile upper field variable value
  [[ -r "${config_file}" ]] || registry_config_die "registry config is not readable: ${config_file}" || return 1
  # shellcheck disable=SC1090
  source "${config_file}"
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
