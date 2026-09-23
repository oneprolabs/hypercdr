#!/usr/bin/env bash
set -euo pipefail

workflow=.github/workflows/release.yml
root=$(pwd)
review_workflow=.github/workflows/pr_agent.yml
installer=scripts/release/install-blue-green.sh
grep -Fq "needs: publish" "$workflow"
grep -Fq 'prepare-release:' "$workflow"
grep -Fq 'build-images:' "$workflow"
grep -Fq 'test-core:' "$workflow"
grep -Fq 'needs: prepare-release' "$workflow"
grep -Fq 'needs: [prepare-release, test-core]' "$workflow"
grep -Fq "go-version: '1.25.13'" "$workflow"
for image in platform-api platform-frontend comm-agent oadp-comm-agent cluster-registration-executor; do
  grep -Fq "image_name: ${image}" "$workflow"
done
grep -Fq 'image_name: dependencies' "$workflow"
grep -Fq 'docker/build-push-action@v5' "$workflow"
grep -Fq 'provenance: false' "$workflow"
grep -Fq 'cache-to: type=gha,mode=max,scope=${{ matrix.image_name }}' "$workflow"
! grep -Fq 'publish-dependencies:' "$workflow"
grep -Fq 'needs: [prepare-release, build-images]' "$workflow"
grep -Fq 'HCDR_RELEASE_PHASE: dependencies' "$workflow"
grep -Fq 'HCDR_RELEASE_PHASE: finalize' "$workflow"
grep -Fq "HCDR_OADP_PARALLELISM: '3'" "$workflow"
grep -Fq "HCDR_RELEASE_PULL_PARALLELISM: '4'" "$workflow"
grep -Fq 'GITHUB_TOKEN: ${{ github.token }}' "$workflow"
grep -Fq 'HCDR_AUTH_CHALLENGE_MODE: ${{ vars.HCDR_AUTH_CHALLENGE_MODE || '\''turnstile'\'' }}' "$workflow"
grep -Fq 'HCDR_TURNSTILE_SITE_KEY: ${{ secrets.HCDR_TURNSTILE_SITE_KEY }}' "$workflow"
grep -Fq 'HCDR_TURNSTILE_SECRET_KEY: ${{ secrets.HCDR_TURNSTILE_SECRET_KEY }}' "$workflow"
grep -Fq 'envs: HCDR_AUTH_CHALLENGE_MODE,HCDR_TURNSTILE_SITE_KEY,HCDR_TURNSTILE_SECRET_KEY' "$workflow"
grep -Fq 'sync_auth_challenge_env' "$root/scripts/release/deploy-blue-green.sh"
grep -Fq 'actions/upload-artifact@v4' "$workflow"
grep -Fq 'actions/download-artifact@v4' "$workflow"
grep -Fq 'resolved-image-lock.json' "$workflow"
grep -Fq "vars.HCDR_AUTO_DEPLOY == 'true'" "$workflow"
grep -Fq 'secrets.SSH_PRIVATE_KEY' "$workflow"
grep -Fq "secrets.ALIYUN_REGISTRY_USERNAME" "$workflow"
grep -Fq "secrets.ALIYUN_REGISTRY_PASSWORD" "$workflow"
grep -Fq 'vars.HCDR_SSH_HOST' "$workflow"
grep -Fq 'vars.HCDR_DEPLOY_PATH' "$workflow"
grep -Fq 'deploy-blue-green.sh' "$workflow"
grep -Fq 'command_timeout: 3600s' "$workflow"
grep -Fq 'release-all.sh --config "${RUNNER_TEMP}/release.conf"' "$workflow"
grep -Fq 'HCDR_RELEASE_SECRETS_FILE=${RUNNER_TEMP}/release.secrets.conf' "$workflow"
grep -Fq 'RELEASE_REGISTRY_USERNAME' "$workflow"
grep -Fq 'export HCDR_REGISTRY_CONFIG="${GITHUB_WORKSPACE}/config/registries.conf"' "$workflow"
grep -Fq 'export HCDR_REGISTRY_PROFILE="${{ needs.prepare-release.outputs.registry_profile }}"' "$workflow"
grep -Fq 'ref="${GITHUB_SHA}"' "$workflow"
grep -Fq 'ref="${GITHUB_REF_NAME}"' "$workflow"
grep -Fq "grep -q '^PLATFORM_API_BLUE_IMAGE='" "$workflow"
grep -Fq "grep -q '^PLATFORM_API_GREEN_IMAGE='" "$workflow"
grep -Fq 'legacy_install=false' "$workflow"
grep -Fq 'legacy_install=true' "$workflow"
grep -Fq 'install-blue-green.sh' "$workflow"
grep -Fq -- "--base-url 'https://\${{ vars.HCDR_DOMAIN }}'" "$workflow"
! grep -Fq 'docker rm -f' "$workflow"
! grep -Fq 'release-all.sh "${{ steps.release.outputs.version }}"' "$workflow"
grep -Fq 'prepare_legacy_migration' "$installer"
grep -Fq 'rollback_legacy_migration' "$installer"
grep -Fq '.migration_complete' "$installer"
grep -Fq 'scripts/ci/notify-pr-review.sh' "$review_workflow"
grep -Fq 'PR_REVIEWER.EXTRA_INSTRUCTIONS' "$review_workflow"
grep -Fq 'Requirement Coverage' scripts/ci/notify-pr-review.sh
grep -Fq 'Tests and Verification' scripts/ci/notify-pr-review.sh
grep -Fq 'mkdir -p "$(dirname "${RELEASE_MANIFEST}")"' scripts/release/release-all.sh
grep -Fq 'docker build --platform linux/amd64 -f "${ROOT_DIR}/backend/Dockerfile"' scripts/release/build-release.sh
grep -Fq 'docker build --platform linux/amd64 -f "${ROOT_DIR}/frontend/Dockerfile"' scripts/release/build-release.sh
grep -Fq 'docker build --platform linux/amd64 -f "${ROOT_DIR}/agent/comm-agent/Dockerfile"' scripts/release/build-release.sh
grep -Fq 'docker build --platform linux/amd64 -f "${ROOT_DIR}/backend/cluster-registration-executor.Dockerfile"' scripts/release/build-release.sh
grep -Fq '"${SCRIPT_DIR}/push-release.sh"' scripts/release/build-release.sh
grep -Fq 'FROM golang:1.25.13-bookworm AS builder' backend/Dockerfile
grep -Fq 'FROM debian:bookworm-slim' backend/Dockerfile
grep -Fq 'FROM node:22-alpine AS builder' frontend/Dockerfile
grep -Fq 'FROM nginx:1.27-alpine' frontend/Dockerfile

echo "release workflow contract passed"
