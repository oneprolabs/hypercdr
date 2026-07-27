#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${HCDR_CERT_DIR:-/data/hypercdr/certs}"
CERT_FILE="${HCDR_TLS_CERT_FILE:-${OUT_DIR}/platform.crt}"
KEY_FILE="${HCDR_TLS_KEY_FILE:-${OUT_DIR}/platform.key}"
DAYS="${HCDR_TLS_CERT_DAYS:-36500}"
COMMON_NAME="${HCDR_TLS_COMMON_NAME:-hypercdr-platform}"
EXTRA_SANS="${HCDR_TLS_EXTRA_SANS:-}"

mkdir -p "${OUT_DIR}"

san_entries=(
  "DNS:localhost"
  "DNS:${COMMON_NAME}"
  "IP:127.0.0.1"
)

if command -v hostname >/dev/null 2>&1; then
  host_name="$(hostname || true)"
  if [ -n "${host_name}" ]; then
    san_entries+=("DNS:${host_name}")
  fi
  for ip in $(hostname -I 2>/dev/null || true); do
    san_entries+=("IP:${ip}")
  done
fi

if [ -n "${EXTRA_SANS}" ]; then
  IFS=',' read -r -a extras <<< "${EXTRA_SANS}"
  for item in "${extras[@]}"; do
    item="$(echo "${item}" | xargs)"
    if [ -n "${item}" ]; then
      san_entries+=("${item}")
    fi
  done
fi

san_csv="$(IFS=,; echo "${san_entries[*]}")"
tmp_conf="$(mktemp)"
trap 'rm -f "${tmp_conf}"' EXIT

cat > "${tmp_conf}" <<EOF
[req]
distinguished_name = req_distinguished_name
x509_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = ${COMMON_NAME}

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = ${san_csv}
EOF

openssl req -x509 -newkey rsa:4096 -sha256 -nodes \
  -keyout "${KEY_FILE}" \
  -out "${CERT_FILE}" \
  -days "${DAYS}" \
  -config "${tmp_conf}"

chmod 600 "${KEY_FILE}"
chmod 644 "${CERT_FILE}"

echo "certificate: ${CERT_FILE}"
echo "private key:  ${KEY_FILE}"
openssl x509 -in "${CERT_FILE}" -noout -subject -dates -ext subjectAltName
