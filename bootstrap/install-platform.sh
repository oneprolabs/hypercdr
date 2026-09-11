#!/usr/bin/env bash
# Compatibility entry point. Canonical implementation lives in scripts/.
exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/install-platform.sh" "$@"
