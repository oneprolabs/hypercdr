# HyperCDR Open Core Extension Architecture

## Repository boundary

`hypercdr-main` is the complete, independently buildable Community product and
is licensed under Apache License 2.0. `hypercdr-enterprise` is a separate private
repository. Dependency direction is always Enterprise to Community; Community
code, builds, tests, and releases never require Enterprise source or services.

Community exposes two intentionally small extension contracts:

- Go package `backend/pkg/platform`, which assembles and runs the control plane
  from edition, capability, and license-provider-neutral options.
- npm entry `frontend/src/public.ts`, which exports the Community application
  and a validated build-time frontend module registration interface.

Enterprise code must not import Community `internal` Go packages and must not be
copied into the Community source tree. The combined Enterprise API is one binary
and the combined Enterprise UI is one frontend bundle; neither depends on a
running Community control-plane service.

## Shared cluster components

Comm-agent, Velero-agent, their protocols, and their cluster deployment flow are
Community components shared permanently by both editions. There are no
Enterprise agent forks. Rolling upgrades must preserve compatibility with
already installed agents.

## Capabilities and licensing

`GET /api/v1/product-info` is public so the login shell and operational tooling
can discover product edition, capabilities, and license state. Capability data
is presentation and product discovery metadata, never an authorization boundary.
Tenant and permission enforcement remains in storage and API paths.

Phase 1 Enterprise uses a development-only License Provider. Signed licenses,
production enforcement, and commercial entitlement delivery are explicitly
deferred. Future expiry behavior must preserve core backup and restore while
making Enterprise governance read-only and preventing new governed resources.

## Dependency locking

Local development uses path dependencies. Enterprise `community.lock` records
the exact Community commit. A dirty Community tree is allowed only for local
cross-repository development; CI and release builds must require the exact clean
commit. Enterprise versions use an `-ent.N` suffix.

