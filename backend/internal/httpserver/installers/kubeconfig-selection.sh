# Shared kubeconfig selection for the three command-based cluster installers.
# Platform-direct registration supplies its temporary file and context through
# internal arguments, so it bypasses the interactive menu.
HCDR_COMMAND_KUBECONFIG_DIR="/etc/hypercdr/kubeconfigs"

select_registration_kubeconfig() {
  local candidate mode choice index context server contexts_output
  local -a candidates=() contexts=()

  if [[ -z "$KUBECONFIG_PATH" ]]; then
    [[ "$INTERACTIVE" == "true" ]] || fail "Command registration needs an interactive terminal to select a kubeconfig from ${HCDR_COMMAND_KUBECONFIG_DIR}."
    (( EUID == 0 )) || fail "Run the registration command from a root shell so it can read ${HCDR_COMMAND_KUBECONFIG_DIR}."
    [[ -d "$HCDR_COMMAND_KUBECONFIG_DIR" ]] || fail "Kubeconfig directory ${HCDR_COMMAND_KUBECONFIG_DIR} does not exist. Create it and place a cluster-admin kubeconfig there."
    mode="$(stat -c '%a' "$HCDR_COMMAND_KUBECONFIG_DIR" 2>/dev/null || true)"
    [[ "$mode" =~ ^[0-7]+$ ]] && (( (8#$mode & 077) == 0 )) || fail "Protect ${HCDR_COMMAND_KUBECONFIG_DIR} with chmod 700."
    if ! { exec 3<>/dev/tty; } 2>/dev/null; then
      fail "Open a terminal and rerun the registration command to choose a kubeconfig from ${HCDR_COMMAND_KUBECONFIG_DIR}."
    fi
    shopt -s nullglob
    for candidate in "$HCDR_COMMAND_KUBECONFIG_DIR"/*.yaml "$HCDR_COMMAND_KUBECONFIG_DIR"/*.yml "$HCDR_COMMAND_KUBECONFIG_DIR"/*.json; do
      [[ -f "$candidate" && ! -L "$candidate" && -r "$candidate" ]] || continue
      mode="$(stat -c '%a' "$candidate" 2>/dev/null || true)"
      [[ "$mode" =~ ^[0-7]+$ ]] || continue
      if (( (8#$mode & 077) != 0 )); then
        printf 'Skipping %s: use chmod 600 to protect cluster credentials.\n' "$candidate" >&3
        continue
      fi
      contexts_output="$(command "$KUBECTL_BIN" --kubeconfig "$candidate" config get-contexts -o name 2>/dev/null || true)"
      [[ -n "$contexts_output" ]] || continue
      candidates+=("$candidate")
    done
    shopt -u nullglob
    if (( ${#candidates[@]} == 0 )); then
      exec 3>&-
      fail "No valid, private .yaml, .yml, or .json kubeconfig was found in ${HCDR_COMMAND_KUBECONFIG_DIR}."
    fi
    printf 'Kubeconfig files in %s:\n' "$HCDR_COMMAND_KUBECONFIG_DIR" >&3
    for index in "${!candidates[@]}"; do
      candidate="${candidates[$index]}"
      context="$(command "$KUBECTL_BIN" --kubeconfig "$candidate" config current-context 2>/dev/null || true)"
      server="$(command "$KUBECTL_BIN" --kubeconfig "$candidate" config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
      printf '%d) %s  [%s · %s]\n' "$((index+1))" "${candidate##*/}" "${context:-no current context}" "${server:-API unknown}" >&3
    done
    while true; do
      printf 'Select a kubeconfig [1-%d], or 0 to cancel: ' "${#candidates[@]}" >&3
      IFS= read -r choice <&3 || { exec 3>&-; fail "Kubeconfig selection was canceled."; }
      [[ "$choice" == "0" ]] && { exec 3>&-; fail "Kubeconfig selection was canceled."; }
      if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= ${#candidates[@]} )); then break; fi
      printf 'Enter a number from 1 to %d.\n' "${#candidates[@]}" >&3
    done
    KUBECONFIG_PATH="${candidates[$((choice-1))]}"
    exec 3>&-
  fi

  [[ -r "$KUBECONFIG_PATH" ]] || fail "Kubeconfig is not readable: ${KUBECONFIG_PATH}"
  export KUBECONFIG="$KUBECONFIG_PATH"
  contexts_output="$(command "$KUBECTL_BIN" --kubeconfig "$KUBECONFIG_PATH" config get-contexts -o name 2>/dev/null || true)"
  [[ -n "$contexts_output" ]] || fail "The selected kubeconfig has no valid contexts: ${KUBECONFIG_PATH}"
  mapfile -t contexts <<< "$contexts_output"
  if [[ -z "$KUBECTL_CONTEXT" ]]; then
    if (( ${#contexts[@]} == 1 )); then
      KUBECTL_CONTEXT="${contexts[0]}"
    elif (( ${#contexts[@]} > 1 )); then
      [[ "$INTERACTIVE" == "true" ]] || fail "The selected kubeconfig contains multiple contexts; choose one interactively."
      if ! { exec 3<>/dev/tty; } 2>/dev/null; then fail "Open a terminal to choose a context from ${KUBECONFIG_PATH}."; fi
      printf 'Contexts in %s:\n' "${KUBECONFIG_PATH##*/}" >&3
      for index in "${!contexts[@]}"; do
        context="${contexts[$index]}"
        server="$(command "$KUBECTL_BIN" --kubeconfig "$KUBECONFIG_PATH" --context "$context" config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
        printf '%d) %s  [%s]\n' "$((index+1))" "$context" "${server:-API unknown}" >&3
      done
      while true; do
        printf 'Select a context [1-%d], or 0 to cancel: ' "${#contexts[@]}" >&3
        IFS= read -r choice <&3 || { exec 3>&-; fail "Context selection was canceled."; }
        [[ "$choice" == "0" ]] && { exec 3>&-; fail "Context selection was canceled."; }
        if [[ "$choice" =~ ^[0-9]+$ ]] && (( choice >= 1 && choice <= ${#contexts[@]} )); then break; fi
        printf 'Enter a number from 1 to %d.\n' "${#contexts[@]}" >&3
      done
      KUBECTL_CONTEXT="${contexts[$((choice-1))]}"
    fi
  fi
  if ! printf '%s\n' "${contexts[@]}" | grep -Fxq "$KUBECTL_CONTEXT"; then
    fail "Context '${KUBECTL_CONTEXT}' was not found in ${KUBECONFIG_PATH}."
  fi
  server="$(command "$KUBECTL_BIN" --kubeconfig "$KUBECONFIG_PATH" --context "$KUBECTL_CONTEXT" config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
  printf 'Using %s · context %s · API %s\n' "${KUBECONFIG_PATH##*/}" "$KUBECTL_CONTEXT" "${server:-unknown}"
}
