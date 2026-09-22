#!/usr/bin/env bash
# HyperCDR control plane installation settings (package template).
# Edit this file before running ./install.sh.

# Address that users and cluster agents use to reach the control plane.
# Replace 192.0.2.10 with the actual control-plane host IP or DNS name.
HCDR_BASE_URL="https://192.0.2.10:12443"

# Alibaba Cloud image repository used by the installer.
HCDR_REGISTRY="registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr"

# Released HyperCDR version. It is updated automatically when the package is built.
HCDR_IMAGE_TAG="1.0.23.20260915"

# Persistent data and API listening port on the target host.
# install.sh derives the frontend port from HCDR_BASE_URL; this value is informational.
HCDR_INSTALL_DIR="/var/lib/hypercdr"
HCDR_HTTP_PORT="12443"
HCDR_API_PORT="18080"

# Optional public address used as an Agent fallback. Leave empty when unused.
HCDR_PUBLIC_BASE_URL=""

# Login challenge mode: turnstile (Cloudflare, default) or image (local numeric CAPTCHA).
# Turnstile requires a Cloudflare Site Key and Secret Key; keep the secret out
# of Git and provide it through a protected runtime environment or .env file.
HCDR_AUTH_CHALLENGE_MODE="turnstile"
HCDR_TURNSTILE_SITE_KEY=""
HCDR_TURNSTILE_SECRET_KEY=""
HCDR_TURNSTILE_VERIFY_URL="https://challenges.cloudflare.com/turnstile/v0/siteverify"

# Optional existing TLS certificate and key. Leave both empty to let the
# installer generate a certificate for HCDR_BASE_URL and HCDR_PUBLIC_BASE_URL.
HCDR_TLS_CERT_FILE=""
HCDR_TLS_KEY_FILE=""
