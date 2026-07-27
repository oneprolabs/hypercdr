package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

func Run(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		create table if not exists schema_migrations (
			version text primary key,
			applied_at timestamptz not null default now()
		)
	`); err != nil {
		return err
	}

	entries, err := migrationFiles.ReadDir("sql")
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := apply(ctx, db, name); err != nil {
			return err
		}
	}
	return nil
}

func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		select exists(select 1 from schema_migrations where version = $1)
	`, version).Scan(&exists)
	return exists, err
}

func apply(ctx context.Context, db *sql.DB, name string) error {
	content, err := migrationFiles.ReadFile(path.Join("sql", name))
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `
		insert into schema_migrations (version) values ($1)
	`, name); err != nil {
		return err
	}
	return tx.Commit()
}
