# OpenShift provider. The public registration workflow remains identical to
# the Kubernetes provider; only platform qualification is provider-specific.
provider_openshift_prepare_dependencies() {
  [[ "$(wc -w <<<"$OADP_RUNTIME_IMAGES")" -eq 6 ]] || fail "The active HyperCDR release does not contain the complete OADP runtime image set."
  AGENT_IMAGE="${OADP_AGENT_IMAGE}"
  AGENT_COMMAND="/oadp-comm-agent"
  BACKUP_BACKEND="oadp"
  # restricted-v2 assigns identity from the namespace's UID/GID range. A
  # fixed fsGroup is rejected by SCC on typical OpenShift installations.
  AGENT_POD_SECURITY_CONTEXT_BLOCK=$'      securityContext:\n        seccompProfile:\n          type: RuntimeDefault'
  AGENT_CONTAINER_SECURITY_CONTEXT_BLOCK=$'          securityContext:\n            allowPrivilegeEscalation: false\n            capabilities:\n              drop:\n                - ALL\n            runAsNonRoot: true'
  AGENT_PLATFORM_RBAC_RULES=$'  - apiGroups: ["operators.coreos.com"]\n    resources: ["catalogsources", "subscriptions", "operatorgroups", "clusterserviceversions"]\n    verbs: ["get", "list", "watch", "delete"]\n  - apiGroups: ["oadp.openshift.io"]\n    resources: ["dataprotectionapplications"]\n    verbs: ["get", "list", "watch", "delete"]'
  # OADP watches Velero CRs in openshift-adp. Keeping the OpenShift Agent in
  # that namespace preserves the existing Agent contract, which uses its own
  # namespace for Backup, Restore, Schedule, BSL, and repository objects.
  NAMESPACE="openshift-adp"
  INSTALL_VELERO="false"
  VELERO_CRDS_URL=""
}
provider_openshift_select_context() {
  local candidate resolved selection
  local -a candidates=()
  declare -A seen_candidates=()
  add_openshift_kubeconfig_candidate() {
    local path="$1"
    [[ -n "$path" && -f "$path" ]] || return 0
    resolved="$(readlink -f "$path" 2>/dev/null || printf '%s' "$path")"
    [[ -n "${seen_candidates[$resolved]:-}" ]] && return 0
    seen_candidates[$resolved]=1
    candidates+=("$resolved")
  }
  for candidate in "${KUBECONFIG:-}" "$HOME/.kube/config" "$PWD/kubeconfig"; do
    add_openshift_kubeconfig_candidate "$candidate"
  done
  shopt -s nullglob
  for candidate in "$HOME/.kube"/*kubeconfig* "$HOME/.kube"/*config*.yaml "$PWD"/*kubeconfig* "$PWD"/*config*.yaml "$HOME/Downloads"/*kubeconfig* "$HOME/Downloads"/*config*.yaml; do
    add_openshift_kubeconfig_candidate "$candidate"
  done
  shopt -u nullglob
  if [[ -z "$KUBECONFIG_PATH" && ${#candidates[@]} -gt 0 && "$INTERACTIVE" == "true" ]] && { exec 3<>/dev/tty; } 2>/dev/null; then
    echo "Detected OpenShift kubeconfig files:" >&3
    for selection in "${!candidates[@]}"; do printf '%d) %s\n' "$((selection+1))" "${candidates[$selection]}" >&3; done
    printf 'Select an OpenShift kubeconfig [1-%d], or 0 to enter another path: ' "${#candidates[@]}" >&3
    IFS= read -r selection <&3 || true
    if [[ "$selection" =~ ^[0-9]+$ ]] && ((selection >= 1 && selection <= ${#candidates[@]})); then
      KUBECONFIG_PATH="${candidates[$((selection-1))]}"
    elif [[ "$selection" == "0" ]]; then
      printf 'Enter the OpenShift kubeconfig file path: ' >&3
      IFS= read -r KUBECONFIG_PATH <&3 || true
    fi
    exec 3>&-
  elif [[ -z "$KUBECONFIG_PATH" && ${#candidates[@]} -eq 1 ]]; then
    KUBECONFIG_PATH="${candidates[0]}"
  fi
  if [[ -z "$KUBECONFIG_PATH" && "$INTERACTIVE" == "true" ]] && { exec 3<>/dev/tty; } 2>/dev/null; then
    printf 'Enter the OpenShift kubeconfig file path: ' >&3
    IFS= read -r KUBECONFIG_PATH <&3 || true
    exec 3>&-
  fi
  [[ -n "$KUBECONFIG_PATH" ]] || fail "OpenShift registration requires a kubeconfig. Rerun with --kubeconfig <path>."
  KUBECONFIG_PATH="${KUBECONFIG_PATH/#\~/$HOME}"
  [[ -r "$KUBECONFIG_PATH" ]] || fail "OpenShift kubeconfig is not readable: ${KUBECONFIG_PATH}"
  export KUBECONFIG="$KUBECONFIG_PATH"
  if [[ -z "$KUBECTL_CONTEXT" ]]; then
    mapfile -t contexts < <(command "$KUBECTL_BIN" config get-contexts -o name)
    if [[ ${#contexts[@]} -eq 1 ]]; then
      KUBECTL_CONTEXT="${contexts[0]}"
    elif [[ ${#contexts[@]} -gt 1 ]]; then
      fail "The OpenShift kubeconfig contains multiple contexts. Rerun with --context <name>."
    fi
  fi
  if [[ -n "$KUBECTL_CONTEXT" ]] && ! command "$KUBECTL_BIN" --kubeconfig "$KUBECONFIG_PATH" config get-contexts -o name | grep -Fxq "$KUBECTL_CONTEXT"; then
    fail "Kubernetes context '${KUBECTL_CONTEXT}' was not found in ${KUBECONFIG_PATH}."
  fi
}
provider_openshift_align_kubectl_version() { :; }
provider_openshift_prepare_platform_trust() {
  PLATFORM_TLS_SKIP_VERIFY="true"
  platform_ca_file=""
}
provider_openshift_prepare_preflight() { :; }
provider_openshift_run_preflight() {
  # CatalogSource is the authoritative catalog pull/readiness check. Starting
  # the catalog as an ordinary Pod duplicates that work and does not exercise
  # the OLM gRPC path. Runtime images are independent, so validate them in
  # bounded batches to shorten registration without overloading a small
  # OpenShift cluster or the registry.
  log_info "The OADP catalog image will be validated by OLM during CatalogSource readiness"
  local image index=0 status=0 job log_file
  local work_dir
  local -a jobs=() logs=()
  work_dir="$(mktemp -d)"
  flush_openshift_image_preflights() {
    local batch_status=0 item
    for item in "${!jobs[@]}"; do
      if ! wait "${jobs[$item]}"; then batch_status=1; fi
      cat "${logs[$item]}"
    done
    jobs=(); logs=()
    (( batch_status == 0 )) || status=1
  }
  for image in $OADP_RUNTIME_IMAGES; do
    index=$((index + 1))
    case "$image" in
      */oadp-bundle@*|*/oadp-bundle:*)
        # An OLM bundle is declarative metadata and intentionally has no
        # executable command. The CatalogSource/Subscription path below is
        # the authoritative pull and validation mechanism for this image.
        log_info "OADP bundle is validated by OLM during Subscription resolution"
        continue
        ;;
    esac
    log_file="${work_dir}/oadp-${index}.log"
    (preflight_image_pull "hypercdr-image-check-oadp-${index}" "$image" "") >"$log_file" 2>&1 &
    jobs+=("$!"); logs+=("$log_file")
    if (( ${#jobs[@]} >= 3 )); then flush_openshift_image_preflights; fi
  done
  if (( ${#jobs[@]} > 0 )); then flush_openshift_image_preflights; fi
  rm -rf "$work_dir"
  (( status == 0 )) || fail "One or more OADP runtime image preflight checks failed. No Agent resources were installed."
}
provider_openshift_install_platform_trust() { :; }

provider_openshift_install_backup_backend() {
  local version channel catalog_image oadp_timeout_seconds
  version="$(kubectl get clusterversion version -o jsonpath='{.status.desired.version}')"
  case "$version" in
    4.14|4.14.*|4.15|4.15.*) channel="${OADP_CHANNEL:-stable-1.3}" ;;
    *) fail "No qualified OADP channel exists for OpenShift ${version}." ;;
  esac
  # The release pipeline publishes a filtered catalog next to the Agent image.
  # Its relatedImages entries must also point at immutable Alibaba registry
  # mirrors, so OLM never falls back to a public Red Hat registry.
  catalog_image="${OADP_CATALOG_IMAGE}"
  oadp_timeout_seconds="${HCDR_OADP_INSTALL_TIMEOUT_SECONDS:-600}"
  [[ "$oadp_timeout_seconds" =~ ^[0-9]+$ ]] && (( oadp_timeout_seconds >= 60 )) || fail "HCDR_OADP_INSTALL_TIMEOUT_SECONDS must be an integer of at least 60 seconds."
  log_section "OADP backend"
  log_info "Installing OADP ${channel} from the HyperCDR Alibaba registry"
  kubectl create namespace openshift-adp --dry-run=client -o yaml | kubectl_apply_retry
	OADP_BACKEND_CREATED="true"
  if [[ -n "$REGISTRY_SERVER" ]]; then
    # OADP relatedImages are pulled by workloads created by the Operator, not
    # only by the CatalogSource. Bind the same private-registry credential to
    # the dedicated namespace's default service account before the DPA exists.
    kubectl -n openshift-adp create secret docker-registry "$IMAGE_PULL_SECRET" \
      --docker-server="$REGISTRY_SERVER" --docker-username="$REGISTRY_USERNAME" \
      --docker-password="$REGISTRY_PASSWORD" --docker-email="$REGISTRY_EMAIL" \
      --dry-run=client -o yaml | kubectl_apply_retry
    kubectl_retry kubectl -n openshift-adp get serviceaccount default >/dev/null
    kubectl -n openshift-adp patch serviceaccount default --type merge \
      -p '{"imagePullSecrets":[{"name":"'"$IMAGE_PULL_SECRET"'"}]}' >/dev/null
    kubectl -n openshift-marketplace create secret docker-registry "$IMAGE_PULL_SECRET" \
      --docker-server="$REGISTRY_SERVER" --docker-username="$REGISTRY_USERNAME" \
      --docker-password="$REGISTRY_PASSWORD" --docker-email="$REGISTRY_EMAIL" \
      --dry-run=client -o yaml | kubectl_apply_retry
    OADP_CATALOG_SECRETS=$'  secrets:\n    - '"$IMAGE_PULL_SECRET"
  else
    OADP_CATALOG_SECRETS=""
  fi
  cat <<YAML | kubectl_apply_retry
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: hypercdr-oadp
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: ${catalog_image}
  displayName: HyperCDR qualified OADP catalog
  publisher: HyperCDR
${OADP_CATALOG_SECRETS}
  updateStrategy:
    registryPoll:
      interval: 30m
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-adp
  namespace: openshift-adp
spec:
  targetNamespaces:
    - openshift-adp
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: hypercdr-oadp-operator
  namespace: openshift-adp
spec:
  channel: ${channel}
  installPlanApproval: Automatic
  name: oadp-operator
  source: hypercdr-oadp
  sourceNamespace: openshift-marketplace
YAML
  if [[ -n "$REGISTRY_SERVER" ]]; then
    local sa_deadline=$((SECONDS + oadp_timeout_seconds))
    until kubectl -n openshift-adp get serviceaccount openshift-adp-controller-manager >/dev/null 2>&1; do
      (( SECONDS < sa_deadline )) || fail "OADP Operator service account was not created within ${oadp_timeout_seconds} seconds."
      sleep 2
    done
    kubectl -n openshift-adp patch serviceaccount openshift-adp-controller-manager --type merge \
      -p '{"imagePullSecrets":[{"name":"'"$IMAGE_PULL_SECRET"'"}]}' >/dev/null
  fi
  local deadline=$((SECONDS + oadp_timeout_seconds)) installed_csv csv_phase
  until
    installed_csv="$(kubectl -n openshift-adp get subscription hypercdr-oadp-operator -o jsonpath='{.status.installedCSV}' 2>/dev/null || true)"
    [[ -n "$installed_csv" ]] &&
      csv_phase="$(kubectl -n openshift-adp get clusterserviceversion "$installed_csv" -o jsonpath='{.status.phase}' 2>/dev/null || true)" &&
      [[ "$csv_phase" == "Succeeded" ]]
  do
    (( SECONDS < deadline )) || fail "OADP Operator CSV did not reach Succeeded within ${oadp_timeout_seconds} seconds (CSV: ${installed_csv:-pending}, phase: ${csv_phase:-pending})."
    sleep 5
  done
  kubectl get crd dataprotectionapplications.oadp.openshift.io >/dev/null 2>&1 || fail "OADP Operator CSV succeeded but the DPA API is unavailable."
  cat <<YAML | kubectl_apply_retry
apiVersion: oadp.openshift.io/v1alpha1
kind: DataProtectionApplication
metadata:
  name: hypercdr-oadp
  namespace: openshift-adp
spec:
  backupImages: false
  configuration:
    velero:
      noDefaultBackupLocation: true
      defaultVolumesToFSBackup: true
      defaultPlugins:
        - openshift
        - aws
    nodeAgent:
      enable: true
      uploaderType: kopia
YAML
  if [[ -n "$REGISTRY_SERVER" ]]; then
    local velero_sa_deadline=$((SECONDS + 120))
    until kubectl -n openshift-adp get serviceaccount velero >/dev/null 2>&1; do
      (( SECONDS < velero_sa_deadline )) || fail "OADP Velero service account was not created within 120 seconds."
      sleep 2
    done
    kubectl -n openshift-adp patch serviceaccount velero --type merge \
      -p '{"imagePullSecrets":[{"name":"'"$IMAGE_PULL_SECRET"'"}]}' >/dev/null
  fi
  # DPA reconciliation is asynchronous. A successful apply only means the CR
  # was accepted; the Operator may need several seconds before it creates the
  # Velero Deployment and node-agent DaemonSet.
  local workload_deadline=$((SECONDS + oadp_timeout_seconds))
  until kubectl -n openshift-adp get deployment velero >/dev/null 2>&1; do
    (( SECONDS < workload_deadline )) || fail "OADP did not create the Velero deployment within ${oadp_timeout_seconds} seconds."
    sleep 2
  done
  kubectl -n openshift-adp rollout status deployment/velero --timeout="${oadp_timeout_seconds}s" || fail "OADP Velero deployment did not become ready."
  workload_deadline=$((SECONDS + oadp_timeout_seconds))
  until kubectl -n openshift-adp get daemonset node-agent >/dev/null 2>&1; do
    (( SECONDS < workload_deadline )) || fail "OADP did not create the node-agent daemonset within ${oadp_timeout_seconds} seconds."
    sleep 2
  done
  kubectl -n openshift-adp rollout status daemonset/node-agent --timeout="${oadp_timeout_seconds}s" || fail "OADP node-agent did not become ready on all eligible workers."
  log_ok "OADP and Kopia node-agent are ready"
}

provider_openshift_rollback_backup_backend() {
  [[ "${OADP_BACKEND_CREATED:-false}" == "true" ]] || return 0
  kubectl -n openshift-adp delete dataprotectionapplication hypercdr-oadp --ignore-not-found --wait=false >/dev/null 2>&1 || true
  kubectl -n openshift-adp delete subscription hypercdr-oadp-operator --ignore-not-found --wait=false >/dev/null 2>&1 || true
  kubectl -n openshift-adp delete operatorgroup openshift-adp --ignore-not-found --wait=false >/dev/null 2>&1 || true
  kubectl -n openshift-marketplace delete catalogsource hypercdr-oadp --ignore-not-found --wait=false >/dev/null 2>&1 || true
}

provider_openshift_verify() {
  log_info "Verifying OpenShift compatibility"
  local version unsupported_nodes permission verb resource
  version="$(kubectl get clusterversion version -o jsonpath='{.status.desired.version}' 2>/dev/null || true)"
  case "$version" in
    4.14|4.14.*|4.15|4.15.*) ;;
    *) fail "OpenShift phase one supports versions 4.14 and 4.15. Detected: ${version:-unknown}." ;;
  esac
  unsupported_nodes="$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"|"}{.status.nodeInfo.operatingSystem}{"|"}{.status.nodeInfo.architecture}{"|"}{.status.nodeInfo.osImage}{"\n"}{end}' | awk -F'|' '$2 != "linux" || ($3 != "amd64" && $3 != "x86_64") || ($4 !~ /Red Hat Enterprise Linux CoreOS/ && $4 !~ /Red Hat Enterprise Linux/)')"
  [[ -z "$unsupported_nodes" ]] || fail "OpenShift phase one supports RHCOS/RHEL Linux AMD64 nodes only. Unsupported nodes: ${unsupported_nodes//$'\n'/, }"
  local existing_oadp
  existing_oadp="$({
    kubectl get subscriptions.operators.coreos.com -A -o jsonpath='{range .items[?(@.spec.name=="oadp-operator")]}subscription/{.metadata.namespace}/{.metadata.name}{"\n"}{end}' 2>/dev/null || true
    kubectl get dataprotectionapplications.oadp.openshift.io -A -o jsonpath='{range .items[*]}dpa/{.metadata.namespace}/{.metadata.name}{"\n"}{end}' 2>/dev/null || true
    kubectl get clusterserviceversions.operators.coreos.com -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{"|"}{.spec.displayName}{"\n"}{end}' 2>/dev/null | awk -F'|' 'tolower($0) ~ /oadp/ {print "csv/" $1}' || true
  } | sed '/^[[:space:]]*$/d')"
  [[ -z "$existing_oadp" ]] || fail "An existing OADP installation was detected (${existing_oadp//$'\n'/, }). Phase one does not reuse existing OADP; uninstall it before registering this cluster."
  for permission in 'create subscriptions.operators.coreos.com' 'create operatorgroups.operators.coreos.com' 'create dataprotectionapplications.oadp.openshift.io' 'create securitycontextconstraints.security.openshift.io'; do
    verb="${permission%% *}"; resource="${permission#* }"
    kubectl auth can-i "$verb" "$resource" --all-namespaces | grep -qx yes || fail "OpenShift registration requires cluster-admin capability: ${verb} ${resource}"
  done
  DETECTED_CLUSTER_NAME="$(kubectl get infrastructure cluster -o jsonpath='{.status.infrastructureName}' 2>/dev/null || true)"
  DETECTED_CLOUD_CLUSTER_ID="$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  [[ -n "$DETECTED_CLUSTER_NAME" ]] || DETECTED_CLUSTER_NAME="openshift-${DETECTED_CLOUD_CLUSTER_ID:0:8}"
  log_ok "OpenShift ${version} Linux AMD64 cluster verified: ${DETECTED_CLUSTER_NAME}"

}
