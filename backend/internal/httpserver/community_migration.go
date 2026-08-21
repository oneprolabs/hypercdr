package httpserver

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

const communityMigrationProtocolV1 = "hypercdr-community-migration/v1"

func (r *Router) createCommunityMigrationAuthorization(w http.ResponseWriter, req *http.Request) {
	actor, ok := requestUser(req)
	if !ok || !actor.SystemAdmin || r.productInfo.Edition != "community" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "community_system_administrator_required"})
		return
	}
	authorization, err := r.store.CreateCommunityMigrationAuthorization(actor.ID, 30*time.Minute)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "migration_authorization_create_failed", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": authorization.ID, "token": authorization.Token, "expiresAt": authorization.ExpiresAt, "protocolVersion": communityMigrationProtocolV1})
}

func (r *Router) listCommunityMigrations(w http.ResponseWriter, _ *http.Request) {
	items, err := r.store.ListCommunityMigrationSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "migration_list_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (r *Router) openCommunityMigrationSession(w http.ResponseWriter, req *http.Request) {
	if r.productInfo.Edition != "community" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "source_must_be_community"})
		return
	}
	var body struct{ Token, TargetInstanceID, ProtocolVersion, TargetPublicKey string }
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Token) == "" || strings.TrimSpace(body.TargetInstanceID) == "" || strings.TrimSpace(body.TargetPublicKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if body.ProtocolVersion != communityMigrationProtocolV1 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "migration_protocol_incompatible", "required": communityMigrationProtocolV1})
		return
	}
	session, err := r.store.ConsumeCommunityMigrationAuthorization(body.Token, body.TargetInstanceID, body.ProtocolVersion, body.TargetPublicKey, 2*time.Hour)
	if err != nil {
		status := http.StatusUnauthorized
		code := "migration_token_invalid"
		if errors.Is(err, store.ErrTokenExpired) {
			status, code = http.StatusGone, "migration_token_expired"
		} else if errors.Is(err, store.ErrTokenUsed) {
			status, code = http.StatusConflict, "migration_token_used"
		} else if strings.Contains(err.Error(), "active") {
			status, code = http.StatusConflict, "migration_already_active"
		}
		writeJSON(w, status, map[string]any{"error": code, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": session.ID, "sessionToken": session.SessionToken, "sourceInstanceId": session.SourceInstanceID, "targetInstanceId": session.TargetInstanceID, "protocolVersion": session.ProtocolVersion, "state": session.State, "expiresAt": session.ExpiresAt})
}

func (r *Router) authenticateCommunityMigration(req *http.Request) (store.CommunityMigrationSession, bool) {
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Migration ") {
		return store.CommunityMigrationSession{}, false
	}
	session, ok, err := r.store.AuthenticateCommunityMigrationSession(strings.TrimSpace(strings.TrimPrefix(header, "Migration ")))
	if err != nil || !ok || session.ID != req.PathValue("id") {
		return store.CommunityMigrationSession{}, false
	}
	return session, true
}

func (r *Router) communityMigrationInventory(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "migration_inventory_failed"})
		return
	}
	storage, _ := r.store.ListStorageRepositories()
	policies, _ := r.store.ListPolicies()
	plans, _ := r.store.ListProtectionPlans("")
	restorePoints, _ := r.store.ListRestorePoints(store.RestorePointFilter{})
	active := []map[string]string{}
	tasksCount := 0
	for _, cluster := range clusters {
		tasks, _ := r.store.ListTasks(cluster.ID)
		tasksCount += len(tasks)
		for _, task := range tasks {
			if isActiveTaskStatus(task.Status) {
				active = append(active, map[string]string{"id": task.ID, "type": task.Type, "clusterId": cluster.ID, "status": task.Status})
			}
		}
	}
	settings, _, _ := r.store.GetPlatformSettings()
	manifest := store.CommunityMigrationExportManifest{}
	manifestErr := errors.New("migration export unavailable")
	if exporter := communityMigrationExporter(r); exporter != nil {
		manifest, manifestErr = exporter.CommunityMigrationManifest(req.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{"migrationId": session.ID, "sourceInstanceId": settings.InstanceID, "protocolVersion": communityMigrationProtocolV1, "exportFormatVersion": store.CommunityMigrationExportVersion, "schemaVersions": manifest.SchemaVersions, "manifestAvailable": manifestErr == nil, "agentNamespace": settings.AgentNamespace, "counts": map[string]int{"clusters": len(clusters), "storageRepositories": len(storage), "policies": len(policies), "protectionPlans": len(plans), "restorePoints": len(restorePoints), "tasks": tasksCount}, "activeTasks": active, "ready": len(active) == 0 && manifestErr == nil})
}

func communityMigrationExporter(r *Router) store.CommunityMigrationExporter {
	exporter, _ := r.store.(store.CommunityMigrationExporter)
	return exporter
}

func (r *Router) backupCommunityMigration(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "frozen" && session.State != "backed-up" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "migration_source_not_frozen"})
		return
	}
	exporter := communityMigrationExporter(r)
	if exporter == nil {
		writeJSON(w, 501, map[string]any{"error": "migration_export_unavailable"})
		return
	}
	manifest, err := exporter.CreateCommunityMigrationBackup(req.Context(), session.ID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "migration_backup_failed", "message": err.Error()})
		return
	}
	updated, found, err := r.store.UpdateCommunityMigrationState(session.ID, "backed-up", "backup_completed", "Mandatory logical database and configuration snapshot completed.", true)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "migration_backup_state_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"migration": updated, "manifest": manifest})
}

func (r *Router) communityMigrationManifest(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "backed-up" && session.State != "exporting" {
		writeJSON(w, 409, map[string]any{"error": "migration_backup_required"})
		return
	}
	exporter := communityMigrationExporter(r)
	if exporter == nil {
		writeJSON(w, 501, map[string]any{"error": "migration_export_unavailable"})
		return
	}
	manifest, err := exporter.CommunityMigrationManifest(req.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "migration_manifest_failed", "message": err.Error()})
		return
	}
	writeJSON(w, 200, manifest)
}

func (r *Router) communityMigrationExportBatch(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "backed-up" && session.State != "exporting" {
		writeJSON(w, 409, map[string]any{"error": "migration_backup_required"})
		return
	}
	offset, offsetErr := strconv.Atoi(req.URL.Query().Get("offset"))
	if req.URL.Query().Get("offset") == "" {
		offset, offsetErr = 0, nil
	}
	limit, limitErr := strconv.Atoi(req.URL.Query().Get("limit"))
	if req.URL.Query().Get("limit") == "" {
		limit, limitErr = 200, nil
	}
	if offsetErr != nil || limitErr != nil {
		writeJSON(w, 400, map[string]any{"error": "migration_pagination_invalid"})
		return
	}
	exporter := communityMigrationExporter(r)
	if exporter == nil {
		writeJSON(w, 501, map[string]any{"error": "migration_export_unavailable"})
		return
	}
	batch, err := exporter.CommunityMigrationBatch(req.Context(), req.PathValue("table"), offset, limit)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "migration_export_failed", "message": err.Error()})
		return
	}
	if session.State == "backed-up" {
		_, _, _ = r.store.UpdateCommunityMigrationState(session.ID, "exporting", "export_started", "Source batch export started from the frozen snapshot.", true)
	}
	writeJSON(w, 200, batch)
}

type migrationCredentialEnvelope struct {
	RepositoryID string `json:"repositoryId"`
	EncryptedKey string `json:"encryptedKey"`
	Ciphertext   string `json:"ciphertext"`
}

func (r *Router) communityMigrationCredentials(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "backed-up" && session.State != "exporting" {
		writeJSON(w, 409, map[string]any{"error": "migration_backup_required"})
		return
	}
	exporter := communityMigrationExporter(r)
	if exporter == nil {
		writeJSON(w, 501, map[string]any{"error": "migration_export_unavailable"})
		return
	}
	publicKey, err := parseMigrationPublicKey(session.TargetPublicKey)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "migration_target_key_invalid"})
		return
	}
	credentials, err := exporter.CommunityMigrationStorageCredentials(req.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "migration_credentials_read_failed", "message": err.Error()})
		return
	}
	envelopes := []migrationCredentialEnvelope{}
	for _, credential := range credentials {
		raw, marshalErr := json.Marshal(credential.Secret)
		if marshalErr != nil {
			writeJSON(w, 500, map[string]any{"error": "migration_credentials_encode_failed"})
			return
		}
		envelope, sealErr := sealMigrationCredential(publicKey, session.ID, credential.RepositoryID, raw)
		for index := range raw {
			raw[index] = 0
		}
		if sealErr != nil {
			writeJSON(w, 500, map[string]any{"error": "migration_credentials_encrypt_failed"})
			return
		}
		envelopes = append(envelopes, envelope)
	}
	writeJSON(w, 200, map[string]any{"algorithm": "RSA-OAEP-3072+AES-256-GCM", "items": envelopes})
}

func (r *Router) communityMigrationSMTP(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "backed-up" && session.State != "exporting" {
		writeJSON(w, 409, map[string]any{"error": "migration_backup_required"})
		return
	}
	publicKey, err := parseMigrationPublicKey(session.TargetPublicKey)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "migration_target_key_invalid"})
		return
	}
	items, err := r.store.ListEmailSettings()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "migration_smtp_read_failed"})
		return
	}
	result := []map[string]any{}
	for _, item := range items {
		entry := map[string]any{"id": item.ID, "name": item.Name, "enabled": item.Enabled, "host": item.Host, "port": item.Port, "security": item.Security, "username": item.Username, "senderName": item.SenderName, "senderEmail": item.SenderEmail}
		if item.PasswordCiphertext != "" {
			plain, decryptErr := r.decryptSetting(item.PasswordCiphertext)
			if decryptErr != nil {
				writeJSON(w, 500, map[string]any{"error": "migration_smtp_decrypt_failed", "configurationId": item.ID})
				return
			}
			envelope, sealErr := sealMigrationCredential(publicKey, session.ID, item.ID, []byte(plain))
			if sealErr != nil {
				writeJSON(w, 500, map[string]any{"error": "migration_smtp_encrypt_failed"})
				return
			}
			entry["passwordEnvelope"] = envelope
		}
		result = append(result, entry)
	}
	writeJSON(w, 200, map[string]any{"items": result, "defaultPreserved": true, "importMode": "disabled-non-default"})
}

func (r *Router) prepareCommunityMigrationHandover(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "exporting" && session.State != "backed-up" {
		writeJSON(w, 409, map[string]any{"error": "migration_export_not_ready"})
		return
	}
	var body struct {
		TargetEndpoint   string            `json:"targetEndpoint"`
		RollbackDeadline time.Time         `json:"rollbackDeadline"`
		ClusterTokens    map[string]string `json:"clusterTokens"`
	}
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.TargetEndpoint) == "" || len(body.ClusterTokens) == 0 || body.RollbackDeadline.Before(time.Now().UTC().Add(4*time.Minute)) || body.RollbackDeadline.After(time.Now().UTC().Add(31*time.Minute)) {
		writeJSON(w, 400, map[string]any{"error": "migration_handover_invalid"})
		return
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "migration_clusters_read_failed"})
		return
	}
	if len(clusters) != len(body.ClusterTokens) {
		writeJSON(w, 409, map[string]any{"error": "migration_cluster_token_mismatch"})
		return
	}
	for _, cluster := range clusters {
		if strings.TrimSpace(body.ClusterTokens[cluster.ID]) == "" {
			writeJSON(w, 409, map[string]any{"error": "migration_cluster_token_missing", "clusterId": cluster.ID})
			return
		}
		if _, connected := r.hub.get(cluster.ID); !connected {
			writeJSON(w, 409, map[string]any{"error": "migration_agent_offline", "clusterId": cluster.ID, "message": "Every Agent must remain connected when handover starts."})
			return
		}
	}
	tasks := []store.Task{}
	for _, cluster := range clusters {
		token := strings.TrimSpace(body.ClusterTokens[cluster.ID])
		task, createErr := r.store.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "control-plane-handover", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"action": "prepare", "migrationId": session.ID, "targetEndpoint": body.TargetEndpoint, "targetInstallToken": token, "rollbackDeadline": body.RollbackDeadline.Format(time.RFC3339Nano)}})
		if createErr != nil {
			writeJSON(w, 500, map[string]any{"error": "migration_handover_task_failed", "clusterId": cluster.ID, "message": createErr.Error()})
			return
		}
		conn, _ := r.hub.get(cluster.ID)
		if dispatchErr := r.dispatchStoredTask(conn, task); dispatchErr != nil {
			writeJSON(w, 502, map[string]any{"error": "migration_handover_dispatch_failed", "clusterId": cluster.ID, "message": dispatchErr.Error()})
			return
		}
		tasks = append(tasks, task)
	}
	updated, found, err := r.store.UpdateCommunityMigrationState(session.ID, "handover", "handover_started", "Existing Agents are switching to the Enterprise control plane in place.", true)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "migration_handover_state_failed"})
		return
	}
	writeJSON(w, 202, map[string]any{"migration": updated, "tasks": tasks, "rollbackDeadline": body.RollbackDeadline})
}

func parseMigrationPublicKey(value string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("public key PEM is invalid")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok || key.N.BitLen() < 3072 {
		return nil, errors.New("RSA public key must be at least 3072 bits")
	}
	return key, nil
}
func sealMigrationCredential(publicKey *rsa.PublicKey, migrationID, repositoryID string, plaintext []byte) (migrationCredentialEnvelope, error) {
	result := migrationCredentialEnvelope{RepositoryID: repositoryID}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return result, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return result, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return result, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return result, err
	}
	aad := []byte(migrationID + ":" + repositoryID)
	sealed := gcm.Seal(nonce, nonce, plaintext, aad)
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, key, aad)
	for index := range key {
		key[index] = 0
	}
	if err != nil {
		return result, err
	}
	result.EncryptedKey = base64.RawStdEncoding.EncodeToString(encryptedKey)
	result.Ciphertext = base64.RawStdEncoding.EncodeToString(sealed)
	return result, nil
}

func (r *Router) freezeCommunityMigration(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	clusters, _ := r.store.ListClusters()
	blockers := []map[string]string{}
	for _, cluster := range clusters {
		tasks, _ := r.store.ListTasks(cluster.ID)
		for _, task := range tasks {
			if isActiveTaskStatus(task.Status) {
				blockers = append(blockers, map[string]string{"id": task.ID, "type": task.Type, "clusterId": cluster.ID})
			}
		}
	}
	if len(blockers) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "migration_active_tasks", "message": "Wait for all Community tasks to finish before migration.", "blockers": blockers})
		return
	}
	updated, found, err := r.store.UpdateCommunityMigrationState(session.ID, "frozen", "freeze_confirmed", "Community business mutations and scheduling are frozen for migration.", true)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "migration_freeze_failed"})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (r *Router) rollbackCommunityMigration(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	updated, found, err := r.store.UpdateCommunityMigrationState(session.ID, "rolled-back", "rollback_requested", "Community migration was rolled back and business mutations were re-enabled.", false)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "migration_rollback_failed"})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (r *Router) commitCommunityMigration(w http.ResponseWriter, req *http.Request) {
	session, ok := r.authenticateCommunityMigration(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "migration_session_invalid"})
		return
	}
	if session.State != "handover" {
		writeJSON(w, 409, map[string]any{"error": "migration_handover_not_active"})
		return
	}
	updated, found, err := r.store.UpdateCommunityMigrationState(session.ID, "committed", "commit_confirmed", "Enterprise committed the migration. Community remains read-only and retained for at least seven days.", true)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "migration_commit_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"migration": updated, "observationEndsAt": time.Now().UTC().Add(7 * 24 * time.Hour), "readOnly": true, "automaticDeletion": false})
}
