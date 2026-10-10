package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

type PostgresStore struct {
	db               *sql.DB
	diagnosticWriter DiagnosticLogWriter
	secretKey        string
}

type DiagnosticLogWriter interface {
	CreateDiagnosticLog(DiagnosticLogInput) (DiagnosticLog, error)
}

func (s *PostgresStore) SetDiagnosticLogWriter(writer DiagnosticLogWriter) {
	s.diagnosticWriter = writer
}

type EditionMigration struct {
	Version string
	SQL     string
}

// ApplyEditionMigrations runs only after the embedded Community migration set.
// Edition versions use a separate ledger so repositories retain ownership of
// their own forward-only history.
func (s *PostgresStore) ApplyEditionMigrations(ctx context.Context, migrations []EditionMigration) error {
	if len(migrations) == 0 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `create table if not exists enterprise_schema_migrations (version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	for _, migration := range migrations {
		version := strings.TrimSpace(migration.Version)
		if version == "" || strings.TrimSpace(migration.SQL) == "" {
			return errors.New("edition migration version and SQL are required")
		}
		var applied bool
		if err := s.db.QueryRowContext(ctx, `select exists(select 1 from enterprise_schema_migrations where version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, migration.SQL); err == nil {
			_, err = tx.ExecContext(ctx, `insert into enterprise_schema_migrations(version) values($1)`, version)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply edition migration %s: %w", version, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	return newPostgresStore(ctx, databaseURL, true)
}

// NewPostgresStoreWithoutMigrations is for auxiliary processes such as the
// external upgrader. The platform API owns schema migration so two containers
// can never race while applying DDL during first startup.
func NewPostgresStoreWithoutMigrations(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	return newPostgresStore(ctx, databaseURL, false)
}

func newPostgresStore(ctx context.Context, databaseURL string, initialize bool) (*PostgresStore, error) {
	// The platform persists instants as UTC and emits UTC over the API. User-local
	// presentation is handled exclusively by the frontend My Time Zone setting.
	if parsed, err := url.Parse(databaseURL); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		query.Set("timezone", "UTC")
		parsed.RawQuery = query.Encode()
		databaseURL = parsed.String()
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &PostgresStore{db: db}
	if initialize {
		if err := migrations.Run(ctx, db); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := store.ensureDefaultTenant(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := store.ensureDefaultAdmin(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return store, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) ListTenants() ([]Tenant, error) {
	rows, err := s.db.Query(`
		select t.id,t.name,coalesce(t.description,''),t.status,t.created_at,t.updated_at,
		       count(distinct u.id),count(distinct c.id)
		from resource_scopes t
		left join users u on u.tenant_id=t.id
		left join clusters c on c.tenant_id=t.id
		group by t.id,t.name,t.description,t.status,t.created_at,t.updated_at
		order by lower(t.name),t.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Tenant
	for rows.Next() {
		var item Tenant
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.UserCount, &item.ClusterCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetTenant(id string) (Tenant, bool, error) {
	var item Tenant
	err := s.db.QueryRow(`select t.id,t.name,coalesce(t.description,''),t.status,t.created_at,t.updated_at,(select count(*) from users u where u.tenant_id=t.id),(select count(*) from clusters c where c.tenant_id=t.id) from resource_scopes t where t.id=$1`, id).Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.UserCount, &item.ClusterCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Tenant{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) CreateTenant(input TenantInput) (Tenant, error) {
	if len(strings.TrimSpace(input.Description)) > 500 {
		return Tenant{}, errors.New("tenant description must not exceed 500 characters")
	}
	status := input.Status
	if status != "disabled" {
		status = "active"
	}
	var item Tenant
	err := s.db.QueryRow(`insert into resource_scopes(id,name,description,status) values($1,$2,nullif($3,''),$4) returning id,name,coalesce(description,''),status,created_at,updated_at`, newID(), strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), status).Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *PostgresStore) UpdateTenant(id string, input TenantInput) (Tenant, bool, error) {
	if len(strings.TrimSpace(input.Description)) > 500 {
		return Tenant{}, false, errors.New("tenant description must not exceed 500 characters")
	}
	status := input.Status
	if status != "disabled" {
		status = "active"
	}
	var item Tenant
	err := s.db.QueryRow(`update resource_scopes set name=$2,description=nullif($3,''),status=$4,updated_at=now() where id=$1 returning id,name,coalesce(description,''),status,created_at,updated_at`, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), status).Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Tenant{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) DeleteTenant(id string) (bool, bool, error) {
	if id == DefaultTenantID {
		return false, true, nil
	}
	var inUse bool
	err := s.db.QueryRow(`select exists(select 1 from users where tenant_id=$1) or exists(select 1 from clusters where tenant_id=$1) or exists(select 1 from storage_repositories where tenant_id=$1) or exists(select 1 from policies where tenant_id=$1) or exists(select 1 from protection_plans where tenant_id=$1)`, id).Scan(&inUse)
	if err != nil || inUse {
		return false, inUse, err
	}
	result, err := s.db.Exec(`delete from resource_scopes where id=$1`, id)
	if err != nil {
		return false, false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, false, nil
}

func (s *PostgresStore) GetPlatformSettings() (PlatformSettings, bool, error) {
	var item PlatformSettings
	err := s.db.QueryRow(`select tenant_id,platform_instance_id,image_registry,agent_namespace,velero_version,coalesce(public_endpoint,''),created_at,updated_at from platform_settings where tenant_id=$1`, DefaultTenantID).Scan(&item.TenantID, &item.InstanceID, &item.ImageRegistry, &item.AgentNamespace, &item.VeleroVersion, &item.PublicEndpoint, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformSettings{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) UpsertPlatformSettings(input PlatformSettingsInput) (PlatformSettings, error) {
	var item PlatformSettings
	err := s.db.QueryRow(`insert into platform_settings(tenant_id,platform_instance_id,image_registry,agent_namespace,velero_version,public_endpoint) values($1,coalesce(nullif($2,''),md5(random()::text || clock_timestamp()::text)),$3,$4,$5,nullif($6,'')) on conflict(tenant_id) do update set image_registry=excluded.image_registry,agent_namespace=excluded.agent_namespace,velero_version=excluded.velero_version,public_endpoint=excluded.public_endpoint,updated_at=now() returning tenant_id,platform_instance_id,image_registry,agent_namespace,velero_version,coalesce(public_endpoint,''),created_at,updated_at`, DefaultTenantID, input.InstanceID, strings.TrimRight(strings.TrimSpace(input.ImageRegistry), "/"), input.AgentNamespace, input.VeleroVersion, strings.TrimRight(strings.TrimSpace(input.PublicEndpoint), "/")).Scan(&item.TenantID, &item.InstanceID, &item.ImageRegistry, &item.AgentNamespace, &item.VeleroVersion, &item.PublicEndpoint, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *PostgresStore) CreateAuditLog(input AuditLogInput) (AuditLog, error) {
	payload := input.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payload["actor"] = input.Actor
	payload["resourceName"] = input.ResourceName
	payload["result"] = input.Result
	payload["message"] = input.Message
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return AuditLog{}, err
	}
	tenantID := DefaultTenantID
	if strings.TrimSpace(input.ActorID) != "" {
		_ = s.db.QueryRow(`select tenant_id from users where id=$1`, input.ActorID).Scan(&tenantID)
	}
	item := AuditLog{ID: newID(), TenantID: tenantID, ActorID: input.ActorID, Actor: input.Actor, Action: input.Action, ResourceType: input.ResourceType, ResourceID: input.ResourceID, ResourceName: input.ResourceName, Result: input.Result, Message: input.Message, Payload: payload}
	var actorID any
	if strings.TrimSpace(input.ActorID) != "" {
		actorID = input.ActorID
	}
	var resourceID any
	if strings.TrimSpace(input.ResourceID) != "" {
		resourceID = input.ResourceID
	}
	err = s.db.QueryRow(`insert into audit_logs(id,tenant_id,actor_id,action,resource_type,resource_id,payload) values($1,$2,$3,$4,$5,$6,$7) returning created_at`, item.ID, item.TenantID, actorID, item.Action, item.ResourceType, resourceID, payloadRaw).Scan(&item.CreatedAt)
	return item, err
}

func (s *PostgresStore) ListAuditLogs(limit, offset int) ([]AuditLog, error) {
	return s.ListTenantAuditLogs("", limit, offset)
}

// Tenant filtering must precede pagination, including for system administrators.
func (s *PostgresStore) ListTenantAuditLogs(tenantID string, limit, offset int) ([]AuditLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`select a.id,a.tenant_id,coalesce(a.actor_id::text,''),coalesce(u.email,''),a.action,a.resource_type,coalesce(a.resource_id::text,''),a.payload,a.created_at from audit_logs a left join users u on u.id=a.actor_id where a.actor_id is not null and a.action not in ('Create Cluster Registration Token','Start Cluster Registration') and ($3 = '' or a.tenant_id::text = $3) order by a.created_at desc,a.id desc limit $1 offset $2`, limit, offset, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuditLog{}
	for rows.Next() {
		var item AuditLog
		var actorEmail string
		var payloadRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ActorID, &actorEmail, &item.Action, &item.ResourceType, &item.ResourceID, &payloadRaw, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payloadRaw, &item.Payload)
		item.Actor = firstString(item.Payload, "actor", actorEmail)
		item.ResourceName = firstString(item.Payload, "resourceName", "")
		item.Result = firstString(item.Payload, "result", "Success")
		item.Message = firstString(item.Payload, "message", "")
		items = append(items, item)
	}
	return items, rows.Err()
}

func firstString(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (s *PostgresStore) ensureDefaultTenant(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		insert into resource_scopes (id, name, status)
		values ($1, 'Admin', 'active')
		on conflict (id) do nothing
	`, DefaultTenantID)
	return err
}

func (s *PostgresStore) ensureDefaultAdmin(ctx context.Context) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(DefaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		insert into users (id, tenant_id, email, password_hash, role, status, is_system_admin, must_change_password, created_at, updated_at)
		values ($1, $2, $3, $4, 'admin', 'active', true, true, $5, $5)
		on conflict (tenant_id, email) do nothing
	`, newID(), DefaultTenantID, DefaultAdminEmail, string(passwordHash), time.Now().UTC())
	return err
}

func (s *PostgresStore) CreateAgentToken(tenantID, createdBy, description string, ttl time.Duration, requestedType ...string) (AgentToken, error) {
	now := time.Now().UTC()
	token := AgentToken{
		ID:          newID(),
		TenantID:    tenantID,
		CreatedBy:   createdBy,
		Token:       "hcdr_" + newID() + newID(),
		Description: description,
		ExpiresAt:   now.Add(ttl),
		ClusterType: normalizedClusterType(firstRequestedString(requestedType)),
	}

	_, err := s.db.Exec(`
		insert into agent_tokens (id, tenant_id, token_hash, description, expires_at, created_by, created_at, cluster_type)
		values ($1, $2, $3, $4, $5, nullif($6,'')::uuid, $7, $8)
	`, token.ID, token.TenantID, token.Token, token.Description, token.ExpiresAt, token.CreatedBy, now, token.ClusterType)
	return token, err
}

func (s *PostgresStore) ValidateAgentToken(value string) error {
	var expiresAt time.Time
	var usedAt sql.NullTime
	err := s.db.QueryRow(`
		select expires_at, used_at
		from agent_tokens
		where token_hash = $1 and revoked_at is null
	`, value).Scan(&expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTokenInvalid
	}
	if err != nil {
		return err
	}
	if usedAt.Valid {
		return ErrTokenUsed
	}
	if time.Now().UTC().After(expiresAt) {
		return ErrTokenExpired
	}
	return nil
}

func (s *PostgresStore) ValidateDisasterHandoverToken(value string) error {
	var description string
	var expiresAt time.Time
	var usedAt sql.NullTime
	err := s.db.QueryRow(`select coalesce(description,''),expires_at,used_at from agent_tokens where token_hash=$1 and revoked_at is null`, value).Scan(&description, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !strings.HasPrefix(description, "disaster-handover:")) {
		return ErrTokenInvalid
	}
	if err != nil {
		return err
	}
	if usedAt.Valid {
		return ErrTokenUsed
	}
	if time.Now().UTC().After(expiresAt) {
		return ErrTokenExpired
	}
	return nil
}

func migrationTokenHash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func (s *PostgresStore) CreateCommunityMigrationAuthorization(createdBy string, ttl time.Duration) (CommunityMigrationAuthorization, error) {
	now := time.Now().UTC()
	item := CommunityMigrationAuthorization{ID: newID(), Token: "hcmig_" + newID() + newID(), CreatedBy: createdBy, ExpiresAt: now.Add(ttl)}
	_, err := s.db.Exec(`insert into community_migration_authorizations(id,token_hash,created_by,expires_at,created_at) values($1,$2,nullif($3,'')::uuid,$4,$5)`, item.ID, migrationTokenHash(item.Token), item.CreatedBy, item.ExpiresAt, now)
	return item, err
}

func (s *PostgresStore) ConsumeCommunityMigrationAuthorization(token, targetInstanceID, protocolVersion, targetPublicKey string, ttl time.Duration) (CommunityMigrationSession, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return CommunityMigrationSession{}, err
	}
	defer tx.Rollback()
	var authorizationID string
	var expiresAt time.Time
	var usedAt sql.NullTime
	err = tx.QueryRow(`select id,expires_at,used_at from community_migration_authorizations where token_hash=$1 and revoked_at is null for update`, migrationTokenHash(token)).Scan(&authorizationID, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CommunityMigrationSession{}, ErrTokenInvalid
	}
	if err != nil {
		return CommunityMigrationSession{}, err
	}
	if usedAt.Valid {
		return CommunityMigrationSession{}, ErrTokenUsed
	}
	if now.After(expiresAt) {
		return CommunityMigrationSession{}, ErrTokenExpired
	}
	if _, err = tx.Exec(`update community_migration_sessions set state='expired',frozen=false,completed_at=$1,updated_at=$1 where expires_at<=$1 and state not in ('committed','rolled-back','failed','revoked','expired')`, now); err != nil {
		return CommunityMigrationSession{}, err
	}
	var active int
	if err = tx.QueryRow(`select count(*) from community_migration_sessions where state not in ('committed','rolled-back','failed','revoked','expired')`).Scan(&active); err != nil {
		return CommunityMigrationSession{}, err
	}
	if active > 0 {
		return CommunityMigrationSession{}, errors.New("another community migration is active")
	}
	var sourceInstanceID string
	if err = tx.QueryRow(`select platform_instance_id from platform_settings where tenant_id=$1`, DefaultTenantID).Scan(&sourceInstanceID); err != nil {
		return CommunityMigrationSession{}, err
	}
	item := CommunityMigrationSession{ID: newID(), SessionToken: "hcms_" + newID() + newID(), SourceInstanceID: sourceInstanceID, TargetInstanceID: targetInstanceID, ProtocolVersion: protocolVersion, TargetPublicKey: targetPublicKey, State: "prechecking", ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now}
	_, err = tx.Exec(`insert into community_migration_sessions(id,authorization_id,session_token_hash,source_instance_id,target_instance_id,protocol_version,target_public_key,state,expires_at,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, item.ID, authorizationID, migrationTokenHash(item.SessionToken), item.SourceInstanceID, item.TargetInstanceID, item.ProtocolVersion, item.TargetPublicKey, item.State, item.ExpiresAt, now)
	if err != nil {
		return CommunityMigrationSession{}, err
	}
	if _, err = tx.Exec(`update community_migration_authorizations set used_at=$1 where id=$2`, now, authorizationID); err != nil {
		return CommunityMigrationSession{}, err
	}
	if err = tx.Commit(); err != nil {
		return CommunityMigrationSession{}, err
	}
	return item, nil
}

func (s *PostgresStore) AuthenticateCommunityMigrationSession(token string) (CommunityMigrationSession, bool, error) {
	var item CommunityMigrationSession
	err := s.db.QueryRow(`select id,source_instance_id,target_instance_id,protocol_version,target_public_key,state,frozen,expires_at,coalesce(last_error_code,''),coalesce(last_error_message,''),created_at,updated_at from community_migration_sessions where session_token_hash=$1`, migrationTokenHash(token)).Scan(&item.ID, &item.SourceInstanceID, &item.TargetInstanceID, &item.ProtocolVersion, &item.TargetPublicKey, &item.State, &item.Frozen, &item.ExpiresAt, &item.LastErrorCode, &item.LastErrorMessage, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && time.Now().UTC().After(item.ExpiresAt)) {
		return CommunityMigrationSession{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) UpdateCommunityMigrationState(id, state, reason, message string, frozen bool) (CommunityMigrationSession, bool, error) {
	var item CommunityMigrationSession
	err := s.db.QueryRow(`update community_migration_sessions set state=$2,frozen=$3,last_error_code=nullif($4,''),last_error_message=nullif($5,''),updated_at=now(),completed_at=case when $2 in ('committed','rolled-back','failed','revoked') then now() else completed_at end,observation_ends_at=case when $2='committed' then now()+interval '7 days' else observation_ends_at end where id=$1 returning id,source_instance_id,target_instance_id,protocol_version,state,frozen,expires_at,coalesce(last_error_code,''),coalesce(last_error_message,''),created_at,updated_at`, id, state, frozen, reason, message).Scan(&item.ID, &item.SourceInstanceID, &item.TargetInstanceID, &item.ProtocolVersion, &item.State, &item.Frozen, &item.ExpiresAt, &item.LastErrorCode, &item.LastErrorMessage, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CommunityMigrationSession{}, false, nil
	}
	if err != nil {
		return item, false, err
	}
	_, err = s.db.Exec(`insert into community_migration_events(migration_id,state,reason,message) values($1,$2,$3,$4)`, id, state, reason, message)
	return item, err == nil, err
}

func (s *PostgresStore) ListCommunityMigrationSessions() ([]CommunityMigrationSession, error) {
	_, _ = s.db.Exec(`update community_migration_sessions set state='expired',frozen=false,completed_at=now(),updated_at=now() where expires_at<=now() and state not in ('committed','rolled-back','failed','revoked','expired')`)
	rows, err := s.db.Query(`select id,source_instance_id,target_instance_id,protocol_version,state,frozen,expires_at,coalesce(last_error_code,''),coalesce(last_error_message,''),created_at,updated_at from community_migration_sessions order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CommunityMigrationSession{}
	for rows.Next() {
		var item CommunityMigrationSession
		if err = rows.Scan(&item.ID, &item.SourceInstanceID, &item.TargetInstanceID, &item.ProtocolVersion, &item.State, &item.Frozen, &item.ExpiresAt, &item.LastErrorCode, &item.LastErrorMessage, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) HasCommunityMigrationFreeze() (bool, error) {
	var frozen bool
	err := s.db.QueryRow(`select exists(select 1 from community_migration_sessions where frozen=true and expires_at>now() and state not in ('committed','rolled-back','failed','revoked','expired'))`).Scan(&frozen)
	return frozen, err
}

func (s *PostgresStore) RegisterCluster(input RegisterClusterInput) (Cluster, string, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Cluster{}, "", err
	}
	defer tx.Rollback()

	var token AgentToken
	var usedAt sql.NullTime
	err = tx.QueryRow(`
		select id, tenant_id, token_hash, coalesce(description, ''), expires_at, used_at, coalesce(cluster_id::text,''), cluster_type
		from agent_tokens
		where token_hash = $1 and revoked_at is null
		for update
	`, input.Token).Scan(&token.ID, &token.TenantID, &token.Token, &token.Description, &token.ExpiresAt, &usedAt, &token.ClusterID, &token.ClusterType)
	if errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, "", ErrTokenInvalid
	}
	if err != nil {
		return Cluster{}, "", err
	}
	if usedAt.Valid {
		return Cluster{}, "", ErrTokenUsed
	}
	if now.After(token.ExpiresAt) {
		return Cluster{}, "", ErrTokenExpired
	}
	// Serialize registrations for a tenant so two first registrations cannot both
	// attempt to become the default cluster.
	if _, err = tx.Exec(`select pg_advisory_xact_lock(hashtext($1))`, token.TenantID); err != nil {
		return Cluster{}, "", err
	}
	if input.ClusterType != "" && normalizedClusterType(input.ClusterType) != normalizedClusterType(token.ClusterType) {
		return Cluster{}, "", errors.New("registration cluster type does not match install token")
	}
	if token.ClusterID != "" {
		if !strings.HasPrefix(token.Description, "community-migration-handover:") {
			return Cluster{}, "", errors.New("pre-bound cluster token is not a migration handover token")
		}
		var cluster Cluster
		var createdAt, updatedAt time.Time
		err = tx.QueryRow(`update clusters set kube_version=nullif($3,''),connection_status='online',agent_version=nullif($4,''),velero_version=nullif($5,''),velero_status=nullif($6,''),registered_at=coalesce(registered_at,$7),last_seen_at=$7,updated_at=$7 where id=$1 and tenant_id=$2 returning id,tenant_id,name,coalesce(kube_version,''),status,connection_status,coalesce(agent_version,''),coalesce(velero_version,''),coalesce(velero_status,''),role,is_default,registered_at,last_seen_at,created_at,updated_at`, token.ClusterID, token.TenantID, input.KubeVersion, input.AgentVersion, input.VeleroVersion, input.VeleroStatus, now).Scan(&cluster.ID, &cluster.TenantID, &cluster.Name, &cluster.KubeVersion, &cluster.Status, &cluster.ConnectionStatus, &cluster.AgentVersion, &cluster.VeleroVersion, &cluster.VeleroStatus, &cluster.Role, &cluster.IsDefault, &cluster.RegisteredAt, &cluster.LastSeenAt, &createdAt, &updatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return Cluster{}, "", errors.New("migration handover cluster is not staged")
		}
		if err != nil {
			return Cluster{}, "", err
		}
		if _, err = tx.Exec(`update agent_tokens set used_at=$1 where id=$2`, now, token.ID); err != nil {
			return Cluster{}, "", err
		}
		credential := "cred_" + newID() + newID()
		if _, err = tx.Exec(`insert into agent_credentials(id,tenant_id,cluster_id,credential_hash,status,created_at) values($1,$2,$3,$4,'active',$5)`, newID(), token.TenantID, cluster.ID, credential, now); err != nil {
			return Cluster{}, "", err
		}
		if err = tx.Commit(); err != nil {
			return Cluster{}, "", err
		}
		return cluster, credential, nil
	}

	clusterName := input.ClusterName
	if strings.TrimSpace(clusterName) == "" || clusterName == "unknown-cluster" || clusterName == "unnamed cluster" {
		clusterName = "registered-cluster"
	}
	clusterID := newID()
	var duplicateName bool
	if err := tx.QueryRow(`select exists(select 1 from clusters where tenant_id=$1 and lower(name)=lower($2))`, token.TenantID, clusterName).Scan(&duplicateName); err != nil {
		return Cluster{}, "", err
	}
	if duplicateName {
		if strings.TrimSpace(input.ControlPlaneIP) != "" {
			clusterName = fmt.Sprintf("%s (%s)", clusterName, strings.TrimSpace(input.ControlPlaneIP))
		} else {
			clusterName = fmt.Sprintf("%s-%s", clusterName, clusterID[:8])
		}
		var duplicateResolvedName bool
		if err := tx.QueryRow(`select exists(select 1 from clusters where tenant_id=$1 and lower(name)=lower($2))`, token.TenantID, clusterName).Scan(&duplicateResolvedName); err != nil {
			return Cluster{}, "", err
		}
		if duplicateResolvedName {
			clusterName = fmt.Sprintf("%s-%s", clusterName, clusterID[:8])
		}
	}

	var hasDefault bool
	if err := tx.QueryRow(`select exists(select 1 from clusters where tenant_id = $1 and is_default)`, token.TenantID).Scan(&hasDefault); err != nil {
		return Cluster{}, "", err
	}

	cluster := Cluster{
		ID:               clusterID,
		TenantID:         token.TenantID,
		Name:             clusterName,
		ClusterType:      normalizedClusterType(token.ClusterType),
		CloudProvider:    input.CloudProvider,
		CloudRegion:      input.CloudRegion,
		CloudClusterID:   input.CloudClusterID,
		KubeVersion:      input.KubeVersion,
		Status:           "healthy",
		ConnectionStatus: "online",
		AgentVersion:     input.AgentVersion,
		VeleroVersion:    input.VeleroVersion,
		VeleroStatus:     input.VeleroStatus,
		NodeCount:        input.NodeCount,
		Role:             "both",
		IsDefault:        !hasDefault,
		RegisteredAt:     now,
		LastSeenAt:       now,
	}

	_, err = tx.Exec(`
		insert into clusters (
			id, tenant_id, name, kube_version, status, connection_status,
			agent_version, velero_version, velero_status, registered_at, last_seen_at,
			role, is_default, node_count, created_at, updated_at, cluster_type, cloud_provider, cloud_region, cloud_cluster_id
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15, $16, nullif($17,''), nullif($18,''), nullif($19,''))
	`, cluster.ID, cluster.TenantID, cluster.Name, cluster.KubeVersion, cluster.Status,
		cluster.ConnectionStatus, cluster.AgentVersion, cluster.VeleroVersion, cluster.VeleroStatus,
		cluster.RegisteredAt, cluster.LastSeenAt, cluster.Role, cluster.IsDefault, cluster.NodeCount, now,
		cluster.ClusterType, cluster.CloudProvider, cluster.CloudRegion, cluster.CloudClusterID)
	if err != nil {
		return Cluster{}, "", err
	}

	_, err = tx.Exec(`
		update agent_tokens set used_at = $1, cluster_id = $2 where id = $3
	`, now, cluster.ID, token.ID)
	if err != nil {
		return Cluster{}, "", err
	}

	credential := "cred_" + newID() + newID()
	_, err = tx.Exec(`
		insert into agent_credentials (id, tenant_id, cluster_id, credential_hash, status, created_at)
		values ($1, $2, $3, $4, 'active', $5)
	`, newID(), token.TenantID, cluster.ID, credential, now)
	if err != nil {
		return Cluster{}, "", err
	}

	if err := tx.Commit(); err != nil {
		return Cluster{}, "", err
	}
	return cluster, credential, nil
}

func (s *PostgresStore) AuthenticateAgentCredential(input AgentCredentialInput) (Cluster, bool, error) {
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		update agent_credentials
		set last_used_at = $3
		where cluster_id = $1 and credential_hash = $2 and status = 'active' and revoked_at is null
	`, input.ClusterID, input.Credential, now)
	if err != nil {
		return Cluster{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Cluster{}, false, err
	}
	if affected == 0 {
		return Cluster{}, false, nil
	}
	_, err = s.db.Exec(`
		update clusters
		set connection_status = 'online', last_seen_at = $2, updated_at = $2
		where id = $1
	`, input.ClusterID, now)
	if err != nil {
		return Cluster{}, false, err
	}
	return s.getCluster(input.ClusterID)
}

func (s *PostgresStore) ListClusters() ([]Cluster, error) {
	rows, err := s.db.Query(`
		select id, tenant_id, name, coalesce(kube_version, ''), status, connection_status,
		       node_count, namespace_count, application_count, 0 as active_tasks,
		       coalesce(agent_version, ''), coalesce(velero_version, ''), coalesce(velero_status, ''),
		       coalesce(metadata->>'inventoryHash', ''), role, is_default,
		       coalesce(registered_at, created_at), coalesce(last_seen_at, created_at), metadata,
		       cluster_type, coalesce(cloud_provider,''), coalesce(cloud_region,''), coalesce(cloud_cluster_id,'')
		from clusters
		order by created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var cluster Cluster
		var metadataRaw []byte
		if err := rows.Scan(
			&cluster.ID, &cluster.TenantID, &cluster.Name, &cluster.KubeVersion, &cluster.Status,
			&cluster.ConnectionStatus, &cluster.NodeCount, &cluster.NamespaceCount, &cluster.ApplicationCount,
			&cluster.ActiveTasks, &cluster.AgentVersion, &cluster.VeleroVersion, &cluster.VeleroStatus,
			&cluster.InventoryHash, &cluster.Role, &cluster.IsDefault, &cluster.RegisteredAt, &cluster.LastSeenAt, &metadataRaw,
			&cluster.ClusterType, &cluster.CloudProvider, &cluster.CloudRegion, &cluster.CloudClusterID,
		); err != nil {
			return nil, err
		}
		applyClusterMetadata(&cluster, metadataRaw)
		applyClusterConnectionFreshness(&cluster)
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func (s *PostgresStore) UpdateCluster(input ClusterUpdateInput) (Cluster, bool, error) {
	now := time.Now().UTC()
	role := ""
	if input.Role != "" {
		role = normalizeClusterRole(input.Role)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Cluster{}, false, err
	}
	defer tx.Rollback()
	var tenantID string
	if err := tx.QueryRow(`select tenant_id from clusters where id=$1`, input.ID).Scan(&tenantID); errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, false, nil
	} else if err != nil {
		return Cluster{}, false, err
	}
	if input.IsDefault != nil && !*input.IsDefault {
		return Cluster{}, false, ErrDefaultClusterRequired
	}

	if input.IsDefault != nil && *input.IsDefault {
		if _, err := tx.Exec(`
			update clusters set is_default = false, updated_at = $2 where tenant_id = $1
		`, tenantID, now); err != nil {
			return Cluster{}, false, err
		}
	}

	result, err := tx.Exec(`
		update clusters
		set name = coalesce(nullif($2, ''), name),
		    role = coalesce(nullif($3, ''), role),
		    is_default = case when $4 then $5 else is_default end,
		    updated_at = $6
		where id = $1
	`, input.ID, input.Name, role, input.IsDefault != nil, boolValue(input.IsDefault), now)
	if err != nil {
		return Cluster{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Cluster{}, false, err
	}
	if affected == 0 {
		return Cluster{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return Cluster{}, false, err
	}
	return s.getCluster(input.ID)
}

func (s *PostgresStore) SetClusterConnectionStatus(clusterID string, status string) (Cluster, bool, error) {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "offline"
	}
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		update clusters
		   set connection_status = $2,
		       updated_at = $3
		 where id = $1
	`, clusterID, status, now)
	if err != nil {
		return Cluster{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Cluster{}, false, err
	}
	if affected == 0 {
		return Cluster{}, false, nil
	}
	return s.getCluster(clusterID)
}

func (s *PostgresStore) SetDefaultCluster(clusterID string) (Cluster, bool, error) {
	value := true
	return s.UpdateCluster(ClusterUpdateInput{ID: clusterID, IsDefault: &value})
}

func (s *PostgresStore) UpdateApplication(input ApplicationUpdateInput) (Application, bool, error) {
	now := time.Now().UTC()
	protection := strings.TrimSpace(input.ProtectionStatus)
	if protection == "" {
		protection = "unprotected"
	}
	switch protection {
	case "unprotected", "pending_protection", "protected":
	default:
		return Application{}, false, errors.New("invalid_protection_status")
	}
	result, err := s.db.Exec(`
		update applications
		set protection_status = $2,
		    updated_at = $3
		where id = $1
	`, input.ID, protection, now)
	if err != nil {
		return Application{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Application{}, false, err
	}
	if affected == 0 {
		return Application{}, false, nil
	}
	apps, err := s.ListApplications("")
	if err != nil {
		return Application{}, false, err
	}
	for _, app := range apps {
		if app.ID == input.ID {
			return app, true, nil
		}
	}
	return Application{}, false, nil
}

func (s *PostgresStore) DeleteCluster(clusterID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var exists, wasDefault bool
	var tenantID, clusterName string
	if err := tx.QueryRow(`select true, is_default, tenant_id, name from clusters where id = $1`, clusterID).Scan(&exists, &wasDefault, &tenantID, &clusterName); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		update tasks
		set payload = jsonb_set(coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb), '{archivedRestorePointId}', to_jsonb(restore_point_id::text), true),
		    restore_point_id = null
		where restore_point_id in (
			select id from restore_points
			where source_cluster_id = $1
			   or protection_plan_id in (
					select id from protection_plans
					where source_cluster_id = $1 or target_cluster_id = $1
			   )
		)
	`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		update tasks
		set payload = jsonb_set(coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb), '{archivedProtectionPlanId}', to_jsonb(protection_plan_id::text), true),
		    protection_plan_id = null
		where protection_plan_id in (
			select id from protection_plans
			where source_cluster_id = $1 or target_cluster_id = $1
		)
	`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		update tasks
		set payload = jsonb_set(coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb), '{archivedAppId}', to_jsonb(app_id::text), true),
		    app_id = null
		where app_id in (select id from applications where cluster_id = $1)
	`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		delete from restore_points
		where source_cluster_id = $1
		   or protection_plan_id in (
				select id from protection_plans
				where source_cluster_id = $1 or target_cluster_id = $1
		   )
	`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from protection_plans where source_cluster_id = $1 or target_cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from applications where cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		update tasks
		set payload = coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb) || jsonb_build_object(
		        'archivedClusterId', cluster_id::text,
		        'archivedClusterName', $2::text
		    ),
		    cluster_id = null
		where cluster_id = $1
	`, clusterID, clusterName); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from cluster_nodes where cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from agent_sessions where cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from agent_tokens where cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	result, err := tx.Exec(`delete from clusters where id = $1`, clusterID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	// Repair both the normal default deletion path and any historical state in
	// which remaining clusters have no default.
	if _, err := tx.Exec(`
			update clusters
			set is_default = true, updated_at = now()
			where id = (
				select id from clusters
				where tenant_id = $1
				  and not exists (select 1 from clusters where tenant_id = $1 and is_default)
				order by registered_at asc, id asc
				limit 1
			)
		`, tenantID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}

func applyClusterMetadata(cluster *Cluster, metadataRaw []byte) {
	if len(metadataRaw) == 0 {
		return
	}
	var metadata struct {
		Nodes                      []ClusterNode         `json:"nodes"`
		StorageClasses             []ClusterStorageClass `json:"storageClasses"`
		APIResources               []ClusterAPIResource  `json:"apiResources"`
		NamespaceAPIs              []ClusterNamespaceAPI `json:"namespaceAPIs"`
		Capabilities               []ClusterCapability   `json:"capabilities"`
		CapabilitiesCollectedAt    time.Time             `json:"capabilitiesCollectedAt"`
		CapabilitiesComplete       bool                  `json:"capabilitiesComplete"`
		AgentImage                 string                `json:"agentImage"`
		AgentImageID               string                `json:"agentImageId"`
		AgentImageDigest           string                `json:"agentImageDigest"`
		LatestAgentVersion         string                `json:"latestAgentVersion"`
		LatestAgentImage           string                `json:"latestAgentImage"`
		LatestAgentImageDigest     string                `json:"latestAgentImageDigest"`
		VeleroImage                string                `json:"veleroImage"`
		VeleroImageDigest          string                `json:"veleroImageDigest"`
		VeleroServerReady          bool                  `json:"veleroServerReady"`
		VeleroNodeAgentDesired     int32                 `json:"veleroNodeAgentDesired"`
		VeleroNodeAgentReady       int32                 `json:"veleroNodeAgentReady"`
		VeleroNodeAgentImageDigest string                `json:"veleroNodeAgentImageDigest"`
		AgentUpgradeStatus         string                `json:"agentUpgradeStatus"`
	}
	if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
		return
	}
	cluster.Nodes = metadata.Nodes
	cluster.StorageClasses = metadata.StorageClasses
	cluster.APIResources = metadata.APIResources
	cluster.NamespaceAPIs = metadata.NamespaceAPIs
	cluster.Capabilities = metadata.Capabilities
	cluster.CapabilitiesCollectedAt = metadata.CapabilitiesCollectedAt
	cluster.CapabilitiesComplete = metadata.CapabilitiesComplete
	cluster.AgentImage = metadata.AgentImage
	cluster.AgentImageID = metadata.AgentImageID
	cluster.AgentImageDigest = metadata.AgentImageDigest
	cluster.LatestAgentVersion = metadata.LatestAgentVersion
	cluster.LatestAgentImage = metadata.LatestAgentImage
	cluster.LatestAgentImageDigest = metadata.LatestAgentImageDigest
	cluster.VeleroImage = metadata.VeleroImage
	cluster.VeleroImageDigest = metadata.VeleroImageDigest
	cluster.VeleroServerReady = metadata.VeleroServerReady
	cluster.VeleroNodeAgentDesired = metadata.VeleroNodeAgentDesired
	cluster.VeleroNodeAgentReady = metadata.VeleroNodeAgentReady
	cluster.VeleroNodeAgentImageDigest = metadata.VeleroNodeAgentImageDigest
	cluster.AgentUpgradeStatus = metadata.AgentUpgradeStatus
}

func (s *PostgresStore) ListApplications(clusterID string) ([]Application, error) {
	return s.ListApplicationsFiltered(ApplicationFilter{ClusterID: clusterID})
}

func (s *PostgresStore) ListApplicationsFiltered(filter ApplicationFilter) ([]Application, error) {
	labelsExpr, resourceExpr := "a.labels", "a.resource_summary"
	if filter.Summary {
		labelsExpr, resourceExpr = "'{}'::jsonb", "'{}'::jsonb"
	}
	query := `
		select a.id, a.cluster_id, a.namespace, a.name, a.status, ` + labelsExpr + `,
		       workload_count, service_count, ingress_count, configmap_count, secret_count,
		       pvc_count, pv_capacity_bytes, ` + resourceExpr + `, coalesce(a.last_collected_at, a.created_at), protection_status,
		       coalesce((select jsonb_agg(at.tag_id::text) from application_tags at where at.application_id=a.id),'[]'::jsonb)
		from applications a join clusters c on c.id=a.cluster_id
	`
	args := []any{}
	conditions := []string{}
	if filter.TenantID != "" {
		args = append(args, filter.TenantID)
		conditions = append(conditions, fmt.Sprintf("c.tenant_id=$%d", len(args)))
	}
	if filter.ClusterID != "" {
		args = append(args, filter.ClusterID)
		conditions = append(conditions, fmt.Sprintf("a.cluster_id=$%d", len(args)))
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += ` order by a.namespace`
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" limit $%d", len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(" offset $%d", len(args))
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []Application
	for rows.Next() {
		var app Application
		var labelsRaw []byte
		var resourceSummaryRaw []byte
		var tagsRaw []byte
		if err := rows.Scan(
			&app.ID, &app.ClusterID, &app.Namespace, &app.Name, &app.Status, &labelsRaw,
			&app.WorkloadCount, &app.ServiceCount, &app.IngressCount, &app.ConfigMapCount, &app.SecretCount,
			&app.PVCCount, &app.PVCapacityBytes, &resourceSummaryRaw, &app.LastCollectedAt, &app.ProtectionStatus, &tagsRaw,
		); err != nil {
			return nil, err
		}
		if len(labelsRaw) > 0 {
			_ = json.Unmarshal(labelsRaw, &app.Labels)
		}
		if len(resourceSummaryRaw) > 0 {
			_ = json.Unmarshal(resourceSummaryRaw, &app.ResourceSummary)
		}
		_ = json.Unmarshal(tagsRaw, &app.Tags)
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

func (s *PostgresStore) ListTags() ([]Tag, error) {
	rows, err := s.db.Query(`select id,tenant_id,name,created_at,updated_at from tags order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.TenantID, &tag.Name, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, tag)
	}
	return items, rows.Err()
}
func (s *PostgresStore) CreateTag(tenantID, name string) (Tag, error) {
	var tag Tag
	now := time.Now().UTC()
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	err := s.db.QueryRow(`insert into tags(id,tenant_id,name,created_at,updated_at) values($1,$2,$3,$4,$4) returning id,tenant_id,name,created_at,updated_at`, newID(), tenantID, strings.TrimSpace(name), now).Scan(&tag.ID, &tag.TenantID, &tag.Name, &tag.CreatedAt, &tag.UpdatedAt)
	return tag, err
}
func (s *PostgresStore) UpdateTag(id, name string) (Tag, bool, error) {
	var tag Tag
	err := s.db.QueryRow(`update tags set name=$2,updated_at=now() where id=$1 returning id,tenant_id,name,created_at,updated_at`, id, strings.TrimSpace(name)).Scan(&tag.ID, &tag.TenantID, &tag.Name, &tag.CreatedAt, &tag.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Tag{}, false, nil
	}
	return tag, err == nil, err
}
func (s *PostgresStore) DeleteTag(id string) (bool, error) {
	result, err := s.db.Exec(`delete from tags where id=$1`, id)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}
func (s *PostgresStore) SetApplicationTags(applicationID string, tagIDs []string) (Application, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Application{}, false, err
	}
	defer tx.Rollback()
	var tenantID string
	if err = tx.QueryRow(`select tenant_id from applications where id=$1 for update`, applicationID).Scan(&tenantID); errors.Is(err, sql.ErrNoRows) {
		return Application{}, false, nil
	} else if err != nil {
		return Application{}, false, err
	}
	// Validate the entire replacement before deleting existing associations.
	// Lock referenced tags against concurrent deletion or ownership changes.
	for _, tagID := range tagIDs {
		var ownedID string
		err = tx.QueryRow(`select id from tags where id=$1 and tenant_id=$2 for share`, tagID, tenantID).Scan(&ownedID)
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, false, ErrTenantResourceMismatch
		}
		if err != nil {
			return Application{}, false, err
		}
	}
	if _, err = tx.Exec(`delete from application_tags where application_id=$1`, applicationID); err != nil {
		return Application{}, false, err
	}
	for _, tagID := range tagIDs {
		if _, err = tx.Exec(`insert into application_tags(application_id,tag_id) select $1,id from tags where id=$2 and tenant_id=$3 on conflict do nothing`, applicationID, tagID, tenantID); err != nil {
			return Application{}, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Application{}, false, err
	}
	apps, err := s.ListApplications("")
	if err != nil {
		return Application{}, false, err
	}
	for _, app := range apps {
		if app.ID == applicationID {
			return app, true, nil
		}
	}
	return Application{}, false, nil
}

func (s *PostgresStore) ApplyInventory(input InventoryInput) (Cluster, bool, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Cluster{}, false, err
	}
	defer tx.Rollback()
	var clusterTenantID string
	var existingNamespaceAPIsJSON []byte
	if err := tx.QueryRow(`select tenant_id, coalesce(metadata->'namespaceAPIs', '[]'::jsonb) from clusters where id=$1`, input.ClusterID).Scan(&clusterTenantID, &existingNamespaceAPIsJSON); errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, false, nil
	} else if err != nil {
		return Cluster{}, false, err
	}
	if input.CapabilityScan && input.CapabilityNamespace != "" {
		var existing []ClusterNamespaceAPI
		if err := json.Unmarshal(existingNamespaceAPIsJSON, &existing); err != nil {
			return Cluster{}, false, fmt.Errorf("decode cached namespace APIs: %w", err)
		}
		input.NamespaceAPIs = mergeNamespaceAPIs(existing, input.NamespaceAPIs, input.CapabilityNamespace)
	}

	result, err := tx.Exec(`
		update clusters
		set kube_version = $2,
		    velero_status = coalesce(nullif($3, ''), velero_status),
		    node_count = $4,
		    namespace_count = $5,
		    application_count = $6,
		    connection_status = 'online',
		    last_seen_at = $7,
		    metadata = coalesce(metadata, '{}'::jsonb) || jsonb_build_object(
		      'inventoryHash', $8::text,
		      'nodes', $9::jsonb,
		      'storageClasses', $10::jsonb,
		      'apiResources', case when $15 then $11::jsonb else coalesce(metadata->'apiResources', '[]'::jsonb) end,
		      'namespaceAPIs', case when $15 then $12::jsonb else coalesce(metadata->'namespaceAPIs', '[]'::jsonb) end,
		      'capabilities', case when $15 then $13::jsonb else coalesce(metadata->'capabilities', '[]'::jsonb) end,
		      'capabilitiesCollectedAt', case when $15 then to_jsonb($14::timestamptz) else coalesce(metadata->'capabilitiesCollectedAt', 'null'::jsonb) end,
		      'capabilitiesComplete', case when $15 then to_jsonb($16::boolean) else coalesce(metadata->'capabilitiesComplete', 'false'::jsonb) end
		    ),
		    updated_at = $7
		where id = $1
	`, input.ClusterID, input.KubeVersion, input.VeleroStatus, input.NodeCount, input.NamespaceCount, len(input.Apps), now, input.Hash, mustJSON(input.Nodes), mustJSON(input.StorageClasses), mustJSON(input.APIResources), mustJSON(input.NamespaceAPIs), mustJSON(input.Capabilities), input.CollectedAt, input.CapabilityScan, input.CapabilitiesComplete)
	if err != nil {
		return Cluster{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Cluster{}, false, err
	}
	if affected == 0 {
		return Cluster{}, false, nil
	}

	reportedIDs := make(map[string]string)
	for _, app := range input.Apps {
		appID := app.ID
		if appID == "" {
			appID = newID()
		}
		name := app.Name
		if name == "" {
			name = app.Namespace
		}
		labels, err := json.Marshal(app.Labels)
		if err != nil {
			return Cluster{}, false, err
		}
		resourceSummary, err := json.Marshal(app.ResourceSummary)
		if err != nil {
			return Cluster{}, false, err
		}
		// Upsert by (cluster_id, namespace). Preserve protection_status and protection_score
		// set by operators via the platform; only refresh inventory-derived fields.
		_, err = tx.Exec(`
			insert into applications (
				id, tenant_id, cluster_id, namespace, name, status, workload_count, service_count,
				ingress_count, configmap_count, secret_count, pvc_count, pv_capacity_bytes,
				labels, resource_summary, last_collected_at, created_at, updated_at
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16, $17, $17)
			on conflict (cluster_id, namespace) do update set
				tenant_id = excluded.tenant_id,
				name = excluded.name,
				status = excluded.status,
				workload_count = excluded.workload_count,
				service_count = excluded.service_count,
				ingress_count = excluded.ingress_count,
				configmap_count = excluded.configmap_count,
				secret_count = excluded.secret_count,
				pvc_count = excluded.pvc_count,
				pv_capacity_bytes = excluded.pv_capacity_bytes,
				labels = excluded.labels,
				resource_summary = excluded.resource_summary,
				last_collected_at = excluded.last_collected_at,
				updated_at = excluded.updated_at
		`, appID, clusterTenantID, input.ClusterID, app.Namespace, name, app.Status, app.WorkloadCount,
			app.ServiceCount, app.IngressCount, app.ConfigMapCount, app.SecretCount, app.PVCCount,
			app.PVCapacityBytes, labels, string(resourceSummary), input.CollectedAt, now)
		if err != nil {
			return Cluster{}, false, err
		}
		// Look up the actual row id used (in case an existing row was kept).
		var realID string
		if err := tx.QueryRow(`select id from applications where cluster_id = $1 and namespace = $2`, input.ClusterID, app.Namespace).Scan(&realID); err != nil {
			return Cluster{}, false, err
		}
		reportedIDs[app.Namespace] = realID
	}

	// Remove only the rows that are no longer reported by the agent so the
	// applications list stays in sync with the cluster. Operator-driven fields
	// (protection_status, protection_score) for surviving rows are preserved.
	if len(reportedIDs) > 0 {
		args := []any{input.ClusterID}
		placeholders := make([]string, 0, len(reportedIDs))
		for ns := range reportedIDs {
			args = append(args, ns)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		query := "delete from applications where cluster_id = $1 and namespace not in (" + strings.Join(placeholders, ",") + ")"
		if _, err := tx.Exec(query, args...); err != nil {
			return Cluster{}, false, err
		}
	} else {
		if _, err := tx.Exec(`delete from applications where cluster_id = $1`, input.ClusterID); err != nil {
			return Cluster{}, false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Cluster{}, false, err
	}

	cluster, ok, err := s.getCluster(input.ClusterID)
	return cluster, ok, err
}

func (s *PostgresStore) UpdateHeartbeat(input HeartbeatInput) (Cluster, bool, error) {
	now := time.Now().UTC()
	result, err := s.db.Exec(`
		update clusters
		set status = coalesce(nullif($2, ''), status),
		    kube_version = coalesce(nullif($3, ''), kube_version),
		    agent_version = coalesce(nullif($4, ''), agent_version),
		    velero_status = coalesce(nullif($5, ''), velero_status),
		    velero_version = coalesce(nullif($14, ''), velero_version),
		    node_count = case when $6 > 0 then $6 else node_count end,
		    namespace_count = case when $7 > 0 then $7 else namespace_count end,
		    application_count = case when $8 > 0 then $8 else application_count end,
		    connection_status = 'online',
		    last_seen_at = $9,
		    metadata = coalesce(metadata, '{}'::jsonb) || jsonb_strip_nulls(jsonb_build_object(
		        'inventoryHash', nullif($10, ''),
		        'agentImage', nullif($11, ''),
		        'agentImageId', nullif($12, ''),
		        'agentImageDigest', nullif($13, ''),
		        'veleroImage', nullif($15, ''),
		        'veleroImageDigest', nullif($16, ''),
		        'veleroServerReady', $17::boolean,
		        'veleroNodeAgentDesired', $18::integer,
		        'veleroNodeAgentReady', $19::integer,
		        'veleroNodeAgentImageDigest', nullif($20, '')
		    )),
		    updated_at = $9
		where id = $1
	`, input.ClusterID, input.Status, input.KubeVersion, input.AgentVersion, input.VeleroStatus,
		input.NodeCount, input.NamespaceCount, input.ApplicationCount, now, input.InventoryHash,
		input.AgentImage, input.AgentImageID, input.AgentImageDigest, input.VeleroVersion,
		input.VeleroImage, input.VeleroImageDigest, input.VeleroServerReady,
		input.VeleroNodeAgentDesired, input.VeleroNodeAgentReady, input.VeleroNodeAgentImageDigest)
	if err != nil {
		return Cluster{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Cluster{}, false, err
	}
	if affected == 0 {
		return Cluster{}, false, nil
	}
	return s.getCluster(input.ClusterID)
}

func (s *PostgresStore) getCluster(clusterID string) (Cluster, bool, error) {
	var cluster Cluster
	var metadataRaw []byte
	err := s.db.QueryRow(`
		select id, tenant_id, name, coalesce(kube_version, ''), status, connection_status,
		       node_count, namespace_count, application_count, 0 as active_tasks,
		       coalesce(agent_version, ''), coalesce(velero_version, ''), coalesce(velero_status, ''),
		       coalesce(metadata->>'inventoryHash', ''), role, is_default,
		       coalesce(registered_at, created_at), coalesce(last_seen_at, created_at), metadata,
		       cluster_type, coalesce(cloud_provider,''), coalesce(cloud_region,''), coalesce(cloud_cluster_id,'')
		from clusters
		where id = $1
	`, clusterID).Scan(
		&cluster.ID, &cluster.TenantID, &cluster.Name, &cluster.KubeVersion, &cluster.Status,
		&cluster.ConnectionStatus, &cluster.NodeCount, &cluster.NamespaceCount, &cluster.ApplicationCount,
		&cluster.ActiveTasks, &cluster.AgentVersion, &cluster.VeleroVersion, &cluster.VeleroStatus,
		&cluster.InventoryHash, &cluster.Role, &cluster.IsDefault, &cluster.RegisteredAt, &cluster.LastSeenAt, &metadataRaw,
		&cluster.ClusterType, &cluster.CloudProvider, &cluster.CloudRegion, &cluster.CloudClusterID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Cluster{}, false, nil
	}
	if err != nil {
		return Cluster{}, false, err
	}
	applyClusterMetadata(&cluster, metadataRaw)
	applyClusterConnectionFreshness(&cluster)
	return cluster, true, nil
}

func (s *PostgresStore) CreateStorageRepository(input StorageRepositoryInput) (StorageRepository, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	input.Region = normalizeStorageRegionValue(input.Region)
	now := time.Now().UTC()
	if input.Type == "" {
		input.Type = "S3"
	}
	repoID := newID()
	secretRef := input.SecretRef
	if secretRef == "" {
		secretRef = storageSecretName(repoID)
	}
	secret := storageSecretPayload(input)
	status := "unknown"
	configRaw, err := json.Marshal(input.Config)
	if err != nil {
		return StorageRepository{}, err
	}
	secretCiphertext, err := s.encodeStorageSecret(secret)
	if err != nil {
		return StorageRepository{}, err
	}
	repo := StorageRepository{
		ID:         repoID,
		TenantID:   input.TenantID,
		Name:       input.Name,
		Type:       input.Type,
		Endpoint:   input.Endpoint,
		Bucket:     input.Bucket,
		Region:     input.Region,
		TLSEnabled: input.TLSEnabled,
		Status:     status,
		Config:     input.Config,
		SecretRef:  secretRef,
		Secret:     secret,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if repo.Config == nil {
		repo.Config = map[string]any{}
	}
	_, err = s.db.Exec(`
		insert into storage_repositories (
			id, tenant_id, name, type, endpoint, bucket, region, tls_enabled, status,
			config, secret_ref, secret_payload, secret_ciphertext, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, '{}'::jsonb, $12, $13, $13)
	`, repo.ID, repo.TenantID, repo.Name, repo.Type, repo.Endpoint, repo.Bucket, repo.Region,
		repo.TLSEnabled, repo.Status, configRaw, repo.SecretRef, secretCiphertext, now)
	return repo, err
}

func (s *PostgresStore) ListStorageRepositories() ([]StorageRepository, error) {
	rows, err := s.db.Query(`
		select id, tenant_id, name, type, coalesce(endpoint, ''), coalesce(bucket, ''),
		       coalesce(region, ''), tls_enabled, status, config, coalesce(secret_ref, ''),
		       secret_payload,secret_ciphertext,coalesce(last_validated_at, '0001-01-01'::timestamptz), created_at, updated_at
		from storage_repositories
		order by created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []StorageRepository
	for rows.Next() {
		var item StorageRepository
		var configRaw, secretRaw []byte
		var secretCiphertext string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Endpoint,
			&item.Bucket, &item.Region, &item.TLSEnabled, &item.Status, &configRaw, &item.SecretRef,
			&secretRaw, &secretCiphertext, &item.LastValidatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(configRaw, &item.Config)
		item.Secret, err = s.decodeStorageSecret(secretCiphertext, secretRaw)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateStorageRepository(id string, input StorageRepositoryInput) (StorageRepository, bool, error) {
	input.Region = normalizeStorageRegionValue(input.Region)
	current, ok, err := s.GetStorageRepository(id)
	if err != nil || !ok {
		return StorageRepository{}, ok, err
	}
	secret := current.Secret
	if next := storageSecretPayload(input); len(next) > 0 {
		if secret == nil {
			secret = map[string]string{}
		}
		for key, value := range next {
			secret[key] = value
		}
	}
	config := input.Config
	if config == nil {
		config = map[string]any{}
	}
	configRaw, err := json.Marshal(config)
	if err != nil {
		return StorageRepository{}, false, err
	}
	secretCiphertext, err := s.encodeStorageSecret(secret)
	if err != nil {
		return StorageRepository{}, false, err
	}
	result, err := s.db.Exec(`update storage_repositories set name=$2,type=$3,endpoint=nullif($4,''),bucket=nullif($5,''),region=nullif($6,''),tls_enabled=$7,config=$8,secret_payload='{}'::jsonb,secret_ciphertext=$9,status='unknown',last_validated_at=null,updated_at=now() where id=$1 and tenant_id=$10`, id, input.Name, input.Type, input.Endpoint, input.Bucket, input.Region, input.TLSEnabled, configRaw, secretCiphertext, current.TenantID)
	if err != nil {
		return StorageRepository{}, false, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return StorageRepository{}, false, err
	}
	return s.GetStorageRepository(id)
}

func (s *PostgresStore) DeleteStorageRepository(id string) (bool, bool, error) {
	current, found, err := s.GetStorageRepository(id)
	if err != nil || !found {
		return false, false, err
	}
	var inUse bool
	if err := s.db.QueryRow(`select exists(select 1 from protection_plans where tenant_id=$1 and storage_repo_id=$2)`, current.TenantID, id).Scan(&inUse); err != nil {
		return false, false, err
	}
	if inUse {
		return false, true, nil
	}
	result, err := s.db.Exec(`delete from storage_repositories where id=$1 and tenant_id=$2`, id, current.TenantID)
	if err != nil {
		return false, false, err
	}
	count, err := result.RowsAffected()
	return count > 0, false, err
}

func (s *PostgresStore) SetStorageRepositoryStatus(id string, status string, lastValidatedAt time.Time) (StorageRepository, bool, error) {
	if status == "" {
		status = "unknown"
	}
	var item StorageRepository
	var configRaw, secretRaw []byte
	var secretCiphertext string
	var lastValidated sql.NullTime
	err := s.db.QueryRow(`
		update storage_repositories
		   set status = $2,
		       last_validated_at = $3,
		       updated_at = now()
		 where id = $1
		returning id, tenant_id, name, type, coalesce(endpoint, ''), coalesce(bucket, ''),
		          coalesce(region, ''), tls_enabled, status, config, coalesce(secret_ref, ''),
		          secret_payload,secret_ciphertext,coalesce(last_validated_at, '0001-01-01'::timestamptz), created_at, updated_at
	`, id, status, lastValidatedAt).Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Endpoint,
		&item.Bucket, &item.Region, &item.TLSEnabled, &item.Status, &configRaw, &item.SecretRef,
		&secretRaw, &secretCiphertext, &lastValidated, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StorageRepository{}, false, nil
	}
	if err != nil {
		return StorageRepository{}, false, err
	}
	if lastValidated.Valid {
		item.LastValidatedAt = lastValidated.Time
	}
	_ = json.Unmarshal(configRaw, &item.Config)
	item.Secret, err = s.decodeStorageSecret(secretCiphertext, secretRaw)
	if err != nil {
		return StorageRepository{}, false, err
	}
	if item.Config == nil {
		item.Config = map[string]any{}
	}
	if item.Secret == nil {
		item.Secret = map[string]string{}
	}
	return item, true, nil
}

func (s *PostgresStore) GetStorageRepository(id string) (StorageRepository, bool, error) {
	var item StorageRepository
	var configRaw, secretRaw []byte
	var secretCiphertext string
	err := s.db.QueryRow(`
		select id, tenant_id, name, type, coalesce(endpoint, ''), coalesce(bucket, ''),
		       coalesce(region, ''), tls_enabled, status, config, coalesce(secret_ref, ''),
		       secret_payload,secret_ciphertext,coalesce(last_validated_at, '0001-01-01'::timestamptz), created_at, updated_at
		from storage_repositories
		where id = $1
	`, id).Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Endpoint,
		&item.Bucket, &item.Region, &item.TLSEnabled, &item.Status, &configRaw,
		&item.SecretRef, &secretRaw, &secretCiphertext, &item.LastValidatedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StorageRepository{}, false, nil
	}
	if err != nil {
		return StorageRepository{}, false, err
	}
	_ = json.Unmarshal(configRaw, &item.Config)
	item.Secret, err = s.decodeStorageSecret(secretCiphertext, secretRaw)
	if err != nil {
		return StorageRepository{}, false, err
	}
	if item.Config == nil {
		item.Config = map[string]any{}
	}
	if item.Secret == nil {
		item.Secret = map[string]string{}
	}
	return item, true, nil
}

func (s *PostgresStore) UpsertClusterStorageBinding(input ClusterStorageBindingInput) (ClusterStorageBinding, error) {
	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = "pending"
	}
	sourceClusterID := input.SourceClusterID
	if sourceClusterID == "" {
		sourceClusterID = input.ClusterID
	}
	bslName := input.BSLName
	if bslName == "" {
		bslName = "default"
	}
	id := newID()
	var tenantID string
	if err := s.db.QueryRow(`select c.tenant_id from clusters c join storage_repositories r on r.id=$2 and r.tenant_id=c.tenant_id where c.id=$1`, input.ClusterID, input.StorageRepoID).Scan(&tenantID); err != nil {
		return ClusterStorageBinding{}, fmt.Errorf("cluster and storage repository must belong to the same tenant: %w", err)
	}
	row := s.db.QueryRow(`
		insert into cluster_storage_bindings (
			id, tenant_id, cluster_id, storage_repo_id, source_cluster_id, bsl_name, object_prefix, status, retry_count,
			repo_updated_at, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, nullif($10, '0001-01-01 00:00:00+00'::timestamptz), $11, $11)
		on conflict (cluster_id, storage_repo_id, source_cluster_id) do update
		   set bsl_name = excluded.bsl_name,
		       object_prefix = excluded.object_prefix,
		       status = excluded.status,
		       retry_count = excluded.retry_count,
		       repo_updated_at = excluded.repo_updated_at,
		       updated_at = excluded.updated_at
		returning id, tenant_id, cluster_id, storage_repo_id, source_cluster_id, bsl_name, coalesce(object_prefix, ''), status, retry_count,
		          coalesce(last_synced_at, '0001-01-01'::timestamptz),
		          coalesce(last_success_at, '0001-01-01'::timestamptz),
		          coalesce(last_error_code, ''), coalesce(last_error_message, ''),
		          coalesce(repo_updated_at, '0001-01-01'::timestamptz), created_at, updated_at
	`, id, tenantID, input.ClusterID, input.StorageRepoID, sourceClusterID, bslName, input.ObjectPrefix, status, input.RetryCount, input.RepoUpdatedAt, now)
	return scanClusterStorageBinding(row)
}

func (s *PostgresStore) GetClusterStorageBinding(clusterID string, storageRepoID string, sourceClusterID string) (ClusterStorageBinding, bool, error) {
	if sourceClusterID == "" {
		sourceClusterID = clusterID
	}
	row := s.db.QueryRow(`
		select id, tenant_id, cluster_id, storage_repo_id, source_cluster_id, bsl_name, coalesce(object_prefix, ''), status, retry_count,
		       coalesce(last_synced_at, '0001-01-01'::timestamptz),
		       coalesce(last_success_at, '0001-01-01'::timestamptz),
		       coalesce(last_error_code, ''), coalesce(last_error_message, ''),
		       coalesce(repo_updated_at, '0001-01-01'::timestamptz), created_at, updated_at
		from cluster_storage_bindings
		where cluster_id = $1 and storage_repo_id = $2 and source_cluster_id = $3
	`, clusterID, storageRepoID, sourceClusterID)
	item, err := scanClusterStorageBinding(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterStorageBinding{}, false, nil
	}
	if err != nil {
		return ClusterStorageBinding{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) UpdateClusterStorageBindingStatus(input ClusterStorageBindingStatusInput) (ClusterStorageBinding, bool, error) {
	status := input.Status
	if status == "" {
		status = "pending"
	}
	sourceClusterID := input.SourceClusterID
	if sourceClusterID == "" {
		sourceClusterID = input.ClusterID
	}
	row := s.db.QueryRow(`
		update cluster_storage_bindings
		   set status = $4,
		       retry_count = greatest(retry_count, $5),
		       last_synced_at = coalesce(nullif($6, '0001-01-01 00:00:00+00'::timestamptz), last_synced_at),
		       last_success_at = coalesce(nullif($7, '0001-01-01 00:00:00+00'::timestamptz), last_success_at),
		       last_error_code = nullif($8, ''),
		       last_error_message = nullif($9, ''),
		       repo_updated_at = coalesce(nullif($10, '0001-01-01 00:00:00+00'::timestamptz), repo_updated_at),
		       updated_at = now()
		 where cluster_id = $1 and storage_repo_id = $2 and source_cluster_id = $3
		returning id, tenant_id, cluster_id, storage_repo_id, source_cluster_id, bsl_name, coalesce(object_prefix, ''), status, retry_count,
		          coalesce(last_synced_at, '0001-01-01'::timestamptz),
		          coalesce(last_success_at, '0001-01-01'::timestamptz),
		          coalesce(last_error_code, ''), coalesce(last_error_message, ''),
		          coalesce(repo_updated_at, '0001-01-01'::timestamptz), created_at, updated_at
	`, input.ClusterID, input.StorageRepoID, sourceClusterID, status, input.RetryCount, input.LastSyncedAt, input.LastSuccessAt,
		input.LastErrorCode, input.LastErrorMessage, input.RepoUpdatedAt)
	item, err := scanClusterStorageBinding(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterStorageBinding{}, false, nil
	}
	if err != nil {
		return ClusterStorageBinding{}, false, err
	}
	return item, true, nil
}

type clusterStorageBindingScanner interface {
	Scan(dest ...any) error
}

func scanClusterStorageBinding(row clusterStorageBindingScanner) (ClusterStorageBinding, error) {
	var item ClusterStorageBinding
	err := row.Scan(&item.ID, &item.TenantID, &item.ClusterID, &item.StorageRepoID, &item.SourceClusterID, &item.BSLName, &item.ObjectPrefix,
		&item.Status, &item.RetryCount, &item.LastSyncedAt, &item.LastSuccessAt, &item.LastErrorCode,
		&item.LastErrorMessage, &item.RepoUpdatedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *PostgresStore) CreateDiagnosticLog(input DiagnosticLogInput) (DiagnosticLog, error) {
	if s.diagnosticWriter != nil {
		return s.diagnosticWriter.CreateDiagnosticLog(input)
	}
	item := diagnosticLogFromInput(input, time.Now().UTC())
	detailsRaw, err := json.Marshal(item.Details)
	if err != nil {
		return DiagnosticLog{}, err
	}
	err = s.db.QueryRow(`insert into diagnostic_logs(id,tenant_id,scope,level,component,operation,message,cluster_id,task_id,command_id,request_id,error_code,status,duration_ms,details,event_at,created_at,fingerprint)
		values($1,nullif($2,'')::uuid,$3,$4,$5,$6,$7,nullif($8,'')::uuid,nullif($9,'')::uuid,nullif($10,'')::uuid,nullif($11,'')::uuid,nullif($12,''),nullif($13,''),nullif($14,0),$15,$16,$17,nullif($18,''))
		on conflict (fingerprint) where fingerprint is not null do update set fingerprint=excluded.fingerprint
		returning event_at,created_at`, item.ID, item.TenantID, item.Scope, item.Level, item.Component, item.Operation, item.Message, item.ClusterID, item.TaskID, item.CommandID, item.RequestID, item.ErrorCode, item.Status, item.DurationMS, detailsRaw, item.EventAt, item.CreatedAt, item.Fingerprint).Scan(&item.EventAt, &item.CreatedAt)
	return item, err
}

func (s *PostgresStore) ListDiagnosticLogs(filter DiagnosticLogFilter) ([]DiagnosticLog, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`select id,coalesce(tenant_id::text,''),scope,level,component,operation,message,coalesce(cluster_id::text,''),coalesce(task_id::text,''),coalesce(command_id::text,''),coalesce(request_id::text,''),coalesce(error_code,''),coalesce(status,''),coalesce(duration_ms,0),details,event_at,created_at
		from diagnostic_logs where ($1='' or tenant_id=nullif($1,'')::uuid) and ($2='' or scope=$2) and ($3='' or level=$3) and ($4='' or component=$4) and ($5='' or cluster_id=nullif($5,'')::uuid) and ($6='' or task_id=nullif($6,'')::uuid) and ($7::timestamptz is null or event_at >= $7) and ($8::timestamptz is null or event_at <= $8) and ($9='' or message ilike '%%'||$9||'%%' or operation ilike '%%'||$9||'%%' or coalesce(error_code,'') ilike '%%'||$9||'%%' or coalesce(task_id::text,'') ilike '%%'||$9||'%%') and ($10='' or ($10='cluster' and component in ('comm-agent','velero','node-agent')) or ($10='platform' and component not in ('comm-agent','velero','node-agent'))) order by event_at desc limit $11 offset $12`, filter.TenantID, filter.Scope, filter.Level, filter.Component, filter.ClusterID, filter.TaskID, nullableTime(filter.From), nullableTime(filter.To), filter.Query, filter.Source, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DiagnosticLog{}
	for rows.Next() {
		var item DiagnosticLog
		var raw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Scope, &item.Level, &item.Component, &item.Operation, &item.Message, &item.ClusterID, &item.TaskID, &item.CommandID, &item.RequestID, &item.ErrorCode, &item.Status, &item.DurationMS, &raw, &item.EventAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &item.Details)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) PurgeDiagnosticLogs(before time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`with expired as (
		select ctid from diagnostic_logs where event_at < $1 order by event_at limit 10000
	) delete from diagnostic_logs where ctid in (select ctid from expired)`, before.UTC())
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(`delete from cluster_log_coverage where covered_to < $1`, before.UTC()); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(`update cluster_log_coverage set covered_from=$1,updated_at=now() where covered_from < $1`, before.UTC()); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *PostgresStore) GetClusterLogCoverage(clusterID string, component string) (ClusterLogCoverage, bool, error) {
	var item ClusterLogCoverage
	err := s.db.QueryRow(`select cluster_id::text,tenant_id::text,component,covered_from,covered_to,last_collected_at,coalesce(last_request_id::text,''),last_entry_count,truncated,updated_at from cluster_log_coverage where cluster_id=$1 and component=$2`, clusterID, component).Scan(&item.ClusterID, &item.TenantID, &item.Component, &item.CoveredFrom, &item.CoveredTo, &item.LastCollectedAt, &item.LastRequestID, &item.LastEntryCount, &item.Truncated, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ClusterLogCoverage{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) UpsertClusterLogCoverage(input ClusterLogCoverageInput) (ClusterLogCoverage, error) {
	var item ClusterLogCoverage
	err := s.db.QueryRow(`insert into cluster_log_coverage(cluster_id,tenant_id,component,covered_from,covered_to,last_collected_at,last_request_id,last_entry_count,truncated,updated_at)
		values($1,$2,$3,$4,$5,$6,nullif($7,'')::uuid,$8,$9,now())
		on conflict(cluster_id,component) do update set tenant_id=excluded.tenant_id,
			covered_from=case when excluded.covered_from <= cluster_log_coverage.covered_to + interval '5 minutes' then least(cluster_log_coverage.covered_from,excluded.covered_from) else excluded.covered_from end,
			covered_to=case when excluded.covered_from <= cluster_log_coverage.covered_to + interval '5 minutes' then greatest(cluster_log_coverage.covered_to,excluded.covered_to) else excluded.covered_to end,
			last_collected_at=excluded.last_collected_at,last_request_id=excluded.last_request_id,last_entry_count=excluded.last_entry_count,
			truncated=case when excluded.covered_from <= cluster_log_coverage.covered_to + interval '5 minutes' then cluster_log_coverage.truncated or excluded.truncated else excluded.truncated end,updated_at=now()
		returning cluster_id::text,tenant_id::text,component,covered_from,covered_to,last_collected_at,coalesce(last_request_id::text,''),last_entry_count,truncated,updated_at`, input.ClusterID, input.TenantID, input.Component, input.CoveredFrom, input.CoveredTo, input.CollectedAt, input.RequestID, input.EntryCount, input.Truncated).Scan(&item.ClusterID, &item.TenantID, &item.Component, &item.CoveredFrom, &item.CoveredTo, &item.LastCollectedAt, &item.LastRequestID, &item.LastEntryCount, &item.Truncated, &item.UpdatedAt)
	return item, err
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func (s *PostgresStore) ListTaskEvents(taskID string) ([]TaskEvent, error) {
	rows, err := s.db.Query(`
		select id, task_id, level, coalesce(reason, ''), message, payload, created_at
		from task_events
		where task_id = $1
		order by created_at asc
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskEvent
	for rows.Next() {
		var item TaskEvent
		var payloadRaw []byte
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Level, &item.Reason, &item.Message, &payloadRaw, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payloadRaw, &item.Payload)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) getTask(taskID string) (Task, bool, error) {
	var task Task
	var payloadRaw []byte
	err := s.db.QueryRow(`
		select id, tenant_id, coalesce(cluster_id::text, ''), coalesce(app_id::text, ''), coalesce(protection_plan_id::text, ''),
		       coalesce(restore_point_id::text, ''), type, status, progress, coalesce(command_id::text, ''),
		       coalesce(error_code, ''), coalesce(error_message, ''), payload,
		       created_at, coalesce(dispatched_at, '0001-01-01'::timestamptz),
		       coalesce(accepted_at, '0001-01-01'::timestamptz),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz)
		from tasks
		where id = $1
	`, taskID).Scan(&task.ID, &task.TenantID, &task.ClusterID, &task.AppID, &task.ProtectionPlanID,
		&task.RestorePointID, &task.Type, &task.Status, &task.Progress, &task.CommandID,
		&task.ErrorCode, &task.ErrorMessage, &payloadRaw, &task.CreatedAt, &task.DispatchedAt,
		&task.AcceptedAt, &task.StartedAt, &task.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	_ = json.Unmarshal(payloadRaw, &task.Payload)
	return task, true, nil
}

func (s *PostgresStore) GetProtectionPlan(id string) (ProtectionPlan, bool, error) {
	row := s.db.QueryRow(`
		select pp.id, pp.tenant_id, pp.source_cluster_id, pp.app_id, pp.scope_type, pp.included_resources, pp.resource_selection, pp.label_selector,
		       pp.include_cluster_scoped, coalesce(pp.storage_repo_id::text, ''), coalesce(pp.policy_id::text, ''),
		       coalesce(pp.target_cluster_id::text, ''), pp.excluded_resources, pp.pre_hooks, pp.post_hooks,
		       pp.plan_storage_size, coalesce(pps.next_fire_at, '0001-01-01'::timestamptz), coalesce(pps.enabled, false),
		       coalesce(pp.latest_sync_task_id::text, ''), coalesce(pp.latest_recovery_task_id::text, ''),
		       pp.status, pp.created_at, pp.updated_at
		from protection_plans pp
		left join protection_plan_schedules pps on pps.protection_plan_id = pp.id
		where pp.id = $1
	`, id)
	var item ProtectionPlan
	var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	if err := row.Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize,
		&item.NextFireAt, &item.ScheduleEnabled, &item.LatestSyncTaskID, &item.LatestRecoveryTaskID, &item.Status,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, false, nil
		}
		return ProtectionPlan{}, false, err
	}
	_ = json.Unmarshal(includedResources, &item.IncludedResources)
	_ = json.Unmarshal(resourceSelection, &item.ResourceSelection)
	if item.ResourceSelection.Mode == "" {
		item.ResourceSelection.Mode = "all"
	}
	_ = json.Unmarshal(labelSelector, &item.LabelSelector)
	_ = json.Unmarshal(excludedResources, &item.ExcludedResources)
	_ = json.Unmarshal(preHooks, &item.PreHooks)
	_ = json.Unmarshal(postHooks, &item.PostHooks)
	_ = json.Unmarshal(planStorageSize, &item.PlanStorageSize)
	item.AppIDs = []string{item.AppID}
	rows, err := s.db.Query(`select app_id from protection_plan_apps where plan_id = $1 order by created_at`, id)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var appID string
		if err := rows.Scan(&appID); err != nil {
			return ProtectionPlan{}, false, err
		}
		item.AppIDs = append(item.AppIDs, appID)
	}
	return item, true, nil
}

func (s *PostgresStore) GetApplication(id string) (Application, bool, error) {
	row := s.db.QueryRow(`
		select id, cluster_id, namespace, name, status, labels,
		       workload_count, service_count, ingress_count, configmap_count, secret_count,
		       pvc_count, pv_capacity_bytes, resource_summary, coalesce(last_collected_at, created_at), protection_status
		from applications where id = $1
	`, id)
	var app Application
	var labelsRaw []byte
	var resourceSummaryRaw []byte
	if err := row.Scan(&app.ID, &app.ClusterID, &app.Namespace, &app.Name, &app.Status, &labelsRaw,
		&app.WorkloadCount, &app.ServiceCount, &app.IngressCount, &app.ConfigMapCount, &app.SecretCount,
		&app.PVCCount, &app.PVCapacityBytes, &resourceSummaryRaw, &app.LastCollectedAt, &app.ProtectionStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Application{}, false, nil
		}
		return Application{}, false, err
	}
	if len(labelsRaw) > 0 {
		_ = json.Unmarshal(labelsRaw, &app.Labels)
	}
	if len(resourceSummaryRaw) > 0 {
		_ = json.Unmarshal(resourceSummaryRaw, &app.ResourceSummary)
	}
	return app, true, nil
}
