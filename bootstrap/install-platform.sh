#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART_DIR="${SCRIPT_DIR}/charts/hypercdr-platform"
RELEASE_NAME="${RELEASE_NAME:-hypercdr}"

usage() {
  cat <<'USAGE'
HyperCDR control plane bootstrap installer

Usage:
  ./install-platform.sh k8s [options]
  ./install-platform.sh docker [options]

Kubernetes options:
  --kubeconfig PATH            Optional kubeconfig path for kubectl and helm
  --namespace NAME             Namespace for the control plane, default: hypercdr-system
  --public-base-url URL        URL users and agents will use, for example https://NODE_IP:NODE_PORT
  --registry REGISTRY          Harbor project prefix, required. Example: 192.168.8.149:5001/hypercdr
  --image-tag TAG              Platform/agent image tag, default v20260714.5.
  --storage-class NAME         StorageClass for bundled PostgreSQL PVC, default: longhorn
  --database-mode MODE         bundled or external, default: bundled
  --node-port PORT             NodePort for platform Service. Defaults to URL port when present.
  --secret-key VALUE           Platform secret key. A development key is used if omitted.
  --timeout DURATION           Wait timeout, default: 5m
  --execute                    Run helm commands. Without this flag, prints and validates the plan only.

Docker options:
  --public-base-url URL        URL users and agents will use, for example https://HOST_IP:3002
  --data-dir PATH              Persistent data directory, default: /data/hypercdr/deploy
  --registry REGISTRY          Harbor project prefix, required. Example: 192.168.8.149:5001/hypercdr
  --image-tag TAG              Platform/agent image tag, default v20260714.5.
  --velero-image IMAGE         Velero image, default <registry>/velero:v1.17.1-helperfix.
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
storage_class="longhorn"
database_mode="bundled"
node_port=""
secret_key="dev-secret-change-me"
data_dir="/data/hypercdr/deploy"
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
  echo "--registry is required" >&2
  exit 2
fi

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

print_common() {
  cat <<EOF
HyperCDR control plane deployment plan

Release name:         ${RELEASE_NAME}
Public base URL:      ${public_base_url}
Agent WebSocket URL:  ${agent_ws_endpoint}
Image registry:       ${registry}
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
    velero_image="${registry}/velero:v1.17.1-helperfix"
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

  helm "${helm_args[@]}" upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" \
    --namespace "${namespace}" \
    --create-namespace \
    --wait --timeout "${timeout}" \
    --set-string "global.publicBaseURL=${public_base_url}" \
    --set-string "global.agentWebSocketURL=${agent_ws_endpoint}" \
    --set-string "global.imageRegistry=${registry}" \
    --set-string "platform.image.tag=${image_tag}" \
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
  local tls_enabled="false"
  local tls_dir="${data_dir}/tls"
  local tls_cert_file="${tls_dir}/platform.crt"
  local tls_key_file="${tls_dir}/platform.key"
  local source_registry_ca_file="${SCRIPT_DIR}/charts/hypercdr-platform/files/registry-ca.crt"
  local registry_ca_file="${data_dir}/certs/registry-ca.crt"
  local target_compose_file="${data_dir}/docker-compose.yaml"
  local public_host
  public_host="$(extract_host_from_url "$public_base_url")"
  if [[ -z "$http_port" ]]; then
    http_port="$(extract_port_from_url "$public_base_url" || true)"
  fi
  if [[ -z "$http_port" ]]; then
    http_port="3002"
  fi
  if [[ -z "${velero_image}" ]]; then
    velero_image="${registry}/velero:v1.17.1-helperfix"
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

  print_common
  cat <<EOF
Mode:                 Docker Compose
Data directory:       ${data_dir}
HTTPS enabled:        ${tls_enabled}
Host port:            ${http_port}
API port:             ${api_port}
Image tag:            ${image_tag}
Agent image:          ${registry}/comm-agent:${image_tag}
Velero image:         ${velero_image}
Velero AWS plugin:    ${velero_aws_plugin_image}
TLS cert file:        $([[ "${tls_enabled}" == "true" ]] && echo "${tls_cert_file}" || echo "(disabled)")
Registry CA file:     ${registry_ca_file}

Planned command:
mkdir -p ${data_dir}/certs
cp ${SCRIPT_DIR}/compose.yaml ${target_compose_file}
cp ${source_registry_ca_file} ${registry_ca_file}
cat > ${data_dir}/.env <<ENV
HCDR_PUBLIC_BASE_URL=${public_base_url}
HCDR_AGENT_WS_ENDPOINT=${agent_ws_endpoint}
HCDR_IMAGE_REGISTRY=${registry}
HCDR_IMAGE_TAG=${image_tag}
HCDR_AGENT_IMAGE=${registry}/comm-agent:${image_tag}
HCDR_VELERO_IMAGE=${velero_image}
HCDR_VELERO_AWS_PLUGIN_IMAGE=${velero_aws_plugin_image}
HCDR_DATA_DIR=${data_dir}
HCDR_FRONTEND_PORT=${http_port}
HCDR_API_PORT=${api_port}
HCDR_TLS_ENABLED=${tls_enabled}
HCDR_TLS_DIR=${tls_dir}
HCDR_REGISTRY_CA_FILE=${registry_ca_file}
HCDR_SECRET_KEY=<redacted>
ENV
cd ${data_dir}
docker compose -f docker-compose.yaml up -d
EOF
  if [[ "$execute" == "true" ]]; then
    mkdir -p "${data_dir}/certs"
    cp "${SCRIPT_DIR}/compose.yaml" "${target_compose_file}"
    cp "${source_registry_ca_file}" "${registry_ca_file}"
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
    cat > "${data_dir}/.env" <<EOF
HCDR_PUBLIC_BASE_URL=${public_base_url}
HCDR_AGENT_WS_ENDPOINT=${agent_ws_endpoint}
HCDR_IMAGE_REGISTRY=${registry}
HCDR_IMAGE_TAG=${image_tag}
HCDR_AGENT_IMAGE=${registry}/comm-agent:${image_tag}
HCDR_VELERO_IMAGE=${velero_image}
HCDR_VELERO_AWS_PLUGIN_IMAGE=${velero_aws_plugin_image}
HCDR_DATA_DIR=${data_dir}
HCDR_FRONTEND_PORT=${http_port}
HCDR_API_PORT=${api_port}
HCDR_TLS_ENABLED=${tls_enabled}
HCDR_TLS_DIR=${tls_dir}
HCDR_REGISTRY_CA_FILE=${registry_ca_file}
HCDR_SECRET_KEY=${secret_key}
EOF
    (cd "${data_dir}" && docker compose -f "${target_compose_file}" up -d)
    cat <<EOF

HyperCDR control plane installed.

Access URL:
  ${public_base_url}

Default administrator:
  Username: admin
  Password: admin123
EOF
  fi
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
