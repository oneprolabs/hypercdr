package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const CommunityMigrationExportVersion = "hypercdr-community-export/v1"

// The order is also the foreign-key-safe import order. Authentication,
// sessions, install tokens, Agent credentials, platform upgrades and global
// settings are intentionally absent.
var communityMigrationTables = []string{
	"clusters", "cluster_nodes", "applications", "tags", "application_tags",
	"storage_repositories", "cluster_storage_bindings", "policies",
	"protection_plans", "protection_plan_apps", "protection_plan_schedules",
	"restore_points", "tasks", "task_events", "audit_logs", "diagnostic_logs",
	"cluster_log_coverage",
}

type CommunityMigrationTableManifest struct {
	Name   string `json:"name"`
	Count  int64  `json:"count"`
	SHA256 string `json:"sha256"`
}

type CommunityMigrationExportManifest struct {
	FormatVersion  string                            `json:"formatVersion"`
	SchemaVersions []string                          `json:"schemaVersions"`
	Tables         []CommunityMigrationTableManifest `json:"tables"`
	SHA256         string                            `json:"sha256"`
}

type CommunityMigrationExportBatch struct {
	FormatVersion string            `json:"formatVersion"`
	Table         string            `json:"table"`
	Offset        int               `json:"offset"`
	Limit         int               `json:"limit"`
	Total         int64             `json:"total"`
	Rows          []json.RawMessage `json:"rows"`
	SHA256        string            `json:"sha256"`
	NextOffset    *int              `json:"nextOffset,omitempty"`
}

type CommunityMigrationExporter interface {
	CommunityMigrationManifest(context.Context) (CommunityMigrationExportManifest, error)
	CommunityMigrationBatch(context.Context, string, int, int) (CommunityMigrationExportBatch, error)
	CreateCommunityMigrationBackup(context.Context, string) (CommunityMigrationExportManifest, error)
	CommunityMigrationStorageCredentials(context.Context) ([]CommunityMigrationStorageCredential, error)
}

type CommunityMigrationStorageCredential struct {
	RepositoryID string            `json:"repositoryId"`
	Secret       map[string]string `json:"secret"`
}

func validCommunityMigrationTable(name string) bool {
	for _, candidate := range communityMigrationTables {
		if name == candidate {
			return true
		}
	}
	return false
}

func migrationRowExpression(table string) string {
	if table == "storage_repositories" {
		// Credentials are exported only through the envelope-encrypted credential
		// channel; an at-rest ciphertext is useless and unsafe on another instance.
		return `(to_jsonb(t)-'secret_ciphertext'-'secret_payload')`
	}
	return `to_jsonb(t)`
}

func (s *PostgresStore) CommunityMigrationManifest(ctx context.Context) (CommunityMigrationExportManifest, error) {
	manifest := CommunityMigrationExportManifest{FormatVersion: CommunityMigrationExportVersion, Tables: []CommunityMigrationTableManifest{}, SchemaVersions: []string{}}
	versions, err := s.db.QueryContext(ctx, `select version from schema_migrations order by version`)
	if err != nil {
		return manifest, err
	}
	for versions.Next() {
		var version string
		if err = versions.Scan(&version); err != nil {
			versions.Close()
			return manifest, err
		}
		manifest.SchemaVersions = append(manifest.SchemaVersions, version)
	}
	versions.Close()
	if err = versions.Err(); err != nil {
		return manifest, err
	}
	for _, table := range communityMigrationTables {
		expression := migrationRowExpression(table)
		var item CommunityMigrationTableManifest
		item.Name = table
		rows, queryErr := s.db.QueryContext(ctx, fmt.Sprintf(`select %s::text from %s t order by %s::text`, expression, table, expression))
		if queryErr != nil {
			return manifest, fmt.Errorf("manifest %s: %w", table, queryErr)
		}
		hash := sha256.New()
		for rows.Next() {
			var raw []byte
			if queryErr = rows.Scan(&raw); queryErr != nil {
				rows.Close()
				return manifest, queryErr
			}
			item.Count++
			_, _ = hash.Write(raw)
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return manifest, fmt.Errorf("manifest %s: %w", table, queryErr)
		}
		item.SHA256 = hex.EncodeToString(hash.Sum(nil))
		manifest.Tables = append(manifest.Tables, item)
	}
	raw, _ := json.Marshal(struct {
		Format  string
		Schemas []string
		Tables  []CommunityMigrationTableManifest
	}{manifest.FormatVersion, manifest.SchemaVersions, manifest.Tables})
	digest := sha256.Sum256(raw)
	manifest.SHA256 = hex.EncodeToString(digest[:])
	return manifest, nil
}

func (s *PostgresStore) CommunityMigrationBatch(ctx context.Context, table string, offset, limit int) (CommunityMigrationExportBatch, error) {
	result := CommunityMigrationExportBatch{FormatVersion: CommunityMigrationExportVersion, Table: table, Offset: offset, Limit: limit, Rows: []json.RawMessage{}}
	if !validCommunityMigrationTable(table) {
		return result, errors.New("migration export table is not allowed")
	}
	if offset < 0 {
		return result, errors.New("migration export offset is invalid")
	}
	if limit < 1 || limit > 500 {
		return result, errors.New("migration export limit must be between 1 and 500")
	}
	expression := migrationRowExpression(table)
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`select count(*) from %s`, table)).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`select %s::text from %s t order by %s::text limit $1 offset $2`, expression, table, expression), limit, offset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return result, err
		}
		result.Rows = append(result.Rows, json.RawMessage(append([]byte(nil), raw...)))
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	raw, _ := json.Marshal(result.Rows)
	digest := sha256.Sum256(raw)
	result.SHA256 = hex.EncodeToString(digest[:])
	if offset+len(result.Rows) < int(result.Total) {
		next := offset + len(result.Rows)
		result.NextOffset = &next
	}
	return result, nil
}

func (s *PostgresStore) CreateCommunityMigrationBackup(ctx context.Context, migrationID string) (CommunityMigrationExportManifest, error) {
	manifest, err := s.CommunityMigrationManifest(ctx)
	if err != nil {
		return manifest, err
	}
	snapshot := map[string][]json.RawMessage{}
	for _, table := range communityMigrationTables {
		if table == "storage_repositories" {
			rows, queryErr := s.db.QueryContext(ctx, `select to_jsonb(t)::text from storage_repositories t order by to_jsonb(t)::text`)
			if queryErr != nil {
				return manifest, queryErr
			}
			items := []json.RawMessage{}
			for rows.Next() {
				var raw []byte
				if queryErr = rows.Scan(&raw); queryErr != nil {
					rows.Close()
					return manifest, queryErr
				}
				items = append(items, json.RawMessage(append([]byte(nil), raw...)))
			}
			queryErr = rows.Err()
			rows.Close()
			if queryErr != nil {
				return manifest, queryErr
			}
			snapshot[table] = items
			continue
		}
		batch, batchErr := s.CommunityMigrationBatch(ctx, table, 0, 500)
		if batchErr != nil {
			return manifest, batchErr
		}
		rows := append([]json.RawMessage(nil), batch.Rows...)
		for batch.NextOffset != nil {
			batch, batchErr = s.CommunityMigrationBatch(ctx, table, *batch.NextOffset, 500)
			if batchErr != nil {
				return manifest, batchErr
			}
			rows = append(rows, batch.Rows...)
		}
		snapshot[table] = rows
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return manifest, err
	}
	digest := sha256.Sum256(raw)
	_, err = s.db.ExecContext(ctx, `insert into community_migration_backups(migration_id,manifest,snapshot,snapshot_sha256) values($1,$2,$3,$4) on conflict(migration_id) do nothing`, migrationID, manifest, raw, hex.EncodeToString(digest[:]))
	return manifest, err
}

func (s *PostgresStore) CommunityMigrationStorageCredentials(_ context.Context) ([]CommunityMigrationStorageCredential, error) {
	items, err := s.ListStorageRepositories()
	if err != nil {
		return nil, err
	}
	result := []CommunityMigrationStorageCredential{}
	for _, item := range items {
		if len(item.Secret) > 0 {
			result = append(result, CommunityMigrationStorageCredential{RepositoryID: item.ID, Secret: item.Secret})
		}
	}
	return result, nil
}
