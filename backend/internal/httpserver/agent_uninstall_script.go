package httpserver

import "net/http"

func (r *Router) agentUninstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(agentUninstallScriptTemplate))
}

const agentUninstallScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="hypercdr-agent"
EXECUTE="false"
FORCE_FINALIZERS="false"
WAIT_SECONDS=90

usage() {
  cat <<'EOF'
HyperCDR Agent offline uninstaller

Usage: ./uninstall-agent.sh [options]
  --namespace NAME          Agent namespace (default: hypercdr-agent)
  --execute                 Perform cleanup; otherwise print a dry-run plan
  --force-finalizers        Remove stale Velero finalizers after normal wait
  --wait-seconds SECONDS    Normal deletion wait (default: 90)
  --help                    Show help

This tool deletes only the HyperCDR Agent namespace and its namespace-scoped
Agent/Velero RBAC. It never deletes application namespaces, Longhorn, backup
objects in object storage, or Velero resources in other namespaces.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace) NAMESPACE="${2:-}"; shift 2 ;;
    --execute) EXECUTE="true"; shift ;;
    --force-finalizers) FORCE_FINALIZERS="true"; shift ;;
    --wait-seconds) WAIT_SECONDS="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ "$NAMESPACE" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "invalid namespace" >&2; exit 2; }
command -v kubectl >/dev/null || { echo "kubectl is required" >&2; exit 2; }
kubectl version --request-timeout=10s >/dev/null

if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
  echo "Agent namespace $NAMESPACE is absent; checking cluster RBAC only."
else
  agents="$(kubectl -n "$NAMESPACE" get deployment hypercdr-comm-agent -o name 2>/dev/null || true)"
  velero="$(kubectl -n "$NAMESPACE" get deployment velero -o name 2>/dev/null || true)"
  [[ -n "$agents$velero" ]] || { echo "Refusing cleanup: $NAMESPACE is not recognizable as a HyperCDR Agent namespace." >&2; exit 3; }
fi

if [[ "$NAMESPACE" == "hypercdr-agent" ]]; then
  suffix=""
else
  command -v sha256sum >/dev/null || { echo "sha256sum is required for a custom namespace" >&2; exit 2; }
  suffix="-$(printf %s "$NAMESPACE" | sha256sum | cut -c1-8)"
fi
agent_rbac="hypercdr-agent${suffix}"
velero_rbac="hypercdr-velero${suffix}"

echo "HyperCDR Agent uninstall plan"
echo "  namespace: $NAMESPACE"
echo "  cluster RBAC: $agent_rbac, $velero_rbac"
echo "  force stale finalizers: $FORCE_FINALIZERS"
if [[ "$EXECUTE" != "true" ]]; then
  echo "Dry-run only. Rerun with --execute after reviewing this plan."
  exit 0
fi

resources=(backups backuprepositories backupstoragelocations datadownloads datauploads deletebackuprequests downloadrequests podvolumebackups podvolumerestores restores schedules serverstatusrequests volumesnapshotlocations)
velero_crds=()
for resource in "${resources[@]}"; do velero_crds+=("${resource}.velero.io"); done

velero_is_shared() {
  local other_workloads other_objects resource
  other_workloads="$(kubectl get deployments,daemonsets -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\n"}{end}' 2>/dev/null | awk -v ns="$NAMESPACE" '$1 != ns && ($2 == "velero" || $2 == "node-agent" || $2 == "hypercdr-comm-agent")')"
  [[ -n "$other_workloads" ]] && return 0
  for resource in "${resources[@]}"; do
    other_objects="$(kubectl get "$resource.velero.io" -A --ignore-not-found -o jsonpath='{range .items[*]}{.metadata.namespace}{"\n"}{end}' 2>/dev/null | awk -v ns="$NAMESPACE" 'NF && $1 != ns')"
    [[ -n "$other_objects" ]] && return 0
  done
  return 1
}

if velero_is_shared; then
  DELETE_VELERO_CRDS="false"
  echo "Shared Velero usage detected; Velero CRDs will be preserved."
else
  DELETE_VELERO_CRDS="true"
  echo "No Velero usage exists outside $NAMESPACE; HyperCDR-installed Velero CRDs will be removed."
fi

echo "[1/5] Requesting normal Velero resource deletion while its controller is available"
for resource in "${resources[@]}"; do
  kubectl -n "$NAMESPACE" delete "$resource.velero.io" --all --ignore-not-found --wait=false 2>/dev/null || true
done

echo "[2/5] Waiting up to ${WAIT_SECONDS}s for Velero finalization"
deadline=$((SECONDS + WAIT_SECONDS))
while (( SECONDS < deadline )); do
  remaining=0
  for resource in "${resources[@]}"; do
    count="$(kubectl -n "$NAMESPACE" get "$resource.velero.io" --ignore-not-found -o name 2>/dev/null | wc -l)"
    remaining=$((remaining + count))
  done
  (( remaining == 0 )) && break
  sleep 2
done

if [[ "$FORCE_FINALIZERS" == "true" ]]; then
  echo "[3/5] Removing finalizers only from remaining Velero objects in $NAMESPACE"
  for resource in "${resources[@]}"; do
    kubectl -n "$NAMESPACE" get "$resource.velero.io" --ignore-not-found -o name 2>/dev/null | while read -r object; do
      [[ -n "$object" ]] && kubectl -n "$NAMESPACE" patch "$object" --type=merge -p '{"metadata":{"finalizers":[]}}'
    done
  done
else
  echo "[3/5] Stale finalizer forcing disabled"
fi

echo "[4/5] Deleting the dedicated Agent namespace"
kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false
if ! kubectl wait --for=delete "namespace/$NAMESPACE" --timeout="${WAIT_SECONDS}s" 2>/dev/null; then
  # Namespace-controller status can lag a finalizer patch briefly. Avoid a
  # false failure at the exact timeout boundary.
  sleep 5
  if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
    echo "Namespace $NAMESPACE deleted after finalizer reconciliation."
  else
  echo "Namespace deletion is still blocked. Inspect with:" >&2
  echo "  kubectl get namespace $NAMESPACE -o jsonpath='{.status.conditions}'" >&2
  echo "Rerun with --execute --force-finalizers if only stale HyperCDR Velero objects remain." >&2
  exit 4
  fi
fi

echo "[5/5] Deleting namespace-scoped HyperCDR cluster RBAC"
kubectl delete clusterrolebinding "$agent_rbac" "$velero_rbac" --ignore-not-found
kubectl delete clusterrole "$agent_rbac" "$velero_rbac" --ignore-not-found
if [[ "$DELETE_VELERO_CRDS" == "true" ]]; then
  kubectl delete customresourcedefinition "${velero_crds[@]}" --ignore-not-found
fi
echo "HyperCDR Agent cleanup completed. Application namespaces, Longhorn and external object storage were not changed."
`
