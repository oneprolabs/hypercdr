# Contributing to HyperCDR

## Workflow

1. Create a focused branch from `main`.
2. Keep functional changes separate from repository or formatting changes.
3. Add or update tests for behavior changes.
4. Update protocol, schema, deployment, and operational documentation when the
   related contract changes.
5. Run `make verify` before opening a pull request.

## Commit scope

Use concise English commit subjects. Recommended prefixes are `feat`, `fix`,
`refactor`, `test`, `docs`, `build`, `ci`, and `chore`.

Do not commit generated output, runtime data, credentials, certificates,
kubeconfigs, database dumps, browser automation traces, or customer data.

## Database migrations

Migrations under `backend/internal/migrations/sql` are immutable once merged.
Create a new sequential migration for every schema change. New installations
run the complete sequence; migration files do not contain development data.

## Pull-request checks

- Backend and comm-agent Go tests pass.
- Frontend type checking and production build pass.
- Shell scripts pass `bash -n`.
- A clean database can apply all migrations.
- Tenant isolation and authorization behavior remain covered by tests.
