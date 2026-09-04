# Common preflight orchestration. Independent image checks run concurrently so
# adding a component does not add another full pull timeout to registration.
run_image_pull_preflights() {
  [[ "$IMAGE_PULL_PREFLIGHT" == "true" ]] || return 0

  if [[ "${IMAGE_PULL_PREFLIGHT_STRATEGY:-parallel}" == "sequential" ]]; then
    preflight_image_pull "hypercdr-image-check-agent" "$AGENT_IMAGE" "      command: [\"${AGENT_COMMAND:-/comm-agent}\"]"
    if [[ "$INSTALL_VELERO" == "true" ]]; then
      preflight_image_pull "hypercdr-image-check-velero" "$VELERO_IMAGE" '      command: ["/velero", "version", "--client-only"]'
      preflight_image_pull "hypercdr-image-check-velero-aws-plugin" "$VELERO_AWS_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-aws"]'
      preflight_image_pull "hypercdr-image-check-velero-azure-plugin" "$VELERO_AZURE_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-microsoft-azure"]'
      preflight_image_pull "hypercdr-image-check-velero-gcp-plugin" "$VELERO_GCP_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-gcp"]'
    fi
    return 0
  fi

  local work_dir status index
  local -a pids=() labels=()
  work_dir="$(mktemp -d)"

  start_image_preflight() {
    local label="$1" name="$2" image="$3" command_yaml="$4"
    labels+=("$label")
    (preflight_image_pull "$name" "$image" "$command_yaml") >"$work_dir/${#pids[@]}.log" 2>&1 &
    pids+=("$!")
  }

  start_image_preflight "Agent" "hypercdr-image-check-agent" "$AGENT_IMAGE" "      command: [\"${AGENT_COMMAND:-/comm-agent}\"]"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    start_image_preflight "Velero" "hypercdr-image-check-velero" "$VELERO_IMAGE" '      command: ["/velero", "version", "--client-only"]'
    start_image_preflight "Velero AWS plugin" "hypercdr-image-check-velero-aws-plugin" "$VELERO_AWS_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-aws"]'
    start_image_preflight "Velero Azure plugin" "hypercdr-image-check-velero-azure-plugin" "$VELERO_AZURE_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-microsoft-azure"]'
    start_image_preflight "Velero GCP plugin" "hypercdr-image-check-velero-gcp-plugin" "$VELERO_GCP_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-gcp"]'
  fi

  status=0
  for index in "${!pids[@]}"; do
    if ! wait "${pids[$index]}"; then status=1; fi
    cat "$work_dir/$index.log"
  done
  rm -rf "$work_dir"
  [[ "$status" -eq 0 ]] || fail "One or more image pull preflight checks failed. No Agent resources were installed."
}
