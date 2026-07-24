#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REGISTRY="${HCDR_IMAGE_REGISTRY:-}"
CA_FILE="${HCDR_REGISTRY_CA_FILE:-}"
REGISTRY_TRUST="${HCDR_REGISTRY_TRUST:-system}"
RESTART_DOCKER="false"

usage() {
  cat <<'USAGE'
Prepare Docker to access the selected image registry.

Usage:
  ./prepare-docker-registry.sh --registry HOST:PORT/hypercdr

Options:
  --registry PREFIX   Harbor project prefix, for example 192.168.8.149:5001/hypercdr.
  --registry-trust MODE
                      system (default) or private-ca.
  --ca-file PATH      PEM CA certificate. Required with --registry-trust private-ca.
  --restart-docker    Restart Docker after installing the CA. This can interrupt
                      all containers on this host, including Harbor.
  -h, --help          Show help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) REGISTRY="${2:?missing value for --registry}"; shift 2 ;;
    --registry-trust) REGISTRY_TRUST="${2:?missing value for --registry-trust}"; shift 2 ;;
    --ca-file) CA_FILE="${2:?missing value for --ca-file}"; shift 2 ;;
    --restart-docker) RESTART_DOCKER="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "${REGISTRY}" ]]; then
  echo "--registry is required" >&2
  exit 2
fi
case "${REGISTRY_TRUST}" in
  system) [[ -z "${CA_FILE}" ]] || { echo "--ca-file requires --registry-trust private-ca" >&2; exit 2; } ;;
  private-ca)
    [[ -n "${CA_FILE}" ]] || { echo "--ca-file is required with --registry-trust private-ca" >&2; exit 2; }
    [[ -f "${CA_FILE}" ]] || { echo "CA file not found: ${CA_FILE}" >&2; exit 1; }
    if command -v openssl >/dev/null 2>&1; then
      openssl x509 -in "${CA_FILE}" -noout >/dev/null 2>&1 || { echo "CA file is not a valid PEM certificate: ${CA_FILE}" >&2; exit 1; }
    fi
    ;;
  *) echo "--registry-trust must be system or private-ca" >&2; exit 2 ;;
esac
if ! command -v docker >/dev/null 2>&1; then
  echo "missing required command: docker" >&2
  exit 1
fi

REGISTRY="${REGISTRY#http://}"
REGISTRY="${REGISTRY#https://}"
REGISTRY_HOST="${REGISTRY%%/*}"
DOCKER_CERT_DIR="/etc/docker/certs.d/${REGISTRY_HOST}"

echo "Prepare Docker registry trust"
echo "Registry host: ${REGISTRY_HOST}"
echo "Trust mode:    ${REGISTRY_TRUST}"
echo "CA file:       ${CA_FILE:-"(system trust store)"}"
echo "Docker dir:    ${DOCKER_CERT_DIR}"

if [[ "${REGISTRY_TRUST}" == "system" ]]; then
  echo "No private CA installation is required. Docker will use the system trust store."
  echo "Run check-harbor.sh to verify registry access."
  exit 0
fi

mkdir -p "${DOCKER_CERT_DIR}"
CA_CHANGED="true"
if [[ -f "${DOCKER_CERT_DIR}/ca.crt" ]] && cmp -s "${CA_FILE}" "${DOCKER_CERT_DIR}/ca.crt"; then
  CA_CHANGED="false"
  echo "Docker CA is already up to date."
else
  cp "${CA_FILE}" "${DOCKER_CERT_DIR}/ca.crt"
  chmod 644 "${DOCKER_CERT_DIR}/ca.crt"
  echo "Docker CA installed or updated."
fi

if [[ "${RESTART_DOCKER}" == "true" ]]; then
  echo "Restarting Docker to load the registry CA..."
  if command -v systemctl >/dev/null 2>&1; then
    systemctl restart docker
  else
    service docker restart
  fi
  if [[ -f /opt/harbor/docker-compose.yml ]]; then
    echo "Harbor compose file detected; starting Harbor after Docker restart..."
    (cd /opt/harbor && docker compose up -d)
  fi
elif [[ "${CA_CHANGED}" == "true" ]]; then
  cat <<EOF

Docker was not restarted.

Next step:
  Run check-harbor.sh. If Docker image pull fails with a certificate error,
  restart Docker during a maintenance window, then run check-harbor.sh again.

Manual restart command:
  systemctl restart docker
EOF
fi

echo
echo "SUCCESS: Docker registry CA is prepared for ${REGISTRY_HOST}."
echo "Run check-harbor.sh before installing HyperCDR."
