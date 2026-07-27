# Database Migrations

Runtime migrations are embedded from:

```text
platform/backend/internal/migrations/sql/
```

This directory is intentionally kept without SQL files to avoid maintaining a
second migration source. Use `cmd/platform-migrate` or backend startup to apply
the embedded migrations.
