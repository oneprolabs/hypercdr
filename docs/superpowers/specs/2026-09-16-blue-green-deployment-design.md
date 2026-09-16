# HyperCDR Blue/Green Deployment Design

## Status

Approved direction from the operator conversation on 2026-09-16. This design
defines the first production deployment for `hypercdr.com` on
`47.236.253.138`. Implementation must not mutate the server until the local
changes pass repository verification and the first-deploy runbook is reviewed.

## Goals

- Build and publish immutable HyperCDR release images to the existing Alibaba
  Cloud ACR namespace.
- Deploy the control plane with health-gated blue/green switching.
- Keep PostgreSQL and stateful worker services single-instance.
- Terminate public TLS at one stable edge proxy on ports 80 and 443.
- Support a failed pre-switch deployment without disturbing the active color.
- Support an explicit application rollback to the previously retained color.
- Preserve the existing development Compose workflow.

## Non-goals

- Blue/green PostgreSQL replication.
- Schema rollback after a destructive migration.
- Simultaneous platform deployment by GitHub Actions and the in-product
  platform upgrader.
- Guaranteeing that long-lived Agent WebSockets never reconnect during a
  release.
- Replacing the existing release image build scripts or ACR registry profile.

## Current State

- Source branch starts at commit `0db6c307eed3819d50fb38362fdf46341284a514`.
- The production Compose file defines one API, one frontend, one upgrader, one
  registration executor, and one PostgreSQL service with fixed container
  names.
- The API already exposes `/healthz` and `/readyz`.
- The API startup command runs `platform-migrate` before `platform-api`.
- The frontend image is an Nginx image that currently terminates TLS and
  proxies API and WebSocket paths to one fixed API service name.
- The in-product `platform-upgrader` performs an in-place Compose replacement.
- The server currently runs only `hypercdr-dev-postgres` from
  `docker-compose.dev.yml`; `/var/lib/hypercdr` and `hypercdr.service` do not
  exist.
- The server has no cached HyperCDR application images and no ACR login.
- The ACR namespace is publicly readable and contains a complete common image
  version `1.0.32.20260915`.

## Registry and Release Identity

The existing profile remains authoritative:

```text
Registry server:
crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com

Image prefix:
crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr
```

Git tags use a leading `v`, for example `v1.0.33.20260916`. Image tags use the
same value without the leading `v`, for example `1.0.33.20260916`. Published
version tags are immutable.

The deploy host pulls public images anonymously. GitHub Actions receives only
the ACR credentials required to push images.

## Runtime Architecture

The production Compose project is named `hypercdr` and contains:

```text
hypercdr-edge
hypercdr-postgres
hypercdr-platform-api-blue
hypercdr-platform-frontend-blue
hypercdr-platform-api-green
hypercdr-platform-frontend-green
hypercdr-cluster-registration-executor
```

The edge proxy and PostgreSQL are always-on shared services. API and frontend
services are selected with Compose profiles `blue` and `green`. The
registration executor remains a singleton because running two executors could
execute the same queued registration work twice.

The first production phase does not run `hypercdr-platform-upgrader`. GitHub
Actions is the only production platform deployment controller. This avoids the
current upgrader rewriting `.env` and performing an in-place replacement while
the blue/green script owns color state. In-product platform upgrade requests
must not be used until the upgrader is changed to call the same deployment
engine.

Only the edge proxy publishes host ports:

```text
22/tcp   SSH, restricted by the cloud firewall where practical
80/tcp   ACME HTTP challenge and HTTPS redirect
443/tcp  HyperCDR UI, API, assets, and WebSocket traffic
```

API port 18080, frontend port 3002, and PostgreSQL port 5432 remain on the
Compose network and are not published by the production stack.

## Edge Routing

The stable Nginx edge owns the public certificate and routes to two logical
upstreams:

- `hypercdr_frontend` for `/` and static application routes.
- `hypercdr_api` for `/api/`, `/ws/`, `/healthz`, `/readyz`, `/install.sh`,
  `/uninstall-agent.sh`, `/prepare-node.sh`, `/assets/registry/`,
  `/assets/velero/`, and `/assets/platform/ca.crt`.

The active upstream file contains one color at a time. A switch is an atomic
replacement of the upstream file followed by `nginx -t` and `nginx -s reload`.
The active color is persisted in `/var/lib/hypercdr/.active_color` only after a
successful reload.

The color frontend serves static files over internal HTTP. Public TLS and all
backend routing move to the edge so a frontend color cannot accidentally proxy
to the other color's API.

## Server State Layout

Runtime state lives outside the source repository:

```text
/var/lib/hypercdr/
├── .env
├── .active_color
├── docker-compose.yaml
├── data/postgres/
├── nginx/conf.d/upstream.conf
├── scripts/deploy-blue-green.sh
├── backups/
└── registration-sessions/
```

Let's Encrypt certificates remain in the host-managed
`/etc/letsencrypt/live/hypercdr.com/` tree and are mounted read-only into the
edge container. Certificate issuance and renewal are host operations, not CI
secrets.

The existing development PostgreSQL container and its data are left untouched
during the first production deployment because its host port 15432 does not
conflict with the production stack. It can be removed only after production
verification and a separate explicit operator action.

## Deployment State Machine

The deployment command accepts exactly one immutable image version.

1. Acquire an atomic host lock so manual and CI deployments cannot overlap.
2. Validate the version format, required environment, Compose file, and current
   color state.
3. Start the shared PostgreSQL service and wait for it to become healthy.
4. On first install, deploy `blue`; otherwise select the inactive color.
5. Pull the inactive API and frontend images for the requested version.
6. Start the inactive API. Its existing startup command runs migrations before
   the API process.
7. Wait for the API container health check and `/readyz` to succeed.
8. Start the matching frontend and verify its internal HTTP readiness.
9. Start the stable edge if this is the first install.
10. Atomically point both API and frontend upstreams at the new color, validate
    Nginx configuration, and reload Nginx.
11. Persist the new active color immediately after the successful reload.
12. Observe public `/readyz` for a bounded interval.
13. Update the singleton registration executor to the new version.
14. Keep the previous color available for a bounded WebSocket drain and
    rollback window, then stop it without removing its containers.

If any operation before step 10 fails, the script stops the candidate color and
leaves the current color and upstream file unchanged. If public observation
fails after the switch, the script restores the previous upstream, reloads
Nginx, restores `.active_color`, and reports failure.

## First Install

First install differs only because no active application color exists:

- Initialize `.active_color` as `blue`.
- Bring up shared services and blue.
- Require blue to pass health gates before starting the edge.
- Start the edge already pointing to blue.
- Do not attempt to stop or roll back to a nonexistent old color.

The existing ACR release `1.0.32.20260915` is the bootstrap candidate. Automatic
deployment remains disabled until this version passes the supervised first
install and a later release proves one complete blue-to-green switch and
rollback.

## Database Migration Contract

Blue and green share one PostgreSQL database. Migrations run when the candidate
API starts, while the active API may still serve requests. Every migration used
by this process must follow expand/contract compatibility:

1. Add new schema without removing behavior required by the active version.
2. Switch application reads and writes in a later compatible release.
3. Remove old schema only after no supported rollback version requires it.

The deployment script does not attempt automatic schema rollback. A release
with a destructive migration must be rejected before publication or deployed
under a separate maintenance procedure.

## WebSocket Semantics

Nginx reload preserves accepted connections in old Nginx workers, but stopping
the old API can still terminate Agent WebSockets. The old color therefore stays
running for a configurable drain period. Agents are expected to reconnect to
the new color through the stable `wss://hypercdr.com/ws/agent` endpoint.

The guarantee is zero planned HTTP cutover downtime, not zero WebSocket
reconnections.

## GitHub Actions

The workflow has four jobs:

1. Resolve a release version from a `v*` tag or a manual input.
2. Run `make verify`.
3. Build existing release images with repository scripts and push them to ACR
   after `docker/login-action` authenticates.
4. When `HCDR_AUTO_DEPLOY` is `true`, install the SSH key and invoke the
   host-side blue/green script with the immutable version.

GitHub repository variables:

```text
HCDR_ACR_SERVER
HCDR_IMAGE_REGISTRY
HCDR_SSH_HOST
HCDR_SSH_USER
HCDR_SSH_PORT
HCDR_DEPLOY_PATH
HCDR_DOMAIN
HCDR_AUTO_DEPLOY
```

GitHub repository secrets:

```text
ACR_USERNAME
ACR_PASSWORD
SSH_PRIVATE_KEY
SSH_KNOWN_HOSTS
```

The server `.env` contains database credentials, application secret keys,
release tokens, registration executor tokens, and optional Turnstile secrets.
These values are never copied into the workflow or committed.

Workflow concurrency uses one non-canceling production deployment group. A
failed health gate must fail the workflow; deployment scripts must never mask a
failed Compose, migration, pull, health, or Nginx command.

## Rollback and Retention

The normal rollback target is the stopped previous color. Rollback performs:

1. Confirm the previous color's containers and image references still exist.
2. Start the previous color if it was stopped.
3. Require its health checks to pass against the current database schema.
4. Atomically restore both upstreams and reload Nginx.
5. Persist the restored color.

At least the active and immediately previous image versions are retained.
Automated image pruning is excluded from the first implementation.

## Validation

Repository validation must include:

- A shell test that exercises first-install color selection, normal color
  selection, pre-switch health failure, successful switch state persistence,
  post-switch rollback, and lock rejection using command fakes rather than a
  live production Docker daemon.
- `bash -n` for every added or modified shell script.
- `docker compose config` with a non-secret test environment.
- Static assertions that only the edge publishes production ports and that
  both color profiles exist.
- Existing `make verify` checks.
- A manual supervised server run using `1.0.32.20260915`, followed by external
  `/readyz`, UI login, API, certificate, and Agent WebSocket verification.

## Operational Gates

No automated production deployment is enabled until all of the following are
true:

- `hypercdr.com` resolves to `47.236.253.138`.
- Cloud firewall ports 80 and 443 are open.
- A valid Let's Encrypt certificate exists on the server.
- The deployment-only SSH key and verified known-host entry are configured.
- ACR push credentials are configured in GitHub.
- The first manual install passes.
- One upgrade switch and one rollback pass under supervision.

