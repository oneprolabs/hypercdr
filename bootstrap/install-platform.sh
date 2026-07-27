#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -r "${SCRIPT_DIR}/scripts/lib/registry-config.sh" ]]; then
  REGISTRY_HELPER="${SCRIPT_DIR}/scripts/lib/registry-config.sh"
  DEFAULT_REGISTRY_CONFIG="${SCRIPT_DIR}/config/registries.conf"
else
  SOURCE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
  REGISTRY_HELPER="${SOURCE_ROOT}/scripts/lib/registry-config.sh"
  DEFAULT_REGISTRY_CONFIG="${SOURCE_ROOT}/config/registries.conf"
fi
if [[ -d "${SCRIPT_DIR}/charts/hypercdr-platform" ]]; then
  CHART_DIR="${SCRIPT_DIR}/charts/hypercdr-platform"
  COMPOSE_TEMPLATE="${SCRIPT_DIR}/compose.yaml"
else
  SOURCE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
  CHART_DIR="${SOURCE_ROOT}/charts/hypercdr-platform"
  COMPOSE_TEMPLATE="${SOURCE_ROOT}/docker-compose.yml"
fi
RELEASE_NAME="${RELEASE_NAME:-hypercdr}"

install_header() {
  printf '\n============================================================\n'
  printf ' HyperCDR Control Plane Installation\n'
  printf '============================================================\n'
}

install_step() {
  printf '\n[%s/%s] %s\n' "$1" "$2" "$3"
}

install_ok() {
  printf '      OK  %s\n' "$1"
}

install_fail() {
  printf '  FAILED  %s\n' "$1" >&2
}

run_logged() {
  local label="$1"
  shift
  local output_file
  output_file="$(mktemp)"
  if "$@" >"${output_file}" 2>&1; then
    rm -f "${output_file}"
    install_ok "${label}"
    return 0
  fi
  install_fail "${label}"
  sed 's/^/          /' "${output_file}" >&2
  rm -f "${output_file}"
  return 1
}

usage() {
  cat <<'USAGE'
HyperCDR control plane bootstrap installer

Usage:
  ./install-platform.sh k8s [options]
  ./install-platform.sh docker [options]

Kubernetes options:
  --kubeconfig PATH            Optional kubeconfig path for kubectl and helm
  --namespace NAME             Namespace for the control plane, default: hypercdr-system
  --public-base-url URL        Container DR control plane URL used by users and agents
  --registry REGISTRY          Optional OCI Registry prefix override.
  --registry-profile NAME      Registry profile; defaults to HCDR_ACTIVE_REGISTRY.
  --registry-config PATH       Registry profiles file.
  --registry-trust MODE        system (default) or private-ca
  --registry-ca-file PATH      PEM CA certificate, required for private-ca
  --image-tag TAG              Platform/agent image tag, default v20260714.5.
  --storage-class NAME         StorageClass for bundled PostgreSQL PVC, default: longhorn
  --database-mode MODE         bundled or external, default: bundled
  --node-port PORT             NodePort for platform Service. Defaults to URL port when present.
  --secret-key VALUE           Platform secret key. A development key is used if omitted.
  --timeout DURATION           Wait timeout, default: 5m
  --execute                    Run helm commands. Without this flag, prints and validates the plan only.

Docker options:
  --public-base-url URL        Container DR control plane URL used by users and agents
  --data-dir PATH              Persistent data directory, default: /var/lib/hypercdr
  --registry REGISTRY          Optional OCI Registry prefix override.
  --registry-profile NAME      Registry profile; defaults to HCDR_ACTIVE_REGISTRY.
  --registry-config PATH       Registry profiles file.
  --registry-trust MODE        system (default) or private-ca
  --registry-ca-file PATH      PEM CA certificate, required for private-ca
  --image-tag TAG              Platform/agent image tag, default v20260714.5.
  --velero-image IMAGE         Velero image, default <registry>/velero:v1.17.1-hcdr.1-20260716.
  --velero-aws-plugin-image IMAGE
                              Velero AWS plugin image, default <registry>/velero-plugin-for-aws:v1.13.0.
  --http-port PORT             Frontend host port. Defaults to the port in --public-base-url, or 3002.
  --api-port PORT              API host port, default 18080.
  --tls-cert-file PATH         Existing platform certificate to use. Optional.
  --tls-key-file PATH          Existing platform private key to use. Optional.
  --execute                    Run docker compose commands. Without this flag, prints the plan only.

Recommended no-DNS Kubernetes flow:
  1. Download and extract hypercdr-bootstrap.tar.gz.
  2. Run this script with --public-base-url https://<node-ip>:<node-port>.
  3. Add --execute after reviewing the rendered plan.
USAGE
}

mode="${1:-}"
if [[ -z "$mode" || "$mode" == "-h" || "$mode" == "--help" ]]; then
  usage
  exit 0
fi
shift

namespace="hypercdr-system"
kubeconfig=""
public_base_url=""
registry=""
registry_profile=""
registry_config="${HCDR_REGISTRY_CONFIG:-${DEFAULT_REGISTRY_CONFIG}}"
registry_trust="system"
registry_ca_file=""
storage_class="longhorn"
database_mode="bundled"
node_port=""
secret_key="dev-secret-change-me"
data_dir="/var/lib/hypercdr"
http_port=""
api_port="18080"
image_tag="${HCDR_IMAGE_TAG:-v20260714.5}"
velero_image=""
velero_aws_plugin_image=""
input_tls_cert_file=""
input_tls_key_file=""
timeout="5m"
execute="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --kubeconfig) kubeconfig="${2:?missing value for --kubeconfig}"; shift 2 ;;
    --namespace) namespace="${2:?missing value for --namespace}"; shift 2 ;;
    --public-base-url) public_base_url="${2:?missing value for --public-base-url}"; shift 2 ;;
    --registry) registry="${2:?missing value for --registry}"; shift 2 ;;
    --registry-profile) registry_profile="${2:?missing value for --registry-profile}"; shift 2 ;;
    --registry-config) registry_config="${2:?missing value for --registry-config}"; shift 2 ;;
    --registry-trust) registry_trust="${2:?missing value for --registry-trust}"; shift 2 ;;
    --registry-ca-file) registry_ca_file="${2:?missing value for --registry-ca-file}"; shift 2 ;;
    --image-tag) image_tag="${2:?missing value for --image-tag}"; shift 2 ;;
    --velero-image) velero_image="${2:?missing value for --velero-image}"; shift 2 ;;
    --velero-aws-plugin-image) velero_aws_plugin_image="${2:?missing value for --velero-aws-plugin-image}"; shift 2 ;;
    --storage-class) storage_class="${2:?missing value for --storage-class}"; shift 2 ;;
    --database-mode) database_mode="${2:?missing value for --database-mode}"; shift 2 ;;
    --node-port) node_port="${2:?missing value for --node-port}"; shift 2 ;;
    --secret-key) secret_key="${2:?missing value for --secret-key}"; shift 2 ;;
    --data-dir) data_dir="${2:?missing value for --data-dir}"; shift 2 ;;
    --http-port) http_port="${2:?missing value for --http-port}"; shift 2 ;;
    --api-port) api_port="${2:?missing value for --api-port}"; shift 2 ;;
    --tls-cert-file) input_tls_cert_file="${2:?missing value for --tls-cert-file}"; shift 2 ;;
    --tls-key-file) input_tls_key_file="${2:?missing value for --tls-key-file}"; shift 2 ;;
    --timeout) timeout="${2:?missing value for --timeout}"; shift 2 ;;
    --execute) execute="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$public_base_url" ]]; then
  echo "--public-base-url is required" >&2
  exit 2
fi

if [[ -z "$registry" ]]; then
  # shellcheck disable=SC1090
  source "${REGISTRY_HELPER}"
  load_registry_profile "${registry_config}" "${registry_profile}"
  registry="${HCDR_IMAGE_REGISTRY}"
  registry_trust="${HCDR_REGISTRY_TRUST:-system}"
  [[ -n "${registry_ca_file}" ]] || registry_ca_file="${HCDR_REGISTRY_CA_FILE:-}"
fi

case "${registry_trust}" in
  system)
    [[ -z "${registry_ca_file}" ]] || { echo "--registry-ca-file requires --registry-trust private-ca" >&2; exit 2; }
    ;;
  private-ca)
    [[ -n "${registry_ca_file}" ]] || { echo "--registry-ca-file is required with --registry-trust private-ca" >&2; exit 2; }
    [[ -r "${registry_ca_file}" ]] || { echo "registry CA file is not readable: ${registry_ca_file}" >&2; exit 1; }
    if command -v openssl >/dev/null 2>&1; then
      openssl x509 -in "${registry_ca_file}" -noout >/dev/null 2>&1 || { echo "registry CA file is not a valid PEM certificate: ${registry_ca_file}" >&2; exit 1; }
    fi
    ;;
  *) echo "--registry-trust must be system or private-ca" >&2; exit 2 ;;
esac

agent_ws_endpoint="${public_base_url/https:/wss:}"
agent_ws_endpoint="${agent_ws_endpoint/http:/ws:}/ws/agent"

extract_port_from_url() {
  local url="$1"
  local without_scheme="${url#*://}"
  local hostport="${without_scheme%%/*}"
  if [[ "$hostport" == *:* ]]; then
    echo "${hostport##*:}"
  fi
}

extract_host_from_url() {
  local url="$1"
  local without_scheme="${url#*://}"
  local hostport="${without_scheme%%/*}"
  if [[ "$hostport" == \[*\]* ]]; then
    echo "${hostport#\[}" | cut -d']' -f1
    return
  fi
  echo "${hostport%%:*}"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

preflight_registry() {
  require_command curl
  local normalized="${registry#http://}"
  normalized="${normalized#https://}"
  local registry_host="${normalized%%/*}"
  local curl_args=(-sS -o /dev/null -w "%{http_code}" --connect-timeout 8 --max-time 20)
  if [[ "${registry_trust}" == "private-ca" ]]; then
    curl_args+=(--cacert "${registry_ca_file}")
  fi
  local status
  status="$(curl "${curl_args[@]}" "https://${registry_host}/v2/" || true)"
  case "${status}" in
    200|401) return 0 ;;
    000) echo "Unable to establish a trusted TLS connection to ${registry_host}. Check DNS, network, and certificate trust." >&2; exit 1 ;;
    *) echo "${registry_host} did not return a valid OCI Registry response (HTTP ${status})." >&2; exit 1 ;;
  esac
}

preflight_host_port() {
  local port="$1"
  local expected_container="$2"
  if docker ps --filter "name=^/${expected_container}$" --format '{{.Names}}' | grep -qx "${expected_container}"; then
    return 0
  fi
  if command -v ss >/dev/null 2>&1 && ss -H -ltn "sport = :${port}" | grep -q .; then
    echo "Host port ${port} is already in use. Stop the conflicting service or select another port." >&2
    return 1
  fi
}

print_common() {
  cat <<EOF
HyperCDR control plane deployment plan

Release name:         ${RELEASE_NAME}
Public base URL:      ${public_base_url}
Agent WebSocket URL:  ${agent_ws_endpoint}
Image registry:       ${registry}
Registry trust:       ${registry_trust}
Registry CA file:     ${registry_ca_file:-"(system trust store)"}
Default admin:        admin / admin123
Execute changes:      ${execute}
EOF
}

kubectl_args=()
helm_args=()
if [[ -n "$kubeconfig" ]]; then
  kubectl_args+=(--kubeconfig "$kubeconfig")
  helm_args+=(--kubeconfig "$kubeconfig")
fi

run_k8s() {
  if [[ "$execute" == "true" ]]; then
    require_command kubectl
    require_command helm
    preflight_registry
  fi

  local tls_enabled="false"
  local tls_dir="${SCRIPT_DIR}/tls"
  local tls_cert_file="${tls_dir}/platform.crt"
  local tls_key_file="${tls_dir}/platform.key"
  local public_host
  public_host="$(extract_host_from_url "$public_base_url")"
  if [[ -z "$http_port" ]]; then
    http_port="$(extract_port_from_url "$public_base_url" || true)"
  fi
  if [[ -z "$http_port" ]]; then
    http_port="3002"
  fi
  if [[ -z "${velero_image}" ]]; then
    velero_image="${registry}/velero:v1.17.1-hcdr.1-20260716"
  fi
  if [[ -z "${velero_aws_plugin_image}" ]]; then
    velero_aws_plugin_image="${registry}/velero-plugin-for-aws:v1.13.0"
  fi
  if [[ "${secret_key}" == "dev-secret-change-me" ]] && command -v openssl >/dev/null 2>&1; then
    secret_key="$(openssl rand -hex 32)"
  fi
  if [[ "$public_base_url" == https://* ]]; then
    tls_enabled="true"
  fi

  if [[ ! -f "${CHART_DIR}/Chart.yaml" ]]; then
    cat >&2 <<EOF
Helm chart not found: ${CHART_DIR}

Download and extract the full bootstrap package first:
  curl -fsSL <portal>/releases/dev/hypercdr-bootstrap.tar.gz -o hypercdr-bootstrap.tar.gz
  tar -xzf hypercdr-bootstrap.tar.gz
  cd hypercdr-bootstrap
EOF
    exit 1
  fi

  if [[ -z "$node_port" ]]; then
    node_port="$(extract_port_from_url "$public_base_url" || true)"
  fi
  if [[ -z "$node_port" ]]; then
    node_port="30080"
  fi

  print_common
  cat <<EOF
Mode:                 Kubernetes
Namespace:            ${namespace}
Kubeconfig:           ${kubeconfig:-"(current context)"}
Chart:                ${CHART_DIR}
StorageClass:         ${storage_class}
Database mode:        ${database_mode}
NodePort:             ${node_port}
HTTPS enabled:        ${tls_enabled}
TLS cert file:        $([[ "${tls_enabled}" == "true" ]] && echo "${tls_cert_file}" || echo "(disabled)")
Image tag:            ${image_tag}
Agent image:          ${registry}/comm-agent:${image_tag}
Velero image:         ${velero_image}
Velero AWS plugin:    ${velero_aws_plugin_image}

Preflight:
  kubectl context: $(kubectl "${kubectl_args[@]}" config current-context 2>/dev/null || echo "unknown")

Planned command:
helm ${helm_args[*]} upgrade --install ${RELEASE_NAME} ${CHART_DIR} \\
  --namespace ${namespace} \\
  --create-namespace \\
  --wait --timeout ${timeout} \\
  --set-string global.publicBaseURL=${public_base_url} \\
  --set-string global.agentWebSocketURL=${agent_ws_endpoint} \\
  --set-string global.imageRegistry=${registry} \\
  --set-string platform.image.tag=${image_tag} \\
  --set platform.registryCA.enabled=$([[ "${registry_trust}" == "private-ca" ]] && echo true || echo false) \\
  $([[ "${registry_trust}" == "private-ca" ]] && echo "--set-file platform.registryCA.certificate=${registry_ca_file} \\" || true)
  --set platform.tls.enabled=${tls_enabled} \\
  $([[ "${tls_enabled}" == "true" ]] && echo "--set-file platform.tls.cert=${tls_cert_file} \\" || true)
  $([[ "${tls_enabled}" == "true" ]] && echo "--set-file platform.tls.key=${tls_key_file} \\" || true)
  --set platform.service.nodePort=${node_port} \\
  --set-string postgresql.mode=${database_mode} \\
  --set-string postgresql.storageClass=${storage_class} \\
  --set-string postgresql.image.repository=${registry}/postgres \\
  --set-string secrets.secretKey=<redacted>
EOF

  if [[ "$execute" != "true" ]]; then
    cat <<EOF

Dry-run mode. Add --execute to install.
EOF
    return
  fi

  if [[ "${tls_enabled}" == "true" ]]; then
    require_command openssl
    mkdir -p "${tls_dir}"
    if [[ ! -f "${tls_cert_file}" || ! -f "${tls_key_file}" ]]; then
      local san_entry="DNS:${public_host}"
      if [[ "${public_host}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        san_entry="IP:${public_host}"
      fi
      openssl req -x509 -nodes -newkey rsa:4096 -sha256 -days 36500 \
        -subj "/CN=${public_host}" \
        -addext "subjectAltName=${san_entry},DNS:localhost,IP:127.0.0.1" \
        -keyout "${tls_key_file}" \
        -out "${tls_cert_file}" >/dev/null 2>&1
      chmod 600 "${tls_key_file}"
      chmod 644 "${tls_cert_file}"
    fi
  fi

  tls_helm_args=(--set "platform.tls.enabled=${tls_enabled}")
  if [[ "${tls_enabled}" == "true" ]]; then
    tls_helm_args+=(--set-file "platform.tls.cert=${tls_cert_file}" --set-file "platform.tls.key=${tls_key_file}")
  fi

  registry_ca_helm_args=(--set "platform.registryCA.enabled=false")
  if [[ "${registry_trust}" == "private-ca" ]]; then
    registry_ca_helm_args=(--set "platform.registryCA.enabled=true" --set-file "platform.registryCA.certificate=${registry_ca_file}")
  fi

  helm "${helm_args[@]}" upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" \
    --namespace "${namespace}" \
    --create-namespace \
    --wait --timeout "${timeout}" \
    --set-string "global.publicBaseURL=${public_base_url}" \
    --set-string "global.agentWebSocketURL=${agent_ws_endpoint}" \
    --set-string "global.imageRegistry=${registry}" \
    --set-string "platform.image.tag=${image_tag}" \
    "${registry_ca_helm_args[@]}" \
    "${tls_helm_args[@]}" \
    --set "platform.service.nodePort=${node_port}" \
    --set-string "postgresql.mode=${database_mode}" \
    --set-string "postgresql.storageClass=${storage_class}" \
    --set-string "postgresql.image.repository=${registry}/postgres" \
    --set-string "secrets.secretKey=${secret_key}"

  kubectl "${kubectl_args[@]}" -n "${namespace}" rollout status "deployment/${RELEASE_NAME}-platform" --timeout="${timeout}"

  cat <<EOF

HyperCDR control plane installed.

Access URL:
  ${public_base_url}

Useful checks:
  kubectl -n ${namespace} get pods,svc
  kubectl -n ${namespace} logs deploy/${RELEASE_NAME}-platform
  curl -fsSL ${public_base_url}/readyz

If image pulls fail, ensure every Kubernetes node trusts Harbor CA for:
  ${registry}
EOF
}

run_docker() {
  require_command docker
  require_command openssl
  local tls_enabled="false"
  local tls_dir="${data_dir}/tls"
  local tls_cert_file="${tls_dir}/platform.crt"
  local tls_key_file="${tls_dir}/platform.key"
  local installed_registry_ca_file="${data_dir}/certs/registry-ca.crt"
  local target_compose_file="${data_dir}/docker-compose.yaml"
  local postgres_password=""
  local release_token=""
  if [[ -f "${data_dir}/.env" ]]; then
    while IFS='=' read -r key value; do
      if [[ "${key}" == "HCDR_POSTGRES_PASSWORD" ]]; then postgres_password="${value}"; break; fi
    done < "${data_dir}/.env"
    # Compatibility with installations created before the password setting was
    # introduced. Their initialized database still uses the legacy password.
    if [[ -z "${postgres_password}" ]]; then postgres_password="hypercdr"; fi
  else
    postgres_password="$(openssl rand -hex 24)"
  fi
  if [[ -f "${data_dir}/release-token" ]]; then
    release_token="$(tr -d '\r\n' < "${data_dir}/release-token")"
  else
    release_token="$(openssl rand -hex 32)"
  fi
  local public_host
  public_host="$(extract_host_from_url "$public_base_url")"
  if [[ -z "$http_port" ]]; then
    http_port="$(extract_port_from_url "$public_base_url" || true)"
  fi
  if [[ -z "$http_port" ]]; then
    http_port="3002"
  fi
  if [[ -z "${velero_image}" ]]; then
    velero_image="${registry}/velero:v1.17.1-hcdr.1-20260716"
  fi
  if [[ -z "${velero_aws_plugin_image}" ]]; then
    velero_aws_plugin_image="${registry}/velero-plugin-for-aws:v1.13.0"
  fi
  if [[ "${secret_key}" == "dev-secret-change-me" ]] && command -v openssl >/dev/null 2>&1; then
    secret_key="$(openssl rand -hex 32)"
  fi
  if [[ "$public_base_url" != https://* ]]; then
    echo "Docker Compose deployment requires an https:// public base URL" >&2
    exit 2
  fi
  tls_enabled="true"
  if [[ -n "${input_tls_cert_file}" || -n "${input_tls_key_file}" ]]; then
    if [[ -z "${input_tls_cert_file}" || -z "${input_tls_key_file}" ]]; then
      echo "--tls-cert-file and --tls-key-file must be supplied together" >&2
      exit 2
    fi
    [[ -r "${input_tls_cert_file}" ]] || { echo "TLS certificate is not readable: ${input_tls_cert_file}" >&2; exit 1; }
    [[ -r "${input_tls_key_file}" ]] || { echo "TLS private key is not readable: ${input_tls_key_file}" >&2; exit 1; }
  fi

  if [[ "$execute" != "true" ]]; then
    install_header
    cat <<EOF
 Mode             Docker Compose
 Platform URL     ${public_base_url}
 Image registry   ${registry}
 Registry trust   ${registry_trust}
 Release          ${image_tag}
 Data directory   ${data_dir}
 Frontend port    ${http_port}
 API port         ${api_port}

 Planned stages
   1. Validate host and registry
   2. Verify required images
   3. Prepare persistent configuration
   4. Prepare platform TLS
   5. Write runtime settings
   6. Start control plane
   7. Initialize and verify release catalog

 Dry-run only. Add --execute to start installation.
EOF
    return
  fi

    install_header
    printf ' Mode             Docker Compose\n'
    printf ' Platform URL     %s\n' "${public_base_url}"
    printf ' Image registry   %s\n' "${registry}"
    printf ' Release          %s\n' "${image_tag}"
    printf ' Data directory   %s\n' "${data_dir}"

    install_step 1 7 "Validate host and registry"
    run_logged "Frontend port ${http_port} is available" preflight_host_port "${http_port}" hypercdr-platform-frontend
    run_logged "API port ${api_port} is available" preflight_host_port "${api_port}" hypercdr-platform-api
    run_logged "Registry connection is trusted" preflight_registry

    install_step 2 7 "Verify required images"
    for required_image in \
      "${registry}/platform-api:${image_tag}" \
      "${registry}/platform-frontend:${image_tag}" \
      "${registry}/platform-upgrader:${image_tag}" \
      "${registry}/postgres:16"; do
      docker manifest inspect "${required_image}" >/dev/null 2>&1 || {
        install_fail "Required image is unavailable: ${required_image}"
        exit 1
      }
    done
    install_ok "All required images are available"

    install_step 3 7 "Prepare persistent configuration"
    mkdir -p "${data_dir}/certs"
    printf '%s\n' "${release_token}" > "${data_dir}/release-token"
    chmod 600 "${data_dir}/release-token"
    cp "${COMPOSE_TEMPLATE}" "${target_compose_file}"
    if [[ "${registry_trust}" == "private-ca" ]]; then
      cp "${registry_ca_file}" "${installed_registry_ca_file}"
      chmod 644 "${installed_registry_ca_file}"
    fi
    install_ok "Configuration files are prepared"

    install_step 4 7 "Prepare platform TLS"
    if [[ "${tls_enabled}" == "true" ]]; then
      require_command openssl
      mkdir -p "${tls_dir}"
      if [[ -n "${input_tls_cert_file}" ]]; then
        cp "${input_tls_cert_file}" "${tls_cert_file}"
        cp "${input_tls_key_file}" "${tls_key_file}"
      elif [[ ! -f "${tls_cert_file}" || ! -f "${tls_key_file}" ]]; then
        local san_entry="DNS:${public_host}"
        if [[ "${public_host}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
          san_entry="IP:${public_host}"
        fi
        openssl req -x509 -nodes -newkey rsa:4096 -sha256 -days 7300 \
          -subj "/CN=${public_host}" \
          -addext "subjectAltName=${san_entry},DNS:localhost,IP:127.0.0.1" \
          -keyout "${tls_key_file}" \
          -out "${tls_cert_file}" >/dev/null 2>&1
      fi
      chmod 600 "${tls_key_file}"
      chmod 644 "${tls_cert_file}"
    fi
    install_ok "Platform TLS is ready"

    install_step 5 7 "Write runtime settings"
    cat > "${data_dir}/.env" <<EOF
HCDR_PUBLIC_BASE_URL=${public_base_url}
HCDR_AGENT_WS_ENDPOINT=${agent_ws_endpoint}
HCDR_IMAGE_REGISTRY=${registry}
HCDR_REGISTRY_PROFILE=${HCDR_SELECTED_REGISTRY:-custom}
HCDR_IMAGE_TAG=${image_tag}
RELEASE_VERSION=${image_tag}
PLATFORM_API_IMAGE=${registry}/platform-api:${image_tag}
PLATFORM_FRONTEND_IMAGE=${registry}/platform-frontend:${image_tag}
PLATFORM_UPGRADER_IMAGE=${registry}/platform-upgrader:${image_tag}
POSTGRES_IMAGE=${registry}/postgres:16
HCDR_POSTGRES_PASSWORD=${postgres_password}
HCDR_DATABASE_URL=postgres://hypercdr:${postgres_password}@hypercdr-postgres:5432/hypercdr?sslmode=disable
HCDR_AGENT_IMAGE=${registry}/comm-agent:${image_tag}
HCDR_VELERO_IMAGE=${velero_image}
HCDR_VELERO_AWS_PLUGIN_IMAGE=${velero_aws_plugin_image}
HCDR_DATA_DIR=${data_dir}
HCDR_FRONTEND_PORT=${http_port}
HCDR_API_PORT=${api_port}
HCDR_TLS_ENABLED=${tls_enabled}
HCDR_TLS_DIR=${tls_dir}
HCDR_REGISTRY_CA_PATH=$([[ "${registry_trust}" == "private-ca" ]] && echo "/etc/hypercdr/registry/ca.crt" || true)
HCDR_REGISTRY_CA_FILE=$([[ "${registry_trust}" == "private-ca" ]] && echo "${installed_registry_ca_file}" || echo "/dev/null")
HCDR_SECRET_KEY=${secret_key}
HCDR_RELEASE_TOKEN=${release_token}
EOF
    chmod 600 "${data_dir}/.env"
    install_ok "Runtime settings saved"

    install_step 6 7 "Start control plane"
    run_logged "Containers started" bash -c 'cd "$1" && docker compose --project-name hypercdr -f "$2" up -d' _ "${data_dir}" "${target_compose_file}"
    local ready="false"
    local attempt
    for attempt in $(seq 1 60); do
      if curl -kfsS --connect-timeout 2 --max-time 5 "${public_base_url%/}/readyz" >/dev/null 2>&1; then
        ready="true"
        break
      fi
      sleep 2
    done
    if [[ "${ready}" != "true" ]]; then
      install_fail "Control plane did not become ready within 120 seconds"
      echo "          Check: cd ${data_dir} && docker compose ps" >&2
      echo "          Logs:  cd ${data_dir} && docker compose logs --tail=200" >&2
      exit 1
    fi
    install_ok "Control plane is ready"

    install_step 7 7 "Initialize and verify release catalog"
    local release_payload
    release_payload="$(printf '{\"version\":\"%s\",\"databaseSchemaVersion\":\"current\",\"minimumAgentVersion\":\"%s\",\"rollbackSupported\":true,\"releaseNotes\":\"Initialized by the control plane installer\"}' "${image_tag}" "${image_tag}")"
    run_logged "Platform release is registered" curl -kfsS --connect-timeout 5 --max-time 30 \
      -H "Content-Type: application/json" \
      -H "X-HyperCDR-Release-Token: ${release_token}" \
      -d "${release_payload}" \
      "${public_base_url%/}/api/v1/platform/releases"
    run_logged "Cluster component releases are initialized" curl -kfsS --connect-timeout 5 --max-time 30 \
      "${public_base_url%/}/install.sh"
    cat <<EOF

============================================================
 Installation completed successfully
============================================================
 Platform URL      ${public_base_url}
 Username          admin
 Initial password  admin123

 Change the initial password after the first sign-in.
EOF
}

case "$mode" in
  k8s) run_k8s ;;
  docker) run_docker ;;
  *)
    echo "unknown mode: $mode" >&2
    usage
    exit 2
    ;;
esac
