# PostgreSQL schema and migrations

The authoritative schema is the ordered SQL in `backend/internal/migrations/sql/`, executed by the backend migration implementation. This document does not duplicate illustrative CREATE TABLE statements: use the migrations to determine exact columns, constraints, indexes and supported schema version.

The schema covers identity/authentication and preferences, registered clusters and inventory, storage and policies, protection plans, tasks and restore points, release/upgrade state, and diagnostics. Table/domain ownership and transactional checks are implemented in `backend/internal/store/`; the HTTP layer applies authenticated resource scope. An edition extension contract must not be confused with tables shipped by the Community migrations.

PostgreSQL is the production source of truth. There is no in-memory persistence fallback. Backend tests create separate databases and apply real migrations; see [database test setup](../development/platform.md).

Add forward migrations for schema changes; do not rewrite already shipped migrations. Back up before upgrades. Application traffic rollback does not reverse an applied SQL migration. Runtime data and database backups belong outside the checkout.
