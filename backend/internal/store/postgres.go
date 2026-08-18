package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/migrations"

	"github.com/jackc/pgx/v5/pgconn"
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
	time.Local = time.UTC
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

func (s *PostgresStore) GetEmailSettings() (EmailSettings, bool, error) {
	return scanEmailSettings(s.db.QueryRow(emailSettingsSelect + ` where is_default=true`))
}

const emailSettingsSelect = `select id::text,name,is_default,enabled,host,port,security,username,password_ciphertext,sender_name,sender_email,last_test_status,last_tested_at,last_test_error,created_at,updated_at from smtp_configurations`

type emailSettingsScanner interface{ Scan(...any) error }

func scanEmailSettings(scanner emailSettingsScanner) (EmailSettings, bool, error) {
	var item EmailSettings
	err := scanner.Scan(&item.ID, &item.Name, &item.IsDefault, &item.Enabled, &item.Host, &item.Port, &item.Security, &item.Username, &item.PasswordCiphertext, &item.SenderName, &item.SenderEmail, &item.LastTestStatus, &item.LastTestedAt, &item.LastTestError, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailSettings{}, false, nil
	}
	item.PasswordConfigured = item.PasswordCiphertext != ""
	return item, err == nil, err
}
func (s *PostgresStore) UpsertEmailSettings(input EmailSettingsInput) (EmailSettings, error) {
	current, found, err := s.GetEmailSettings()
	if err != nil {
		return EmailSettings{}, err
	}
	if found {
		item, _, updateErr := s.UpdateEmailSettings(current.ID, EmailSettingsInput{Name: current.Name, Enabled: input.Enabled, Host: input.Host, Port: input.Port, Security: input.Security, Username: input.Username, PasswordCiphertext: input.PasswordCiphertext, SenderName: input.SenderName, SenderEmail: input.SenderEmail, UpdatedBy: input.UpdatedBy})
		return item, updateErr
	}
	return s.CreateEmailSettings(EmailSettingsInput{Name: defaultSMTPName(input.Name), Enabled: input.Enabled, Host: input.Host, Port: input.Port, Security: input.Security, Username: input.Username, PasswordCiphertext: input.PasswordCiphertext, SenderName: input.SenderName, SenderEmail: input.SenderEmail, UpdatedBy: input.UpdatedBy})
}

func (s *PostgresStore) ListEmailSettings() ([]EmailSettings, error) {
	rows, err := s.db.Query(emailSettingsSelect + ` order by is_default desc,lower(name),created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EmailSettings{}
	for rows.Next() {
		item, _, scanErr := scanEmailSettings(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetEmailSettingsByID(id string) (EmailSettings, bool, error) {
	return scanEmailSettings(s.db.QueryRow(emailSettingsSelect+` where id=$1`, id))
}

func emailSettingsNameConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "smtp_configurations_name_unique" {
		return ErrEmailSettingsNameExists
	}
	return err
}

func (s *PostgresStore) CreateEmailSettings(input EmailSettingsInput) (EmailSettings, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return EmailSettings{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`lock table smtp_configurations in share row exclusive mode`); err != nil {
		return EmailSettings{}, err
	}
	var count int
	if err = tx.QueryRow(`select count(*) from smtp_configurations`).Scan(&count); err != nil {
		return EmailSettings{}, err
	}
	id := NewPublicID()
	_, err = tx.Exec(`insert into smtp_configurations(id,name,is_default,enabled,host,port,security,username,password_ciphertext,sender_name,sender_email,updated_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,nullif($12,'')::uuid)`, id, strings.TrimSpace(input.Name), count == 0, input.Enabled, input.Host, input.Port, input.Security, input.Username, input.PasswordCiphertext, input.SenderName, input.SenderEmail, input.UpdatedBy)
	if err != nil {
		return EmailSettings{}, emailSettingsNameConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return EmailSettings{}, err
	}
	item, _, err := s.GetEmailSettingsByID(id)
	return item, err
}

func (s *PostgresStore) UpdateEmailSettings(id string, input EmailSettingsInput) (EmailSettings, bool, error) {
	result, err := s.db.Exec(`update smtp_configurations set name=$2,enabled=$3,host=$4,port=$5,security=$6,username=$7,password_ciphertext=$8,sender_name=$9,sender_email=$10,updated_by=nullif($11,'')::uuid,updated_at=now() where id=$1`, id, strings.TrimSpace(input.Name), input.Enabled, input.Host, input.Port, input.Security, input.Username, input.PasswordCiphertext, input.SenderName, input.SenderEmail, input.UpdatedBy)
	if err != nil {
		return EmailSettings{}, false, emailSettingsNameConflict(err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return EmailSettings{}, false, nil
	}
	item, _, err := s.GetEmailSettingsByID(id)
	return item, true, err
}

func (s *PostgresStore) DeleteEmailSettings(id string) (bool, bool, error) {
	result, err := s.db.Exec(`delete from smtp_configurations where id=$1 and not is_default`, id)
	if err != nil {
		return false, false, err
	}
	count, _ := result.RowsAffected()
	if count > 0 {
		return true, false, nil
	}
	var isDefault bool
	err = s.db.QueryRow(`select is_default from smtp_configurations where id=$1`, id).Scan(&isDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return false, isDefault, err
}

func (s *PostgresStore) SetDefaultEmailSettings(id string) (EmailSettings, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return EmailSettings{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`lock table smtp_configurations in share row exclusive mode`); err != nil {
		return EmailSettings{}, false, err
	}
	var exists bool
	if err = tx.QueryRow(`select exists(select 1 from smtp_configurations where id=$1)`, id).Scan(&exists); err != nil || !exists {
		return EmailSettings{}, false, err
	}
	if _, err = tx.Exec(`update smtp_configurations set is_default=false where is_default and id<>$1`, id); err != nil {
		return EmailSettings{}, false, err
	}
	if _, err = tx.Exec(`update smtp_configurations set is_default=true,updated_at=now() where id=$1`, id); err != nil {
		return EmailSettings{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return EmailSettings{}, false, err
	}
	item, _, err := s.GetEmailSettingsByID(id)
	return item, true, err
}

func (s *PostgresStore) UpdateEmailSettingsTestResult(id, status, message string, testedAt time.Time) error {
	_, err := s.db.Exec(`update smtp_configurations set last_test_status=$2,last_tested_at=$3,last_test_error=$4 where id=$1`, id, status, testedAt.UTC(), message)
	return err
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
	err := s.db.QueryRow(`select tenant_id,image_registry,agent_namespace,velero_version,coalesce(public_endpoint,''),created_at,updated_at from platform_settings where tenant_id=$1`, DefaultTenantID).Scan(&item.TenantID, &item.ImageRegistry, &item.AgentNamespace, &item.VeleroVersion, &item.PublicEndpoint, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformSettings{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) UpsertPlatformSettings(input PlatformSettingsInput) (PlatformSettings, error) {
	var item PlatformSettings
	err := s.db.QueryRow(`insert into platform_settings(tenant_id,image_registry,agent_namespace,velero_version,public_endpoint) values($1,$2,$3,$4,nullif($5,'')) on conflict(tenant_id) do update set image_registry=excluded.image_registry,agent_namespace=excluded.agent_namespace,velero_version=excluded.velero_version,public_endpoint=excluded.public_endpoint,updated_at=now() returning tenant_id,image_registry,agent_namespace,velero_version,coalesce(public_endpoint,''),created_at,updated_at`, DefaultTenantID, strings.TrimRight(strings.TrimSpace(input.ImageRegistry), "/"), input.AgentNamespace, input.VeleroVersion, strings.TrimRight(strings.TrimSpace(input.PublicEndpoint), "/")).Scan(&item.TenantID, &item.ImageRegistry, &item.AgentNamespace, &item.VeleroVersion, &item.PublicEndpoint, &item.CreatedAt, &item.UpdatedAt)
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
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`select a.id,a.tenant_id,coalesce(a.actor_id::text,''),coalesce(u.email,''),a.action,a.resource_type,coalesce(a.resource_id::text,''),a.payload,a.created_at from audit_logs a left join users u on u.id=a.actor_id where a.actor_id is not null and a.action not in ('Create Cluster Registration Token','Start Cluster Registration') order by a.created_at desc limit $1 offset $2`, limit, offset)
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

func (s *PostgresStore) ListComponentReleases(component string) ([]ComponentRelease, error) {
	query := `select id, tenant_id, component, version, image, image_digest, status,
	                 coalesce(release_notes, ''), coalesce(published_by, ''),
	                 coalesce(published_at, '0001-01-01'::timestamptz), created_at, updated_at
	            from component_releases where tenant_id = $1`
	args := []any{DefaultTenantID}
	if component != "" {
		query += ` and component = $2`
		args = append(args, component)
	}
	query += ` order by created_at desc`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ComponentRelease{}
	for rows.Next() {
		var item ComponentRelease
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Component, &item.Version, &item.Image, &item.ImageDigest, &item.Status, &item.ReleaseNotes, &item.PublishedBy, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) GetActiveComponentRelease(component string) (ComponentRelease, bool, error) {
	var item ComponentRelease
	err := s.db.QueryRow(`select id, tenant_id, component, version, image, image_digest, status,
	                            coalesce(release_notes, ''), coalesce(published_by, ''),
	                            coalesce(published_at, '0001-01-01'::timestamptz), created_at, updated_at
	                       from component_releases
	                      where tenant_id = $1 and component = $2 and status = 'active'`, DefaultTenantID, component).Scan(
		&item.ID, &item.TenantID, &item.Component, &item.Version, &item.Image, &item.ImageDigest, &item.Status, &item.ReleaseNotes, &item.PublishedBy, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ComponentRelease{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) UpsertComponentRelease(input ComponentReleaseInput) (ComponentRelease, error) {
	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = "candidate"
	}
	var item ComponentRelease
	err := s.db.QueryRow(`insert into component_releases
		(id, tenant_id, component, version, image, image_digest, status, release_notes, published_by, published_at, created_at, updated_at)
		values ($1,$2,$3,$4,$5,$6,$7,nullif($8,''),nullif($9,''),case when $7::text='active' then $10::timestamptz else null::timestamptz end,$10::timestamptz,$10::timestamptz)
		on conflict (tenant_id, component, image_digest) do update
		set version=excluded.version, image=excluded.image, release_notes=excluded.release_notes, updated_at=excluded.updated_at
		returning id, tenant_id, component, version, image, image_digest, status,
		          coalesce(release_notes,''), coalesce(published_by,''), coalesce(published_at,'0001-01-01'::timestamptz), created_at, updated_at`,
		newID(), DefaultTenantID, input.Component, input.Version, input.Image, input.ImageDigest, status, input.ReleaseNotes, input.PublishedBy, now).Scan(
		&item.ID, &item.TenantID, &item.Component, &item.Version, &item.Image, &item.ImageDigest, &item.Status, &item.ReleaseNotes, &item.PublishedBy, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (s *PostgresStore) ActivateComponentRelease(id string, publishedBy string) (ComponentRelease, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ComponentRelease{}, false, err
	}
	defer tx.Rollback()
	var component string
	if err := tx.QueryRow(`select component from component_releases where id=$1 and tenant_id=$2 for update`, id, DefaultTenantID).Scan(&component); errors.Is(err, sql.ErrNoRows) {
		return ComponentRelease{}, false, nil
	} else if err != nil {
		return ComponentRelease{}, false, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`update component_releases set status='retired', updated_at=$3 where tenant_id=$1 and component=$2 and status='active' and id<>$4`, DefaultTenantID, component, now, id); err != nil {
		return ComponentRelease{}, false, err
	}
	var item ComponentRelease
	err = tx.QueryRow(`update component_releases set status='active', published_by=nullif($2,''), published_at=$3, updated_at=$3 where id=$1
		returning id, tenant_id, component, version, image, image_digest, status, coalesce(release_notes,''), coalesce(published_by,''), coalesce(published_at,'0001-01-01'::timestamptz), created_at, updated_at`, id, publishedBy, now).Scan(
		&item.ID, &item.TenantID, &item.Component, &item.Version, &item.Image, &item.ImageDigest, &item.Status, &item.ReleaseNotes, &item.PublishedBy, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return ComponentRelease{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ComponentRelease{}, false, err
	}
	return item, true, nil
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

func (s *PostgresStore) AuthenticateUser(input UserAuthInput) (User, bool, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" || input.Password == "" {
		return User{}, false, nil
	}
	row := s.db.QueryRow(`
		select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.password_hash,u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.is_system_admin,u.must_change_password
		from users u join resource_scopes t on t.id=u.tenant_id
		where lower(u.email)=$1 and u.status='active' and (u.is_system_admin or t.status='active')
	`, email)
	var user User
	var passwordHash string
	if err := row.Scan(&user.ID, &user.TenantID, &user.TenantName, &user.Email, &user.DisplayName, &passwordHash, &user.Role, &user.Status, &user.AuthProvider, &user.TimeZone, &user.SystemAdmin, &user.MustChangePassword); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, false, nil
		}
		return User{}, false, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)); err != nil {
		return User{}, false, nil
	}
	return user, true, nil
}

func (s *PostgresStore) CreateUser(tenantID, email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{ID: newID(), TenantID: tenantID, Email: email, Role: "operator", Status: "active", AuthProvider: "password"}
	_, err = s.db.Exec(`insert into users (id, tenant_id, email, password_hash, role, status, auth_provider, must_change_password) values ($1,$2,$3,$4,$5,$6,$7,true)`, u.ID, u.TenantID, u.Email, string(hash), u.Role, u.Status, u.AuthProvider)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUserExists
		}
		return User{}, err
	}
	return u, nil
}

func (s *PostgresStore) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.is_system_admin,u.must_change_password from users u join resource_scopes t on t.id=u.tenant_id order by case when u.is_system_admin then 0 else 1 end, u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.AuthProvider, &u.TimeZone, &u.SystemAdmin, &u.MustChangePassword); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *PostgresStore) GetUser(id string) (User, bool, error) {
	var u User
	err := s.db.QueryRow(`select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.is_system_admin,u.must_change_password from users u join resource_scopes t on t.id=u.tenant_id where u.id=$1`, id).Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.AuthProvider, &u.TimeZone, &u.SystemAdmin, &u.MustChangePassword)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	return u, err == nil, err
}

func (s *PostgresStore) UpdateUser(input UserUpdateInput) (User, bool, error) {
	result, err := s.db.Exec(`update users set tenant_id=case when is_system_admin then tenant_id else $2 end,email=case when is_system_admin then email else $3 end,display_name=nullif($4,''),role=case when is_system_admin then role else $5 end,status=case when is_system_admin then status else $6 end,time_zone=nullif($7,''),updated_at=now() where id=$1`, input.ID, input.TenantID, strings.ToLower(strings.TrimSpace(input.Email)), strings.TrimSpace(input.DisplayName), input.Role, input.Status, strings.TrimSpace(input.TimeZone))
	if err != nil {
		return User{}, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return User{}, false, nil
	}
	return s.GetUser(input.ID)
}

func (s *PostgresStore) DeleteUser(id string) (bool, error) {
	result, err := s.db.Exec(`delete from users where id=$1 and not is_system_admin`, id)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func (s *PostgresStore) SetUserPassword(id, password string, mustChangePassword bool) (User, bool, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, false, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`update users set password_hash=$2,must_change_password=$3,updated_at=now() where id=$1`, id, string(hash), mustChangePassword)
	if err != nil {
		return User{}, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return User{}, false, nil
	}
	if _, err = tx.Exec(`delete from platform_sessions where user_id=$1`, id); err != nil {
		return User{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, false, err
	}
	return s.GetUser(id)
}

func (s *PostgresStore) GetAdminRecoveryEmail(userID string) (string, bool, error) {
	var email string
	err := s.db.QueryRow(`select email from admin_recovery_email where user_id=$1`, userID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return email, err == nil, err
}
func (s *PostgresStore) SetAdminRecoveryEmail(userID, email string) (string, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	result, err := s.db.Exec(`insert into admin_recovery_email(user_id,email) select id,$2 from users where id=$1 and is_system_admin on conflict(user_id) do update set email=excluded.email,verified_at=now(),updated_at=now()`, userID, email)
	if err != nil {
		return "", false, err
	}
	n, _ := result.RowsAffected()
	return email, n == 1, nil
}

func (s *PostgresStore) CreatePlatformSession(userID string, ttl time.Duration) (PlatformSession, error) {
	token := "hcs_" + newID() + newID()
	expires := time.Now().UTC().Add(ttl)
	_, err := s.db.Exec(`insert into platform_sessions(id,user_id,token_hash,expires_at) values($1,$2,$3,$4)`, newID(), userID, resetTokenDigest(token), expires)
	return PlatformSession{Token: token, UserID: userID, ExpiresAt: expires}, err
}

func (s *PostgresStore) AuthenticatePlatformSession(token string) (User, bool, error) {
	var u User
	err := s.db.QueryRow(`select u.id,u.tenant_id,t.name,u.email,coalesce(u.display_name,''),u.role,u.status,u.auth_provider,coalesce(u.time_zone,''),u.is_system_admin,u.must_change_password from platform_sessions s join users u on u.id=s.user_id join resource_scopes t on t.id=u.tenant_id where s.token_hash=$1 and s.expires_at>now() and u.status='active' and (u.is_system_admin or t.status='active')`, resetTokenDigest(token)).Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.DisplayName, &u.Role, &u.Status, &u.AuthProvider, &u.TimeZone, &u.SystemAdmin, &u.MustChangePassword)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	_, _ = s.db.Exec(`update platform_sessions set last_seen_at=now() where token_hash=$1`, resetTokenDigest(token))
	return u, true, nil
}

func (s *PostgresStore) DeletePlatformSession(token string) error {
	_, err := s.db.Exec(`delete from platform_sessions where token_hash=$1`, resetTokenDigest(token))
	return err
}

func (s *PostgresStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	token := "hpr_" + newID() + newID()
	result, err := s.db.Exec(`insert into password_reset_tokens (id,user_id,token_hash,expires_at) select $1,u.id,$2,$3 from users u join resource_scopes t on t.id=u.tenant_id left join admin_recovery_email recovery on recovery.user_id=u.id where (lower(u.email)=$4 and not u.is_system_admin or lower(recovery.email)=$4 and u.is_system_admin) and u.status='active' and (u.is_system_admin or t.status='active')`, newID(), resetTokenDigest(token), time.Now().UTC().Add(ttl), email)
	if err != nil {
		return "", false, err
	}
	n, _ := result.RowsAffected()
	return token, n > 0, nil
}

func (s *PostgresStore) ResetPassword(token, password string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var u User
	digest := resetTokenDigest(token)
	err = tx.QueryRow(`update users u set password_hash=$1,must_change_password=false,updated_at=now() from password_reset_tokens t where t.user_id=u.id and t.token_hash=$2 and t.used_at is null and t.expires_at>now() returning u.id,u.tenant_id,u.email,u.role,u.status`, string(hash), digest).Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrResetInvalid
	}
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(`update password_reset_tokens set used_at=now() where token_hash=$1`, digest); err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func resetTokenDigest(token string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) }

func (s *PostgresStore) FindOrCreateGoogleUser(email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := s.db.QueryRow(`select id,tenant_id,email,role,status,coalesce(time_zone,'') from users where tenant_id=$1 and email=$2`, DefaultTenantID, email).Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Status, &u.TimeZone)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	randomPassword := "google:" + newID() + newID()
	u, err = s.CreateUser(DefaultTenantID, email, randomPassword)
	if err != nil {
		return User{}, err
	}
	_, err = s.db.Exec(`update users set auth_provider='google',role='operator' where id=$1`, u.ID)
	if err != nil {
		return User{}, err
	}
	return s.GetUserValue(u.ID)
}

func (s *PostgresStore) GetUserValue(id string) (User, error) {
	u, ok, err := s.GetUser(id)
	if err != nil {
		return User{}, err
	}
	if !ok {
		return User{}, sql.ErrNoRows
	}
	return u, nil
}

func (s *PostgresStore) CreateAgentToken(tenantID, createdBy, description string, ttl time.Duration) (AgentToken, error) {
	now := time.Now().UTC()
	token := AgentToken{
		ID:          newID(),
		TenantID:    tenantID,
		CreatedBy:   createdBy,
		Token:       "hcdr_" + newID() + newID(),
		Description: description,
		ExpiresAt:   now.Add(ttl),
	}

	_, err := s.db.Exec(`
		insert into agent_tokens (id, tenant_id, token_hash, description, expires_at, created_by, created_at)
		values ($1, $2, $3, $4, $5, nullif($6,'')::uuid, $7)
	`, token.ID, token.TenantID, token.Token, token.Description, token.ExpiresAt, token.CreatedBy, now)
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
		select id, tenant_id, token_hash, coalesce(description, ''), expires_at, used_at
		from agent_tokens
		where token_hash = $1 and revoked_at is null
		for update
	`, input.Token).Scan(&token.ID, &token.TenantID, &token.Token, &token.Description, &token.ExpiresAt, &usedAt)
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

	clusterName := input.ClusterName
	if clusterName == "" {
		clusterName = "registered-cluster"
	}

	var clusterCount int
	if err := tx.QueryRow(`select count(*) from clusters where tenant_id = $1`, token.TenantID).Scan(&clusterCount); err != nil {
		return Cluster{}, "", err
	}
	isFirstCluster := clusterCount == 0

	cluster := Cluster{
		ID:               newID(),
		TenantID:         token.TenantID,
		Name:             clusterName,
		KubeVersion:      input.KubeVersion,
		Status:           "healthy",
		ConnectionStatus: "online",
		AgentVersion:     input.AgentVersion,
		VeleroVersion:    input.VeleroVersion,
		VeleroStatus:     input.VeleroStatus,
		Role:             "both",
		IsDefault:        isFirstCluster,
		RegisteredAt:     now,
		LastSeenAt:       now,
	}

	_, err = tx.Exec(`
		insert into clusters (
			id, tenant_id, name, kube_version, status, connection_status,
			agent_version, velero_version, velero_status, registered_at, last_seen_at,
			role, is_default, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
	`, cluster.ID, cluster.TenantID, cluster.Name, cluster.KubeVersion, cluster.Status,
		cluster.ConnectionStatus, cluster.AgentVersion, cluster.VeleroVersion, cluster.VeleroStatus,
		cluster.RegisteredAt, cluster.LastSeenAt, cluster.Role, cluster.IsDefault, now)
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
		       coalesce(registered_at, created_at), coalesce(last_seen_at, created_at), metadata
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
	var tenantID string
	if err := tx.QueryRow(`select true, is_default, tenant_id from clusters where id = $1`, clusterID).Scan(&exists, &wasDefault, &tenantID); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`
		update tasks
		set payload = jsonb_set(coalesce(payload, '{}'::jsonb), '{archivedRestorePointId}', to_jsonb(restore_point_id::text), true),
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
		set payload = jsonb_set(coalesce(payload, '{}'::jsonb), '{archivedProtectionPlanId}', to_jsonb(protection_plan_id::text), true),
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
		set payload = jsonb_set(coalesce(payload, '{}'::jsonb), '{archivedAppId}', to_jsonb(app_id::text), true),
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
		set payload = jsonb_set(coalesce(payload, '{}'::jsonb), '{archivedClusterId}', to_jsonb(cluster_id::text), true),
		    cluster_id = null
		where cluster_id = $1
	`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from cluster_nodes where cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`delete from agent_sessions where cluster_id = $1`, clusterID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`update agent_tokens set cluster_id = null where cluster_id = $1`, clusterID); err != nil {
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
	if wasDefault {
		if _, err := tx.Exec(`
			update clusters
			set is_default = true, updated_at = now()
			where id = (
				select id from clusters
				where tenant_id = $1
				order by registered_at asc, id asc
				limit 1
			)
		`, tenantID); err != nil {
			return false, err
		}
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
	query := `
		select id, cluster_id, namespace, name, status, labels,
		       workload_count, service_count, ingress_count, configmap_count, secret_count,
		       pvc_count, pv_capacity_bytes, resource_summary, coalesce(last_collected_at, created_at), protection_status,
		       coalesce((select jsonb_agg(at.tag_id::text) from application_tags at where at.application_id=applications.id),'[]'::jsonb)
		from applications
	`
	args := []any{}
	if clusterID != "" {
		query += ` where cluster_id = $1`
		args = append(args, clusterID)
	}
	query += ` order by namespace`

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
	if err = tx.QueryRow(`select tenant_id from applications where id=$1`, applicationID).Scan(&tenantID); errors.Is(err, sql.ErrNoRows) {
		return Application{}, false, nil
	} else if err != nil {
		return Application{}, false, err
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
		       coalesce(registered_at, created_at), coalesce(last_seen_at, created_at), metadata
		from clusters
		where id = $1
	`, clusterID).Scan(
		&cluster.ID, &cluster.TenantID, &cluster.Name, &cluster.KubeVersion, &cluster.Status,
		&cluster.ConnectionStatus, &cluster.NodeCount, &cluster.NamespaceCount, &cluster.ApplicationCount,
		&cluster.ActiveTasks, &cluster.AgentVersion, &cluster.VeleroVersion, &cluster.VeleroStatus,
		&cluster.InventoryHash, &cluster.Role, &cluster.IsDefault, &cluster.RegisteredAt, &cluster.LastSeenAt, &metadataRaw,
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

func (s *PostgresStore) CreatePolicy(input PolicyInput) (Policy, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	now := time.Now().UTC()
	if input.Composition == "" {
		input.Composition = "manual"
	}
	if input.ScheduleType == "" {
		input.ScheduleType = "manual"
	}
	if input.Status == "" {
		input.Status = "pending_activation"
	}
	policy := Policy{
		ID:             newID(),
		TenantID:       input.TenantID,
		Name:           input.Name,
		Composition:    input.Composition,
		ScheduleType:   input.ScheduleType,
		IntervalValue:  input.IntervalValue,
		IntervalUnit:   input.IntervalUnit,
		Hour:           input.Hour,
		Minute:         input.Minute,
		WeekDay:        input.WeekDay,
		MonthDay:       input.MonthDay,
		RetentionCount: input.RetentionCount,
		RetentionDays:  input.RetentionDays,
		Status:         input.Status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_, err := s.db.Exec(`
		insert into policies (
			id, tenant_id, name, composition, schedule_type, interval_value, interval_unit,
			hour, minute, week_day, month_day, retention_count, retention_days, status,
			created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)
	`, policy.ID, policy.TenantID, policy.Name, policy.Composition, policy.ScheduleType,
		policy.IntervalValue, policy.IntervalUnit, policy.Hour, policy.Minute, policy.WeekDay,
		policy.MonthDay, policy.RetentionCount, policy.RetentionDays, policy.Status, now)
	return policy, err
}

func (s *PostgresStore) ListPolicies() ([]Policy, error) {
	rows, err := s.db.Query(`
		select id, tenant_id, name, composition, schedule_type,
		       coalesce(interval_value, 0), coalesce(interval_unit, ''),
		       coalesce(hour, 0), coalesce(minute, 0), coalesce(week_day, 0), coalesce(month_day, 0),
		       coalesce(retention_count, 0), coalesce(retention_days, 0),
		       status, coalesce(policy_bindings.bound_count, 0), created_at, updated_at
		from policies
		left join (
			select policy_id, count(distinct app_id) as bound_count
			from (
				select policy_id, app_id
				from protection_plans
				where policy_id is not null
				union all
				select pp.policy_id, ppa.app_id
				from protection_plans pp
				join protection_plan_apps ppa on ppa.plan_id = pp.id
				where pp.policy_id is not null
			) bound_apps
			group by policy_id
		) policy_bindings on policy_bindings.policy_id = policies.id
		order by policies.created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Policy
	for rows.Next() {
		var item Policy
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Composition, &item.ScheduleType,
			&item.IntervalValue, &item.IntervalUnit, &item.Hour, &item.Minute, &item.WeekDay,
			&item.MonthDay, &item.RetentionCount, &item.RetentionDays, &item.Status, &item.BoundCount,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdatePolicy(id string, input PolicyInput) (Policy, bool, error) {
	result, err := s.db.Exec(`update policies set name=$2,composition=$3,schedule_type=$4,interval_value=$5,interval_unit=nullif($6,''),hour=$7,minute=$8,week_day=$9,month_day=$10,retention_count=$11,retention_days=$12,status=$13,updated_at=now() where id=$1`, id, input.Name, input.Composition, input.ScheduleType, input.IntervalValue, input.IntervalUnit, input.Hour, input.Minute, input.WeekDay, input.MonthDay, input.RetentionCount, input.RetentionDays, input.Status)
	if err != nil {
		return Policy{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return Policy{}, false, err
	}
	items, err := s.ListPolicies()
	if err != nil {
		return Policy{}, false, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, true, nil
		}
	}
	return Policy{}, false, nil
}

func (s *PostgresStore) DeletePolicy(id string) (bool, bool, error) {
	var tenantID string
	if err := s.db.QueryRow(`select tenant_id from policies where id=$1`, id).Scan(&tenantID); errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}
	var inUse bool
	if err := s.db.QueryRow(`select exists(select 1 from protection_plans where tenant_id=$1 and policy_id=$2)`, tenantID, id).Scan(&inUse); err != nil {
		return false, false, err
	}
	if inUse {
		return false, true, nil
	}
	result, err := s.db.Exec(`delete from policies where id=$1 and tenant_id=$2`, id, tenantID)
	if err != nil {
		return false, false, err
	}
	count, err := result.RowsAffected()
	return count > 0, false, err
}

func (s *PostgresStore) CreateProtectionPlan(input ProtectionPlanInput) (ProtectionPlan, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	now := time.Now().UTC()
	if input.ScopeType == "" {
		input.ScopeType = "all"
	}
	if input.Status == "" {
		input.Status = "active"
	}
	includedResources, err := json.Marshal(input.IncludedResources)
	if err != nil {
		return ProtectionPlan{}, err
	}
	selection := input.ResourceSelection
	if selection.Mode == "" {
		selection.Mode = "all"
	}
	resourceSelection, err := json.Marshal(selection)
	if err != nil {
		return ProtectionPlan{}, err
	}
	labelSelector, err := json.Marshal(input.LabelSelector)
	if err != nil {
		return ProtectionPlan{}, err
	}
	excludedResources, err := json.Marshal(input.ExcludedResources)
	if err != nil {
		return ProtectionPlan{}, err
	}
	preHooks, err := json.Marshal(input.PreHooks)
	if err != nil {
		return ProtectionPlan{}, err
	}
	postHooks, err := json.Marshal(input.PostHooks)
	if err != nil {
		return ProtectionPlan{}, err
	}
	// Merge AppIDs (new) with the legacy AppID for backward compatibility.
	appIDs := dedupNonEmpty(append([]string{}, input.AppIDs...))
	if input.AppID != "" {
		appIDs = dedupNonEmpty(append(appIDs, input.AppID))
	}
	primary := ""
	if len(appIDs) > 0 {
		primary = appIDs[0]
	}
	plan := ProtectionPlan{
		ID:                   newID(),
		TenantID:             input.TenantID,
		SourceClusterID:      input.SourceClusterID,
		AppID:                primary,
		AppIDs:               appIDs,
		ScopeType:            input.ScopeType,
		IncludedResources:    input.IncludedResources,
		ResourceSelection:    selection,
		LabelSelector:        input.LabelSelector,
		IncludeClusterScoped: input.IncludeClusterScoped,
		StorageRepoID:        input.StorageRepoID,
		PolicyID:             input.PolicyID,
		TargetClusterID:      input.TargetClusterID,
		ExcludedResources:    input.ExcludedResources,
		PreHooks:             input.PreHooks,
		PostHooks:            input.PostHooks,
		Status:               input.Status,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ProtectionPlan{}, err
	}
	defer tx.Rollback()
	// Lock the application rows so concurrent requests for the same app cannot
	// both pass the ownership check and create duplicate plans.
	if len(appIDs) > 0 {
		rows, err := tx.Query(`select id from applications where id = any($1::uuid[]) order by id for update`, planIDsSlice(appIDs))
		if err != nil {
			return ProtectionPlan{}, err
		}
		for rows.Next() {
			var lockedID string
			if err := rows.Scan(&lockedID); err != nil {
				rows.Close()
				return ProtectionPlan{}, err
			}
		}
		if err := rows.Close(); err != nil {
			return ProtectionPlan{}, err
		}
		var existingPlanID, existingAppID string
		err = tx.QueryRow(`
			select p.id, ppa.app_id
			from protection_plans p
			join protection_plan_apps ppa on ppa.plan_id = p.id
			where p.tenant_id = $1 and p.source_cluster_id = $2
			  and ppa.app_id = any($3::uuid[])
			limit 1
		`, plan.TenantID, plan.SourceClusterID, planIDsSlice(appIDs)).Scan(&existingPlanID, &existingAppID)
		if err == nil {
			return ProtectionPlan{}, &ApplicationAlreadyProtectedError{ProtectionPlanID: existingPlanID, ApplicationID: existingAppID}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, err
		}
	}
	_, err = tx.Exec(`
		insert into protection_plans (
			id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, resource_selection, label_selector,
			include_cluster_scoped, storage_repo_id, policy_id, target_cluster_id,
			excluded_resources, pre_hooks, post_hooks, status, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, nullif($10, '')::uuid, nullif($11, '')::uuid,
			nullif($12, '')::uuid, $13, $14, $15, $16, $17, $17)
	`, plan.ID, plan.TenantID, plan.SourceClusterID, plan.AppID, plan.ScopeType, includedResources, resourceSelection, labelSelector,
		plan.IncludeClusterScoped, plan.StorageRepoID, plan.PolicyID, plan.TargetClusterID,
		excludedResources, preHooks, postHooks, plan.Status, now)
	if err != nil {
		return ProtectionPlan{}, err
	}
	for _, appID := range appIDs {
		if _, err := tx.Exec(`
			insert into protection_plan_apps (plan_id, app_id, created_at)
			values ($1, $2, $3)
			on conflict (plan_id, app_id) do nothing
		`, plan.ID, appID, now); err != nil {
			return ProtectionPlan{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProtectionPlan{}, err
	}
	return plan, nil
}

func (s *PostgresStore) UpdateProtectionPlanStatus(id string, status string) (ProtectionPlan, bool, error) {
	if id == "" {
		return ProtectionPlan{}, false, nil
	}
	if status == "" {
		status = "pending_activation"
	}
	_, err := s.db.Exec(`
		update protection_plans
		set status = $2,
		    updated_at = $3
		where id = $1
	`, id, status, time.Now().UTC())
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	return s.GetProtectionPlan(id)
}

func (s *PostgresStore) UpdateProtectionPlanStorageSize(id string, size map[string]any) (ProtectionPlan, bool, error) {
	if id == "" || len(size) == 0 {
		return ProtectionPlan{}, false, nil
	}
	raw, err := json.Marshal(size)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := s.db.Exec(`
		update protection_plans
		set plan_storage_size = $2::jsonb,
		    updated_at = $3
		where id = $1
	`, id, raw, time.Now().UTC()); err != nil {
		return ProtectionPlan{}, false, err
	}
	return s.GetProtectionPlan(id)
}

func (s *PostgresStore) UpsertProtectionPlanSchedule(input ProtectionPlanScheduleInput) (ProtectionPlanSchedule, error) {
	now := time.Now().UTC()
	enabled := input.Enabled
	var item ProtectionPlanSchedule
	err := s.db.QueryRow(`
		insert into protection_plan_schedules (
			protection_plan_id, next_fire_at, enabled, created_at, updated_at
		)
		values ($1, nullif($2, '0001-01-01'::timestamptz), $3, $4, $4)
		on conflict (protection_plan_id) do update
		   set next_fire_at = excluded.next_fire_at,
		       enabled = excluded.enabled,
		       updated_at = excluded.updated_at
		returning protection_plan_id::text,
		          coalesce(last_fired_at, '0001-01-01'::timestamptz),
		          coalesce(next_fire_at, '0001-01-01'::timestamptz),
		          enabled, created_at, updated_at
	`, input.ProtectionPlanID, input.NextFireAt, enabled, now).Scan(
		&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *PostgresStore) GetProtectionPlanSchedule(planID string) (ProtectionPlanSchedule, bool, error) {
	var item ProtectionPlanSchedule
	err := s.db.QueryRow(`
		select pps.protection_plan_id::text,
		       coalesce(pps.last_fired_at, '0001-01-01'::timestamptz),
		       coalesce(pps.next_fire_at, '0001-01-01'::timestamptz),
		       pps.enabled, pps.created_at, pps.updated_at
		from protection_plan_schedules pps
		where pps.protection_plan_id = $1
	`, planID).Scan(&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProtectionPlanSchedule{}, false, nil
	}
	if err != nil {
		return ProtectionPlanSchedule{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) ListDueProtectionPlanSchedules(now time.Time) ([]ProtectionPlanSchedule, error) {
	rows, err := s.db.Query(`
		select pps.protection_plan_id::text,
		       coalesce(pps.last_fired_at, '0001-01-01'::timestamptz),
		       coalesce(pps.next_fire_at, '0001-01-01'::timestamptz),
		       pps.enabled, pps.created_at, pps.updated_at
		from protection_plan_schedules pps
		join protection_plans pp on pp.id=pps.protection_plan_id
		join resource_scopes t on t.id=pp.tenant_id and t.status='active'
		where pps.enabled = true
		  and pps.next_fire_at is not null
		  and pps.next_fire_at <= $1
		order by pps.next_fire_at asc
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProtectionPlanSchedule{}
	for rows.Next() {
		var item ProtectionPlanSchedule
		if err := rows.Scan(&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) MarkProtectionPlanScheduleFired(input ProtectionPlanScheduleFiredInput) (ProtectionPlanSchedule, bool, error) {
	var item ProtectionPlanSchedule
	err := s.db.QueryRow(`
		update protection_plan_schedules
		   set last_fired_at = nullif($2, '0001-01-01'::timestamptz),
		       next_fire_at = nullif($3, '0001-01-01'::timestamptz),
		       updated_at = $4
		 where protection_plan_id = $1
		returning protection_plan_id::text,
		          coalesce(last_fired_at, '0001-01-01'::timestamptz),
		          coalesce(next_fire_at, '0001-01-01'::timestamptz),
		          enabled, created_at, updated_at
	`, input.ProtectionPlanID, input.LastFiredAt, input.NextFireAt, time.Now().UTC()).Scan(
		&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProtectionPlanSchedule{}, false, nil
	}
	if err != nil {
		return ProtectionPlanSchedule{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) DisableProtectionPlanSchedule(planID string) error {
	_, err := s.db.Exec(`
		update protection_plan_schedules
		   set enabled = false,
		       updated_at = $2
		 where protection_plan_id = $1
	`, planID, time.Now().UTC())
	return err
}

func (s *PostgresStore) ListProtectionPlans(clusterID string) ([]ProtectionPlan, error) {
	query := `
		select pp.id, pp.tenant_id, pp.source_cluster_id, pp.app_id, pp.scope_type, pp.included_resources, pp.resource_selection, pp.label_selector,
		       pp.include_cluster_scoped, coalesce(pp.storage_repo_id::text, ''), coalesce(pp.policy_id::text, ''),
		       coalesce(pp.target_cluster_id::text, ''), pp.excluded_resources, pp.pre_hooks, pp.post_hooks,
		       pp.plan_storage_size, coalesce(pps.next_fire_at, '0001-01-01'::timestamptz), coalesce(pps.enabled, false),
		       pp.status, pp.created_at, pp.updated_at
		from protection_plans pp
		left join protection_plan_schedules pps on pps.protection_plan_id = pp.id
	`
	args := []any{}
	if clusterID != "" {
		query += ` where pp.source_cluster_id = $1`
		args = append(args, clusterID)
	}
	query += ` order by pp.created_at desc`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	planIDs := make([]string, 0, 32)
	planMap := map[string]*ProtectionPlan{}
	for rows.Next() {
		var item ProtectionPlan
		var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
			&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
			&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize,
			&item.NextFireAt, &item.ScheduleEnabled, &item.Status,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
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
		item.AppIDs = []string{}
		planIDs = append(planIDs, item.ID)
		planCopy := item
		planMap[item.ID] = &planCopy
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(planIDs) > 0 {
		if err := s.loadProtectionPlanApps(planIDs, planMap); err != nil {
			return nil, err
		}
	}
	items := make([]ProtectionPlan, 0, len(planMap))
	for _, id := range planIDs {
		items = append(items, *planMap[id])
	}
	return items, nil
}

func (s *PostgresStore) loadProtectionPlanApps(planIDs []string, planMap map[string]*ProtectionPlan) error {
	rows, err := s.db.Query(`
		select plan_id, app_id
		from protection_plan_apps
		where plan_id = any($1::uuid[])
		order by plan_id, created_at
	`, planIDsSlice(planIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var planID, appID string
		if err := rows.Scan(&planID, &appID); err != nil {
			return err
		}
		if plan, ok := planMap[planID]; ok {
			plan.AppIDs = append(plan.AppIDs, appID)
		}
	}
	return rows.Err()
}

func (s *PostgresStore) DeleteProtectionPlan(id string) (ProtectionPlan, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	defer tx.Rollback()

	var item ProtectionPlan
	var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	err = tx.QueryRow(`
		select id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, resource_selection, label_selector,
		       include_cluster_scoped, coalesce(storage_repo_id::text, ''), coalesce(policy_id::text, ''),
		       coalesce(target_cluster_id::text, ''), excluded_resources, pre_hooks, post_hooks,
		       plan_storage_size, status, created_at, updated_at
		from protection_plans
		where id = $1
		for update
	`, id).Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
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

	rows, err := tx.Query(`select app_id from protection_plan_apps where plan_id = $1 order by created_at`, id)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	for rows.Next() {
		var appID string
		if err := rows.Scan(&appID); err != nil {
			rows.Close()
			return ProtectionPlan{}, false, err
		}
		item.AppIDs = append(item.AppIDs, appID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ProtectionPlan{}, false, err
	}
	rows.Close()
	if len(item.AppIDs) == 0 && item.AppID != "" {
		item.AppIDs = []string{item.AppID}
	}

	if _, err := tx.Exec(`
		update tasks
		set payload = jsonb_set(coalesce(payload, '{}'::jsonb), '{archivedProtectionPlanId}', to_jsonb(protection_plan_id::text), true),
		    protection_plan_id = null
		where protection_plan_id = $1
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`
		update restore_points
		set metadata = jsonb_set(coalesce(metadata, '{}'::jsonb), '{archivedProtectionPlanId}', to_jsonb(protection_plan_id::text), true),
		    protection_plan_id = null
		where protection_plan_id = $1
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plan_apps where plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plans where id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if len(item.AppIDs) > 0 {
		if _, err := tx.Exec(`
			update applications
			set protection_status = 'unprotected',
			    updated_at = $2
			where id = any($1::uuid[])
		`, planIDsSlice(item.AppIDs), time.Now().UTC()); err != nil {
			return ProtectionPlan{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProtectionPlan{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) CleanupProtectionPlanRecords(id string) (ProtectionPlan, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	defer tx.Rollback()

	var item ProtectionPlan
	var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	err = tx.QueryRow(`
		select id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, resource_selection, label_selector,
		       include_cluster_scoped, coalesce(storage_repo_id::text, ''), coalesce(policy_id::text, ''),
		       coalesce(target_cluster_id::text, ''), excluded_resources, pre_hooks, post_hooks,
		       plan_storage_size, status, created_at, updated_at
		from protection_plans
		where id = $1
		for update
	`, id).Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
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

	rows, err := tx.Query(`select app_id from protection_plan_apps where plan_id = $1 order by created_at`, id)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	for rows.Next() {
		var appID string
		if err := rows.Scan(&appID); err != nil {
			rows.Close()
			return ProtectionPlan{}, false, err
		}
		item.AppIDs = append(item.AppIDs, appID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ProtectionPlan{}, false, err
	}
	rows.Close()
	if len(item.AppIDs) == 0 && item.AppID != "" {
		item.AppIDs = []string{item.AppID}
	}

	if _, err := tx.Exec(`
		delete from tasks
		where protection_plan_id = $1
		   or restore_point_id in (
		     select id from restore_points where protection_plan_id = $1
		   )
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from restore_points where protection_plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plan_apps where plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plans where id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if len(item.AppIDs) > 0 {
		if _, err := tx.Exec(`
			update applications
			set protection_status = 'pending_protection',
			    updated_at = $2
			where id = any($1::uuid[])
			  and not exists (
			    select 1
			    from protection_plan_apps ppa
			    join protection_plans pp on pp.id = ppa.plan_id
			    where ppa.app_id = applications.id
			      and pp.tenant_id = $3
			      and pp.source_cluster_id = $4
			  )
		`, planIDsSlice(item.AppIDs), time.Now().UTC(), item.TenantID, item.SourceClusterID); err != nil {
			return ProtectionPlan{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProtectionPlan{}, false, err
	}
	return item, true, nil
}

func planIDsSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dedupNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func (s *PostgresStore) CreateRestorePoint(input RestorePointInput) (RestorePoint, error) {
	now := time.Now().UTC()
	if input.PointType == "" {
		input.PointType = "backup"
	}
	if input.Status == "" {
		input.Status = "available"
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if input.SourceNamespace != "" {
		metadata["sourceNamespace"] = input.SourceNamespace
	}
	if input.LabelSelector != "" {
		metadata["labelSelector"] = input.LabelSelector
	}
	if input.BackupTaskID != "" {
		metadata["backupTaskId"] = input.BackupTaskID
	}
	if input.BackupStorageName != "" {
		metadata["backupStorageName"] = input.BackupStorageName
	}
	metadataRaw, err := json.Marshal(metadata)
	if err != nil {
		return RestorePoint{}, err
	}
	sizeMetricsRaw, err := json.Marshal(input.SizeMetricsV2)
	if err != nil {
		return RestorePoint{}, err
	}
	tenantID := DefaultTenantID
	_ = s.db.QueryRow(`select tenant_id from clusters where id=$1`, input.SourceClusterID).Scan(&tenantID)
	point := RestorePoint{
		ID:                newID(),
		TenantID:          tenantID,
		ProtectionPlanID:  input.ProtectionPlanID,
		SourceClusterID:   input.SourceClusterID,
		AppID:             input.AppID,
		StorageRepoID:     input.StorageRepoID,
		DisplayName:       "",
		VeleroBackupName:  input.VeleroBackupName,
		PointType:         input.PointType,
		Status:            input.Status,
		SizeBytes:         input.SizeBytes,
		StartedAt:         input.StartedAt,
		CompletedAt:       input.CompletedAt,
		ExpiresAt:         input.ExpiresAt,
		SourceNamespace:   input.SourceNamespace,
		LabelSelector:     input.LabelSelector,
		BackupTaskID:      input.BackupTaskID,
		BackupStorageName: input.BackupStorageName,
		TaskCreatedAt:     input.TaskCreatedAt,
		Metadata:          metadata,
		SizeMetricsV2:     input.SizeMetricsV2,
		CreatedAt:         now,
	}
	if point.TaskCreatedAt.IsZero() {
		point.TaskCreatedAt = now
	}

	_, err = s.db.Exec(`
		insert into restore_points (
			id, tenant_id, protection_plan_id, source_cluster_id, app_id, storage_repo_id,
			display_name, velero_backup_name, point_type, status, size_bytes, started_at, completed_at,
			expires_at, metadata, created_at, task_created_at, size_metrics_v2
		)
		values ($1, $2, nullif($3, '')::uuid, $4, nullif($5, '')::uuid, nullif($6, '')::uuid,
			$7, $8, $9, $10, nullif($11, 0), nullif($12, '0001-01-01'::timestamptz),
			nullif($13, '0001-01-01'::timestamptz), nullif($14, '0001-01-01'::timestamptz), $15, $16, $17, $18)
		on conflict (source_cluster_id, velero_backup_name) do update
		   set display_name = '',
		       size_bytes = coalesce(excluded.size_bytes, restore_points.size_bytes),
		       completed_at = coalesce(excluded.completed_at, restore_points.completed_at),
		       app_id = coalesce(restore_points.app_id, excluded.app_id),
		       storage_repo_id = coalesce(restore_points.storage_repo_id, excluded.storage_repo_id),
		       task_created_at = coalesce(restore_points.task_created_at, excluded.task_created_at),
		       metadata = coalesce(restore_points.metadata, '{}'::jsonb)
		           || (coalesce(excluded.metadata, '{}'::jsonb)
		               - array['velero', 'size', 'restorePointSize', 'planStorageSize', 'sizeStatus', 'sizeWarnings']),
		       size_metrics_v2 = case when excluded.size_metrics_v2 = '{}'::jsonb then restore_points.size_metrics_v2 else excluded.size_metrics_v2 end
	`, point.ID, point.TenantID, point.ProtectionPlanID, point.SourceClusterID, point.AppID,
		point.StorageRepoID, point.DisplayName, point.VeleroBackupName, point.PointType, point.Status, point.SizeBytes,
		point.StartedAt, point.CompletedAt, point.ExpiresAt, metadataRaw, now, point.TaskCreatedAt, sizeMetricsRaw)
	if err != nil {
		return RestorePoint{}, err
	}
	points, err := s.ListRestorePoints(RestorePointFilter{ClusterID: input.SourceClusterID})
	if err != nil {
		return RestorePoint{}, err
	}
	for _, existing := range points {
		if existing.VeleroBackupName == input.VeleroBackupName {
			return existing, nil
		}
	}
	return point, nil
}

func (s *PostgresStore) ListRestorePoints(filter RestorePointFilter) ([]RestorePoint, error) {
	query := `
		select id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       coalesce(task_created_at, created_at), metadata, created_at, size_metrics_v2
		from restore_points
	`
	args := []any{}
	conditions := []string{}
	if filter.ClusterID != "" {
		args = append(args, filter.ClusterID)
		conditions = append(conditions, "source_cluster_id = $"+strconv.Itoa(len(args)))
	}
	if filter.AppID != "" {
		args = append(args, filter.AppID)
		conditions = append(conditions, "app_id = $"+strconv.Itoa(len(args))+"::uuid")
	}
	if filter.ProtectionPlanID != "" {
		args = append(args, filter.ProtectionPlanID)
		conditions = append(conditions, "protection_plan_id = $"+strconv.Itoa(len(args))+"::uuid")
	}
	if !filter.IncludeDeleted {
		conditions = append(conditions, "status <> 'deleted'")
		conditions = append(conditions, `exists (
			select 1 from protection_plans pp
			where pp.id = restore_points.protection_plan_id and pp.source_cluster_id = restore_points.source_cluster_id
		)`)
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += ` order by created_at desc`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RestorePoint
	for rows.Next() {
		var item RestorePoint
		var metadataRaw, sizeMetricsRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
			&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
			&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.TaskCreatedAt, &metadataRaw, &item.CreatedAt, &sizeMetricsRaw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadataRaw, &item.Metadata)
		_ = json.Unmarshal(sizeMetricsRaw, &item.SizeMetricsV2)
		hydrateRestorePointMetadata(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

func hydrateRestorePointMetadata(item *RestorePoint) {
	if item.Metadata == nil {
		return
	}
	item.SourceNamespace, _ = item.Metadata["sourceNamespace"].(string)
	item.LabelSelector, _ = item.Metadata["labelSelector"].(string)
	item.BackupTaskID, _ = item.Metadata["backupTaskId"].(string)
	item.BackupStorageName, _ = item.Metadata["backupStorageName"].(string)
}

func (s *PostgresStore) GetRestorePoint(id string) (RestorePoint, bool, error) {
	var item RestorePoint
	var metadataRaw, sizeMetricsRaw []byte
	err := s.db.QueryRow(`
		select id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       coalesce(task_created_at, created_at), metadata, created_at, size_metrics_v2
		from restore_points
		where id = $1
	`, id).Scan(&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
		&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
		&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.TaskCreatedAt, &metadataRaw, &item.CreatedAt, &sizeMetricsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return RestorePoint{}, false, nil
	}
	if err != nil {
		return RestorePoint{}, false, err
	}
	_ = json.Unmarshal(metadataRaw, &item.Metadata)
	_ = json.Unmarshal(sizeMetricsRaw, &item.SizeMetricsV2)
	hydrateRestorePointMetadata(&item)
	return item, true, nil
}

func (s *PostgresStore) UpdateRestorePointState(input RestorePointStateInput) (RestorePoint, bool, error) {
	if input.ID == "" {
		return RestorePoint{}, false, nil
	}
	status := input.Status
	if status == "" {
		status = "available"
	}
	metadataRaw, err := json.Marshal(input.Metadata)
	if err != nil {
		return RestorePoint{}, false, err
	}
	var item RestorePoint
	var metadataRawOut []byte
	err = s.db.QueryRow(`
		update restore_points
		   set status = $2,
		       metadata = coalesce(metadata, '{}'::jsonb) || $3::jsonb
		 where id = $1
		returning id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       coalesce(task_created_at, created_at), metadata, created_at
	`, input.ID, status, metadataRaw).Scan(
		&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
		&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
		&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.TaskCreatedAt, &metadataRawOut, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RestorePoint{}, false, nil
	}
	if err != nil {
		return RestorePoint{}, false, err
	}
	_ = json.Unmarshal(metadataRawOut, &item.Metadata)
	hydrateRestorePointMetadata(&item)
	return item, true, nil
}

func (s *PostgresStore) CreateTask(input TaskInput) (Task, error) {
	now := time.Now().UTC()
	if input.Type == "" {
		input.Type = "backup"
	}
	if input.Status == "" {
		input.Status = "queued"
	}
	payloadRaw, err := json.Marshal(input.Payload)
	if err != nil {
		return Task{}, err
	}
	tenantID := DefaultTenantID
	_ = s.db.QueryRow(`select tenant_id from clusters where id=$1`, input.ClusterID).Scan(&tenantID)
	task := Task{
		ID:               newID(),
		TenantID:         tenantID,
		ClusterID:        input.ClusterID,
		AppID:            input.AppID,
		ProtectionPlanID: input.ProtectionPlanID,
		RestorePointID:   input.RestorePointID,
		Type:             input.Type,
		Status:           input.Status,
		CommandID:        input.CommandID,
		Payload:          input.Payload,
		CreatedAt:        now,
	}
	if task.Payload == nil {
		task.Payload = map[string]any{}
	}
	_, err = s.db.Exec(`
		insert into tasks (
			id, tenant_id, cluster_id, app_id, protection_plan_id, restore_point_id, type, status,
			progress, command_id, payload, created_at
		)
		values ($1, $2, $3, nullif($4, '')::uuid, nullif($5, '')::uuid, nullif($6, '')::uuid,
			$7, $8, 0, nullif($9, '')::uuid, $10, $11)
	`, task.ID, task.TenantID, task.ClusterID, task.AppID, task.ProtectionPlanID, task.RestorePointID,
		task.Type, task.Status, task.CommandID, payloadRaw, now)
	return task, err
}

func (s *PostgresStore) ListTasks(clusterID string) ([]Task, error) {
	query := `
		select id, tenant_id, cluster_id, coalesce(app_id::text, ''), coalesce(protection_plan_id::text, ''),
		       coalesce(restore_point_id::text, ''), type, status, progress, coalesce(command_id::text, ''),
		       coalesce(error_code, ''), coalesce(error_message, ''), payload,
		       created_at, coalesce(dispatched_at, '0001-01-01'::timestamptz),
		       coalesce(accepted_at, '0001-01-01'::timestamptz),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz)
		from tasks
	`
	args := []any{}
	if clusterID != "" {
		query += ` where cluster_id = $1`
		args = append(args, clusterID)
	}
	query += ` order by created_at desc`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Task
	for rows.Next() {
		var item Task
		var clusterID sql.NullString
		var payloadRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &clusterID, &item.AppID,
			&item.ProtectionPlanID, &item.RestorePointID, &item.Type, &item.Status, &item.Progress,
			&item.CommandID, &item.ErrorCode, &item.ErrorMessage, &payloadRaw, &item.CreatedAt,
			&item.DispatchedAt, &item.AcceptedAt, &item.StartedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		if clusterID.Valid {
			item.ClusterID = clusterID.String
		}
		_ = json.Unmarshal(payloadRaw, &item.Payload)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateTaskStatus(input TaskStatusInput) (Task, bool, error) {
	now := time.Now().UTC()
	payloadRaw := []byte("{}")
	if input.Payload != nil {
		var err error
		payloadRaw, err = json.Marshal(input.Payload)
		if err != nil {
			return Task{}, false, err
		}
	}
	result, err := s.db.Exec(`
		update tasks
		set status = case
		        when completed_at is not null and $2 in ('queued', 'dispatched', 'accepted', 'running', 'syncing', 'finalizing', 'canceling') then status
		        else coalesce(nullif($2, ''), status)
		    end,
		    restore_point_id = coalesce(nullif($11, '')::uuid, restore_point_id),
		    progress = greatest(progress, $3),
		    error_code = nullif($4, ''),
		    error_message = nullif($5, ''),
		    payload = coalesce(payload, '{}'::jsonb) || coalesce($10::jsonb, '{}'::jsonb),
		    accepted_at = case when $6 then coalesce(accepted_at, $9) else accepted_at end,
		    started_at = case when $7 then coalesce(started_at, $9) else started_at end,
		    completed_at = case when $8 then coalesce(completed_at, $9) else completed_at end,
		    dispatched_at = case when $2 = 'dispatched' then coalesce(dispatched_at, $9) else dispatched_at end
		where id = $1
	`, input.TaskID, input.Status, input.Progress, input.ErrorCode, input.ErrorMessage,
		input.MarkAccepted, input.MarkStarted, input.MarkDone, now, payloadRaw, input.RestorePointID)
	if err != nil {
		return Task{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Task{}, false, err
	}
	if affected == 0 {
		return Task{}, false, nil
	}
	task, ok, err := s.getTask(input.TaskID)
	return task, ok, err
}

func (s *PostgresStore) AddTaskEvent(input TaskEventInput) error {
	payloadRaw, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	_, err = tx.Exec(`
		insert into task_events (id, task_id, level, reason, message, payload, created_at)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, newID(), input.TaskID, input.Level, input.Reason, input.Message, payloadRaw, now)
	if err != nil {
		return err
	}
	if s.diagnosticWriter == nil {
		_, err = tx.Exec(`insert into diagnostic_logs(id,tenant_id,scope,level,component,operation,message,cluster_id,task_id,command_id,error_code,status,details,created_at)
			select $1,t.tenant_id,'tenant',$2,'task',t.type,$3,t.cluster_id,t.id,t.command_id,nullif($4,''),t.status,$5,$6 from tasks t where t.id=$7`, newID(), normalizeDiagnosticLevel(input.Level), input.Message, input.Reason, payloadRaw, now, input.TaskID)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.diagnosticWriter != nil {
		task, found, taskErr := s.getTask(input.TaskID)
		if taskErr != nil || !found {
			return taskErr
		}
		_, err = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: task.TenantID, Scope: "tenant", Level: input.Level, Component: "task", Operation: task.Type, Message: input.Message, ClusterID: task.ClusterID, TaskID: task.ID, CommandID: task.CommandID, ErrorCode: input.Reason, Status: task.Status, Details: input.Payload, EventAt: now})
	}
	return err
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
		select id, tenant_id, cluster_id, coalesce(app_id::text, ''), coalesce(protection_plan_id::text, ''),
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
		&item.NextFireAt, &item.ScheduleEnabled, &item.Status,
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
