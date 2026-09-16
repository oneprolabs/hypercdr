# HyperCDR Blue/Green Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add health-gated blue/green production deployment for HyperCDR, publishing immutable images to the existing Alibaba ACR and deploying through GitHub Actions without changing the development stack.

**Architecture:** One stable Nginx edge and one PostgreSQL service are shared by two profiled API/frontend colors. A host-side Bash script owns the single deployment lock, candidate startup, health gates, atomic Nginx reload, persisted active color, observation rollback, and old-color drain. The existing release workflow remains the only image publisher and gains a gated deploy job; the in-product upgrader is excluded from this production topology to avoid two deployment controllers.

**Tech Stack:** Docker Compose V2, Bash, Nginx, GitHub Actions, Alibaba Cloud ACR, Go API health endpoints, PostgreSQL 16.

**Spec:** `docs/superpowers/specs/2026-09-16-blue-green-deployment-design.md`

## Global Constraints

- Keep `docker-compose.dev.yml` unchanged.
- Runtime state and generated files belong under `/var/lib/hypercdr` or `../hypercdr-runtime`, never inside the repository.
- Use the existing `aliyun_acr` profile and immutable version tags.
- Only the stable edge publishes production host ports 80 and 443.
- PostgreSQL and `cluster-registration-executor` remain singleton services.
- Do not run `platform-upgrader` in the first blue/green production topology.
- Database migrations must remain expand/contract compatible while both colors overlap.
- A pre-switch failure must leave the active upstream and `.active_color` unchanged.
- A post-switch public health failure must restore the previous upstream and color marker.
- Run `make verify` before completion.

---

## File Structure

- Modify `docker-compose.yml`: define shared services plus blue and green profiles.
- Keep `docker/platform-frontend.Dockerfile` unchanged so existing ACR frontend images remain compatible.
- Create `docker/nginx/edge.conf`: stable public TLS proxy configuration.
- Create `docker/nginx/upstream.conf.default`: initial blue API/frontend upstreams.
- Create `scripts/release/deploy-blue-green.sh`: deployment and rollback state machine.
- Create `scripts/release/install-blue-green.sh`: initialize a production runtime without changing the legacy installer.
- Modify `scripts/release/start-platform.sh`: start shared services and the persisted active color after reboot.
- Modify `scripts/release/stop-platform.sh`: stop both profiles plus shared services while preserving data.
- Modify `scripts/release/templates/hypercdr.service`: keep systemd lifecycle pointed at the blue/green-aware helpers.
- Modify `.github/workflows/release.yml`: retain publication and add gated SSH deployment.
- Create `scripts/tests/blue-green-deploy.sh`: state-machine and failure-path tests using fake commands.
- Create `scripts/tests/blue-green-compose.sh`: static Compose/Nginx contract checks.
- Create `scripts/tests/release-workflow.sh`: workflow variable, secret, trigger, and deploy-gate checks.
- Modify `Makefile` and `.github/workflows/pr-check.yml`: run the new deployment tests.
- Create `docs/deployment/blue-green-deployment.zh.md`: operator setup, first install, verification, rollback, and enablement gates.

---

### Task 1: Define the Blue/Green State Machine with Failing Tests

**Files:**
- Create: `scripts/tests/blue-green-deploy.sh`
- Create: `scripts/release/deploy-blue-green.sh`

**Interfaces:**
- Produces: `other_color COLOR`, `read_active_color`, `select_deploy_color CURRENT IS_RUNNING`, `set_env_value FILE KEY VALUE`, `render_upstream COLOR OUTPUT`, `wait_for_service SERVICE`, `switch_traffic FROM TO`, `deploy_version VERSION`, and `rollback_color`.
- Consumes: `HCDR_INSTALL_DIR`, `HCDR_COMPOSE_FILE`, `HCDR_HEALTH_RETRIES`, `HCDR_HEALTH_INTERVAL`, `HCDR_POST_SWITCH_OBSERVE_SECONDS`, `HCDR_OLD_COLOR_DRAIN_SECONDS`, and the installed `.env`.

- [ ] **Step 1: Write the function-contract tests**

Create `scripts/tests/blue-green-deploy.sh` with a temporary runtime directory and source the production script. The first assertions must be:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_DIR="$(mktemp -d)"
trap 'rm -rf "${RUNTIME_DIR}"' EXIT
export HCDR_INSTALL_DIR="${RUNTIME_DIR}"
export HCDR_COMPOSE_FILE="${RUNTIME_DIR}/docker-compose.yaml"

# shellcheck source=../release/deploy-blue-green.sh
source "${ROOT_DIR}/scripts/release/deploy-blue-green.sh"

[[ "$(other_color blue)" == green ]]
[[ "$(other_color green)" == blue ]]
if other_color red >/dev/null 2>&1; then
  echo "invalid color was accepted" >&2
  exit 1
fi

[[ "$(select_deploy_color blue false)" == blue ]]
[[ "$(select_deploy_color blue true)" == green ]]

printf 'PLATFORM_API_BLUE_IMAGE=old\n' > "${RUNTIME_DIR}/.env"
set_env_value "${RUNTIME_DIR}/.env" PLATFORM_API_BLUE_IMAGE registry/platform-api:new
grep -Fxq 'PLATFORM_API_BLUE_IMAGE=registry/platform-api:new' "${RUNTIME_DIR}/.env"
set_env_value "${RUNTIME_DIR}/.env" PLATFORM_FRONTEND_BLUE_IMAGE registry/platform-frontend:new
grep -Fxq 'PLATFORM_FRONTEND_BLUE_IMAGE=registry/platform-frontend:new' "${RUNTIME_DIR}/.env"

render_upstream green "${RUNTIME_DIR}/upstream.conf"
grep -Fq 'hypercdr-platform-api-green:18080' "${RUNTIME_DIR}/upstream.conf"
grep -Fq 'hypercdr-platform-frontend-green:3002' "${RUNTIME_DIR}/upstream.conf"
```

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
bash scripts/tests/blue-green-deploy.sh
```

Expected: failure because `scripts/release/deploy-blue-green.sh` does not exist.

- [ ] **Step 3: Implement only the tested pure helpers**

Create `scripts/release/deploy-blue-green.sh` with `set -Eeuo pipefail`, no work while sourced, and these behaviors:

```bash
other_color() {
  case "${1:-}" in
    blue) echo green ;;
    green) echo blue ;;
    *) echo "invalid color: ${1:-}" >&2; return 2 ;;
  esac
}

select_deploy_color() {
  local current="$1" running="$2"
  if [[ "$running" == true ]]; then other_color "$current"; else echo "$current"; fi
}

set_env_value() {
  local file="$1" key="$2" value="$3" tmp="${file}.tmp"
  awk -F= -v key="$key" -v value="$value" '
    BEGIN { found=0 }
    $1 == key { print key "=" value; found=1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
  chmod 600 "$file"
}

render_upstream() {
  local color="$1" output="$2"
  case "$color" in blue|green) ;; *) return 2 ;; esac
  cat > "${output}.tmp" <<EOF
upstream hypercdr_api { server hypercdr-platform-api-${color}:18080; }
upstream hypercdr_frontend { server hypercdr-platform-frontend-${color}:3002; }
EOF
  mv "${output}.tmp" "$output"
}
```

Add a `main()` guard:

```bash
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
```

- [ ] **Step 4: Run the helper test and verify GREEN**

Run `bash scripts/tests/blue-green-deploy.sh`.

Expected: exit 0.

- [ ] **Step 5: Add failing command-flow tests**

Extend the test with a `fake-bin/docker` executable that records arguments and implements:

```bash
case "$*" in
  *"inspect -f {{.State.Running}} hypercdr-platform-api-blue"*) echo false ;;
  *"inspect -f {{.State.Health.Status}}"*) echo healthy ;;
  *"exec hypercdr-edge nginx -t"*) exit "${FAKE_NGINX_TEST_EXIT:-0}" ;;
  *"exec hypercdr-edge nginx -s reload"*) exit "${FAKE_NGINX_RELOAD_EXIT:-0}" ;;
  *) exit 0 ;;
esac
```

Add a `fake-bin/curl` that exits with `FAKE_CURL_EXIT`, prepend `fake-bin` to
`PATH`, create a minimal `.env`, and assert these flows separately:

```text
first install: blue selected and .active_color becomes blue
normal deploy: active blue selects green
candidate health failure: upstream and .active_color remain blue
successful switch: upstream and .active_color become green
post-switch curl failure: upstream and .active_color return to blue
second process cannot acquire the deployment lock
```

- [ ] **Step 6: Run the command-flow tests and verify RED**

Run `bash scripts/tests/blue-green-deploy.sh`.

Expected: failure at the first missing deployment-flow function.

- [ ] **Step 7: Implement the minimal deployment flow**

Implement the following exact CLI:

```text
deploy-blue-green.sh VERSION
deploy-blue-green.sh --rollback
deploy-blue-green.sh --start-current
```

`VERSION` must pass the existing `require_version` rule after an optional
leading `v` is removed. Use host `flock -n` on
`${HCDR_INSTALL_DIR}/.deploy.lock`. `deploy_version` must:

```text
load .env
start hypercdr-postgres
determine first install from the active API container state
update only the candidate color image variables
pull and start only candidate API/frontend services with their profile
wait for Docker health on both candidate services
switch Nginx only after both are healthy
write .active_color only after a successful Nginx reload
observe https://${HCDR_DOMAIN}/readyz via --resolve to 127.0.0.1
roll back upstream and marker on observation failure
update and restart the singleton registration executor after observation
sleep for the configured drain period
stop the old API/frontend services without removing them
```

Use defaults suitable for production but short overrideable values for tests:

```bash
HCDR_HEALTH_RETRIES="${HCDR_HEALTH_RETRIES:-60}"
HCDR_HEALTH_INTERVAL="${HCDR_HEALTH_INTERVAL:-2}"
HCDR_POST_SWITCH_OBSERVE_SECONDS="${HCDR_POST_SWITCH_OBSERVE_SECONDS:-30}"
HCDR_OLD_COLOR_DRAIN_SECONDS="${HCDR_OLD_COLOR_DRAIN_SECONDS:-180}"
```

- [ ] **Step 8: Run deployment tests and syntax checks**

Run:

```bash
bash scripts/tests/blue-green-deploy.sh
bash -n scripts/release/deploy-blue-green.sh scripts/tests/blue-green-deploy.sh
```

Expected: both commands exit 0.

- [ ] **Step 9: Commit Task 1**

```bash
git add scripts/release/deploy-blue-green.sh scripts/tests/blue-green-deploy.sh
git commit -m "feat: add health gated blue green deployer"
```

---

### Task 2: Add the Production Compose and Nginx Topology

**Files:**
- Modify: `docker-compose.yml`
- Create: `docker/nginx/edge.conf`
- Create: `docker/nginx/upstream.conf.default`
- Create: `scripts/tests/blue-green-compose.sh`

**Interfaces:**
- Consumes: per-color image variables written by Task 1 and existing application secrets.
- Produces: Compose services named exactly as the Task 1 deployer invokes and Nginx upstream names `hypercdr_api` and `hypercdr_frontend`.

- [ ] **Step 1: Write the failing static topology test**

Create `scripts/tests/blue-green-compose.sh` that builds a temporary env file
containing non-secret dummy values, runs:

```bash
docker compose --env-file "$env_file" -f docker-compose.yml config > "$rendered"
```

Assert all of the following:

```bash
for service in \
  hypercdr-edge hypercdr-postgres \
  hypercdr-platform-api-blue hypercdr-platform-frontend-blue \
  hypercdr-platform-api-green hypercdr-platform-frontend-green \
  hypercdr-cluster-registration-executor; do
  grep -Fq "  ${service}:" "$rendered"
done

! grep -Fq 'hypercdr-platform-upgrader:' "$rendered"
grep -Fq 'profiles:' "$rendered"
grep -Fq '80:80' "$rendered"
grep -Fq '443:443' "$rendered"
[[ "$(grep -Ec 'published: (18080|3002|5432)' "$rendered" || true)" == 0 ]]
grep -Fq 'hypercdr-platform-api-blue:18080' docker/nginx/upstream.conf.default
grep -Fq 'proxy_pass http://hypercdr_api' docker/nginx/edge.conf
grep -Fq 'proxy_pass http://hypercdr_frontend' docker/nginx/edge.conf
```

- [ ] **Step 2: Run the topology test and verify RED**

Run `bash scripts/tests/blue-green-compose.sh`.

Expected: failure because the colored services and edge files do not exist.

- [ ] **Step 3: Implement the colored Compose services**

Replace only the production `docker-compose.yml` topology. Preserve the
existing environment variables and mounts, but split API/frontend image keys:

```text
PLATFORM_API_BLUE_IMAGE
PLATFORM_FRONTEND_BLUE_IMAGE
PLATFORM_API_GREEN_IMAGE
PLATFORM_FRONTEND_GREEN_IMAGE
```

Give the four application services matching `profiles: [blue]` or
`profiles: [green]`. Add Docker health checks:

```yaml
healthcheck:
  test: ["CMD", "curl", "-fsS", "http://127.0.0.1:18080/readyz"]
  interval: 5s
  timeout: 3s
  retries: 24
```

For each colored frontend, mount the existing platform certificate and key at
`/etc/hypercdr/tls/platform.crt` and `/etc/hypercdr/tls/platform.key` so the
published frontend images remain compatible. Its health check is:

```yaml
healthcheck:
  test: ["CMD", "wget", "--no-check-certificate", "-q", "-O", "/dev/null", "https://127.0.0.1:3002/"]
```

The edge publishes 80/443, mounts `${HCDR_TLS_CERT_FILE}` and
`${HCDR_TLS_KEY_FILE}` read-only at fixed container paths, and mounts the
installed Nginx configuration directory read-only.

- [ ] **Step 4: Implement stable and internal Nginx configurations**

Keep `docker/nginx/default.conf` unchanged for legacy installer compatibility.
The blue/green frontend containers use the existing internal HTTPS listener and
proxy behavior; the edge proxy disables certificate verification only on the
private Compose network.

Add `edge.conf` with:

```nginx
include /etc/nginx/conf.d/upstream.conf;

server {
    listen 80;
    server_name hypercdr.com;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    server_name hypercdr.com;
    ssl_certificate /etc/hypercdr/tls/tls.crt;
    ssl_certificate_key /etc/hypercdr/tls/tls.key;

    location / { proxy_pass http://hypercdr_frontend; }
    location ~ ^/(api/|ws/|healthz$|readyz$|install\.sh$|uninstall-agent\.sh$|prepare-node\.sh$|assets/registry/|assets/velero/|assets/platform/ca\.crt$) {
        proxy_pass http://hypercdr_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Define the Nginx `map` for `$connection_upgrade` in the top-level HTTP include
file used by the edge. `upstream.conf.default` points both upstreams to blue.

- [ ] **Step 5: Run topology and syntax tests**

Run:

```bash
bash scripts/tests/blue-green-compose.sh
docker run --rm \
  -v "$PWD/docker/nginx:/source:ro" \
  nginx:1.27-alpine nginx -t -c /source/edge-test.conf
```

The test fixture must provide resolvable dummy upstreams without weakening the
production configuration. Expected: exit 0.

- [ ] **Step 6: Commit Task 2**

```bash
git add docker-compose.yml docker/nginx scripts/tests/blue-green-compose.sh
git commit -m "feat: define blue green production topology"
```

---

### Task 3: Integrate First Install and Host Lifecycle

**Files:**
- Create: `scripts/release/install-blue-green.sh`
- Modify: `scripts/release/start-platform.sh`
- Modify: `scripts/release/stop-platform.sh`
- Modify: `scripts/release/restart-platform.sh`
- Modify: `scripts/release/templates/hypercdr.service`
- Modify: `scripts/tests/blue-green-deploy.sh`

**Interfaces:**
- Consumes: Task 1 deployer and Task 2 Compose/Nginx files.
- Produces: `/var/lib/hypercdr` with secrets, color image variables, edge assets, lifecycle helpers, and boot recovery.

- [ ] **Step 1: Add failing installer rendering tests**

Extend `scripts/tests/blue-green-deploy.sh` to invoke the new installer in dry
run mode and a temporary runtime directory. Assert the rendered env
contains:

```text
HCDR_DOMAIN=hypercdr.com
PLATFORM_API_BLUE_IMAGE=<registry>/platform-api:<version>
PLATFORM_FRONTEND_BLUE_IMAGE=<registry>/platform-frontend:<version>
PLATFORM_API_GREEN_IMAGE=<registry>/platform-api:<version>
PLATFORM_FRONTEND_GREEN_IMAGE=<registry>/platform-frontend:<version>
REGISTRATION_EXECUTOR_IMAGE=<registry>/cluster-registration-executor:<version>
```

Assert installation copies:

```text
docker-compose.yaml
nginx/conf.d/default.conf
nginx/conf.d/upstream.conf
deploy-blue-green.sh
start-platform.sh
stop-platform.sh
restart-platform.sh
```

- [ ] **Step 2: Run the installer test and verify RED**

Run `bash scripts/tests/blue-green-deploy.sh`.

Expected: failure on the first missing blue/green variable or asset.

- [ ] **Step 3: Implement the blue/green installer**

Implement `scripts/release/install-blue-green.sh` with `VERSION`, `--base-url`,
`--domain`, `--registry`, `--install-dir`, `--tls-cert-file`,
`--tls-key-file`, and `--execute`. Require the certificate and key to be
readable, preserve existing generated secrets, copy the Compose, edge config,
upstream default, deploy script, and lifecycle helpers into the install
directory, and initialize `.active_color` only when absent. Execute:

```bash
HCDR_INSTALL_DIR="${install_dir}" \
  "${install_dir}/deploy-blue-green.sh" "${VERSION}"
```

Do not overwrite existing secret files, `.env` secret values, or
`.active_color` during an upgrade.

- [ ] **Step 4: Make lifecycle scripts color-aware**

`start-platform.sh` calls:

```text
deploy-blue-green.sh --start-current
```

`stop-platform.sh` stops the blue profile, green profile, and shared services
without deleting volumes. `restart-platform.sh` remains stop then start.
Systemd continues to call these helpers and therefore needs no new deployment
logic.

- [ ] **Step 5: Run installer and lifecycle tests**

Run:

```bash
bash scripts/tests/blue-green-deploy.sh
bash -n scripts/release/install-blue-green.sh \
  scripts/release/start-platform.sh \
  scripts/release/stop-platform.sh \
  scripts/release/restart-platform.sh
```

Expected: exit 0.

- [ ] **Step 6: Commit Task 3**

```bash
git add scripts/release/install-platform.sh scripts/release/start-platform.sh \
  scripts/release/stop-platform.sh scripts/release/restart-platform.sh \
  scripts/release/templates/hypercdr.service scripts/tests/blue-green-deploy.sh
git commit -m "feat: install and recover blue green runtime"
```

---

### Task 4: Extend the Existing Release Workflow with Gated Deployment

**Files:**
- Modify: `.github/workflows/release.yml`
- Create: `scripts/tests/release-workflow.sh`
- Modify: `.github/workflows/pr-check.yml`
- Modify: `Makefile`

**Interfaces:**
- Consumes: images published by the existing `publish` job and the Task 1 host script.
- Produces: an optional deploy job gated by repository variable `HCDR_AUTO_DEPLOY`.

- [ ] **Step 1: Write the failing workflow contract test**

Create `scripts/tests/release-workflow.sh` with exact checks:

```bash
#!/usr/bin/env bash
set -euo pipefail
workflow=.github/workflows/release.yml
grep -Fq 'needs: publish' "$workflow"
grep -Fq "vars.HCDR_AUTO_DEPLOY == 'true'" "$workflow"
grep -Fq 'secrets.SSH_PRIVATE_KEY' "$workflow"
grep -Fq 'secrets.SSH_KNOWN_HOSTS' "$workflow"
grep -Fq 'vars.HCDR_SSH_HOST' "$workflow"
grep -Fq 'vars.HCDR_DEPLOY_PATH' "$workflow"
grep -Fq 'deploy-blue-green.sh' "$workflow"
grep -Fq 'command_timeout: 3600s' "$workflow"
```

- [ ] **Step 2: Run the workflow test and verify RED**

Run `bash scripts/tests/release-workflow.sh`.

Expected: failure because no deploy job exists.

- [ ] **Step 3: Add the gated deploy job**

Keep the existing `publish` job unchanged except for exposing the normalized
image version as a job output. Add one `deploy` job:

```yaml
deploy:
  needs: publish
  if: ${{ vars.HCDR_AUTO_DEPLOY == 'true' }}
  runs-on: ubuntu-latest
  environment: production
  steps:
    - uses: actions/checkout@v4
    - uses: shimataro/ssh-key-action@v2
      with:
        key: ${{ secrets.SSH_PRIVATE_KEY }}
        known_hosts: ${{ secrets.SSH_KNOWN_HOSTS }}
        if_key_exists: fail
    - uses: appleboy/ssh-action@v1.0.3
      with:
        host: ${{ vars.HCDR_SSH_HOST }}
        username: ${{ vars.HCDR_SSH_USER }}
        port: ${{ vars.HCDR_SSH_PORT }}
        key: ${{ secrets.SSH_PRIVATE_KEY }}
        command_timeout: 3600s
        script_stop: true
```

The remote script must fetch `deploy-blue-green.sh`, `docker-compose.yml`, and
the Nginx files atomically from the exact release tag, preserve `.env` and
`.active_color`, then call:

```bash
HCDR_INSTALL_DIR="${{ vars.HCDR_DEPLOY_PATH }}" \
  "${{ vars.HCDR_DEPLOY_PATH }}/deploy-blue-green.sh" \
  "${{ needs.publish.outputs.image_version }}"
```

Do not put ACR credentials or application secrets into the SSH script.

- [ ] **Step 4: Wire contract tests into PR and local verification**

Add these commands to the repository check job and `make verify`:

```bash
bash scripts/tests/blue-green-deploy.sh
bash scripts/tests/blue-green-compose.sh
bash scripts/tests/release-workflow.sh
```

- [ ] **Step 5: Run workflow checks**

Run:

```bash
bash scripts/tests/release-workflow.sh
bash scripts/tests/blue-green-compose.sh
```

Expected: exit 0.

- [ ] **Step 6: Commit Task 4**

```bash
git add .github/workflows/release.yml .github/workflows/pr-check.yml Makefile \
  scripts/tests/release-workflow.sh
git commit -m "ci: deploy published releases through blue green switch"
```

---

### Task 5: Add the Production Runbook and Complete Verification

**Files:**
- Create: `docs/deployment/blue-green-deployment.zh.md`
- Modify: `scripts/release/README.md`

**Interfaces:**
- Documents the exact GitHub variables/secrets, server prerequisites, first install, health verification, rollback, and auto-deploy enablement required by the spec.

- [ ] **Step 1: Write the operator runbook**

Document these exact repository variables:

```text
HCDR_ACR_SERVER=crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com
HCDR_IMAGE_REGISTRY=crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr
HCDR_SSH_HOST=47.236.253.138
HCDR_SSH_USER=root
HCDR_SSH_PORT=22
HCDR_DEPLOY_PATH=/var/lib/hypercdr
HCDR_DOMAIN=hypercdr.com
HCDR_AUTO_DEPLOY=false
```

Document secrets by name only: `REGISTRY_USERNAME`, `REGISTRY_PASSWORD`,
`SSH_PRIVATE_KEY`, and `SSH_KNOWN_HOSTS`. Use the existing secret names for ACR
instead of creating duplicate `ACR_*` secrets.

Include commands for:

```text
DNS verification
cloud firewall verification
Let's Encrypt certificate issuance
installer dry run
manual first deploy with 1.0.32.20260915
docker compose ps for both profiles
curl https://hypercdr.com/readyz
deploy-blue-green.sh --rollback
setting HCDR_AUTO_DEPLOY=true only after one switch and rollback succeed
```

State explicitly that the existing `hypercdr-dev-postgres` is not removed by
installation and requires separate explicit cleanup later.

- [ ] **Step 2: Link the runbook from the release guide**

Add one production blue/green section to `scripts/release/README.md` linking to
`docs/deployment/blue-green-deployment.zh.md` and identifying GitHub Actions as
the sole platform deploy controller for this topology.

- [ ] **Step 3: Run focused verification**

Run:

```bash
bash scripts/tests/blue-green-deploy.sh
bash scripts/tests/blue-green-compose.sh
bash scripts/tests/release-workflow.sh
find scripts bootstrap -type f -name '*.sh' -print0 | xargs -0 -n1 bash -n
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 4: Run full repository verification**

Run:

```bash
make verify
```

Expected: backend, Agent, frontend, release-center, Bootstrap, repository
hygiene, shell syntax, and new deployment tests all pass.

- [ ] **Step 5: Review the final diff**

Run:

```bash
git status --short
git diff --stat HEAD~4..HEAD
git diff --check HEAD~4..HEAD
```

Confirm no `.env`, certificate, private key, password, runtime directory,
generated frontend output, or ACR credential is tracked.

- [ ] **Step 6: Commit Task 5**

```bash
git add docs/deployment/blue-green-deployment.zh.md scripts/release/README.md
git commit -m "docs: add blue green deployment runbook"
```
