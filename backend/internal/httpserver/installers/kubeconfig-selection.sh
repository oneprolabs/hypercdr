# Shared by the read-only access checker and all three installers. Keep the
# caller's identity and merged KUBECONFIG intact; never switch to a root shell.
select_registration_kubeconfig() {
  local contexts_output context server identity answer candidate
  local -a contexts=() files=()
  local supplied="${KUBECONFIG_PATH:-}"
  if [[ -n "$supplied" ]]; then
    [[ -r "$supplied" && -f "$supplied" ]] || fail "Kubeconfig is not readable: ${supplied}"
    export KUBECONFIG="$supplied"
  fi
  contexts_output="$(command "$KUBECTL_BIN" config get-contexts -o name 2>/dev/null || true)"
  if [[ -z "$contexts_output" ]]; then
    [[ "$INTERACTIVE" == "true" ]] || fail "No cluster configuration found. Configure KUBECONFIG or supply --kubeconfig before retrying."
    if ! { exec 3<>/dev/tty; } 2>/dev/null; then fail "Open an interactive terminal to provide your kubeconfig path."; fi
    printf 'No cluster configuration found.\nEnter the full path to your kubeconfig (empty to cancel): ' >&3
    IFS= read -r candidate <&3 || candidate=""
    exec 3>&-
    [[ -n "$candidate" ]] || fail "Cluster access check canceled; no changes were made."
    [[ "$candidate" == '~/'* ]] && candidate="$HOME/${candidate:2}"
    [[ -f "$candidate" && -r "$candidate" ]] || fail "Kubeconfig is not readable: ${candidate}. Upload the downloaded file to this Linux host and retry."
    export KUBECONFIG="$candidate"
    contexts_output="$(command "$KUBECTL_BIN" config get-contexts -o name 2>/dev/null || true)"
  fi
  [[ -n "$contexts_output" ]] || fail "No valid contexts found. Check your kubeconfig, KUBECONFIG environment variable, or complete oc login for OpenShift."
  # Do not silently change permissions on users' existing credential files.
  IFS=: read -r -a files <<< "${KUBECONFIG:-$HOME/.kube/config}"
  for candidate in "${files[@]}"; do
    [[ -n "$candidate" ]] || continue
    [[ ! -e "$candidate" || -r "$candidate" ]] || fail "Kubeconfig is not readable: ${candidate}"
  done
  mapfile -t contexts <<< "$contexts_output"
  if [[ -z "$KUBECTL_CONTEXT" ]]; then
    KUBECTL_CONTEXT="$(command "$KUBECTL_BIN" config current-context 2>/dev/null || true)"
    if [[ -z "$KUBECTL_CONTEXT" && ${#contexts[@]} -eq 1 ]]; then KUBECTL_CONTEXT="${contexts[0]}"; fi
    if [[ -z "$KUBECTL_CONTEXT" ]]; then
      [[ "$INTERACTIVE" == "true" ]] || fail "No current context. Select a context before retrying."
      if ! { exec 3<>/dev/tty; } 2>/dev/null; then fail "Open a terminal to select your cluster context."; fi
      printf 'Available contexts:\n' >&3
      printf '  %s\n' "${contexts[@]}" >&3
      printf 'Enter the context name (empty to cancel): ' >&3
      IFS= read -r KUBECTL_CONTEXT <&3 || KUBECTL_CONTEXT=""
      exec 3>&-
    fi
  fi
  printf '%s\n' "${contexts[@]}" | grep -Fxq -- "$KUBECTL_CONTEXT" || fail "Context '${KUBECTL_CONTEXT}' was not found. Select the intended cluster context and retry."
  # Every request, including the installer, uses the exact context shown here.
  export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
  KUBECONFIG_PATH="$KUBECONFIG"
  server="$(command "$KUBECTL_BIN" --context "$KUBECTL_CONTEXT" config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
  identity="$(command "$KUBECTL_BIN" --context "$KUBECTL_CONTEXT" config view --minify -o jsonpath='{.contexts[0].context.user}' 2>/dev/null || true)"
  [[ -n "$server" ]] || fail "The selected context has no API endpoint. Repair the kubeconfig and retry."
  printf '\nCluster access check\nContext:          %s\nAPI endpoint:     %s\nCredential entry: %s\n\n' "$KUBECTL_CONTEXT" "$server" "${identity:-not named}"
}

verify_registration_access() {
  local answer permissions access_output
  # A direct, bounded request avoids discovery retries and does not download
  # the full namespace inventory merely to prove access.
  if ! access_output="$(command "$KUBECTL_BIN" --context "$KUBECTL_CONTEXT" --request-timeout=15s get --raw='/api/v1/namespaces?limit=1' 2>&1)"; then
    case "$access_output" in
      *Unauthorized*|*"must be logged in"*|*"provide credentials"*|*"token has expired"*) fail "Cluster credentials are invalid or expired. Refresh the kubeconfig; for OpenShift, complete oc login again. No cluster changes were made." ;;
      *Forbidden*|*forbidden*) fail "The credentials cannot list namespaces. Use a cluster administrator account and retry. No cluster changes were made." ;;
      *x509*|*certificate*) fail "Cluster API certificate verification failed. Use a valid kubeconfig with the correct cluster CA. No cluster changes were made." ;;
      *) fail "Cannot access the cluster API. Check the API address, network, VPN, and kubeconfig. No cluster changes were made." ;;
    esac
  fi
  printf '✓ API connection and namespace access verified\n'
  permissions="$(command "$KUBECTL_BIN" --context "$KUBECTL_CONTEXT" --request-timeout=15s auth can-i '*' '*' --all-namespaces 2>/dev/null || true)"
  [[ "$permissions" == "yes" || "$permissions" == "no" ]] || fail "Could not verify administrator permissions. Check API availability and credential validity, then retry. No cluster changes were made."
  [[ "$permissions" == "yes" ]] || fail "Cluster administrator permissions are required. Ask your cluster administrator to provide suitable credentials, then retry. No cluster changes were made."
  printf '✓ Cluster administrator permissions verified\n'
  if [[ "${CLUSTER_TYPE:-}" == "openshift" ]]; then
    command "$KUBECTL_BIN" --context "$KUBECTL_CONTEXT" --request-timeout=15s get --raw=/apis/config.openshift.io/v1/clusterversions/version >/dev/null || fail "The selected context could not be verified as OpenShift. Check the target cluster and retry."
    printf '✓ OpenShift API verified\n'
  fi
  if [[ "$INTERACTIVE" == "true" ]]; then
    if ! { exec 3<>/dev/tty; } 2>/dev/null; then fail "Open an interactive terminal to confirm the target cluster."; fi
    printf '\nIs this the cluster you want to register? [y/N]: ' >&3
    IFS= read -r answer <&3 || answer=""
    exec 3>&-
    [[ "$answer" =~ ^([Yy]|[Yy][Ee][Ss])$ ]] || fail "Registration canceled; no cluster changes were made."
  fi
}
