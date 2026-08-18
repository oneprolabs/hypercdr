#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

for generated_path in \
  frontend/node_modules frontend/dist frontend/coverage frontend/playwright-report \
  frontend/test-results .playwright-cli output dist .cache; do
  [[ ! -e "${ROOT_DIR}/${generated_path}" ]] || {
    echo "Generated path must be stored under hypercdr-runtime: ${generated_path}" >&2
    exit 1
  }
done

while IFS= read -r tracked_path; do
  [[ ! -e "${ROOT_DIR}/${tracked_path}" ]] || {
    echo "Generated artifact is tracked by Git: ${tracked_path}" >&2
    exit 1
  }
done < <(git -C "${ROOT_DIR}" ls-files | grep -E '^(\.playwright-cli|output|dist|\.cache)/|^frontend/(node_modules(\.before-[^/]*)?|dist|coverage|test-results|playwright-report)/' || true)

echo "Repository source/runtime boundary verified"
