#!/usr/bin/env bash
set -Eeuo pipefail
"$(dirname "$0")/stop-platform.sh" "$@"
"$(dirname "$0")/start-platform.sh" "$@"
