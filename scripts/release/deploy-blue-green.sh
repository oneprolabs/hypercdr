#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -r "$SCRIPT_DIR/registry-config.sh" ]]; then
  source "$SCRIPT_DIR/registry-config.sh"
else
  source "$SCRIPT_DIR/../lib/registry-config.sh"
fi
INSTALL_DIR="${HCDR_INSTALL_DIR:-/var/lib/hypercdr}"
COMPOSE_FILE="${HCDR_COMPOSE_FILE:-${INSTALL_DIR}/docker-compose.yaml}"
ENV_FILE="${INSTALL_DIR}/.env"
EDGE_SERVICE="${HCDR_EDGE_SERVICE:-hypercdr-edge}"
DOMAIN="${HCDR_DOMAIN:-hypercdr.com}"
HEALTH_RETRIES="${HCDR_HEALTH_RETRIES:-60}"
HEALTH_INTERVAL="${HCDR_HEALTH_INTERVAL:-2}"
OBSERVE_SECONDS="${HCDR_POST_SWITCH_OBSERVE_SECONDS:-30}"
DRAIN_SECONDS="${HCDR_OLD_COLOR_DRAIN_SECONDS:-180}"
LOCK_DIR=""
DEPLOY_AUTH_CHALLENGE_MODE="${HCDR_AUTH_CHALLENGE_MODE:-}"
DEPLOY_TURNSTILE_SITE_KEY="${HCDR_TURNSTILE_SITE_KEY:-}"
DEPLOY_TURNSTILE_SECRET_KEY="${HCDR_TURNSTILE_SECRET_KEY:-}"

log() { printf '[blue-green] %s\n' "$*"; }
die() { printf '[blue-green] ERROR: %s\n' "$*" >&2; exit 1; }

other_color() {
  case "${1:-}" in
    blue) echo green ;;
    green) echo blue ;;
    *) echo "invalid color: ${1:-}" >&2; return 2 ;;
  esac
}

select_deploy_color() {
  local current="$1" running="$2"
  if [[ "$running" == true ]]; then other_color "$current"; else echo "$current"; fi
}

set_env_value() {
  local file="$1" key="$2" value="$3"
  local tmp="${file}.tmp"
  awk -F= -v key="$key" -v value="$value" '
    BEGIN { found=0 }
    $1 == key { print key "=" value; found=1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
  chmod 600 "$file"
}

read_active_color() {
  local marker="${INSTALL_DIR}/.active_color"
  if [[ -s "$marker" ]]; then
    local color
    color="$(tr -d '[:space:]' < "$marker")"
    case "$color" in blue|green) echo "$color"; return ;; esac
  fi
  echo blue
}

read_color_version() {
  local color="$1" key="PLATFORM_API_${1^^}_IMAGE" image
  image="$(sed -n "s/^${key}=//p" "$ENV_FILE" | tail -1)"
  local tag="${image##*:}"
  printf '%s\n' "${tag#platform-api-}"
}

render_upstream() {
  local color="$1" output="$2"
  case "$color" in blue|green) ;; *) return 2 ;; esac
  mkdir -p "$(dirname "$output")"
  cat > "${output}.tmp" <<EOF
map \$host \$hypercdr_api_active { default hypercdr-platform-api-${color}:18080; }
map \$host \$hypercdr_frontend_active { default hypercdr-platform-frontend-${color}:80; }
EOF
  mv "${output}.tmp" "$output"
}

compose() {
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" --project-name hypercdr "$@"
}

check_proxy_network() {
  local network="${HCDR_PROXY_NETWORK:-nginx-proxy-manager_default}"
  [[ "${HCDR_NPM_UPSTREAM_READY:-false}" == true ]] ||
    die "confirm Nginx Proxy Manager forwards via HTTPS to the server on port 12443, then set HCDR_NPM_UPSTREAM_READY=true in ${ENV_FILE}"
  docker network inspect "$network" >/dev/null 2>&1 ||
    die "shared Nginx Proxy Manager network does not exist: ${network}; set HCDR_PROXY_NETWORK to its exact Docker network name"
}

ensure_edge_tls() {
  local cert="${HCDR_TLS_CERT_FILE:-${INSTALL_DIR}/tls.crt}"
  local key="${HCDR_TLS_KEY_FILE:-${INSTALL_DIR}/tls.key}"
  local san="DNS:${DOMAIN}"
  [[ -s "$cert" && -s "$key" ]] && return 0
  [[ ! -e "$cert" && ! -e "$key" ]] || die "edge TLS certificate and key must both exist: ${cert} ${key}"
  command -v openssl >/dev/null 2>&1 || die "openssl is required to create the edge TLS certificate"
  if [[ "${DOMAIN}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    san="IP:${DOMAIN}"
  fi
  mkdir -p "$(dirname "$cert")"
  openssl req -x509 -nodes -newkey rsa:2048 -sha256 -days 3650 \
    -subj "/CN=${DOMAIN}" -addext "subjectAltName=${san},IP:127.0.0.1" \
    -keyout "$key" -out "$cert" >/dev/null 2>&1 || die "could not create edge TLS certificate"
  chmod 600 "$key"
  chmod 644 "$cert"
  set_env_value "$ENV_FILE" HCDR_TLS_CERT_FILE "$cert"
  set_env_value "$ENV_FILE" HCDR_TLS_KEY_FILE "$key"
  load_runtime_env
}

container_running() {
  [[ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null || true)" == true ]]
}

wait_for_service() {
  local service="$1" attempt status
  for ((attempt=1; attempt<=HEALTH_RETRIES; attempt++)); do
    status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$service" 2>/dev/null || true)"
    if [[ "$status" == healthy || "$status" == running ]]; then return 0; fi
    sleep "$HEALTH_INTERVAL"
  done
  return 1
}

wait_for_http() {
  local target="$1" port="$2" path="$3" scheme="${4:-http}" attempt
  for ((attempt=1; attempt<=HEALTH_RETRIES; attempt++)); do
    local tls_args=()
    [[ "$scheme" == https ]] && tls_args+=(--no-check-certificate)
    if docker exec "$EDGE_SERVICE" wget "${tls_args[@]}" -q -O /dev/null "${scheme}://${target}:${port}${path}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$HEALTH_INTERVAL"
  done
  return 1
}

switch_traffic() {
  local from_color="$1" to_color="$2" upstream_file="${INSTALL_DIR}/nginx/conf.d/upstream.conf"
  mkdir -p "$(dirname "$upstream_file")"
  render_upstream "$to_color" "$upstream_file"
  if ! docker exec "$EDGE_SERVICE" nginx -t >/dev/null; then
    render_upstream "$from_color" "$upstream_file"
    return 1
  fi
  if ! docker exec "$EDGE_SERVICE" nginx -s reload >/dev/null; then
    render_upstream "$from_color" "$upstream_file"
    docker exec "$EDGE_SERVICE" nginx -t >/dev/null 2>&1 || true
    return 1
  fi
}

wait_for_public_ready() {
  local attempt
  for ((attempt=1; attempt<=HEALTH_RETRIES; attempt++)); do
    if docker exec "$EDGE_SERVICE" wget --no-check-certificate -q -O /dev/null "https://127.0.0.1/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$HEALTH_INTERVAL"
  done
  return 1
}

validate_runtime() {
  [[ -f "$ENV_FILE" ]] || die "missing runtime environment: $ENV_FILE"
  [[ -f "$COMPOSE_FILE" ]] || die "missing Compose file: $COMPOSE_FILE"
  [[ -n "${HCDR_IMAGE_REGISTRY:-}" ]] || die "HCDR_IMAGE_REGISTRY is required"
  [[ -n "${HCDR_POSTGRES_PASSWORD:-}" ]] || die "HCDR_POSTGRES_PASSWORD is required"
  [[ -n "${HCDR_DATABASE_URL:-}" ]] || die "HCDR_DATABASE_URL is required"
  [[ -n "${PLATFORM_API_BLUE_IMAGE:-}" && -n "${PLATFORM_FRONTEND_BLUE_IMAGE:-}" ]] || die "blue image variables are required"
  [[ -n "${PLATFORM_API_GREEN_IMAGE:-}" && -n "${PLATFORM_FRONTEND_GREEN_IMAGE:-}" ]] || die "green image variables are required"
}

load_runtime_env() {
  [[ -f "$ENV_FILE" ]] || die "missing runtime environment: $ENV_FILE"
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
  DOMAIN="${HCDR_DOMAIN:-$DOMAIN}"
}

sync_auth_challenge_env() {
  [[ -n "$DEPLOY_AUTH_CHALLENGE_MODE" ]] || return 0
  case "$DEPLOY_AUTH_CHALLENGE_MODE" in
    image)
      set_env_value "$ENV_FILE" HCDR_AUTH_CHALLENGE_MODE image
      set_env_value "$ENV_FILE" HCDR_TURNSTILE_SITE_KEY ""
      set_env_value "$ENV_FILE" HCDR_TURNSTILE_SECRET_KEY ""
      ;;
    turnstile)
      [[ -n "$DEPLOY_TURNSTILE_SITE_KEY" && -n "$DEPLOY_TURNSTILE_SECRET_KEY" ]] ||
        die "Turnstile mode requires both GitHub Turnstile keys"
      set_env_value "$ENV_FILE" HCDR_AUTH_CHALLENGE_MODE turnstile
      set_env_value "$ENV_FILE" HCDR_TURNSTILE_SITE_KEY "$DEPLOY_TURNSTILE_SITE_KEY"
      set_env_value "$ENV_FILE" HCDR_TURNSTILE_SECRET_KEY "$DEPLOY_TURNSTILE_SECRET_KEY"
      ;;
    *) die "HCDR_AUTH_CHALLENGE_MODE must be image or turnstile" ;;
  esac
  load_runtime_env
}

start_color() {
  local color="$1"
  compose --profile "$color" up -d "hypercdr-platform-api-${color}" "hypercdr-platform-frontend-${color}"
  wait_for_service "hypercdr-platform-api-${color}" || return 1
  wait_for_service "hypercdr-platform-frontend-${color}" || return 1
  wait_for_http "hypercdr-platform-api-${color}" 18080 /readyz || return 1
  wait_for_http "hypercdr-platform-frontend-${color}" 80 / || return 1
}

rollback_color() {
  validate_runtime
  local current target rollback_version registry
  current="$(read_active_color)"
  target="$(other_color "$current")"
  rollback_version="$(sed -n 's/^ *//p' "${INSTALL_DIR}/.rollback_version" 2>/dev/null | head -1 || true)"
  registry="${HCDR_IMAGE_REGISTRY%/}"
  if [[ "${rollback_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ ]]; then
    set_env_value "$ENV_FILE" "PLATFORM_API_${target^^}_IMAGE" "$(image_ref "${registry}" "platform-api" "${rollback_version}")"
    set_env_value "$ENV_FILE" "PLATFORM_FRONTEND_${target^^}_IMAGE" "$(image_ref "${registry}" "platform-frontend" "${rollback_version}")"
    load_runtime_env
  fi
  log "rolling back from $current to $target"
  start_color "$target" || die "rollback target $target did not become healthy"
  switch_traffic "$current" "$target" || die "Nginx rejected rollback"
  printf '%s\n' "$target" > "${INSTALL_DIR}/.active_color"
  log "rollback completed: $target"
}

start_current() {
  validate_runtime
  check_proxy_network
  ensure_edge_tls
  local current
  current="$(read_active_color)"
  compose up -d hypercdr-postgres hypercdr-edge hypercdr-cluster-registration-executor
  start_color "$current" || die "active color $current did not become healthy"
  log "active color started: $current"
}

acquire_deploy_lock() {
  if command -v flock >/dev/null 2>&1; then
    exec 9>"${INSTALL_DIR}/.deploy.lock"
    flock -n 9 || die "another deployment is already running"
    return
  fi
  LOCK_DIR="${INSTALL_DIR}/.deploy.lock.d"
  mkdir "$LOCK_DIR" 2>/dev/null || die "another deployment is already running"
  trap 'rmdir "${LOCK_DIR}" 2>/dev/null || true' EXIT
}

deploy_version() {
  local version="$1" current candidate previous_version first_install=false
  validate_runtime
  current="$(read_active_color)"
  if ! container_running "hypercdr-platform-api-${current}"; then
    first_install=true
  fi
  previous_version="$(read_color_version "$current")"
  candidate="$(select_deploy_color "$current" "$([[ "$first_install" == true ]] && echo false || echo true)")"
  log "deploying version $version to $candidate (current=$current, first_install=$first_install)"

  local registry="${HCDR_IMAGE_REGISTRY%/}"
  set_env_value "$ENV_FILE" "PLATFORM_API_${candidate^^}_IMAGE" "$(image_ref "${registry}" "platform-api" "${version}")"
  set_env_value "$ENV_FILE" "PLATFORM_FRONTEND_${candidate^^}_IMAGE" "$(image_ref "${registry}" "platform-frontend" "${version}")"
  set_env_value "$ENV_FILE" RELEASE_VERSION "$version"
  set_env_value "$ENV_FILE" HCDR_IMAGE_TAG "$version"
  load_runtime_env

  check_proxy_network
  ensure_edge_tls
  compose up -d hypercdr-postgres hypercdr-edge
  compose pull "hypercdr-platform-api-${candidate}" "hypercdr-platform-frontend-${candidate}"
  start_color "$candidate" || die "candidate color $candidate failed health checks; active color remains $current"

  if [[ "$first_install" == true ]]; then
    render_upstream "$candidate" "${INSTALL_DIR}/nginx/conf.d/upstream.conf"
    docker exec "$EDGE_SERVICE" nginx -t >/dev/null || die "initial Nginx configuration is invalid"
    docker exec "$EDGE_SERVICE" nginx -s reload >/dev/null || die "initial Nginx reload failed"
  else
    switch_traffic "$current" "$candidate" || die "Nginx rejected the blue/green switch; $current remains active"
  fi
  printf '%s\n' "$candidate" > "${INSTALL_DIR}/.active_color"
  if [[ "$first_install" != true && "${previous_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ ]]; then
    printf '%s\n' "${previous_version}" > "${INSTALL_DIR}/.rollback_version"
    chmod 600 "${INSTALL_DIR}/.rollback_version"
  fi
  if ! wait_for_public_ready; then
    log "public health failed after switch; restoring $current"
    if [[ "$first_install" != true ]]; then
      switch_traffic "$candidate" "$current" || true
      printf '%s\n' "$current" > "${INSTALL_DIR}/.active_color"
    fi
    die "public /readyz did not become healthy"
  fi
  log "traffic switched to $candidate"
  set_env_value "$ENV_FILE" REGISTRATION_EXECUTOR_IMAGE "$(image_ref "${registry}" "cluster-registration-executor" "${version}")"
  load_runtime_env
  compose pull hypercdr-cluster-registration-executor
  compose up -d hypercdr-cluster-registration-executor
  if [[ "$first_install" != true && "$DRAIN_SECONDS" != 0 ]]; then sleep "$DRAIN_SECONDS"; fi
  if [[ "$first_install" != true ]]; then
    compose stop "hypercdr-platform-api-${current}" "hypercdr-platform-frontend-${current}"
  fi
}

main() {
  mkdir -p "$INSTALL_DIR"
  acquire_deploy_lock
  load_runtime_env
  sync_auth_challenge_env
  if [[ "${1:-}" == --rollback ]]; then rollback_color; return; fi
  if [[ "${1:-}" == --start-current ]]; then start_current; return; fi
  local version="${1:-}"
  version="${version#v}"
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]{8}$ ]] || die "version must match MAJOR.MINOR.PATCH.YYYYMMDD"
  deploy_version "$version"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
