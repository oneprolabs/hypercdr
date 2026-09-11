# Registry preflight proves the cluster can resolve, authenticate to, and pull
# from the managed registry. Release validation is responsible for checking
# that every published component image exists, so registration only pulls one
# representative image instead of pre-downloading the complete runtime set.
run_image_pull_preflights() {
  [[ "$IMAGE_PULL_PREFLIGHT" == "true" ]] || return 0
  log_info "Validating managed registry access with one representative image"
  preflight_image_pull "hypercdr-registry-check" "$AGENT_IMAGE" "      command: [\"${AGENT_COMMAND:-/comm-agent}\"]"
}
