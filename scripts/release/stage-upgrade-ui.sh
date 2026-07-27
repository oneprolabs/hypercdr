#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
DEPLOY_DIR="${HCDR_DEPLOY_DIR:-/var/lib/hypercdr}"

if [[ -z "${VERSION}" || ! "${VERSION}" =~ ^v[0-9]{8}\.[0-9]+$ ]]; then
  echo "Usage: stage-upgrade-ui.sh <version>" >&2
  exit 2
fi

ENV_FILE="${DEPLOY_DIR}/.env"
COMPOSE_FILE="${DEPLOY_DIR}/docker-compose.yaml"
[[ -r "${ENV_FILE}" ]] || { echo "Missing ${ENV_FILE}" >&2; exit 1; }
[[ -r "${COMPOSE_FILE}" ]] || { echo "Missing ${COMPOSE_FILE}" >&2; exit 1; }

REGISTRY="$(awk -F= '$1 == "HCDR_IMAGE_REGISTRY" { print substr($0, index($0, "=") + 1); exit }' "${ENV_FILE}")"
[[ -n "${REGISTRY}" ]] || { echo "HCDR_IMAGE_REGISTRY is missing" >&2; exit 1; }
FRONTEND_IMAGE="${REGISTRY%/}/platform-frontend:${VERSION}"
UPGRADER_IMAGE="${REGISTRY%/}/platform-upgrader:${VERSION}"

echo "Staging upgrade UI ${FRONTEND_IMAGE}"
echo "Staging upgrade executor ${UPGRADER_IMAGE}"
echo "The API version is not changed."
docker pull "${FRONTEND_IMAGE}"
docker pull "${UPGRADER_IMAGE}"
PLATFORM_FRONTEND_IMAGE="${FRONTEND_IMAGE}" PLATFORM_UPGRADER_IMAGE="${UPGRADER_IMAGE}" docker compose \
  --project-name hypercdr \
  --env-file "${ENV_FILE}" \
  -f "${COMPOSE_FILE}" \
  up -d --no-deps hypercdr-platform-frontend hypercdr-platform-upgrader

RUNNING_FRONTEND="$(docker inspect -f '{{.Config.Image}}' hypercdr-platform-frontend)"
RUNNING_UPGRADER="$(docker inspect -f '{{.Config.Image}}' hypercdr-platform-upgrader)"
[[ "${RUNNING_FRONTEND}" == "${FRONTEND_IMAGE}" ]] || { echo "Unexpected frontend image: ${RUNNING_FRONTEND}" >&2; exit 1; }
[[ "${RUNNING_UPGRADER}" == "${UPGRADER_IMAGE}" ]] || { echo "Unexpected upgrader image: ${RUNNING_UPGRADER}" >&2; exit 1; }
echo "SUCCESS: upgrade UI and executor staged for ${VERSION}"
