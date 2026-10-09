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
