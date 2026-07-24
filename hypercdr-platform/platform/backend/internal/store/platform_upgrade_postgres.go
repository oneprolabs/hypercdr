package store

import (
	"database/sql"
	"errors"
	"time"
)

const platformReleaseColumns = `id,tenant_id,version,api_image,api_image_digest,frontend_image,frontend_image_digest,database_schema_version,coalesce(minimum_agent_version,''),rollback_supported,coalesce(release_notes,''),status,coalesce(published_by,''),coalesce(published_at,'0001-01-01'::timestamptz),created_at,updated_at`

func scanPlatformRelease(row interface{ Scan(...any) error }) (PlatformRelease, error) {
	var v PlatformRelease
	err := row.Scan(&v.ID, &v.TenantID, &v.Version, &v.APIImage, &v.APIImageDigest, &v.FrontendImage, &v.FrontendImageDigest, &v.DatabaseSchemaVersion, &v.MinimumAgentVersion, &v.RollbackSupported, &v.ReleaseNotes, &v.Status, &v.PublishedBy, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (s *PostgresStore) ListPlatformReleases() ([]PlatformRelease, error) {
	rows, err := s.db.Query(`select `+platformReleaseColumns+` from platform_releases where tenant_id=$1 order by created_at desc`, DefaultTenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PlatformRelease{}
	for rows.Next() {
		v, err := scanPlatformRelease(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (s *PostgresStore) UpsertPlatformRelease(input PlatformReleaseInput) (PlatformRelease, error) {
	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = "candidate"
	}
	return scanPlatformRelease(s.db.QueryRow(`insert into platform_releases(id,tenant_id,version,api_image,api_image_digest,frontend_image,frontend_image_digest,database_schema_version,minimum_agent_version,rollback_supported,release_notes,status,published_by,published_at,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,nullif($9,''),$10,nullif($11,''),$12,nullif($13,''),case when $12::text='active' then $14::timestamptz else null::timestamptz end,$14,$14) on conflict(tenant_id,version) do update set api_image=excluded.api_image,api_image_digest=excluded.api_image_digest,frontend_image=excluded.frontend_image,frontend_image_digest=excluded.frontend_image_digest,database_schema_version=excluded.database_schema_version,minimum_agent_version=excluded.minimum_agent_version,rollback_supported=excluded.rollback_supported,release_notes=excluded.release_notes,updated_at=excluded.updated_at returning `+platformReleaseColumns, newID(), DefaultTenantID, input.Version, input.APIImage, input.APIImageDigest, input.FrontendImage, input.FrontendImageDigest, input.DatabaseSchemaVersion, input.MinimumAgentVersion, input.RollbackSupported, input.ReleaseNotes, status, input.PublishedBy, now))
}
func (s *PostgresStore) ActivatePlatformRelease(id, publishedBy string) (PlatformRelease, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return PlatformRelease{}, false, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRow(`select exists(select 1 from platform_releases where id=$1 and tenant_id=$2)`, id, DefaultTenantID).Scan(&exists); err != nil || !exists {
		return PlatformRelease{}, false, err
	}
	now := time.Now().UTC()
	if _, err = tx.Exec(`update platform_releases set status='retired',updated_at=$2 where tenant_id=$1 and status='active' and id<>$3`, DefaultTenantID, now, id); err != nil {
		return PlatformRelease{}, false, err
	}
	v, err := scanPlatformRelease(tx.QueryRow(`update platform_releases set status='active',published_by=nullif($2,''),published_at=$3,updated_at=$3 where id=$1 returning `+platformReleaseColumns, id, publishedBy, now))
	if err != nil {
		return PlatformRelease{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return PlatformRelease{}, false, err
	}
	return v, true, nil
}

const platformJobColumns = `id,tenant_id,release_id,from_version,target_version,status,step,progress,api_image,api_image_digest,frontend_image,frontend_image_digest,database_schema_version,rollback_supported,coalesce(backup_path,''),coalesce(previous_api_image,''),coalesce(previous_frontend_image,''),coalesce(error_code,''),coalesce(error_message,''),coalesce(requested_by,''),coalesce(executor_id,''),coalesce(executor_heartbeat_at,'0001-01-01'::timestamptz),created_at,coalesce(started_at,'0001-01-01'::timestamptz),coalesce(completed_at,'0001-01-01'::timestamptz),updated_at`

func scanPlatformJob(row interface{ Scan(...any) error }) (PlatformUpgradeJob, error) {
	var j PlatformUpgradeJob
	err := row.Scan(&j.ID, &j.TenantID, &j.ReleaseID, &j.FromVersion, &j.TargetVersion, &j.Status, &j.Step, &j.Progress, &j.APIImage, &j.APIImageDigest, &j.FrontendImage, &j.FrontendImageDigest, &j.DatabaseSchemaVersion, &j.RollbackSupported, &j.BackupPath, &j.PreviousAPIImage, &j.PreviousFrontendImage, &j.ErrorCode, &j.ErrorMessage, &j.RequestedBy, &j.ExecutorID, &j.ExecutorHeartbeatAt, &j.CreatedAt, &j.StartedAt, &j.CompletedAt, &j.UpdatedAt)
	return j, err
}
func (s *PostgresStore) ListPlatformUpgradeJobs() ([]PlatformUpgradeJob, error) {
	rows, err := s.db.Query(`select `+platformJobColumns+` from platform_upgrade_jobs where tenant_id=$1 order by created_at desc`, DefaultTenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PlatformUpgradeJob{}
	for rows.Next() {
		j, err := scanPlatformJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, j)
	}
	return items, rows.Err()
}
func (s *PostgresStore) CreatePlatformUpgradeJob(input PlatformUpgradeJobInput) (PlatformUpgradeJob, error) {
	r := input.Release
	now := time.Now().UTC()
	return scanPlatformJob(s.db.QueryRow(`insert into platform_upgrade_jobs(id,tenant_id,release_id,from_version,target_version,status,step,progress,api_image,api_image_digest,frontend_image,frontend_image_digest,database_schema_version,rollback_supported,previous_api_image,previous_frontend_image,requested_by,created_at,updated_at) values($1,$2,$3,$4,$5,'queued','queued',0,$6,$7,$8,$9,$10,$11,nullif($12,''),nullif($13,''),nullif($14,''),$15,$15) returning `+platformJobColumns, newID(), DefaultTenantID, r.ID, input.FromVersion, r.Version, r.APIImage, r.APIImageDigest, r.FrontendImage, r.FrontendImageDigest, r.DatabaseSchemaVersion, r.RollbackSupported, input.PreviousAPIImage, input.PreviousFrontendImage, input.RequestedBy, now))
}
func (s *PostgresStore) UpdatePlatformUpgradeJob(input PlatformUpgradeJobUpdate) (PlatformUpgradeJob, bool, error) {
	now := time.Now().UTC()
	j, err := scanPlatformJob(s.db.QueryRow(`update platform_upgrade_jobs set status=coalesce(nullif($2,''),status),step=coalesce(nullif($3,''),step),progress=case when $4::integer>=0 then $4 else progress end,backup_path=coalesce(nullif($5,''),backup_path),error_code=nullif($6,''),error_message=nullif($7,''),executor_id=coalesce(nullif($8,''),executor_id),executor_heartbeat_at=case when nullif($8,'') is not null then $9 else executor_heartbeat_at end,started_at=case when $10::boolean then coalesce(started_at,$9) else started_at end,completed_at=case when $11::boolean then $9 else completed_at end,previous_api_image=coalesce(nullif($12,''),previous_api_image),previous_frontend_image=coalesce(nullif($13,''),previous_frontend_image),updated_at=$9 where id=$1 returning `+platformJobColumns, input.ID, input.Status, input.Step, input.Progress, input.BackupPath, input.ErrorCode, input.ErrorMessage, input.ExecutorID, now, input.MarkStarted, input.MarkDone, input.PreviousAPIImage, input.PreviousFrontendImage))
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformUpgradeJob{}, false, nil
	}
	return j, err == nil, err
}
