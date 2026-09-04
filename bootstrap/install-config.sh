#!/usr/bin/env bash
# HyperCDR control plane installation settings.
# Edit this file before running ./install.sh.

# Address that users and cluster agents use to reach the control plane.
# Replace 192.0.2.10 with the actual control-plane host IP or DNS name.
HCDR_PUBLIC_BASE_URL="https://192.0.2.10:3002"

# Alibaba Cloud image repository used by the installer.
HCDR_REGISTRY="crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr"

# Released HyperCDR version. It is updated automatically when the package is built.
HCDR_IMAGE_TAG="v20260714.5"

# Persistent data and listening ports on the target host.
HCDR_DATA_DIR="/var/lib/hypercdr"
HCDR_HTTP_PORT="3002"
HCDR_API_PORT="18080"

# Optional public address used as an Agent fallback. Leave empty when unused.
HCDR_AGENT_PUBLIC_URL=""

# Optional existing TLS certificate and key. Leave both empty to let the
# installer generate a certificate for HCDR_PUBLIC_BASE_URL.
HCDR_TLS_CERT_FILE=""
HCDR_TLS_KEY_FILE=""

