# Huawei Cloud CCE provider. No function in this module is called by the Native
# provider; cloud-specific validation remains behind the provider contract.
IMAGE_PULL_PREFLIGHT_STRATEGY="sequential"

provider_huaweicloud_cce_download_kubectl() {
  local version="$1" arch cache_dir binary checksum expected actual
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) fail "CCE registration supports amd64 and arm64 Linux execution hosts. Detected: $(uname -m)." ;;
  esac
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "The trusted kubectl source returned an invalid version: ${version}."
  cache_dir="${XDG_CACHE_HOME:-$HOME/.cache}/hypercdr/kubectl/${version}/linux-${arch}"
  binary="${cache_dir}/kubectl"
  if [[ ! -x "$binary" ]]; then
    mkdir -p "$cache_dir"
    curl -fsSL --retry 2 --connect-timeout 10 "https://dl.k8s.io/release/${version}/bin/linux/${arch}/kubectl" -o "${binary}.tmp" || fail "Could not download kubectl ${version} from the trusted Kubernetes release service. Check DNS and outbound HTTPS."
    curl -fsSL --retry 2 --connect-timeout 10 "https://dl.k8s.io/release/${version}/bin/linux/${arch}/kubectl.sha256" -o "${binary}.sha256" || fail "Could not download the kubectl ${version} checksum."
    expected="$(tr -d '[:space:]' < "${binary}.sha256")"
    actual="$(sha256sum "${binary}.tmp" | awk '{print $1}')"
    [[ "$expected" =~ ^[0-9a-f]{64}$ && "$actual" == "$expected" ]] || { rm -f "${binary}.tmp"; fail "kubectl ${version} failed SHA256 verification; no executable was installed."; }
    chmod 0755 "${binary}.tmp"
    mv "${binary}.tmp" "$binary"
  fi
  KUBECTL_BIN="$binary"
  log_ok "Using checksum-verified kubectl ${version} from the HyperCDR tool cache"
}

provider_huaweicloud_cce_prepare_dependencies() {
  [[ "$(uname -s)" == "Linux" ]] || fail "CCE command-based registration phase one requires a Linux execution host."
  [[ -r /etc/os-release ]] || fail "Unable to identify this Linux distribution from /etc/os-release."
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-}" in
    ubuntu|debian|rhel|centos|rocky|almalinux) ;;
    *) fail "Unsupported Linux distribution '${ID:-unknown}'. Supported: Ubuntu, Debian, RHEL, CentOS, Rocky Linux, and AlmaLinux." ;;
  esac
  command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required to verify kubectl downloads. Install coreutils and retry."
  if command -v kubectl >/dev/null 2>&1; then
    KUBECTL_BIN="$(command -v kubectl)"
    return 0
  fi
  if [[ "$INTERACTIVE" != "true" ]] || ! { exec 3<>/dev/tty; } 2>/dev/null; then
    fail "kubectl is missing. Install it first, or rerun interactively to let HyperCDR install a checksum-verified client in its tool cache."
  fi
  printf 'kubectl is not installed. Install a checksum-verified client in the HyperCDR tool cache? [Y/n] ' >&3
  local answer latest
  IFS= read -r answer <&3 || true
  exec 3>&-
  [[ -z "$answer" || "$answer" =~ ^[Yy]$ ]] || fail "kubectl installation was declined. Install kubectl and rerun registration."
  latest="$(curl -fsSL --retry 2 --connect-timeout 10 https://dl.k8s.io/release/stable.txt || true)"
  [[ -n "$latest" ]] || fail "Could not determine the current kubectl version from the trusted Kubernetes release service."
  provider_huaweicloud_cce_download_kubectl "$latest"
}

provider_huaweicloud_cce_align_kubectl_version() {
  local server_version server_minor client_version client_minor compatible
  server_version="$(kubectl version -o json 2>/dev/null | grep -o '"gitVersion"[[:space:]]*:[[:space:]]*"v[0-9][^"]*"' | tail -n1 | cut -d'"' -f4)"
  [[ "$server_version" =~ ^v([0-9]+)\.([0-9]+)\. ]] || fail "Unable to determine the CCE Kubernetes server version."
  server_minor="v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}"
  client_version="$(kubectl version --client -o json 2>/dev/null | grep -o '"gitVersion"[[:space:]]*:[[:space:]]*"v[0-9][^"]*"' | head -n1 | cut -d'"' -f4)"
  client_minor="$(sed -E 's/^(v[0-9]+\.[0-9]+).*/\1/' <<<"$client_version")"
  if [[ "$client_minor" != "$server_minor" ]]; then
    # Kubernetes publishes every patch release at a stable, immutable URL.  Do
    # not rely on the undocumented stable-vX.Y alias; CCE may run a patch that
    # has no such alias.  The API's full server version is the source of truth.
    compatible="$server_version"
    provider_huaweicloud_cce_download_kubectl "$compatible"
  fi
  log_ok "kubectl client is aligned with CCE Kubernetes ${server_minor}"
}

provider_huaweicloud_cce_select_context() {
  local candidate resolved selection
  local -a candidates=()
  declare -A seen_candidates=()
  add_kubeconfig_candidate() {
    local path="$1"
    [[ -n "$path" && -f "$path" ]] || return 0
    resolved="$(readlink -f "$path" 2>/dev/null || printf '%s' "$path")"
    [[ -n "${seen_candidates[$resolved]:-}" ]] && return 0
    seen_candidates[$resolved]=1
    candidates+=("$resolved")
  }
  for candidate in "${KUBECONFIG:-}" "$HOME/.kube/hypercdr-cce.yaml" "$HOME/.kube/config"; do
    add_kubeconfig_candidate "$candidate"
  done
  shopt -s nullglob
  for candidate in "$HOME/.kube"/*kubeconfig* "$HOME/.kube"/*config*.yaml "$PWD"/*kubeconfig* "$HOME/Downloads"/*kubeconfig* "$HOME/Downloads"/*config*.yaml; do
    add_kubeconfig_candidate "$candidate"
  done
  shopt -u nullglob
  if [[ -z "$KUBECONFIG_PATH" && ${#candidates[@]} -gt 0 && "$INTERACTIVE" == "true" ]] && { exec 3<>/dev/tty; } 2>/dev/null; then
    echo "Detected Kubernetes configuration files:" >&3
    for selection in "${!candidates[@]}"; do printf '%d) %s\n' "$((selection+1))" "${candidates[$selection]}" >&3; done
    printf 'Select a CCE kubeconfig [1-%d], or 0 to enter another path: ' "${#candidates[@]}" >&3
    IFS= read -r selection <&3 || true
    if [[ "$selection" =~ ^[0-9]+$ ]] && ((selection >= 1 && selection <= ${#candidates[@]})); then
      KUBECONFIG_PATH="${candidates[$((selection-1))]}"
    fi
    exec 3>&-
  elif [[ -z "$KUBECONFIG_PATH" && ${#candidates[@]} -eq 1 ]]; then
    KUBECONFIG_PATH="${candidates[0]}"
  fi
  if [[ -z "$KUBECONFIG_PATH" && "$INTERACTIVE" == "true" ]] && { exec 3<>/dev/tty; } 2>/dev/null; then
    printf 'Enter the CCE kubeconfig file path: ' >&3
    IFS= read -r KUBECONFIG_PATH <&3 || true
    exec 3>&-
  fi
  [[ -n "$KUBECONFIG_PATH" ]] || fail "CCE registration requires a kubeconfig. Rerun with --kubeconfig <path>."
  KUBECONFIG_PATH="${KUBECONFIG_PATH/#\~/$HOME}"
  [[ -r "$KUBECONFIG_PATH" ]] || fail "CCE kubeconfig is not readable: ${KUBECONFIG_PATH}"
  export KUBECONFIG="$KUBECONFIG_PATH"
  if [[ -z "$KUBECTL_CONTEXT" ]]; then
    mapfile -t contexts < <(kubectl config get-contexts -o name)
    if [[ ${#contexts[@]} -eq 1 ]]; then
      KUBECTL_CONTEXT="${contexts[0]}"
    elif [[ ${#contexts[@]} -gt 1 ]]; then
      fail "The CCE kubeconfig contains multiple contexts. Rerun with --context <name>."
    fi
  fi
  if [[ -n "$KUBECTL_CONTEXT" ]] && ! kubectl config get-contexts -o name | grep -Fxq "$KUBECTL_CONTEXT"; then
    fail "Kubernetes context '${KUBECTL_CONTEXT}' was not found in ${KUBECONFIG_PATH}."
  fi
}

provider_huaweicloud_cce_verify() {
  log_info "Verifying Huawei Cloud CCE compatibility"
  local provider_ids cce_markers unsupported_nodes permission verb resource
  provider_ids="$(kubectl get nodes -o jsonpath='{range .items[*]}{.spec.providerID}{"\n"}{end}' 2>/dev/null || true)"
  cce_markers="$(kubectl get nodes --show-labels 2>/dev/null || true) $(kubectl -n kube-system get deployments,daemonsets 2>/dev/null || true)"
  grep -Eqi 'huaweicloud|cce' <<<"${provider_ids} ${cce_markers}" || fail "The selected cluster could not be verified as Huawei Cloud CCE. Check the kubeconfig and target context."
  unsupported_nodes="$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.status.nodeInfo.operatingSystem}{" "}{.status.nodeInfo.architecture}{"\n"}{end}' | awk '$2 != "linux" || ($3 != "amd64" && $3 != "x86_64" && $3 != "arm64" && $3 != "aarch64")')"
  [[ -z "$unsupported_nodes" ]] || fail "CCE registration supports Linux amd64 or arm64 workers. Unsupported nodes: ${unsupported_nodes//$'\n'/, }"
  for permission in 'create namespaces' 'create clusterroles.rbac.authorization.k8s.io' 'create clusterrolebindings.rbac.authorization.k8s.io' 'create deployments.apps' 'create daemonsets.apps' 'create secrets' 'create persistentvolumeclaims'; do
    verb="${permission%% *}"; resource="${permission#* }"
    kubectl auth can-i "$verb" "$resource" --all-namespaces | grep -qx yes || fail "CCE kubeconfig lacks required permission: ${verb} ${resource}"
  done
  # CCE kubeconfigs commonly expose the generic context "internal". The
  # provider-owned ConfigMap contains the user-visible cluster name and is
  # authoritative when available.
  DETECTED_CLUSTER_NAME="$(kubectl -n kube-system get configmap cluster-config -o jsonpath='{.data.alias}' 2>/dev/null || true)"
  [[ -n "$DETECTED_CLUSTER_NAME" ]] || DETECTED_CLUSTER_NAME="${KUBECTL_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
  DETECTED_CLOUD_REGION="$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.topology\.kubernetes\.io/region}' 2>/dev/null || true)"
  DETECTED_CLOUD_CLUSTER_ID="$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  [[ -n "$DETECTED_CLOUD_CLUSTER_ID" ]] || fail "Unable to read the stable CCE cluster identity from namespace/kube-system."
  if [[ -z "$DETECTED_CLUSTER_NAME" || "$DETECTED_CLUSTER_NAME" == "kubernetes-admin@kubernetes" || "$DETECTED_CLUSTER_NAME" == "kubernetes" ]]; then
    DETECTED_CLUSTER_NAME="cce-${DETECTED_CLOUD_CLUSTER_ID:0:8}"
  fi
  log_ok "Huawei Cloud CCE cluster verified: ${DETECTED_CLUSTER_NAME:-unknown}"
}

provider_huaweicloud_cce_prepare_platform_trust() {
  platform_ca_file="$(mktemp)"
  curl -k -fsSL "$PLATFORM_CA_URL" -o "$platform_ca_file" && grep -q 'BEGIN CERTIFICATE' "$platform_ca_file" || fail "Platform TLS certificate could not be downloaded or is invalid. No Agent resources were installed with insecure TLS."
  PLATFORM_TLS_SKIP_VERIFY="false"
}

provider_huaweicloud_cce_prepare_preflight() {
  kubectl -n "$PREFLIGHT_NAMESPACE" create configmap hypercdr-platform-ca --from-file=ca.crt="$platform_ca_file" --dry-run=client -o yaml | kubectl_apply_retry
}

provider_huaweicloud_cce_dynamic_pvc_preflight() {
  log_info "Checking dynamic PVC provisioning with StorageClass ${STORAGE_CLASS}"
  cat <<YAML | kubectl_apply_retry >/dev/null
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: hypercdr-storage-preflight
  namespace: ${PREFLIGHT_NAMESPACE}
spec:
  accessModes: ["ReadWriteOnce"]
  storageClassName: ${STORAGE_CLASS}
  resources:
    requests:
      storage: 1Gi
---
apiVersion: v1
kind: Pod
metadata:
  name: hypercdr-storage-preflight
  namespace: ${PREFLIGHT_NAMESPACE}
spec:
  serviceAccountName: default
  automountServiceAccountToken: false
  restartPolicy: Never
${IMAGE_PULL_SECRETS_BLOCK}
  containers:
    - name: storage-check
      image: ${VELERO_IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["/velero", "version", "--client-only"]
      volumeMounts:
        - {name: data, mountPath: /preflight}
  volumes:
    - name: data
      persistentVolumeClaim: {claimName: hypercdr-storage-preflight}
YAML
  local deadline=$((SECONDS + 120)) phase
  while [[ $SECONDS -lt $deadline ]]; do
    phase="$(kubectl -n "$PREFLIGHT_NAMESPACE" get pvc hypercdr-storage-preflight -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    if [[ "$phase" == "Bound" ]]; then
      log_ok "Dynamic PVC provisioning passed with StorageClass ${STORAGE_CLASS}"
      return 0
    fi
    sleep 3
  done
  kubectl -n "$PREFLIGHT_NAMESPACE" describe pvc hypercdr-storage-preflight >&2 || true
  kubectl -n "$PREFLIGHT_NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 30 >&2 || true
  fail "Dynamic PVC provisioning did not bind within 120s for StorageClass ${STORAGE_CLASS}."
}

provider_huaweicloud_cce_connectivity_preflight() {
  log_info "Checking Agent WebSocket and TLS connectivity from a CCE Worker Pod"
  cat <<YAML | kubectl_apply_retry >/dev/null
apiVersion: v1
kind: Pod
metadata: {name: hypercdr-platform-preflight, namespace: ${PREFLIGHT_NAMESPACE}}
spec:
  serviceAccountName: default
  automountServiceAccountToken: false
  restartPolicy: Never
${IMAGE_PULL_SECRETS_BLOCK}
  containers:
    - name: platform-check
      image: ${AGENT_IMAGE}
      imagePullPolicy: IfNotPresent
      env:
        - {name: HCDR_PLATFORM_ENDPOINT, value: "${ENDPOINT}"}
        - {name: HCDR_PLATFORM_PRIVATE_ENDPOINT, value: "${ENDPOINT_PRIVATE}"}
        - {name: HCDR_PLATFORM_PUBLIC_ENDPOINT, value: "${ENDPOINT_PUBLIC}"}
        - {name: HCDR_PLATFORM_TLS_INSECURE_SKIP_VERIFY, value: "false"}
        - {name: HCDR_PLATFORM_CA_FILE, value: "/etc/hypercdr/platform-ca/ca.crt"}
        - {name: HCDR_PLATFORM_PREFLIGHT_ONLY, value: "true"}
      volumeMounts:
        - {name: platform-ca, mountPath: /etc/hypercdr/platform-ca, readOnly: true}
  volumes:
    - name: platform-ca
      configMap: {name: hypercdr-platform-ca}
YAML
  local deadline=$((SECONDS + 60)) phase
  while [[ $SECONDS -lt $deadline ]]; do
    phase="$(kubectl -n "$PREFLIGHT_NAMESPACE" get pod hypercdr-platform-preflight -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    [[ "$phase" == "Succeeded" ]] && { log_ok "Agent WebSocket and TLS connectivity preflight passed"; return 0; }
    if [[ "$phase" == "Failed" ]]; then
      kubectl -n "$PREFLIGHT_NAMESPACE" logs hypercdr-platform-preflight >&2 || true
      fail "A CCE Worker Pod could not establish a trusted WebSocket connection to the control plane. Check VPC routing, security groups, NAT, endpoint addresses, and certificate SANs."
    fi
    sleep 2
  done
  kubectl -n "$PREFLIGHT_NAMESPACE" describe pod hypercdr-platform-preflight >&2 || true
  kubectl -n "$PREFLIGHT_NAMESPACE" logs hypercdr-platform-preflight >&2 || true
  fail "Agent WebSocket connectivity preflight timed out after 60s."
}

provider_huaweicloud_cce_run_preflight() {
  provider_huaweicloud_cce_dynamic_pvc_preflight
  provider_huaweicloud_cce_connectivity_preflight
}

provider_huaweicloud_cce_install_platform_trust() {
  kubectl -n "$NAMESPACE" create configmap hypercdr-platform-ca --from-file=ca.crt="$platform_ca_file" --dry-run=client -o yaml | kubectl_apply_retry
  log_ok "Platform TLS certificate is pinned for Agent connections"
  rm -f "$platform_ca_file"
  platform_ca_file=""
}
