#!/usr/bin/env bash
set -euo pipefail

workflow=.github/workflows/release.yml
installer=scripts/release/install-blue-green.sh
grep -Fq "needs: publish" "$workflow"
grep -Fq "vars.HCDR_AUTO_DEPLOY == 'true'" "$workflow"
grep -Fq 'secrets.SSH_PRIVATE_KEY' "$workflow"
grep -Fq "secrets.ALIYUN_REGISTRY_USERNAME" "$workflow"
grep -Fq "secrets.ALIYUN_REGISTRY_PASSWORD" "$workflow"
grep -Fq 'vars.HCDR_SSH_HOST' "$workflow"
grep -Fq 'vars.HCDR_DEPLOY_PATH' "$workflow"
grep -Fq 'deploy-blue-green.sh' "$workflow"
grep -Fq 'command_timeout: 3600s' "$workflow"
grep -Fq 'release-all.sh --config "${RUNNER_TEMP}/release.conf"' "$workflow"
grep -Fq 'export HCDR_REGISTRY_CONFIG="${GITHUB_WORKSPACE}/config/registries.conf"' "$workflow"
grep -Fq 'export HCDR_REGISTRY_PROFILE="${{ steps.release.outputs.profile }}"' "$workflow"
grep -Fq 'ref="${GITHUB_SHA}"' "$workflow"
grep -Fq 'ref="${GITHUB_REF_NAME}"' "$workflow"
grep -Fq "grep -q '^PLATFORM_API_BLUE_IMAGE='" "$workflow"
grep -Fq "grep -q '^PLATFORM_API_GREEN_IMAGE='" "$workflow"
grep -Fq 'legacy_install=false' "$workflow"
grep -Fq 'legacy_install=true' "$workflow"
grep -Fq 'install-blue-green.sh' "$workflow"
! grep -Fq 'docker rm -f' "$workflow"
! grep -Fq 'release-all.sh "${{ steps.release.outputs.version }}"' "$workflow"
grep -Fq 'prepare_legacy_migration' "$installer"
grep -Fq 'rollback_legacy_migration' "$installer"
grep -Fq '.migration_complete' "$installer"

echo "release workflow contract passed"
