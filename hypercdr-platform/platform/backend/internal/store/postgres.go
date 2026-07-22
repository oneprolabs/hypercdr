package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &PostgresStore{db: db}
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
	return store, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) ensureDefaultTenant(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		insert into tenants (id, name, status)
		values ($1, 'Default Tenant', 'active')
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
		insert into users (id, tenant_id, email, password_hash, role, status, created_at, updated_at)
		values ($1, $2, $3, $4, 'admin', 'active', $5, $5)
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
		select id, tenant_id, email, password_hash, role, status
		from users
		where tenant_id = $1 and email = $2 and status = 'active'
	`, DefaultTenantID, email)
	var user User
	var passwordHash string
	if err := row.Scan(&user.ID, &user.TenantID, &user.Email, &passwordHash, &user.Role, &user.Status); err != nil {
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

func (s *PostgresStore) CreateUser(email, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{ID: newID(), TenantID: DefaultTenantID, Email: email, Role: "member", Status: "active"}
	_, err = s.db.Exec(`insert into users (id, tenant_id, email, password_hash, role, status) values ($1,$2,$3,$4,$5,$6)`, u.ID, u.TenantID, u.Email, string(hash), u.Role, u.Status)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return User{}, ErrUserExists
		}
		return User{}, err
	}
	return u, nil
}

func (s *PostgresStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	token := "hpr_" + newID() + newID()
	result, err := s.db.Exec(`insert into password_reset_tokens (id,user_id,token_hash,expires_at) select $1,id,$2,$3 from users where tenant_id=$4 and email=$5 and status='active'`, newID(), resetTokenDigest(token), time.Now().UTC().Add(ttl), DefaultTenantID, email)
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
	err = tx.QueryRow(`update users u set password_hash=$1,updated_at=now() from password_reset_tokens t where t.user_id=u.id and t.token_hash=$2 and t.used_at is null and t.expires_at>now() returning u.id,u.tenant_id,u.email,u.role,u.status`, string(hash), digest).Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Status)
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
	err := s.db.QueryRow(`select id,tenant_id,email,role,status from users where tenant_id=$1 and email=$2`, DefaultTenantID, email).Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.Status)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	randomPassword := "google:" + newID() + newID()
	return s.CreateUser(email, randomPassword)
}

func (s *PostgresStore) CreateAgentToken(description string, ttl time.Duration) (AgentToken, error) {
	now := time.Now().UTC()
	token := AgentToken{
		ID:          newID(),
		Token:       "hcdr_" + newID() + newID(),
		Description: description,
		ExpiresAt:   now.Add(ttl),
	}

	_, err := s.db.Exec(`
		insert into agent_tokens (id, tenant_id, token_hash, description, expires_at, created_at)
		values ($1, $2, $3, $4, $5, $6)
	`, token.ID, DefaultTenantID, token.Token, token.Description, token.ExpiresAt, now)
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
		select id, token_hash, coalesce(description, ''), expires_at, used_at
		from agent_tokens
		where token_hash = $1 and revoked_at is null
		for update
	`, input.Token).Scan(&token.ID, &token.Token, &token.Description, &token.ExpiresAt, &usedAt)
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
	if err := tx.QueryRow(`select count(*) from clusters where tenant_id = $1`, DefaultTenantID).Scan(&clusterCount); err != nil {
		return Cluster{}, "", err
	}
	isFirstCluster := clusterCount == 0

	cluster := Cluster{
		ID:               newID(),
		TenantID:         DefaultTenantID,
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
	`, newID(), DefaultTenantID, cluster.ID, credential, now)
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

	if input.IsDefault != nil && *input.IsDefault {
		if _, err := tx.Exec(`
			update clusters set is_default = false, updated_at = $2 where tenant_id = $1
		`, DefaultTenantID, now); err != nil {
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
	if err := tx.QueryRow(`select true, is_default from clusters where id = $1`, clusterID).Scan(&exists, &wasDefault); errors.Is(err, sql.ErrNoRows) {
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
		`, DefaultTenantID); err != nil {
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
	cluster.AgentUpgradeAvailable = metadata.AgentImageDigest != "" && metadata.LatestAgentImageDigest != "" && metadata.AgentImageDigest != metadata.LatestAgentImageDigest
}

func (s *PostgresStore) ListApplications(clusterID string) ([]Application, error) {
	query := `
		select id, cluster_id, namespace, name, status, labels,
		       workload_count, service_count, ingress_count, configmap_count, secret_count,
		       pvc_count, pv_capacity_bytes, resource_summary, coalesce(last_collected_at, created_at), protection_status
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
		if err := rows.Scan(
			&app.ID, &app.ClusterID, &app.Namespace, &app.Name, &app.Status, &labelsRaw,
			&app.WorkloadCount, &app.ServiceCount, &app.IngressCount, &app.ConfigMapCount, &app.SecretCount,
			&app.PVCCount, &app.PVCapacityBytes, &resourceSummaryRaw, &app.LastCollectedAt, &app.ProtectionStatus,
		); err != nil {
			return nil, err
		}
		if len(labelsRaw) > 0 {
			_ = json.Unmarshal(labelsRaw, &app.Labels)
		}
		if len(resourceSummaryRaw) > 0 {
			_ = json.Unmarshal(resourceSummaryRaw, &app.ResourceSummary)
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

func (s *PostgresStore) ApplyInventory(input InventoryInput) (Cluster, bool, error) {
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Cluster{}, false, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		update clusters
		set kube_version = $2,
		    velero_status = coalesce(nullif($3, ''), velero_status),
		    node_count = $4,
		    namespace_count = $5,
		    application_count = $6,
		    connection_status = 'online',
		    last_seen_at = $7,
		    metadata = jsonb_set(
		      jsonb_set(
		        jsonb_set(coalesce(metadata, '{}'::jsonb), '{inventoryHash}', to_jsonb($8::text), true),
		        '{nodes}', $9::jsonb, true
		      ),
		      '{storageClasses}', $10::jsonb, true
		    ),
		    updated_at = $7
		where id = $1
	`, input.ClusterID, input.KubeVersion, input.VeleroStatus, input.NodeCount, input.NamespaceCount, len(input.Apps), now, input.Hash, mustJSON(input.Nodes), mustJSON(input.StorageClasses))
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
		`, appID, DefaultTenantID, input.ClusterID, app.Namespace, name, app.Status, app.WorkloadCount,
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
	secretRaw, err := json.Marshal(secret)
	if err != nil {
		return StorageRepository{}, err
	}
	repo := StorageRepository{
		ID:         repoID,
		TenantID:   DefaultTenantID,
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
			config, secret_ref, secret_payload, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
	`, repo.ID, repo.TenantID, repo.Name, repo.Type, repo.Endpoint, repo.Bucket, repo.Region,
		repo.TLSEnabled, repo.Status, configRaw, repo.SecretRef, secretRaw, now)
	return repo, err
}

func (s *PostgresStore) ListStorageRepositories() ([]StorageRepository, error) {
	rows, err := s.db.Query(`
		select id, tenant_id, name, type, coalesce(endpoint, ''), coalesce(bucket, ''),
		       coalesce(region, ''), tls_enabled, status, config, coalesce(secret_ref, ''),
		       secret_payload, coalesce(last_validated_at, '0001-01-01'::timestamptz), created_at, updated_at
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
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Endpoint,
			&item.Bucket, &item.Region, &item.TLSEnabled, &item.Status, &configRaw, &item.SecretRef,
			&secretRaw, &item.LastValidatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(configRaw, &item.Config)
		_ = json.Unmarshal(secretRaw, &item.Secret)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) SetStorageRepositoryStatus(id string, status string, lastValidatedAt time.Time) (StorageRepository, bool, error) {
	if status == "" {
		status = "unknown"
	}
	var item StorageRepository
	var configRaw, secretRaw []byte
	var lastValidated sql.NullTime
	err := s.db.QueryRow(`
		update storage_repositories
		   set status = $2,
		       last_validated_at = $3,
		       updated_at = now()
		 where id = $1
		returning id, tenant_id, name, type, coalesce(endpoint, ''), coalesce(bucket, ''),
		          coalesce(region, ''), tls_enabled, status, config, coalesce(secret_ref, ''),
		          secret_payload, coalesce(last_validated_at, '0001-01-01'::timestamptz), created_at, updated_at
	`, id, status, lastValidatedAt).Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Endpoint,
		&item.Bucket, &item.Region, &item.TLSEnabled, &item.Status, &configRaw, &item.SecretRef,
		&secretRaw, &lastValidated, &item.CreatedAt, &item.UpdatedAt)
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
	_ = json.Unmarshal(secretRaw, &item.Secret)
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
	err := s.db.QueryRow(`
		select id, tenant_id, name, type, coalesce(endpoint, ''), coalesce(bucket, ''),
		       coalesce(region, ''), tls_enabled, status, config, coalesce(secret_ref, ''),
		       secret_payload, coalesce(last_validated_at, '0001-01-01'::timestamptz), created_at, updated_at
		from storage_repositories
		where id = $1
	`, id).Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Endpoint,
		&item.Bucket, &item.Region, &item.TLSEnabled, &item.Status, &configRaw,
		&item.SecretRef, &secretRaw, &item.LastValidatedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StorageRepository{}, false, nil
	}
	if err != nil {
		return StorageRepository{}, false, err
	}
	_ = json.Unmarshal(configRaw, &item.Config)
	_ = json.Unmarshal(secretRaw, &item.Secret)
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
	`, id, DefaultTenantID, input.ClusterID, input.StorageRepoID, sourceClusterID, bslName, input.ObjectPrefix, status, input.RetryCount, input.RepoUpdatedAt, now)
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
		TenantID:       DefaultTenantID,
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

func (s *PostgresStore) CreateProtectionPlan(input ProtectionPlanInput) (ProtectionPlan, error) {
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
		TenantID:             DefaultTenantID,
		SourceClusterID:      input.SourceClusterID,
		AppID:                primary,
		AppIDs:               appIDs,
		ScopeType:            input.ScopeType,
		IncludedResources:    input.IncludedResources,
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
	_, err = tx.Exec(`
		insert into protection_plans (
			id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, label_selector,
			include_cluster_scoped, storage_repo_id, policy_id, target_cluster_id,
			excluded_resources, pre_hooks, post_hooks, status, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, nullif($9, '')::uuid, nullif($10, '')::uuid,
			nullif($11, '')::uuid, $12, $13, $14, $15, $16, $16)
	`, plan.ID, plan.TenantID, plan.SourceClusterID, plan.AppID, plan.ScopeType, includedResources, labelSelector,
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
		select protection_plan_id::text,
		       coalesce(last_fired_at, '0001-01-01'::timestamptz),
		       coalesce(next_fire_at, '0001-01-01'::timestamptz),
		       enabled, created_at, updated_at
		from protection_plan_schedules
		where protection_plan_id = $1
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
		select protection_plan_id::text,
		       coalesce(last_fired_at, '0001-01-01'::timestamptz),
		       coalesce(next_fire_at, '0001-01-01'::timestamptz),
		       enabled, created_at, updated_at
		from protection_plan_schedules
		where enabled = true
		  and next_fire_at is not null
		  and next_fire_at <= $1
		order by next_fire_at asc
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
		select pp.id, pp.tenant_id, pp.source_cluster_id, pp.app_id, pp.scope_type, pp.included_resources, pp.label_selector,
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
		var includedResources, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
			&includedResources, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
			&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize,
			&item.NextFireAt, &item.ScheduleEnabled, &item.Status,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(includedResources, &item.IncludedResources)
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
	var includedResources, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	err = tx.QueryRow(`
		select id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, label_selector,
		       include_cluster_scoped, coalesce(storage_repo_id::text, ''), coalesce(policy_id::text, ''),
		       coalesce(target_cluster_id::text, ''), excluded_resources, pre_hooks, post_hooks,
		       plan_storage_size, status, created_at, updated_at
		from protection_plans
		where id = $1
		for update
	`, id).Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, false, nil
		}
		return ProtectionPlan{}, false, err
	}
	_ = json.Unmarshal(includedResources, &item.IncludedResources)
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
			set protection_status = 'pending_protection',
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
	var includedResources, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	err = tx.QueryRow(`
		select id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, label_selector,
		       include_cluster_scoped, coalesce(storage_repo_id::text, ''), coalesce(policy_id::text, ''),
		       coalesce(target_cluster_id::text, ''), excluded_resources, pre_hooks, post_hooks,
		       plan_storage_size, status, created_at, updated_at
		from protection_plans
		where id = $1
		for update
	`, id).Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, false, nil
		}
		return ProtectionPlan{}, false, err
	}
	_ = json.Unmarshal(includedResources, &item.IncludedResources)
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
		`, planIDsSlice(item.AppIDs), time.Now().UTC()); err != nil {
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
	displayName := restorePointDisplayName(input.DisplayName, input.TaskCreatedAt, now)
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
	point := RestorePoint{
		ID:                newID(),
		TenantID:          DefaultTenantID,
		ProtectionPlanID:  input.ProtectionPlanID,
		SourceClusterID:   input.SourceClusterID,
		AppID:             input.AppID,
		StorageRepoID:     input.StorageRepoID,
		DisplayName:       displayName,
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
		Metadata:          metadata,
		CreatedAt:         now,
	}

	_, err = s.db.Exec(`
		insert into restore_points (
			id, tenant_id, protection_plan_id, source_cluster_id, app_id, storage_repo_id,
			display_name, velero_backup_name, point_type, status, size_bytes, started_at, completed_at,
			expires_at, metadata, created_at
		)
		values ($1, $2, nullif($3, '')::uuid, $4, nullif($5, '')::uuid, nullif($6, '')::uuid,
			$7, $8, $9, $10, nullif($11, 0), nullif($12, '0001-01-01'::timestamptz),
			nullif($13, '0001-01-01'::timestamptz), nullif($14, '0001-01-01'::timestamptz), $15, $16)
		on conflict (source_cluster_id, velero_backup_name) do update
		   set display_name = coalesce(nullif(restore_points.display_name, ''), excluded.display_name),
		       size_bytes = coalesce(excluded.size_bytes, restore_points.size_bytes),
		       completed_at = coalesce(excluded.completed_at, restore_points.completed_at),
		       app_id = coalesce(restore_points.app_id, excluded.app_id),
		       storage_repo_id = coalesce(restore_points.storage_repo_id, excluded.storage_repo_id),
		       metadata = coalesce(restore_points.metadata, '{}'::jsonb)
		           || (coalesce(excluded.metadata, '{}'::jsonb)
		               - array['velero', 'size', 'restorePointSize', 'planStorageSize', 'sizeStatus', 'sizeWarnings'])
	`, point.ID, point.TenantID, point.ProtectionPlanID, point.SourceClusterID, point.AppID,
		point.StorageRepoID, point.DisplayName, point.VeleroBackupName, point.PointType, point.Status, point.SizeBytes,
		point.StartedAt, point.CompletedAt, point.ExpiresAt, metadataRaw, now)
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
		       metadata, created_at
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
		var metadataRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
			&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
			&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &metadataRaw, &item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadataRaw, &item.Metadata)
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
	var metadataRaw []byte
	err := s.db.QueryRow(`
		select id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       metadata, created_at
		from restore_points
		where id = $1
	`, id).Scan(&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
		&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
		&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &metadataRaw, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RestorePoint{}, false, nil
	}
	if err != nil {
		return RestorePoint{}, false, err
	}
	_ = json.Unmarshal(metadataRaw, &item.Metadata)
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
		       metadata, created_at
	`, input.ID, status, metadataRaw).Scan(
		&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
		&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
		&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &metadataRawOut, &item.CreatedAt,
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
	task := Task{
		ID:               newID(),
		TenantID:         DefaultTenantID,
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
	_, err = s.db.Exec(`
		insert into task_events (id, task_id, level, reason, message, payload, created_at)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, newID(), input.TaskID, input.Level, input.Reason, input.Message, payloadRaw, time.Now().UTC())
	return err
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
		select pp.id, pp.tenant_id, pp.source_cluster_id, pp.app_id, pp.scope_type, pp.included_resources, pp.label_selector,
		       pp.include_cluster_scoped, coalesce(pp.storage_repo_id::text, ''), coalesce(pp.policy_id::text, ''),
		       coalesce(pp.target_cluster_id::text, ''), pp.excluded_resources, pp.pre_hooks, pp.post_hooks,
		       pp.plan_storage_size, coalesce(pps.next_fire_at, '0001-01-01'::timestamptz), coalesce(pps.enabled, false),
		       pp.status, pp.created_at, pp.updated_at
		from protection_plans pp
		left join protection_plan_schedules pps on pps.protection_plan_id = pp.id
		where pp.id = $1
	`, id)
	var item ProtectionPlan
	var includedResources, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	if err := row.Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize,
		&item.NextFireAt, &item.ScheduleEnabled, &item.Status,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, false, nil
		}
		return ProtectionPlan{}, false, err
	}
	_ = json.Unmarshal(includedResources, &item.IncludedResources)
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
