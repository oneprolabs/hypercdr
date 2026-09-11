#!/usr/bin/env bash
exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/uninstall-platform.sh" "$@"
