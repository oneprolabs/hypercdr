# HyperCDR control-plane backend

The backend is the Community Go module for the control-plane API, persistence,
scheduler, task engine, migrations, agent protocol, and public Open Core
extension package.

- `cmd/` contains executable entry points.
- `internal/` contains private HTTP, storage, task, protocol, migration, and
  embedded Velero asset implementations.
- `pkg/platform/` is the supported public assembly boundary used by Enterprise.

Run `./scripts/test-backend.sh` from the repository root for isolated PostgreSQL tests. See [test setup](../docs/development/platform.md). Generated binaries and Go caches must
be written under the repository sibling `hypercdr-runtime`, not into the module tree.
