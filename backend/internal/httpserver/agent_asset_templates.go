package httpserver

const prepareNodeScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

REGISTRY_HOST="{{REGISTRY_HOST}}"
REGISTRY_CA_URL="{{REGISTRY_CA_URL}}"
PLATFORM_CA_URL="{{PLATFORM_CA_URL}}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry-host)
      REGISTRY_HOST="${2:-}"
      shift 2
      ;;
    --registry-ca-url)
      REGISTRY_CA_URL="${2:-}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$REGISTRY_HOST" ]]; then
  echo "--registry-host is required" >&2
  exit 2
fi
if [[ -z "$REGISTRY_CA_URL" ]]; then
  echo "--registry-ca-url is required" >&2
  exit 2
fi

run_root() {
  if [[ "$(id -u)" == "0" ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    echo "root privileges are required; run as root or install sudo" >&2
    exit 1
  fi
}

download_ca() {
  local output="$1"
  if command -v curl >/dev/null 2>&1; then
    if [[ "$REGISTRY_CA_URL" == https://* ]]; then
      curl -k -fsSL "$REGISTRY_CA_URL" -o "$output"
    else
      curl -fsSL "$REGISTRY_CA_URL" -o "$output"
    fi
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    if [[ "$REGISTRY_CA_URL" == https://* ]]; then
      wget --no-check-certificate -qO "$output" "$REGISTRY_CA_URL"
    else
      wget -qO "$output" "$REGISTRY_CA_URL"
    fi
    return
  fi
  echo "curl or wget is required" >&2
  exit 1
}

ensure_containerd_config_path() {
  local config_file="/etc/containerd/config.toml"
  if ! command -v containerd >/dev/null 2>&1 && ! run_root test -d /etc/containerd; then
    return 0
  fi
  run_root mkdir -p /etc/containerd
  if ! run_root test -f "$config_file"; then
    if command -v containerd >/dev/null 2>&1; then
      containerd config default >"${TMP_DIR}/containerd-config.toml"
    else
      cat >"${TMP_DIR}/containerd-config.toml" <<EOF
version = 2
EOF
    fi
    run_root install -m 0644 "${TMP_DIR}/containerd-config.toml" "$config_file"
  fi

  if run_root grep -Eq '^[[:space:]]*config_path[[:space:]]*=' "$config_file"; then
    run_root sed -i -E 's#^([[:space:]]*config_path[[:space:]]*=).*#\1 "/etc/containerd/certs.d"#' "$config_file"
    return 0
  fi

  if grep -q '^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]' "$config_file"; then
    awk '
      /^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]/ && inserted == 0 {
        print
        print "  config_path = \"/etc/containerd/certs.d\""
        inserted = 1
        next
      }
      { print }
    ' "$config_file" >"${TMP_DIR}/containerd-config.toml"
  else
    cp "$config_file" "${TMP_DIR}/containerd-config.toml"
    cat >>"${TMP_DIR}/containerd-config.toml" <<EOF

[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
EOF
  fi
  run_root install -m 0644 "${TMP_DIR}/containerd-config.toml" "$config_file"
}

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
CA_FILE="${TMP_DIR}/hypercdr-registry-ca.crt"

echo "Downloading HyperCDR registry CA from ${REGISTRY_CA_URL}"
download_ca "$CA_FILE"

if command -v openssl >/dev/null 2>&1; then
  openssl x509 -in "$CA_FILE" -noout -subject -dates
else
  echo "warning: openssl is not installed; skipping certificate details"
fi

CONTAINERD_CA_DIR="/etc/containerd/certs.d/${REGISTRY_HOST}"
DOCKER_CA_DIR="/etc/docker/certs.d/${REGISTRY_HOST}"

echo "Installing CA into ${CONTAINERD_CA_DIR} and ${DOCKER_CA_DIR}"
run_root mkdir -p "$CONTAINERD_CA_DIR" "$DOCKER_CA_DIR"
run_root install -m 0644 "$CA_FILE" "${CONTAINERD_CA_DIR}/ca.crt"
run_root install -m 0644 "$CA_FILE" "${DOCKER_CA_DIR}/ca.crt"

HOSTS_FILE="${TMP_DIR}/hosts.toml"
cat >"$HOSTS_FILE" <<EOF
server = "https://${REGISTRY_HOST}"

[host."https://${REGISTRY_HOST}"]
  capabilities = ["pull", "resolve"]
  ca = "ca.crt"
EOF
run_root install -m 0644 "$HOSTS_FILE" "${CONTAINERD_CA_DIR}/hosts.toml"
ensure_containerd_config_path

if command -v systemctl >/dev/null 2>&1; then
  echo "Restarting container runtimes when present"
  run_root systemctl restart containerd || true
  run_root systemctl restart docker || true
else
  echo "systemctl is unavailable; restart containerd/docker manually if image pulls still fail"
fi

echo "HyperCDR registry CA is installed on this node for ${REGISTRY_HOST}"
`

const installScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

TOKEN=""
ENDPOINT="{{AGENT_WS_ENDPOINT}}"
ENDPOINT_PRIVATE=""
ENDPOINT_PUBLIC=""
CLUSTER_TYPE="native-kubernetes"
KUBECONFIG_PATH=""
KUBECTL_CONTEXT=""
TOKEN_VALIDATE_URL="{{TOKEN_VALIDATE_URL}}"
AGENT_UNINSTALL_URL="{{AGENT_UNINSTALL_URL}}"
NAMESPACE="{{AGENT_NAMESPACE}}"
AGENT_IMAGE="{{AGENT_IMAGE}}"
OADP_AGENT_IMAGE="{{OADP_AGENT_IMAGE}}"
OADP_CATALOG_IMAGE="{{OADP_CATALOG_IMAGE}}"
OADP_RUNTIME_IMAGES="{{OADP_RUNTIME_IMAGES}}"
AGENT_COMMAND="/comm-agent"
BACKUP_BACKEND="velero"
VELERO_IMAGE="{{VELERO_IMAGE}}"
VELERO_AWS_PLUGIN_IMAGE="{{VELERO_AWS_PLUGIN_IMAGE}}"
VELERO_AZURE_PLUGIN_IMAGE="{{VELERO_AZURE_PLUGIN_IMAGE}}"
VELERO_GCP_PLUGIN_IMAGE="{{VELERO_GCP_PLUGIN_IMAGE}}"
EXECUTOR_MODE="kubernetes"
VELERO_CRDS_URL="{{VELERO_CRDS_URL}}"
REGISTRY_CA_URL="{{REGISTRY_CA_URL}}"
PLATFORM_CA_URL="{{PLATFORM_CA_URL}}"
INSTALL_VELERO="true"
ALLOW_EXISTING_VELERO="false"
REGISTRY_SERVER=""
REGISTRY_USERNAME=""
REGISTRY_PASSWORD=""
REGISTRY_EMAIL="hypercdr@example.local"
IMAGE_PULL_SECRET="hypercdr-registry"
IMAGE_PULL_SECRETS_BLOCK=""
AGENT_POD_SECURITY_CONTEXT_BLOCK=$'      securityContext:\n        fsGroup: 65532'
AGENT_CONTAINER_SECURITY_CONTEXT_BLOCK=""
AGENT_PLATFORM_RBAC_RULES=""
RESET_AGENT_CREDENTIAL="true"
SKIP_IMAGE_PREFLIGHT="false"
IMAGE_PULL_PREFLIGHT="true"
WAIT_READY="true"
WAIT_TIMEOUT="300s"
INSTALL_REGISTRY_CA="true"
NODE_SSH_USER=""
NODE_SSH_KEY=""
NODE_SSH_PORT="22"
INTERACTIVE="true"
SCENARIO="fresh-install"
STORAGE_CLASS=""
AGENT_CPU_REQUEST="${HCDR_AGENT_CPU_REQUEST:-50m}"
AGENT_MEMORY_REQUEST="${HCDR_AGENT_MEMORY_REQUEST:-128Mi}"
AGENT_CPU_LIMIT="${HCDR_AGENT_CPU_LIMIT:-500m}"
AGENT_MEMORY_LIMIT="${HCDR_AGENT_MEMORY_LIMIT:-512Mi}"
VELERO_CPU_REQUEST="${HCDR_VELERO_CPU_REQUEST:-100m}"
VELERO_MEMORY_REQUEST="${HCDR_VELERO_MEMORY_REQUEST:-128Mi}"
VELERO_CPU_LIMIT="${HCDR_VELERO_CPU_LIMIT:-500m}"
VELERO_MEMORY_LIMIT="${HCDR_VELERO_MEMORY_LIMIT:-512Mi}"
NODE_AGENT_CPU_REQUEST="${HCDR_NODE_AGENT_CPU_REQUEST:-50m}"
NODE_AGENT_MEMORY_REQUEST="${HCDR_NODE_AGENT_MEMORY_REQUEST:-128Mi}"
NODE_AGENT_CPU_LIMIT="${HCDR_NODE_AGENT_CPU_LIMIT:-750m}"
NODE_AGENT_MEMORY_LIMIT="${HCDR_NODE_AGENT_MEMORY_LIMIT:-1Gi}"
KUBECTL_BIN="kubectl"
DETECTED_CLUSTER_NAME=""
DETECTED_CLOUD_REGION=""
DETECTED_CLOUD_CLUSTER_ID=""

log_time() {
  date '+%H:%M:%S'
}

log_section() {
  echo
  echo "==> $1"
}

log_info() {
  echo "[INFO  $(log_time)] $1"
}

log_ok() {
  echo "[OK    $(log_time)] $1"
}

log_warn() {
  echo "[WARN  $(log_time)] $1" >&2
}

log_error() {
  echo "[ERROR $(log_time)] $1" >&2
}

fail() {
  local message="$1"
  local code="${2:-1}"
  log_error "$message"
  # Explicit exit does not trigger Bash's ERR trap. Once installation has
  # entered its mutating phase, invoke the same rollback path directly so a
  # provider or core validation cannot leave a partial namespace behind.
  if [[ "${ROLLBACK_ACTIVE:-false}" == "true" ]] && declare -F rollback_failed_registration >/dev/null; then
    ROLLBACK_ACTIVE="false"
    rollback_failed_registration
  fi
  exit "$code"
}

{{INSTALLER_PROVIDER_MODULES}}

print_install_summary() {
  log_section "HyperCDR agent installer"
  log_info "Target namespace: ${NAMESPACE}"
  log_info "Platform endpoint: ${ENDPOINT}"
  log_info "Executor mode: ${EXECUTOR_MODE}"
  log_info "Cluster type: ${CLUSTER_TYPE}"
  log_info "Agent image: ${AGENT_IMAGE}"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    log_info "Velero image: ${VELERO_IMAGE}"
  else
    log_info "Velero install: skipped"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --token)
      TOKEN="${2:-}"
      shift 2
      ;;
    --endpoint)
      ENDPOINT="${2:-}"
      shift 2
      ;;
    --endpoint-private)
      ENDPOINT_PRIVATE="${2:-}"
      shift 2
      ;;
    --endpoint-public)
      ENDPOINT_PUBLIC="${2:-}"
      shift 2
      ;;
    --cluster-type)
      CLUSTER_TYPE="${2:-}"
      shift 2
      ;;
    --kubeconfig)
      KUBECONFIG_PATH="${2:-}"
      shift 2
      ;;
    --context)
      KUBECTL_CONTEXT="${2:-}"
      shift 2
      ;;
    --namespace)
      NAMESPACE="${2:-}"
      shift 2
      ;;
    --executor-mode)
      EXECUTOR_MODE="${2:-}"
      shift 2
      ;;
    --install-velero)
      INSTALL_VELERO="${2:-}"
      shift 2
      ;;
    --allow-existing-velero)
      ALLOW_EXISTING_VELERO="${2:-}"
      shift 2
      ;;
    --velero-crds-url)
      VELERO_CRDS_URL="${2:-}"
      shift 2
      ;;
    --registry-ca-url)
      REGISTRY_CA_URL="${2:-}"
      shift 2
      ;;
    --registry-server)
      REGISTRY_SERVER="${2:-}"
      shift 2
      ;;
    --registry-username)
      REGISTRY_USERNAME="${2:-}"
      shift 2
      ;;
    --registry-password)
      REGISTRY_PASSWORD="${2:-}"
      shift 2
      ;;
    --registry-email)
      REGISTRY_EMAIL="${2:-}"
      shift 2
      ;;
    --image-pull-secret)
      IMAGE_PULL_SECRET="${2:-}"
      shift 2
      ;;
    --reset-agent-credential)
      RESET_AGENT_CREDENTIAL="${2:-}"
      shift 2
      ;;
    --skip-image-preflight)
      SKIP_IMAGE_PREFLIGHT="${2:-}"
      shift 2
      ;;
    --image-pull-preflight)
      IMAGE_PULL_PREFLIGHT="${2:-}"
      shift 2
      ;;
    --wait)
      WAIT_READY="${2:-}"
      shift 2
      ;;
    --wait-timeout)
      WAIT_TIMEOUT="${2:-}"
      shift 2
      ;;
    --install-registry-ca)
      INSTALL_REGISTRY_CA="${2:-}"
      shift 2
      ;;
    --node-ssh-user)
      NODE_SSH_USER="${2:-}"
      shift 2
      ;;
    --node-ssh-key)
      NODE_SSH_KEY="${2:-}"
      shift 2
      ;;
    --node-ssh-port)
      NODE_SSH_PORT="${2:-}"
      shift 2
      ;;
    --interactive)
      INTERACTIVE="${2:-}"
      shift 2
      ;;
    --scenario)
      SCENARIO="${2:-}"
      shift 2
      ;;
    --storage-class)
      STORAGE_CLASS="${2:-}"
      shift 2
      ;;
    *)
      fail "Unknown argument: $1" 2
      ;;
  esac
done

if [[ -z "$TOKEN" ]]; then
  fail "Missing required argument: --token" 2
fi
if [[ -z "$ENDPOINT" && -z "$ENDPOINT_PRIVATE" && -z "$ENDPOINT_PUBLIC" ]]; then
  fail "Missing required argument: --endpoint" 2
fi
if [[ "$CLUSTER_TYPE" != "native-kubernetes" && "$CLUSTER_TYPE" != "huaweicloud-cce" && "$CLUSTER_TYPE" != "openshift" ]]; then
  fail "Unsupported --cluster-type '${CLUSTER_TYPE}'. Use native-kubernetes, huaweicloud-cce, or openshift." 2
fi
validate_registration_scenario
if [[ "$CLUSTER_TYPE" != "openshift" && "$NAMESPACE" != "hypercdr-agent" ]]; then
  fail "HyperCDR uses the single canonical namespace 'hypercdr-agent'. Community and Enterprise Agent installations are identical; use the control-plane handover workflow to change editions." 2
fi
if [[ "$CLUSTER_TYPE" == "openshift" && "$NAMESPACE" != "openshift-adp" ]]; then
  fail "OpenShift OADP Agent must use the canonical namespace 'openshift-adp'." 2
fi
if ! command -v curl >/dev/null 2>&1; then
  fail "curl is required but was not found in PATH"
fi
kubectl() {
  if [[ -n "$KUBECTL_CONTEXT" ]]; then command "$KUBECTL_BIN" --context "$KUBECTL_CONTEXT" "$@"; else command "$KUBECTL_BIN" "$@"; fi
}

provider_prepare_dependencies
if [[ ! -x "$KUBECTL_BIN" ]] && ! command -v "$KUBECTL_BIN" >/dev/null 2>&1; then
  fail "kubectl is required but was not found in PATH"
fi
provider_select_context

if [[ -z "$ENDPOINT" ]]; then
  if [[ -n "$ENDPOINT_PRIVATE" ]]; then
    ENDPOINT="$ENDPOINT_PRIVATE"
  elif [[ -n "$ENDPOINT_PUBLIC" ]]; then
    ENDPOINT="$ENDPOINT_PUBLIC"
  fi
fi
AGENT_RBAC_NAME="hypercdr-agent"
VELERO_RBAC_NAME="hypercdr-velero"
if [[ "$NAMESPACE" != "hypercdr-agent" ]]; then
  namespace_rbac_suffix="$(printf '%s' "$NAMESPACE" | sha256sum | cut -c1-8)"
  AGENT_RBAC_NAME="hypercdr-agent-${namespace_rbac_suffix}"
  VELERO_RBAC_NAME="hypercdr-velero-${namespace_rbac_suffix}"
fi
print_install_summary

log_section "Preflight checks"
log_info "Validating install token before changing the cluster"
token_check_file="$(mktemp)"
token_check_status="$(curl -k -sS -o "$token_check_file" -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  --data "{\"token\":\"${TOKEN}\"}" \
  "$TOKEN_VALIDATE_URL" || true)"
if [[ "$token_check_status" != "200" ]]; then
  token_check_error="$(grep -o '"error"[[:space:]]*:[[:space:]]*"[^"]*"' "$token_check_file" | head -n1 | cut -d'"' -f4 || true)"
  rm -f "$token_check_file"
  case "$token_check_error" in
    TOKEN_EXPIRED) fail "Install token has expired. Generate a new registration command in the platform." ;;
    TOKEN_USED) fail "Install token has already been used. Generate a new registration command in the platform." ;;
    TOKEN_INVALID) fail "Install token is invalid. Generate a new registration command in the platform." ;;
    *) fail "Could not validate the install token with the platform (HTTP ${token_check_status:-000}). Check platform connectivity and try again." ;;
  esac
fi
rm -f "$token_check_file"
log_ok "Install token is valid"
log_info "Checking kubectl access to the current Kubernetes cluster"
if ! kubectl version --client >/dev/null 2>&1; then
  fail "kubectl is installed, but kubectl version --client failed"
fi
if ! kubectl cluster-info >/dev/null 2>&1; then
  log_error "Cannot connect to the Kubernetes API server with the current kubeconfig."
  log_error "Check KUBECONFIG, cluster network connectivity, and Kubernetes API server certificate trust."
  exit 1
fi
log_ok "Kubernetes API is reachable"

provider_align_kubectl_version

provider_verify

log_info "Checking whether this cluster is already managed by HyperCDR"
existing_agents="$(kubectl get deployments -A -o jsonpath='{range .items[?(@.metadata.name=="hypercdr-comm-agent")]}{.metadata.namespace}{"\n"}{end}' 2>/dev/null | sed '/^[[:space:]]*$/d' | sort -u || true)"
if [[ -n "$existing_agents" ]]; then
  log_error "This cluster is already managed by a HyperCDR Agent in namespace(s): $(echo "$existing_agents" | paste -sd ', ' -)."
  log_error "Community and Enterprise use the same Agent and cannot coexist in one cluster."
  log_error "Use Community Migration for a normal edition upgrade, or Disaster Handover only when the original control plane is permanently unavailable."
  exit 1
fi
log_ok "No existing HyperCDR Agent installation was found"

select_agent_storage_class() {
  log_section "Storage preflight"
  local existing_storage_class default_storage_class selection attempts index
  local -a storage_class_names

  if kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state >/dev/null 2>&1; then
    existing_storage_class="$(kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state -o jsonpath='{.spec.storageClassName}' 2>/dev/null || true)"
    if [[ -z "$existing_storage_class" ]]; then
      if [[ -n "$STORAGE_CLASS" ]]; then
        fail "Existing PVC hypercdr-agent-state has no StorageClass and cannot be changed to '${STORAGE_CLASS}'. Remove the old installation safely before selecting another StorageClass."
      fi
      log_warn "Existing agent PVC has no StorageClass; keeping its current static volume binding."
      return 0
    fi
    if [[ -n "$STORAGE_CLASS" && "$STORAGE_CLASS" != "$existing_storage_class" ]]; then
      fail "Existing PVC hypercdr-agent-state uses StorageClass '${existing_storage_class}', which cannot be changed to '${STORAGE_CLASS}'. Remove the old installation safely or rerun with --storage-class ${existing_storage_class}."
    fi
    STORAGE_CLASS="$existing_storage_class"
    log_ok "Existing agent PVC uses StorageClass: ${STORAGE_CLASS}"
    return 0
  fi

  mapfile -t storage_class_names < <(kubectl get storageclass -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sed '/^[[:space:]]*$/d' | sort -u)
  if [[ ${#storage_class_names[@]} -eq 0 ]]; then
    log_error "Installation stopped: no StorageClass is available in this cluster."
    log_error "HyperCDR Agent requires a 1 GiB persistent volume for its state."
    log_error "Recommended actions:"
    log_error "1. Install a CSI storage provider."
    log_error "2. Create a StorageClass."
    log_error "3. Set it as the default, or rerun with --storage-class <name>."
    log_error "4. Verify with: kubectl get storageclass"
    exit 1
  fi

  if [[ -n "$STORAGE_CLASS" ]]; then
    if ! kubectl get storageclass "$STORAGE_CLASS" >/dev/null 2>&1; then
      log_error "StorageClass '${STORAGE_CLASS}' does not exist."
      log_error "Available StorageClasses: ${storage_class_names[*]}"
      exit 1
    fi
    log_ok "Selected StorageClass: ${STORAGE_CLASS}"
    return 0
  fi

  default_storage_class="$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' | head -n1)"
  if [[ -z "$default_storage_class" ]]; then
    default_storage_class="$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.beta\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' | head -n1)"
  fi
  if [[ -n "$default_storage_class" ]]; then
    STORAGE_CLASS="$default_storage_class"
    log_ok "Default StorageClass detected: ${STORAGE_CLASS}"
    return 0
  fi

  log_warn "No default StorageClass was found."
  if [[ "$INTERACTIVE" != "true" ]] || ! { exec 3<>/dev/tty; } 2>/dev/null; then
    log_error "A StorageClass must be selected in this non-interactive environment."
    log_error "Available StorageClasses: ${storage_class_names[*]}"
    log_error "Rerun this command with: --storage-class <name>"
    exit 1
  fi
  echo >&3
  echo "Select a StorageClass for the HyperCDR Agent state volume:" >&3
  for index in "${!storage_class_names[@]}"; do
    local name provisioner binding_mode
    name="${storage_class_names[$index]}"
    provisioner="$(kubectl get storageclass "$name" -o jsonpath='{.provisioner}')"
    binding_mode="$(kubectl get storageclass "$name" -o jsonpath='{.volumeBindingMode}')"
    printf '%d) %s\n   Provisioner: %s\n   Binding mode: %s\n' "$((index + 1))" "$name" "${provisioner:-unknown}" "${binding_mode:-Immediate}" >&3
  done
  attempts=0
  while [[ $attempts -lt 3 ]]; do
    printf 'Enter selection [1-%d]: ' "${#storage_class_names[@]}" >&3
    if ! IFS= read -r selection <&3; then
      break
    fi
    if [[ "$selection" =~ ^[0-9]+$ ]] && (( selection >= 1 && selection <= ${#storage_class_names[@]} )); then
      STORAGE_CLASS="${storage_class_names[$((selection - 1))]}"
      exec 3>&-
      log_ok "Selected StorageClass: ${STORAGE_CLASS}"
      return 0
    fi
    attempts=$((attempts + 1))
    log_warn "Invalid selection. Enter a number between 1 and ${#storage_class_names[@]}."
  done
  exec 3>&-
  fail "No valid StorageClass was selected. Rerun with --storage-class <name>."
}

select_agent_storage_class

select_velero_cache_storage_class() {
  CACHE_STORAGE_CLASS=""
  CACHE_PVC_CONFIG='"cachePVC": null'
  local reclaim_policy provisioner
  reclaim_policy="$(kubectl get storageclass "$STORAGE_CLASS" -o jsonpath='{.reclaimPolicy}' 2>/dev/null || true)"
  provisioner="$(kubectl get storageclass "$STORAGE_CLASS" -o jsonpath='{.provisioner}' 2>/dev/null || true)"
  if [[ -n "$provisioner" && "${reclaim_policy:-Delete}" == "Delete" ]]; then
    CACHE_STORAGE_CLASS="$STORAGE_CLASS"
    CACHE_PVC_CONFIG='"cachePVC": {"storageClass": "'"${CACHE_STORAGE_CLASS}"'", "residentThresholdInMB": 1024}'
    log_ok "Velero restore cache enabled automatically with StorageClass: ${CACHE_STORAGE_CLASS}"
  else
    log_warn "Velero restore cache disabled: StorageClass ${STORAGE_CLASS} must support dynamic provisioning and use reclaimPolicy Delete"
  fi
}

select_velero_cache_storage_class

image_registry_host() {
  local image="$1"
  local first="${image%%/*}"
  if [[ "$first" == *.* || "$first" == *:* || "$first" == "localhost" ]]; then
    echo "${first%%:*}"
  fi
}

is_ipv4_address() {
  local host="$1"
  [[ "$host" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
}

check_registry_host() {
  local image="$1"
  local host
  host="$(image_registry_host "$image")"
  if [[ -z "$host" ]]; then
    return 0
  fi
  if is_ipv4_address "$host"; then
    return 0
  fi
  if command -v getent >/dev/null 2>&1; then
    if getent hosts "$host" >/dev/null 2>&1; then
      return 0
    fi
  elif command -v nslookup >/dev/null 2>&1; then
    if nslookup "$host" >/dev/null 2>&1; then
      return 0
    fi
  else
    log_warn "Cannot verify registry host ${host}; getent/nslookup is unavailable"
    return 0
  fi
  log_error "Image registry host '${host}' from image '${image}' is not resolvable on this machine."
  log_error "The component images in the active HyperCDR release manifest must be reachable from every managed cluster node."
  log_error "Use --skip-image-preflight true only when cluster nodes already have these images cached or resolve the registry through node-local configuration."
  exit 1
}

download_registry_ca() {
  if [[ -z "$REGISTRY_CA_URL" ]]; then
    return 1
  fi
  local ca_file="$1"
  download_url "$REGISTRY_CA_URL" "$ca_file"
}

download_url() {
  local url="$1"
  local output="$2"
  if command -v curl >/dev/null 2>&1; then
    if [[ "$url" == https://* ]]; then
      curl -k -fsSL "$url" -o "$output"
    else
      curl -fsSL "$url" -o "$output"
    fi
    return $?
  fi
  if command -v wget >/dev/null 2>&1; then
    if [[ "$url" == https://* ]]; then
      wget --no-check-certificate -qO "$output" "$url"
    else
      wget -qO "$output" "$url"
    fi
    return $?
  fi
  echo "curl or wget is required to download ${url}" >&2
  return 1
}

root_exec() {
  if [[ "$(id -u)" == "0" ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    return 1
  fi
}

ensure_containerd_registry_config_path() {
  local config_file="/etc/containerd/config.toml"
  local tmp_file next_file
  tmp_file="$(mktemp)"
  next_file="$(mktemp)"
  root_exec mkdir -p /etc/containerd || {
    rm -f "$tmp_file" "$next_file"
    fail "Root or sudo is required to configure containerd registry trust"
  }
  if ! root_exec test -f "$config_file"; then
    if command -v containerd >/dev/null 2>&1; then
      containerd config default >"$tmp_file"
    else
      echo "version = 2" >"$tmp_file"
    fi
    root_exec install -m 0644 "$tmp_file" "$config_file"
  fi

  if root_exec grep -Eq '^[[:space:]]*config_path[[:space:]]*=' "$config_file"; then
    root_exec sed -i -E 's#^([[:space:]]*config_path[[:space:]]*=).*#\1 "/etc/containerd/certs.d"#' "$config_file"
    rm -f "$tmp_file" "$next_file"
    return 0
  fi

  root_exec cat "$config_file" >"$tmp_file"
  if grep -q '^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]' "$tmp_file"; then
    awk '
      /^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]/ && inserted == 0 {
        print
        print "  config_path = \"/etc/containerd/certs.d\""
        inserted = 1
        next
      }
      { print }
    ' "$tmp_file" >"$next_file"
  else
    cp "$tmp_file" "$next_file"
    cat >>"$next_file" <<EOF

[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
EOF
  fi
  root_exec install -m 0644 "$next_file" "$config_file"
  rm -f "$tmp_file" "$next_file"
}

install_registry_ca_local() {
  local registry_host="$1"
  if [[ -z "$registry_host" || -z "$REGISTRY_CA_URL" || "$INSTALL_REGISTRY_CA" != "true" ]]; then
    return 0
  fi
  local ca_file
  ca_file="$(mktemp)"
  download_registry_ca "$ca_file" || {
    rm -f "$ca_file"
    fail "Failed to download registry CA from ${REGISTRY_CA_URL}"
  }

  log_info "Installing registry CA for ${registry_host} on the current node"
  if command -v sudo >/dev/null 2>&1 && [[ "$(id -u)" != "0" ]]; then
    sudo mkdir -p "/etc/containerd/certs.d/${registry_host}" "/etc/docker/certs.d/${registry_host}"
    sudo cp "$ca_file" "/etc/containerd/certs.d/${registry_host}/ca.crt"
    sudo cp "$ca_file" "/etc/docker/certs.d/${registry_host}/ca.crt"
    ensure_containerd_registry_config_path
    sudo systemctl restart containerd >/dev/null 2>&1 || true
    sudo systemctl restart docker >/dev/null 2>&1 || true
  elif [[ "$(id -u)" == "0" ]]; then
    mkdir -p "/etc/containerd/certs.d/${registry_host}" "/etc/docker/certs.d/${registry_host}"
    cp "$ca_file" "/etc/containerd/certs.d/${registry_host}/ca.crt"
    cp "$ca_file" "/etc/docker/certs.d/${registry_host}/ca.crt"
    ensure_containerd_registry_config_path
    systemctl restart containerd >/dev/null 2>&1 || true
    systemctl restart docker >/dev/null 2>&1 || true
  else
    log_error "Root or sudo is required to install registry CA on the current node"
    rm -f "$ca_file"
    exit 1
  fi
  log_ok "Registry CA installed on the current node"
  rm -f "$ca_file"
}

install_registry_ca_remote_nodes() {
  local registry_host="$1"
  if [[ -z "$registry_host" || -z "$NODE_SSH_USER" || -z "$NODE_SSH_KEY" || -z "$REGISTRY_CA_URL" ]]; then
    return 0
  fi
  if ! command -v ssh >/dev/null 2>&1 || ! command -v scp >/dev/null 2>&1; then
    fail "ssh and scp are required for --node-ssh-user/--node-ssh-key registry CA distribution"
  fi
  local ca_file
  ca_file="$(mktemp)"
  download_registry_ca "$ca_file" || {
    rm -f "$ca_file"
    fail "Failed to download registry CA from ${REGISTRY_CA_URL}"
  }
  local node_ips
  node_ips="$(kubectl get nodes -o jsonpath='{range .items[*]}{range .status.addresses[?(@.type=="InternalIP")]}{.address}{"\n"}{end}{end}' | sort -u)"
  for node_ip in $node_ips; do
    log_info "Installing registry CA on node ${node_ip} through ssh"
    scp -P "$NODE_SSH_PORT" -i "$NODE_SSH_KEY" -o StrictHostKeyChecking=accept-new "$ca_file" "${NODE_SSH_USER}@${node_ip}:/tmp/hypercdr-registry-ca.crt"
    ssh -p "$NODE_SSH_PORT" -i "$NODE_SSH_KEY" -o StrictHostKeyChecking=accept-new "${NODE_SSH_USER}@${node_ip}" \
      "sudo mkdir -p /etc/containerd/certs.d/${registry_host} /etc/docker/certs.d/${registry_host} && sudo cp /tmp/hypercdr-registry-ca.crt /etc/containerd/certs.d/${registry_host}/ca.crt && sudo cp /tmp/hypercdr-registry-ca.crt /etc/docker/certs.d/${registry_host}/ca.crt && sudo systemctl restart containerd >/dev/null 2>&1 || true; sudo systemctl restart docker >/dev/null 2>&1 || true"
  done
  log_ok "Registry CA distribution completed"
  rm -f "$ca_file"
}

prompt_registry_ca_remote_nodes() {
  local registry_host="$1"
  if [[ -z "$registry_host" || "$INSTALL_REGISTRY_CA" != "true" || "$INTERACTIVE" != "true" ]]; then
    return 0
  fi
  local node_count
  node_count="$(kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "${node_count:-0}" -le 1 ]]; then
    return 0
  fi
  if [[ -n "$NODE_SSH_USER" && -n "$NODE_SSH_KEY" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    log_error "Cluster has ${node_count} nodes. Registry CA was installed only on the current node."
    log_error "Rerun with --node-ssh-user and --node-ssh-key so the installer can distribute registry CA to all nodes, or install the CA on each node manually before continuing."
    exit 1
  fi
  log_warn "Cluster has ${node_count} nodes. HyperCDR images may run on worker nodes, so the registry CA must be installed on every node."
  read -r -p "SSH user for Kubernetes nodes [root]: " NODE_SSH_USER
  NODE_SSH_USER="${NODE_SSH_USER:-root}"
  read -r -p "SSH private key path for node access: " NODE_SSH_KEY
  if [[ -z "$NODE_SSH_KEY" || ! -f "$NODE_SSH_KEY" ]]; then
    fail "Valid SSH private key is required for multi-node automatic CA distribution"
  fi
  read -r -p "SSH port [22]: " NODE_SSH_PORT
  NODE_SSH_PORT="${NODE_SSH_PORT:-22}"
}

print_diagnostics() {
  log_warn "Collecting HyperCDR install diagnostics from namespace ${NAMESPACE}"
  kubectl -n "$NAMESPACE" get pods -o wide >&2 || true
  kubectl -n "$NAMESPACE" get deploy,daemonset >&2 || true
  kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 30 >&2 || true
}

wait_timeout_seconds() {
  if [[ "$WAIT_TIMEOUT" =~ ^([0-9]+)s$ ]]; then
    echo "${BASH_REMATCH[1]}"
  elif [[ "$WAIT_TIMEOUT" =~ ^([0-9]+)m$ ]]; then
    echo $(( BASH_REMATCH[1] * 60 ))
  else
    echo 300
  fi
}

print_retry_guidance() {
  log_section "How to resolve and retry"
  log_error "1. Correct the reported Kubernetes, storage, registry, or network problem."
  log_error "2. Verify the control plane: kubectl get --raw='/readyz?verbose'"
  log_error "3. Verify install resources: kubectl -n ${NAMESPACE} get pods,pvc"
  log_error "4. If this cluster is already Online in HyperCDR, do not register it again."
  log_error "5. Otherwise rerun this installer with --wait-timeout 600s."
  log_error "6. If the token is expired or already used, generate a new registration command in HyperCDR before retrying."
  log_error "The installer uses idempotent Kubernetes apply operations, so a partial installation can be retried after the underlying problem is fixed."
}

diagnose_rollout_failure() {
  local workload_kind="$1"
  local workload_name="$2"
  local selector="$3"
  local diagnostic_text="" pending_pvcs=""

  diagnostic_text="$(
    kubectl -n "$NAMESPACE" describe "${workload_kind}/${workload_name}" 2>&1 || true
    kubectl -n "$NAMESPACE" describe pods -l "$selector" 2>&1 || true
    kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp 2>&1 | tail -n 40 || true
  )"
  pending_pvcs="$(kubectl -n "$NAMESPACE" get pvc --no-headers 2>/dev/null | awk '$2 != "Bound" {print $1}' | paste -sd ', ' - || true)"

  log_section "Installation failed"
  if ! kubectl --request-timeout=5s get --raw='/readyz' >/dev/null 2>&1 || echo "$diagnostic_text" | grep -Eqi 'connection refused|Unable to connect to the server|etcdserver: request timed out'; then
    log_error "Reason: the Kubernetes API server or etcd is unavailable or timing out."
    log_error "Impact: nodes cannot read PVC or VolumeAttachment objects, so workloads cannot become Ready."
  elif echo "$diagnostic_text" | grep -Eqi 'ErrImagePull|ImagePullBackOff|Failed to pull image|pull access denied|x509: certificate|unauthorized: authentication required'; then
    log_error "Reason: a required container image could not be pulled."
    log_error "Check registry reachability, credentials, CA trust, and whether the requested image tag exists."
  elif echo "$diagnostic_text" | grep -Eqi 'FailedMount|FailedAttachVolume|WaitForAttach|Unable to attach or mount volumes'; then
    log_error "Reason: a persistent volume could not be attached or mounted."
    log_error "Check the PVC, StorageClass, CSI controller, VolumeAttachment, and node storage health."
  elif [[ -n "$pending_pvcs" ]] && echo "$diagnostic_text" | grep -Eqi 'unbound immediate PersistentVolumeClaims|ProvisioningFailed|no storage class is set|storageclass.*not found'; then
    log_error "Reason: the installer PVC could not be provisioned."
    log_error "Affected PVC: ${pending_pvcs}."
    log_error "Check that a default or selected StorageClass exists and its CSI provisioner is healthy."
  elif echo "$diagnostic_text" | grep -Eqi 'CrashLoopBackOff|Back-off restarting failed container'; then
    log_error "Reason: the workload container is repeatedly crashing."
    log_error "Inspect its logs: kubectl -n ${NAMESPACE} logs -l ${selector} --all-containers --tail=100"
  elif echo "$diagnostic_text" | grep -Eqi 'forbidden:|cannot (get|list|watch|create|update|patch|delete) resource'; then
    log_error "Reason: Kubernetes RBAC denied an operation required by the installer."
    log_error "Check the HyperCDR ServiceAccount, ClusterRole, and ClusterRoleBinding."
  else
    log_error "Reason: ${workload_kind}/${workload_name} did not become Ready within ${WAIT_TIMEOUT}."
    log_error "Review the diagnostics below for scheduling, probe, runtime, or node errors."
  fi
  print_diagnostics
  print_retry_guidance
}

wait_for_rollout() {
  local workload_kind="$1"
  local workload_name="$2"
  local selector="$3"
  local display_name="$4"
  local timeout_seconds deadline next_update status_output pod_status pvc_status
  timeout_seconds="$(wait_timeout_seconds)"
  if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]]; then
    timeout_seconds=300
  fi
  deadline=$((SECONDS + timeout_seconds))
  next_update=$SECONDS
  log_info "Waiting up to ${timeout_seconds}s for ${display_name} to become ready"
  while [[ $SECONDS -lt $deadline ]]; do
    if status_output="$(kubectl -n "$NAMESPACE" rollout status "${workload_kind}/${workload_name}" --timeout=2s 2>&1)"; then
      log_ok "${display_name} is ready"
      return 0
    fi
    if [[ $SECONDS -ge $next_update ]]; then
      if ! kubectl --request-timeout=5s get --raw='/readyz' >/dev/null 2>&1; then
        log_warn "${display_name}: Kubernetes API server is currently unavailable; waiting for recovery"
      else
        pod_status="$(kubectl -n "$NAMESPACE" get pods -l "$selector" --no-headers 2>/dev/null | awk '{print $1 "=" $2 "/" $3}' | paste -sd ', ' - || true)"
        pvc_status="$(kubectl -n "$NAMESPACE" get pvc --no-headers 2>/dev/null | awk '{print $1 "=" $2}' | paste -sd ', ' - || true)"
        log_info "${display_name}: ${pod_status:-no matching pod}; PVC: ${pvc_status:-none}"
      fi
      next_update=$((SECONDS + 15))
    fi
    sleep 3
  done
  # A rollout can become ready at the timeout boundary while the last short
  # kubectl poll is returning. Check once more before reporting a failure.
  if kubectl -n "$NAMESPACE" rollout status "${workload_kind}/${workload_name}" --timeout=2s >/dev/null 2>&1; then
    log_ok "${display_name} is ready"
    return 0
  fi
  log_error "${display_name} did not become ready within ${WAIT_TIMEOUT}"
  diagnose_rollout_failure "$workload_kind" "$workload_name" "$selector"
  return 1
}

wait_agent_registration() {
  local timeout_seconds="${WAIT_TIMEOUT%s}"
  if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]]; then
    timeout_seconds=300
  fi
  local deadline=$((SECONDS + timeout_seconds))
  log_info "Waiting for comm-agent to register with the HyperCDR platform"
  while [[ $SECONDS -lt $deadline ]]; do
    if kubectl -n "$NAMESPACE" get secret hypercdr-agent-credential >/dev/null 2>&1; then
      log_ok "comm-agent registration is confirmed"
      return 0
    fi
    local pod logs
    pod="$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=hypercdr-comm-agent -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    if [[ -n "$pod" ]]; then
      logs="$(kubectl -n "$NAMESPACE" logs "$pod" --tail=40 2>/dev/null || true)"
      if echo "$logs" | grep -Eq 'TOKEN_USED|TOKEN_EXPIRED|TOKEN_INVALID|TOKEN_NOT_FOUND|CREDENTIAL_AUTH_FAILED|CREDENTIAL_INVALID|LICENSE_[A-Z_]+|TENANT_LICENSE_[A-Z_]+'; then
        log_error "comm-agent registration was rejected by the platform. See agent logs below."
        echo "$logs" >&2
        return 1
      fi
    fi
    sleep 3
  done
  log_error "comm-agent did not register with the platform within ${timeout_seconds}s"
  log_error "Check whether the agent pod can reach ${ENDPOINT}, and whether the install token is still valid."
  print_diagnostics
  local pod
  pod="$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=hypercdr-comm-agent -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$pod" ]]; then
    kubectl -n "$NAMESPACE" logs "$pod" --tail=80 >&2 || true
  fi
  return 1
}

kubectl_retry() {
  local attempt=1
  local max_attempts=5
  local delay=3
  while true; do
    if "$@"; then
      return 0
    fi
    if [[ "$attempt" -ge "$max_attempts" ]]; then
      log_error "kubectl command failed after ${max_attempts} attempts: $*"
      return 1
    fi
    log_warn "kubectl command failed; retrying in ${delay}s (${attempt}/${max_attempts}): $*"
    sleep "$delay"
    attempt=$((attempt + 1))
    delay=$((delay * 2))
  done
}

kubectl_apply_retry() {
  local manifest
  manifest="$(mktemp)"
  cat >"$manifest"
	if kubectl_retry kubectl apply -f "$manifest"; then
		rm -f "$manifest"
		return 0
	else
		local status=$?
		log_error "Failed to apply Kubernetes manifest"
		rm -f "$manifest"
		return "$status"
	fi
}

preflight_image_pull() {
  local name="$1"
  local image="$2"
  local command_yaml="$3"
  local target_namespace="${PREFLIGHT_NAMESPACE:-$NAMESPACE}"
  log_info "Checking whether cluster nodes can pull image ${image}"
  kubectl -n "$target_namespace" delete pod "$name" --ignore-not-found --wait=true >/dev/null 2>&1 || true
  cat <<YAML | kubectl_apply_retry >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
  namespace: ${target_namespace}
  labels:
    app.kubernetes.io/name: hypercdr-image-preflight
spec:
  serviceAccountName: default
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext:
    seccompProfile:
      type: RuntimeDefault
${IMAGE_PULL_SECRETS_BLOCK}
  containers:
    - name: image-check
      image: ${image}
      imagePullPolicy: IfNotPresent
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
        runAsNonRoot: true
        # Some qualified images declare a named non-root USER (for example
        # "cnb"). Kubernetes cannot validate a non-numeric image user when
        # runAsNonRoot is enabled, so pin the disposable preflight container
        # to a numeric non-root UID.
        runAsUser: 1000
${command_yaml}
YAML
  local deadline=$((SECONDS + 90))
  local pull_retry_logged="false"
  while [[ $SECONDS -lt $deadline ]]; do
    local phase waiting terminated
    phase="$(kubectl -n "$target_namespace" get pod "$name" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    waiting="$(kubectl -n "$target_namespace" get pod "$name" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)"
    terminated="$(kubectl -n "$target_namespace" get pod "$name" -o jsonpath='{.status.containerStatuses[0].state.terminated.reason}' 2>/dev/null || true)"
    case "$waiting" in
      ErrImagePull|ImagePullBackOff)
        if [[ "$pull_retry_logged" != "true" ]]; then
          log_warn "Image pull was interrupted (${waiting}); Kubernetes will retry until the 90s preflight deadline."
          pull_retry_logged="true"
        fi
        ;;
      InvalidImageName)
        log_error "Image pull preflight failed for ${image}: ${waiting}"
        log_error "The image reference is invalid. Check the managed release manifest."
        kubectl -n "$target_namespace" describe pod "$name" >&2 || true
        kubectl -n "$target_namespace" get events --sort-by=.lastTimestamp | tail -n 20 >&2 || true
        kubectl -n "$target_namespace" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
        return 1
        ;;
      CreateContainerConfigError|CreateContainerError)
        log_error "Image preflight container could not start for ${image}: ${waiting}"
        log_error "Check the image entrypoint and Kubernetes security-context compatibility. The image itself may already be present on the node."
        kubectl -n "$target_namespace" describe pod "$name" >&2 || true
        kubectl -n "$target_namespace" get events --sort-by=.lastTimestamp | tail -n 20 >&2 || true
        kubectl -n "$target_namespace" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
        return 1
        ;;
    esac
    if [[ "$phase" == "Running" || "$phase" == "Succeeded" || "$terminated" != "" ]]; then
      kubectl -n "$target_namespace" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
      log_ok "Image pull preflight passed for ${image}"
      return 0
    fi
    sleep 3
  done
  log_error "Image pull preflight timed out for ${image}"
  log_error "The cluster did not start the preflight pod within 90s. Check node scheduling, image pull, and registry connectivity."
  kubectl -n "$target_namespace" describe pod "$name" >&2 || true
  kubectl -n "$target_namespace" get events --sort-by=.lastTimestamp | tail -n 20 >&2 || true
  kubectl -n "$target_namespace" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  return 1
}

check_existing_velero_installation() {
  if [[ "$INSTALL_VELERO" != "true" || "$ALLOW_EXISTING_VELERO" == "true" ]]; then
    return 0
  fi
  is_hypercdr_managed_namespace() {
    local namespace="$1"
    [[ -n "$namespace" ]] && kubectl -n "$namespace" get deployment hypercdr-comm-agent >/dev/null 2>&1
  }
  local conflicts=()
  if kubectl get namespace velero >/dev/null 2>&1; then
    conflicts+=("namespace/velero")
  fi
  local deployments
  deployments="$(kubectl get deployments -A --no-headers 2>/dev/null | awk '$2=="velero"{print $1"/"$2}' || true)"
  if [[ -n "$deployments" ]]; then
    while IFS= read -r item; do
      if [[ -z "$item" ]]; then
        continue
      fi
      local namespace="${item%%/*}"
      if [[ "$item" == "${NAMESPACE}/velero" && "$AGENT_DEPLOYMENT_EXISTS" == "true" ]]; then
        continue
      fi
      if [[ "$namespace" != "$NAMESPACE" ]] && is_hypercdr_managed_namespace "$namespace"; then
        continue
      fi
      conflicts+=("deployment/${item}")
    done <<< "$deployments"
  fi
  local resource
  for resource in backups.velero.io restores.velero.io schedules.velero.io backupstoragelocations.velero.io backuprepositories.velero.io podvolumebackups.velero.io podvolumerestores.velero.io; do
    local resource_namespaces
    resource_namespaces="$(kubectl get "$resource" -A --ignore-not-found --no-headers 2>/dev/null | awk '{print $1}' | sort -u || true)"
    while IFS= read -r namespace; do
      [[ -n "$namespace" ]] || continue
      if [[ "$namespace" != "$NAMESPACE" ]] && is_hypercdr_managed_namespace "$namespace"; then
        continue
      fi
      conflicts+=("${resource}/${namespace}")
    done <<< "$resource_namespaces"
  done
  if [[ ${#conflicts[@]} -gt 0 ]]; then
    echo "existing Velero installation or Velero resources were found in this cluster:" >&2
    printf '  - %s\n' "${conflicts[@]}" >&2
    echo "A non-HyperCDR or residual Velero installation cannot be reused safely by the standard installer." >&2
    echo "Remove the residual installation, or use the documented disaster recovery workflow when preserving an existing repository is required." >&2
    exit 1
  fi
}

if [[ "$SKIP_IMAGE_PREFLIGHT" != "true" ]]; then
  log_info "Checking registry host resolution"
  check_registry_host "$AGENT_IMAGE"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    check_registry_host "$VELERO_IMAGE"
  fi
  log_ok "Registry host resolution check passed"
fi

NAMESPACE_EXISTS="false"
if kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
  NAMESPACE_EXISTS="true"
fi
AGENT_DEPLOYMENT_EXISTS="false"
if kubectl -n "$NAMESPACE" get deployment hypercdr-comm-agent >/dev/null 2>&1; then
  AGENT_DEPLOYMENT_EXISTS="true"
fi
check_existing_velero_installation

rollback_failed_registration() {
	trap - ERR
  provider_rollback_backup_backend
  [[ -z "${platform_ca_file:-}" ]] || rm -f "$platform_ca_file"
  log_warn "Rolling back changes because comm-agent registration did not complete"
  if [[ -n "${PREFLIGHT_NAMESPACE:-}" ]]; then
    kubectl delete namespace "$PREFLIGHT_NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  fi
  if [[ "$NAMESPACE_EXISTS" == "false" && "$AGENT_DEPLOYMENT_EXISTS" == "false" ]]; then
    kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    kubectl delete clusterrolebinding "$AGENT_RBAC_NAME" "$VELERO_RBAC_NAME" --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete clusterrole "$AGENT_RBAC_NAME" "$VELERO_RBAC_NAME" --ignore-not-found >/dev/null 2>&1 || true
    # Velero CRDs are cluster-scoped and may be shared by another HyperCDR
    # edition. Registration rollback therefore removes only resources owned by
    # this namespace and never deletes shared CRDs.
    log_ok "Failed first-time installation was rolled back"
  else
    kubectl -n "$NAMESPACE" delete secret hypercdr-agent-credential --ignore-not-found >/dev/null 2>&1 || true
    log_ok "Existing installation was left retryable; rerun with a new registration command"
  fi
}

ROLLBACK_ACTIVE="true"
trap 'status=$?; if [[ "$ROLLBACK_ACTIVE" == "true" ]]; then rollback_failed_registration; fi; exit $status' ERR
trap 'status=130; if [[ "$ROLLBACK_ACTIVE" == "true" ]]; then rollback_failed_registration; fi; exit $status' TERM INT

log_section "Registry trust"
REGISTRY_HOST="$(image_registry_host "$AGENT_IMAGE")"
AGENT_VERSION="${AGENT_IMAGE##*:}"
install_registry_ca_local "$REGISTRY_HOST"
prompt_registry_ca_remote_nodes "$REGISTRY_HOST"
install_registry_ca_remote_nodes "$REGISTRY_HOST"
if [[ -z "$REGISTRY_HOST" || "$INSTALL_REGISTRY_CA" != "true" ]]; then
  log_info "Registry CA installation skipped"
fi

provider_prepare_platform_trust

log_section "Isolated installation preflight"
PREFLIGHT_NAMESPACE="${NAMESPACE}-preflight-$(date +%s)-${RANDOM}"
kubectl create namespace "$PREFLIGHT_NAMESPACE" --dry-run=client -o yaml | kubectl_apply_retry
# OpenShift may admit the namespace before its service-account controller has
# created the default account. Ensure it exists before creating the probe Pod.
kubectl -n "$PREFLIGHT_NAMESPACE" create serviceaccount default --dry-run=client -o yaml | kubectl_apply_retry
provider_prepare_preflight
if [[ -n "$REGISTRY_SERVER" || -n "$REGISTRY_USERNAME" || -n "$REGISTRY_PASSWORD" ]]; then
  if [[ -z "$REGISTRY_SERVER" || -z "$REGISTRY_USERNAME" || -z "$REGISTRY_PASSWORD" ]]; then
    fail "--registry-server, --registry-username, and --registry-password must be provided together" 2
  fi
  kubectl -n "$PREFLIGHT_NAMESPACE" create secret docker-registry "$IMAGE_PULL_SECRET" \
    --docker-server="$REGISTRY_SERVER" --docker-username="$REGISTRY_USERNAME" \
    --docker-password="$REGISTRY_PASSWORD" --docker-email="$REGISTRY_EMAIL" \
    --dry-run=client -o yaml | kubectl_apply_retry
  IMAGE_PULL_SECRETS_BLOCK=$'  imagePullSecrets:\n    - name: '"$IMAGE_PULL_SECRET"
fi
run_image_pull_preflights
provider_run_preflight
kubectl delete namespace "$PREFLIGHT_NAMESPACE" --ignore-not-found --wait=true --timeout=60s >/dev/null
PREFLIGHT_NAMESPACE=""
IMAGE_PULL_SECRETS_BLOCK=""
log_ok "Isolated image and storage preflight completed"

log_section "Namespace and credentials"
log_info "Creating or updating namespace ${NAMESPACE}"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl_apply_retry
log_ok "Namespace ${NAMESPACE} is ready"
provider_install_platform_trust
log_info "Installing the offline Agent uninstaller in the cluster"
uninstaller_file="$(mktemp)"
if curl -k -fsSL "$AGENT_UNINSTALL_URL" -o "$uninstaller_file" && head -n 1 "$uninstaller_file" | grep -qx '#!/usr/bin/env bash' && kubectl -n "$NAMESPACE" create configmap hypercdr-agent-uninstaller --from-file=uninstall-agent.sh="$uninstaller_file" --dry-run=client -o yaml | kubectl_apply_retry; then
  kubectl -n "$NAMESPACE" label configmap hypercdr-agent-uninstaller app.kubernetes.io/managed-by=hypercdr --overwrite >/dev/null
  rm -f "$uninstaller_file"
  log_ok "Offline uninstaller is available in configmap/hypercdr-agent-uninstaller"
else
  rm -f "$uninstaller_file"
  fail "Unable to install the offline Agent uninstaller" 1
fi
if [[ -n "$REGISTRY_SERVER" || -n "$REGISTRY_USERNAME" || -n "$REGISTRY_PASSWORD" ]]; then
  if [[ -z "$REGISTRY_SERVER" || -z "$REGISTRY_USERNAME" || -z "$REGISTRY_PASSWORD" ]]; then
    fail "--registry-server, --registry-username, and --registry-password must be provided together" 2
  fi
  log_info "Creating or updating image pull secret ${IMAGE_PULL_SECRET}"
  kubectl -n "$NAMESPACE" create secret docker-registry "$IMAGE_PULL_SECRET" \
    --docker-server="$REGISTRY_SERVER" \
    --docker-username="$REGISTRY_USERNAME" \
    --docker-password="$REGISTRY_PASSWORD" \
    --docker-email="$REGISTRY_EMAIL" \
    --dry-run=client -o yaml | kubectl_apply_retry
  IMAGE_PULL_SECRETS_BLOCK=$'      imagePullSecrets:\n        - name: '"$IMAGE_PULL_SECRET"
  log_ok "Image pull secret ${IMAGE_PULL_SECRET} is ready"
fi
provider_install_backup_backend
if [[ -n "$VELERO_CRDS_URL" ]]; then
  log_section "Velero CRDs"
  log_info "Installing Velero CRDs from ${VELERO_CRDS_URL}"
  crds_file="$(mktemp)"
  download_url "$VELERO_CRDS_URL" "$crds_file" || {
    rm -f "$crds_file"
    fail "Failed to download Velero CRDs from ${VELERO_CRDS_URL}"
  }
  kubectl_retry kubectl apply -f "$crds_file" || {
    rm -f "$crds_file"
    log_error "Failed to apply Velero CRDs"
        exit 1
  }
  rm -f "$crds_file"
  log_ok "Velero CRDs are ready"
fi
if [[ "$INSTALL_VELERO" == "true" ]]; then
log_section "Velero workloads"
log_info "Creating or updating Velero server and node-agent"
cat <<YAML | kubectl_apply_retry
apiVersion: v1
kind: ServiceAccount
metadata:
  name: velero
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${VELERO_RBAC_NAME}
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${VELERO_RBAC_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${VELERO_RBAC_NAME}
subjects:
  - kind: ServiceAccount
    name: velero
    namespace: ${NAMESPACE}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-agent-config
  namespace: ${NAMESPACE}
data:
  node-agent-config.json: |
    {
      "loadConcurrency": {
        "globalConfig": 2,
        "prepareQueueLength": 4
      },
      ${CACHE_PVC_CONFIG}
    }
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: backup-repository-config
  namespace: ${NAMESPACE}
data:
  kopia: |
    {
      "cacheLimitMB": 5120
    }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: velero
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: velero
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: velero
  template:
    metadata:
      labels:
        app.kubernetes.io/name: velero
    spec:
      serviceAccountName: velero
${IMAGE_PULL_SECRETS_BLOCK}
      initContainers:
        - name: velero-plugin-for-aws
          image: ${VELERO_AWS_PLUGIN_IMAGE}
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: plugins
              mountPath: /target
        - name: velero-plugin-for-microsoft-azure
          image: ${VELERO_AZURE_PLUGIN_IMAGE}
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: plugins
              mountPath: /target
        - name: velero-plugin-for-gcp
          image: ${VELERO_GCP_PLUGIN_IMAGE}
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: plugins
              mountPath: /target
      containers:
        - name: velero
          image: ${VELERO_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - /velero
          args:
            - server
            - --default-volumes-to-fs-backup
            - --concurrent-backups=2
            - --plugin-dir=/plugins
          env:
            - name: VELERO_SCRATCH_DIR
              value: /scratch
            - name: VELERO_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: LD_LIBRARY_PATH
              value: /plugins
            - name: HOME
              value: /udmrepo
            - name: XDG_CACHE_HOME
              value: /udmrepo/.cache
          ports:
            - name: metrics
              containerPort: 8085
          volumeMounts:
            - name: plugins
              mountPath: /plugins
            - name: scratch
              mountPath: /scratch
            - name: tmp
              mountPath: /tmp
            - name: udmrepo
              mountPath: /udmrepo
      volumes:
        - name: plugins
          emptyDir: {}
        - name: scratch
          emptyDir: {}
        - name: tmp
          emptyDir: {}
        - name: udmrepo
          emptyDir: {}
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-agent
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: velero-node-agent
    role: node-agent
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: velero-node-agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: velero-node-agent
        role: node-agent
    spec:
      serviceAccountName: velero
${IMAGE_PULL_SECRETS_BLOCK}
      securityContext:
        runAsUser: 0
      containers:
        - name: node-agent
          image: ${VELERO_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - /velero
          args:
            - node-agent
            - server
            - --node-agent-configmap=node-agent-config
            - --backup-repository-configmap=backup-repository-config
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: VELERO_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          volumeMounts:
            - name: host-pods
              mountPath: /host_pods
              mountPropagation: HostToContainer
            - name: scratch
              mountPath: /scratch
      volumes:
        - name: host-pods
          hostPath:
            path: /var/lib/kubelet/pods
        - name: scratch
          emptyDir: {}
YAML
log_ok "Velero workloads submitted"
fi
log_section "HyperCDR agent"
log_info "Creating or updating agent bootstrap secret"
kubectl -n "$NAMESPACE" create secret generic hypercdr-agent-bootstrap \
  --from-literal=HCDR_INSTALL_TOKEN="$TOKEN" \
  --from-literal=HCDR_PLATFORM_ENDPOINT="$ENDPOINT" \
  --from-literal=HCDR_PLATFORM_PRIVATE_ENDPOINT="$ENDPOINT_PRIVATE" \
  --from-literal=HCDR_PLATFORM_PUBLIC_ENDPOINT="$ENDPOINT_PUBLIC" \
  --from-literal=HCDR_CLUSTER_TYPE="$CLUSTER_TYPE" \
  --from-literal=HCDR_CLOUD_PROVIDER="$(if [[ "$CLUSTER_TYPE" == "huaweicloud-cce" ]]; then echo huaweicloud; fi)" \
  --from-literal=HCDR_CLOUD_REGION="$DETECTED_CLOUD_REGION" \
  --from-literal=HCDR_CLOUD_CLUSTER_ID="$DETECTED_CLOUD_CLUSTER_ID" \
  --from-literal=HCDR_CLUSTER_NAME="$DETECTED_CLUSTER_NAME" \
  --dry-run=client -o yaml | kubectl_apply_retry
log_ok "Agent bootstrap secret is ready"
if [[ "$RESET_AGENT_CREDENTIAL" == "true" ]]; then
  log_info "Resetting previous agent credential secret if it exists"
  kubectl -n "$NAMESPACE" delete secret hypercdr-agent-credential --ignore-not-found
fi

if ! kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state >/dev/null 2>&1; then
  log_info "Creating comm-agent state PVC"
  cat <<YAML | kubectl_apply_retry
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: hypercdr-agent-state
  namespace: ${NAMESPACE}
spec:
  storageClassName: ${STORAGE_CLASS}
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
YAML
else
  log_info "Keeping existing comm-agent state PVC and StorageClass"
fi

log_info "Creating or updating comm-agent RBAC and deployment"
cat <<YAML | kubectl_apply_retry
apiVersion: v1
kind: ServiceAccount
metadata:
  name: hypercdr-agent
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${AGENT_RBAC_NAME}
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["namespaces", "nodes", "pods", "services", "configmaps", "serviceaccounts", "persistentvolumeclaims", "persistentvolumes", "resourcequotas", "limitranges"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["create", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "patch", "update", "delete"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "patch", "update"]
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["patch", "update"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "csidrivers"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingressclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["node.k8s.io"]
    resources: ["runtimeclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["scheduling.k8s.io"]
    resources: ["priorityclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["snapshot.storage.k8s.io"]
    resources: ["volumesnapshotclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["cert-manager.io"]
    resources: ["clusterissuers"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch", "patch", "update", "delete"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch", "create", "patch", "update", "delete"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "patch", "update"]
  - apiGroups: ["apps"]
    resources: ["statefulsets", "replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["daemonsets"]
    verbs: ["get", "list", "watch", "patch", "update"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses", "networkpolicies"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["policy"]
    resources: ["poddisruptionbudgets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["velero.io"]
    resources: ["backuprepositories", "backups", "backupstoragelocations", "datadownloads", "datauploads", "deletebackuprequests", "downloadrequests", "podvolumebackups", "podvolumerestores", "restores", "schedules", "serverstatusrequests", "volumesnapshotlocations"]
    verbs: ["get", "list", "watch", "create", "patch", "update", "delete"]
${AGENT_PLATFORM_RBAC_RULES}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${AGENT_RBAC_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${AGENT_RBAC_NAME}
subjects:
  - kind: ServiceAccount
    name: hypercdr-agent
    namespace: ${NAMESPACE}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hypercdr-comm-agent
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app.kubernetes.io/name: hypercdr-comm-agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: hypercdr-comm-agent
    spec:
      serviceAccountName: hypercdr-agent
${AGENT_POD_SECURITY_CONTEXT_BLOCK}
${IMAGE_PULL_SECRETS_BLOCK}
      containers:
        - name: comm-agent
${AGENT_CONTAINER_SECURITY_CONTEXT_BLOCK}
          image: ${AGENT_IMAGE}
          imagePullPolicy: Always
          resources:
            requests:
              cpu: ${AGENT_CPU_REQUEST}
              memory: ${AGENT_MEMORY_REQUEST}
            limits:
              cpu: ${AGENT_CPU_LIMIT}
              memory: ${AGENT_MEMORY_LIMIT}
          envFrom:
            - secretRef:
                name: hypercdr-agent-bootstrap
          env:
            - name: HCDR_AGENT_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: HCDR_POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: HCDR_EXECUTOR_MODE
              value: "${EXECUTOR_MODE}"
            - name: HCDR_AGENT_IMAGE
              value: "${AGENT_IMAGE}"
            - name: HCDR_BACKUP_BACKEND
              value: "${BACKUP_BACKEND}"
            - name: HCDR_AGENT_VERSION
              value: "${AGENT_VERSION}"
            - name: HCDR_INVENTORY_MODE
              value: "kubernetes"
            - name: HCDR_CREDENTIAL_SECRET_ENABLED
              value: "true"
            - name: HCDR_CREDENTIAL_SECRET_NAME
              value: "hypercdr-agent-credential"
            - name: HCDR_AGENT_STATE_DIR
              value: "/var/lib/hypercdr-agent"
            - name: HCDR_PLATFORM_TLS_INSECURE_SKIP_VERIFY
              value: "${PLATFORM_TLS_SKIP_VERIFY}"
            - name: HCDR_PLATFORM_CA_FILE
              value: "/etc/hypercdr/platform-ca/ca.crt"
          volumeMounts:
            - name: agent-state
              mountPath: /var/lib/hypercdr-agent
            - name: platform-ca
              mountPath: /etc/hypercdr/platform-ca
              readOnly: true
      volumes:
        - name: agent-state
          persistentVolumeClaim:
            claimName: hypercdr-agent-state
        - name: platform-ca
          configMap:
            name: hypercdr-platform-ca
            optional: true
YAML

if [[ "$AGENT_DEPLOYMENT_EXISTS" == "true" && "$RESET_AGENT_CREDENTIAL" == "true" ]]; then
  log_info "Restarting existing comm-agent deployment to use the new bootstrap token"
  kubectl_retry kubectl -n "$NAMESPACE" rollout restart deployment/hypercdr-comm-agent
  log_ok "Existing comm-agent deployment restarted with the new bootstrap token"
fi
log_ok "comm-agent deployment submitted in namespace ${NAMESPACE}"
if [[ "$WAIT_READY" == "true" ]]; then
  log_section "Readiness"
  log_info "Waiting for HyperCDR workloads to become ready in namespace ${NAMESPACE}"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    wait_for_rollout deployment velero app.kubernetes.io/name=velero "Velero deployment" || exit 1
    aws_plugin_exit_code="$(kubectl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=velero -o jsonpath='{.items[0].status.initContainerStatuses[?(@.name=="velero-plugin-for-aws")].state.terminated.exitCode}' 2>/dev/null || true)"
    if [[ "$aws_plugin_exit_code" != "0" ]]; then
      log_error "Velero AWS ObjectStore plugin was not installed successfully"
      kubectl -n "$NAMESPACE" describe pod -l app.kubernetes.io/name=velero >&2 || true
  return 1
    fi
    log_ok "Velero AWS ObjectStore plugin is installed"
    wait_for_rollout daemonset node-agent app.kubernetes.io/name=velero-node-agent "Velero node-agent daemonset" || exit 1
  fi
  wait_for_rollout deployment hypercdr-comm-agent app.kubernetes.io/name=hypercdr-comm-agent "comm-agent deployment" || exit 1
  if ! wait_agent_registration; then
    rollback_failed_registration
    exit 1
  fi
  ROLLBACK_ACTIVE="false"
  trap - ERR
  log_section "Completed"
  log_ok "HyperCDR agent installation is ready"
fi
`
