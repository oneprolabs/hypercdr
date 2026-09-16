#!/usr/bin/env bash
set -euo pipefail

workflow=.github/workflows/release.yml
grep -Fq "needs: publish" "$workflow"
grep -Fq "vars.HCDR_AUTO_DEPLOY == 'true'" "$workflow"
grep -Fq 'secrets.SSH_PRIVATE_KEY' "$workflow"
grep -Fq 'vars.HCDR_SSH_HOST' "$workflow"
grep -Fq 'vars.HCDR_DEPLOY_PATH' "$workflow"
grep -Fq 'deploy-blue-green.sh' "$workflow"
grep -Fq 'command_timeout: 3600s' "$workflow"

echo "release workflow contract passed"
