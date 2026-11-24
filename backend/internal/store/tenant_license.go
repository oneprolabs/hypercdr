package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"hypercdr-platform/platform/backend/pkg/accesscontrol"
	"hypercdr-platform/platform/backend/pkg/licensing"
)

var ErrLicenseRevision = errors.New("a newer or different license revision is already activated")
var ErrClusterCapacity = errors.New("the organization's cluster limit has been reached")
var ErrWorkerNodeCapacity = errors.New("the organization's worker node subscription is over quota")
var ErrRegistrationPermission = errors.New("the installation credential owner no longer has cluster registration permission")

func (s *PostgresStore) EnableCommunityEntitlements() {
	s.communityEntitlements = true
}

func (s *PostgresStore) EnableWorkerNodeEntitlements() {
	s.workerNodeEntitlements = true
}

func (s *PostgresStore) checkNewClusterAdmission(ctx context.Context, reader TenantLicenseReader, tenantID, createdBy string) error {
	var active bool
	if err := reader.QueryRowContext(ctx, `select status='active' from resource_scopes where id=$1`, tenantID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrRegistrationPermission
	}
	if createdBy != "" {
		var role, roleID, status string
		var systemAdmin bool
		var raw []byte
		err := reader.QueryRowContext(ctx, `select u.role,coalesce(u.custom_role_id::text,''),u.status,u.is_system_admin,coalesce(r.permissions,'[]'::jsonb) from users u left join organization_roles r on r.id=u.custom_role_id and r.tenant_id=u.tenant_id where u.id=$1 and u.tenant_id=$2`, createdBy, tenantID).Scan(&role, &roleID, &status, &systemAdmin, &raw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRegistrationPermission
		}
		if err != nil {
			return err
		}
		var permissions []string
		if err := json.Unmarshal(raw, &permissions); err != nil {
			return err
		}
		if status != "active" || !accesscontrol.Allows(role, systemAdmin, roleID, permissions, "cluster.register") {
			return ErrRegistrationPermission
		}
	}
	if s.workerNodeEntitlements {
		return nil
	}
	usage, err := ReadTenantLicenseUsage(ctx, reader, tenantID)
	if err != nil {
		return err
	}
	if !usage.CanRegister {
		return ErrClusterCapacity
	}
	return nil
}

type TenantLicenseUsage struct {
	Source                  string     `json:"source"`
	TenantID                string     `json:"tenantId"`
	Plan                    string     `json:"plan"`
	Status                  string     `json:"status"`
	MaxClusters             int        `json:"maxClusters"`
	Unlimited               bool       `json:"unlimited"`
	UsedClusters            int        `json:"usedClusters"`
	OverLimit               bool       `json:"overLimit"`
	CanRegister             bool       `json:"canRegister"`
	WorkerNodes             int        `json:"workerNodes"`
	MaxWorkerNodes          int        `json:"maxWorkerNodes"`
	WorkerNodesOverLimit    bool       `json:"workerNodesOverLimit"`
	WorkerOverageStartedAt  *time.Time `json:"workerOverageStartedAt,omitempty"`
	WorkerGracePeriodEndsAt *time.Time `json:"workerGracePeriodEndsAt,omitempty"`
	WorkerTasksBlocked      bool       `json:"workerTasksBlocked"`
	LicenseID               string     `json:"licenseId,omitempty"`
	Revision                int64      `json:"revision,omitempty"`
	ValidUntil              *time.Time `json:"validUntil,omitempty"`
}

type TenantLicenseReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ReadTenantLicenseUsage(ctx context.Context, reader TenantLicenseReader, tenantID string) (TenantLicenseUsage, error) {
	usage := TenantLicenseUsage{TenantID: tenantID, Plan: "free", Status: "active", MaxClusters: 1, MaxWorkerNodes: 2}
	var licenseID sql.NullString
	var validFrom, validUntil sql.NullTime
	var clusters sql.NullInt64
	var revision sql.NullInt64
	var subscriptionStatus sql.NullString
	var subscriptionClusters sql.NullInt64
	var paidUntil, periodStart sql.NullTime
	var workerOverageStarted sql.NullTime
	err := reader.QueryRowContext(ctx, `select e.source,l.id,l.revision,l.max_clusters,l.valid_from,l.valid_until,(select count(*) from clusters c where c.tenant_id=e.tenant_id),coalesce((select sum(greatest(c.node_count,0)) from clusters c where c.tenant_id=e.tenant_id),0),s.status,s.max_clusters,s.paid_until,s.period_start,twes.overage_started_at from tenant_entitlements e left join tenant_licenses l on l.id=e.license_id and l.tenant_id=e.tenant_id left join tenant_subscriptions s on s.tenant_id=e.tenant_id left join tenant_worker_entitlement_state twes on twes.tenant_id=e.tenant_id where e.tenant_id=$1`, tenantID).Scan(&usage.Source, &licenseID, &revision, &clusters, &validFrom, &validUntil, &usage.UsedClusters, &usage.WorkerNodes, &subscriptionStatus, &subscriptionClusters, &paidUntil, &periodStart, &workerOverageStarted)
	if err != nil {
		return TenantLicenseUsage{}, err
	}
	if usage.Source == "subscription" {
		if subscriptionStatus.Valid {
			if paidUntil.Valid {
				usage.ValidUntil = &paidUntil.Time
			}
			usage.Status = "inactive"
			now := time.Now().UTC()
			if paidUntil.Valid && !paidUntil.Time.After(now) {
				usage.Status = "expired"
			}
			if paidUntil.Valid && paidUntil.Time.After(now) && !periodStart.Time.After(now) && (subscriptionStatus.String == "active" || subscriptionStatus.String == "past_due" || subscriptionStatus.String == "canceled") {
				usage.Plan, usage.MaxClusters, usage.MaxWorkerNodes, usage.Status = "commercial", int(subscriptionClusters.Int64), int(subscriptionClusters.Int64), "active"
				usage.Unlimited = subscriptionClusters.Valid && subscriptionClusters.Int64 == 0
			}
		}
	} else if licenseID.Valid {
		usage.LicenseID, usage.Revision, usage.ValidUntil = licenseID.String, revision.Int64, &validUntil.Time
		now := time.Now().UTC()
		if !validFrom.Time.After(now) && validUntil.Time.After(now) {
			usage.Plan, usage.MaxClusters = "commercial", int(clusters.Int64)
			usage.MaxWorkerNodes = int(clusters.Int64)
		} else {
			usage.Status = "expired"
		}
	}
	usage.OverLimit = !usage.Unlimited && usage.UsedClusters > usage.MaxClusters
	usage.WorkerNodesOverLimit = !usage.Unlimited && usage.WorkerNodes > usage.MaxWorkerNodes
	if workerOverageStarted.Valid && usage.WorkerNodesOverLimit {
		started := workerOverageStarted.Time.UTC()
		usage.WorkerOverageStartedAt = &started
		ends := started.Add(30 * 24 * time.Hour)
		usage.WorkerGracePeriodEndsAt = &ends
		usage.WorkerTasksBlocked = !ends.After(time.Now().UTC())
	}
	usage.CanRegister = usage.Unlimited || usage.UsedClusters < usage.MaxClusters
	return usage, nil
}

func workerTaskRequiresCapacity(taskType string) bool {
	switch taskType {
	case "backup", "drill":
		return true
	default:
		return false
	}
}

func CheckWorkerTaskAdmission(ctx context.Context, tx *sql.Tx, tenantID, taskType string, now time.Time) error {
	if !workerTaskRequiresCapacity(taskType) {
		return nil
	}
	var workerNodes int
	if err := tx.QueryRowContext(ctx, `select coalesce(sum(greatest(node_count,0)),0) from clusters where tenant_id=$1`, tenantID).Scan(&workerNodes); err != nil {
		return err
	}
	maxNodes := 2
	var subscriptionStatus string
	var subscriptionMax int
	var paidUntil, periodStart sql.NullTime
	err := tx.QueryRowContext(ctx, `select s.status,s.max_clusters,s.paid_until,s.period_start from tenant_subscriptions s join tenant_entitlements e on e.tenant_id=s.tenant_id and e.source='subscription' where s.tenant_id=$1`, tenantID).Scan(&subscriptionStatus, &subscriptionMax, &paidUntil, &periodStart)
	if err == nil && paidUntil.Valid && paidUntil.Time.After(now) && !periodStart.Time.After(now) && (subscriptionStatus == "active" || subscriptionStatus == "past_due" || subscriptionStatus == "canceled") {
		maxNodes = subscriptionMax
	} else if !errors.Is(err, sql.ErrNoRows) && err != nil {
		return err
	}
	var overageStarted sql.NullTime
	if err := tx.QueryRowContext(ctx, `select overage_started_at from tenant_worker_entitlement_state where tenant_id=$1 for update`, tenantID).Scan(&overageStarted); err != nil {
		return err
	}
	if maxNodes == 0 || workerNodes <= maxNodes {
		_, err = tx.ExecContext(ctx, `update tenant_worker_entitlement_state set overage_started_at=null,updated_at=$2 where tenant_id=$1`, tenantID, now)
		return err
	}
	if !overageStarted.Valid {
		_, err = tx.ExecContext(ctx, `update tenant_worker_entitlement_state set overage_started_at=$2,updated_at=$2 where tenant_id=$1`, tenantID, now)
		return err
	}
	if overageStarted.Time.Add(30 * 24 * time.Hour).After(now) {
		return nil
	}
	return fmt.Errorf("%w: using %d worker nodes with a %d-node quota; the 30-day grace period has ended", ErrWorkerNodeCapacity, workerNodes, maxNodes)
}

func ActivateTenantLicense(ctx context.Context, db *sql.DB, verified licensing.Verified, raw, actorID string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `select pg_advisory_xact_lock(hashtext($1))`, verified.TenantID); err != nil {
		return false, err
	}
	var revision int64
	var existingID, existingRaw string
	err = tx.QueryRowContext(ctx, `select l.revision,l.id,l.signed_license from tenant_entitlements e join tenant_licenses l on l.id=e.license_id and l.tenant_id=e.tenant_id where e.tenant_id=$1`, verified.TenantID).Scan(&revision, &existingID, &existingRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if existingID == verified.ID && existingRaw == raw {
		return false, tx.Commit()
	}
	if revision >= verified.Revision {
		return false, ErrLicenseRevision
	}
	_, err = tx.ExecContext(ctx, `insert into tenant_licenses(id,tenant_id,revision,max_clusters,issued_at,valid_from,valid_until,key_id,signed_license,activated_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,'')::uuid)`, verified.ID, verified.TenantID, verified.Revision, verified.MaxClusters, verified.IssuedAt, verified.ValidFrom, verified.ValidUntil, verified.KeyID, raw, actorID)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return false, ErrLicenseRevision
		}
		return false, err
	}
	result, err := tx.ExecContext(ctx, `update tenant_entitlements set license_id=$2,source='license',updated_at=now() where tenant_id=$1`, verified.TenantID, verified.ID)
	if err != nil {
		return false, err
	}
	if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
		return false, errors.New("organization entitlement not found")
	}
	return true, tx.Commit()
}
