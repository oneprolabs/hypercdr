# Platform development

- `frontend/src/`: React UI and shared API client.
- `backend/internal/httpserver/`: REST API, agent WebSocket, and policy scheduler.
- `backend/internal/store/`: PostgreSQL persistence and task state transitions.
- `backend/internal/migrations/sql/`: embedded, forward PostgreSQL migrations.
- `backend/pkg/platform/`: control-plane startup and runtime configuration.

## Database tests

Run `./scripts/test-backend.sh` (or `make test-backend`). The wrapper starts a
temporary Docker PostgreSQL instance, runs the tests, and removes the instance.
Each test uses a separate database with real migrations and foreign keys.
There is no in-memory storage implementation or production storage fallback.

For an existing **dedicated test server**, set `HCDR_TEST_DATABASE_URL` to a
PostgreSQL URL with user `hypercdr_test` and database `hypercdr_test_admin`.
The user must have permission to create and drop databases. Tests never read
`HCDR_DATABASE_URL`, and reject URLs without the dedicated test user/database.
Do not point this variable at a business database. The wrapper image defaults to
`postgres:16-alpine`; `HCDR_TEST_POSTGRES_IMAGE` can select an available mirror.

```sh
./scripts/test-backend.sh -count=1 ./...
./scripts/test-backend.sh -race ./internal/store ./internal/httpserver
make verify
```

`make verify` includes Go vet/format checks, isolated PostgreSQL tests, agent
tests, frontend lint/type checks/tests/build, and deployment contract checks.
Builds, test databases, caches, and evidence live in `../hypercdr-runtime`.

## Which worktree is running?

`make status` identifies the actual fixed-name systemd launchers and prints each
component's build source directory, branch, commit, dirty-worktree flag and UTC
build time. API and frontend are recorded separately. Source and artifact
fingerprints detect edits since the build and unrecorded artifact replacement;
the API check also compares the running process binary with the installed file.
A different current worktree produces a warning. Missing provenance is reported
as unknown, never guessed from the latest checkout. Inactive services do not
claim to be running an old recorded build.

Use `./scripts/dev/update-dev.sh api`, `frontend`, or `all` (default), or
`make update-dev`, to rebuild from the current worktree and restart the selected
services. The updater refuses a different active runtime and serializes updates.
It does not recreate PostgreSQL or change runtime configuration. Capture occurs
before building, and recording refuses a changed source snapshot. Records live
beside the binary/dist in the external runtime; they are not public frontend
assets and contain no runtime configuration. Manual builds/copies must retain
matching provenance; otherwise status warns rather than attributing them to
current source. A dirty build records that fact permanently even after commit.

The host/systemd development flow remains supported. `docker-compose.dev.yml`
supplies its PostgreSQL dependency. An independent full container alternative
is now available through `scripts/dev/source-compose.sh`; it builds the API,
frontend and registration executor from the current checkout and retains its
own PostgreSQL volume. It uses explicit rebuilds, not bind-mounted live reload,
and does not replace host provenance reporting or production blue/green.
See [source container development](../deployment/build-release-install.md#portable-source-development)
for setup, external manifest requirements, HTTP/network limits and teardown.
