#!/usr/bin/env bash
# HyperCDR control plane installation settings (package template).
# Edit this file before running ./install.sh.

# Address that users and cluster agents use to reach the control plane.
# Use the public HTTPS address served by the outer reverse proxy.
HCDR_BASE_URL="https://hypercdr.example.com"

# Alibaba Cloud image repository used by the installer.
HCDR_REGISTRY="registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr"

# Released HyperCDR version. It is updated automatically when the package is built.
HCDR_IMAGE_TAG="1.0.23.20260915"

# Persistent data directory. HTTPS is terminated by the outer reverse proxy
# (for example Nginx Proxy Manager); HyperCDR services use HTTP on Docker networks.
HCDR_INSTALL_DIR="/var/lib/hypercdr"
HCDR_PROXY_NETWORK="" # Optional; defaults to the installed NPM network or nginx-proxy-manager_default.
HCDR_NPM_UPSTREAM_READY="" # Set true only after HTTPS forwarding to port 12443 is confirmed.

# Optional public address used as an Agent fallback. Leave empty when unused.
HCDR_PUBLIC_BASE_URL=""

# Login challenge mode: turnstile (Cloudflare, default) or image (local numeric CAPTCHA).
# Turnstile requires a Cloudflare Site Key and Secret Key; keep the secret out
# of Git and provide it through a protected runtime environment or .env file.
HCDR_AUTH_CHALLENGE_MODE="turnstile"
HCDR_TURNSTILE_SITE_KEY=""
HCDR_TURNSTILE_SECRET_KEY=""
HCDR_TURNSTILE_VERIFY_URL="https://challenges.cloudflare.com/turnstile/v0/siteverify"
