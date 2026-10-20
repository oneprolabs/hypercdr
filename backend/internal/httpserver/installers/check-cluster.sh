# Read-only entry point. Does not install clients, create resources, upload
# credentials, or modify kubeconfig. Shared checks also run at installation.
CLUSTER_TYPE="native-kubernetes"
KUBECTL_BIN="kubectl"
KUBECONFIG_PATH=""
KUBECTL_CONTEXT=""
INTERACTIVE="true"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster-type) [[ $# -ge 2 ]] || fail "Missing cluster type."; CLUSTER_TYPE="$2"; shift 2 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done
case "$CLUSTER_TYPE" in native-kubernetes|huaweicloud-cce|openshift) ;; *) fail "Unsupported cluster type: $CLUSTER_TYPE" ;; esac
[[ "$(uname -s)" == "Linux" ]] || fail "Run this check on a Linux administration host."
command -v curl >/dev/null 2>&1 || fail "Install curl on this Linux host and retry."
command -v "$KUBECTL_BIN" >/dev/null 2>&1 || fail "Install kubectl on this Linux host and retry."
select_registration_kubeconfig
verify_registration_access
printf '\nAccess check passed. No cluster resources were changed.\nRun the installation command on this host as the SAME Linux user.\nKeep the same kubeconfig and context. Installation checks access again.\nThis check does not validate image pulls, storage, or full provider compatibility; the installer checks these separately.\n'
