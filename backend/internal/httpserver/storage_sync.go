package httpserver

import (
	"context"
	"errors"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (r *Router) cleanupClusterObjectStorage(ctx context.Context, clusterID string) ([]objectStorageCleanupResult, error) {
	audit, err := r.auditClusterUnregister(clusterID)
	if err != nil {
		return nil, err
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		return nil, err
	}
	clusterTenantID := ""
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			clusterTenantID = cluster.TenantID
			break
		}
	}
	if clusterTenantID == "" {
		return nil, errors.New("cluster tenant could not be resolved for object storage cleanup")
	}
	if !audit.ObjectStorageNeeded {
		return []objectStorageCleanupResult{}, nil
	}
	return r.cleanupClusterObjectStorageRepositories(ctx, clusterID, audit.StorageRepositoryIDs)
}

func (r *Router) cleanupClusterObjectStorageRepositories(ctx context.Context, clusterID string, repositoryIDs []string) ([]objectStorageCleanupResult, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return nil, errors.New("cluster id is required for object storage cleanup")
	}
	if len(repositoryIDs) == 0 {
		return nil, errors.New("restore points exist but no storage repository is associated with them")
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		return nil, err
	}
	clusterTenantID := ""
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			clusterTenantID = cluster.TenantID
			break
		}
	}
	if clusterTenantID == "" {
		return nil, errors.New("cluster tenant could not be resolved for object storage cleanup")
	}
	repositories, err := r.store.ListStorageRepositories()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(repositoryIDs))
	for _, id := range repositoryIDs {
		wanted[id] = struct{}{}
	}
	// Repositories are tenant scoped; use their tenant at deletion time and
	// verify every selected repository belongs to the same cluster tenant.
	results := make([]objectStorageCleanupResult, 0, len(repositoryIDs))
	for _, repo := range repositories {
		if _, ok := wanted[repo.ID]; !ok {
			continue
		}
		if repo.TenantID != clusterTenantID {
			return results, fmt.Errorf("repository %s does not belong to cluster tenant", repo.Name)
		}
		prefix := strings.TrimSuffix(storageDomainPrefix(clusterTenantID, clusterID), "/") + "/"
		result, err := cleanObjectStoragePrefix(ctx, repo, prefix)
		if err != nil {
			return results, fmt.Errorf("cleanup repository %s prefix %s: %w", repo.Name, prefix, err)
		}
		results = append(results, result)
		delete(wanted, repo.ID)
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return results, fmt.Errorf("associated storage repositories not found: %s", strings.Join(missing, ", "))
	}
	return results, nil
}

func deleteObjectStoragePrefix(ctx context.Context, repo store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
	result := objectStorageCleanupResult{
		RepositoryID:   repo.ID,
		RepositoryName: repo.Name,
		Prefix:         prefix,
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return result, errors.New("refusing to delete empty object storage prefix")
	}
	if !validStorageDomainPrefix(prefix) || !strings.HasSuffix(prefix, "/") {
		return result, fmt.Errorf("refusing to delete unexpected object storage prefix %q", prefix)
	}
	if strings.TrimSpace(repo.Endpoint) == "" {
		return result, errors.New("storage repository endpoint is empty")
	}
	if strings.TrimSpace(repo.Bucket) == "" {
		return result, errors.New("storage repository bucket is empty")
	}
	creds := storageCredentials(repo)
	if creds == nil || creds.AccessKey == "" || creds.SecretKey == "" {
		return result, errors.New("storage repository credentials are empty")
	}
	endpoint, secure := minioEndpoint(repo)
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure:       secure,
		Region:       normalizedStorageRegion(repo.Type, repo.Region),
		BucketLookup: storageBucketLookup(repo),
	})
	if err != nil {
		return result, err
	}
	for object := range client.ListObjects(ctx, repo.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return result, object.Err
		}
		if err := client.RemoveObject(ctx, repo.Bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return result, err
		}
		result.ObjectsDeleted++
		result.BytesDeleted += object.Size
	}
	return result, nil
}

func minioEndpoint(repo store.StorageRepository) (string, bool) {
	endpoint := strings.TrimRight(strings.TrimSpace(repo.Endpoint), "/")
	secure := repo.TLSEnabled
	if strings.HasPrefix(endpoint, "http://") {
		secure = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		secure = true
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	return endpoint, secure
}

func storageBucketLookup(repo store.StorageRepository) minio.BucketLookupType {
	style, _ := repo.Config["urlStyle"].(string)
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "virtual", "virtual-host", "virtual_host", "dns":
		return minio.BucketLookupDNS
	case "path", "path-style", "path_style":
		return minio.BucketLookupPath
	default:
		return minio.BucketLookupAuto
	}
}

func (r *Router) syncStorageRepository(w http.ResponseWriter, req *http.Request) {
	repositoryID := req.PathValue("id")
	if repositoryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "repository_id_required"})
		return
	}
	var body struct {
		ClusterID string `json:"clusterId"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.ClusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		r.logger.Error("failed to get storage repository", "repository_id", repositoryID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_storage_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}
	repo.Region = normalizedStorageRegion(repo.Type, repo.Region)
	bslName := storageDomainBSLName(repo, body.ClusterID)
	objectPrefix := storageDomainPrefix(repo.TenantID, body.ClusterID)
	config := storageConfigWithPrefix(repo, objectPrefix)
	binding, err := r.store.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID:       body.ClusterID,
		StorageRepoID:   repo.ID,
		SourceClusterID: body.ClusterID,
		BSLName:         bslName,
		ObjectPrefix:    objectPrefix,
		Status:          "configuring",
		RetryCount:      1,
		RepoUpdatedAt:   repo.UpdatedAt,
	})
	if err != nil {
		r.logger.Error("failed to upsert cluster storage binding", "cluster_id", body.ClusterID, "repository_id", repo.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "upsert_cluster_storage_binding_failed"})
		return
	}

	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID: body.ClusterID,
		Type:      "storage-sync",
		Status:    "queued",
		CommandID: commandID,
		Payload: map[string]any{
			"requestedBy":     requestActor(req),
			"repositoryId":    repo.ID,
			"name":            bslName,
			"displayName":     repo.Name,
			"sourceClusterId": body.ClusterID,
			"objectPrefix":    objectPrefix,
			"type":            repo.Type,
			"endpoint":        repo.Endpoint,
			"bucket":          repo.Bucket,
			"region":          repo.Region,
			"tlsEnabled":      repo.TLSEnabled,
			"secretRef":       repo.SecretRef,
			"hasSecret":       len(repo.Secret) > 0,
			"config":          config,
			"bindingId":       binding.ID,
		},
	})
	if err != nil {
		r.logger.Error("failed to create storage sync task", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}

	conn, ok := r.hub.get(body.ClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected",
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "agent is offline; task remains queued",
		})
		return
	}

	dispatch := protocol.Message[protocol.TaskDispatchPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformTaskDispatch,
		ClusterID:   body.ClusterID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.TaskDispatchPayload{
			TaskID:    task.ID,
			CommandID: commandID,
			Type:      "storage-sync",
			Deadline:  time.Now().UTC().Add(10 * time.Minute),
			StorageSync: &protocol.StorageSyncCommand{
				RepositoryID: repo.ID,
				Name:         bslName,
				Type:         repo.Type,
				Endpoint:     repo.Endpoint,
				Bucket:       repo.Bucket,
				Region:       repo.Region,
				TLSEnabled:   repo.TLSEnabled,
				SecretRef:    repo.SecretRef,
				Credentials:  storageCredentials(repo),
				Config:       config,
			},
		},
	}
	if err := conn.WriteJSON(dispatch); err != nil {
		r.logger.Error("failed to dispatch storage sync task", "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "task created but dispatch failed",
		})
		return
	}

	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	writeJSON(w, http.StatusCreated, task)
}

func storageCredentials(repo store.StorageRepository) *protocol.S3Credentials {
	if len(repo.Secret) == 0 {
		return nil
	}
	credentials := &protocol.S3Credentials{
		AccessKey: repo.Secret["accessKey"], SecretKey: repo.Secret["secretKey"], AccountName: repo.Secret["accountName"], AccountKey: repo.Secret["accountKey"], ServiceAccountKey: repo.Secret["serviceAccountKey"],
	}
	if credentials.AccessKey == "" && credentials.SecretKey == "" && credentials.AccountKey == "" && credentials.ServiceAccountKey == "" {
		return nil
	}
	return credentials
}

func (r *Router) dispatchStorageSyncTask(clusterID string, repositoryID string) (store.Task, string, error) {
	return r.dispatchStorageSyncTaskForPlan(clusterID, repositoryID, "")
}

func (r *Router) dispatchStorageSyncTaskForPlan(clusterID string, repositoryID string, protectionPlanID string) (store.Task, string, error) {
	return r.dispatchStorageSyncTaskForPlanActivation(clusterID, repositoryID, protectionPlanID, "", "", false)
}

const storageSyncMaxAttempts = 3
const protectionPlanActivationTaskTimeout = 90 * time.Second

func storageDomainPrefix(tenantID string, sourceClusterID string) string {
	tenantID = strings.TrimSpace(tenantID)
	sourceClusterID = strings.TrimSpace(sourceClusterID)
	if tenantID == "" {
		tenantID = "unknown"
	}
	if sourceClusterID == "" {
		sourceClusterID = "unknown"
	}
	return "hypercdr/v1/tenants/" + tenantID + "/clusters/" + sourceClusterID
}

func validStorageDomainPrefix(prefix string) bool {
	parts := strings.Split(strings.Trim(strings.TrimSpace(prefix), "/"), "/")
	return len(parts) >= 6 && parts[0] == "hypercdr" && parts[1] == "v1" && parts[2] == "tenants" && parts[3] != "" && parts[3] != "unknown" && parts[4] == "clusters" && parts[5] != "" && parts[5] != "unknown"
}

func storageDomainBSLName(repo store.StorageRepository, sourceClusterID string) string {
	base := sanitizeKubernetesName(repo.Name)
	sourceID := sanitizeKubernetesName(strings.TrimSpace(sourceClusterID))
	if sourceID == "" {
		sourceID = "unknown"
	}
	name := base + "-" + sourceID
	if len(name) > 63 {
		maxBase := 63 - len(sourceID) - 1
		if maxBase < 1 {
			maxBase = 1
		}
		name = strings.Trim(base[:min(len(base), maxBase)], "-") + "-" + sourceID
	}
	return strings.Trim(name, "-")
}

func sanitizeKubernetesName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}

func storageConfigWithPrefix(repo store.StorageRepository, prefix string) map[string]any {
	config := map[string]any{}
	for key, value := range repo.Config {
		config[key] = value
	}
	config["prefix"] = prefix
	return config
}

func normalizedStorageRegion(storageType, region string) string {
	region = normalizedStoredRegion(region)
	typeName := strings.ToLower(strings.TrimSpace(storageType))
	if region == "" && (typeName == "s3" || typeName == "s3-compatible" || typeName == "s3 compatible") {
		return "us-east-1"
	}
	return region
}

func normalizedStoredRegion(region string) string {
	region = strings.TrimSpace(region)
	switch strings.ToLower(region) {
	case "n/a", "na", "-":
		return ""
	default:
		return region
	}
}

func (r *Router) dispatchStorageSyncTaskForPlanActivation(clusterID string, repositoryID string, protectionPlanID string, activationAttempt string, activationRole string, reconfigureStorage bool) (store.Task, string, error) {
	return r.dispatchStorageSyncTaskForPlanActivationAttempt(clusterID, repositoryID, protectionPlanID, clusterID, activationAttempt, activationRole, reconfigureStorage, 1)
}

func (r *Router) dispatchStorageSyncTaskForPlanActivationAttempt(clusterID string, repositoryID string, protectionPlanID string, sourceClusterID string, activationAttempt string, activationRole string, reconfigureStorage bool, retryAttempt int) (store.Task, string, error) {
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok {
		return store.Task{}, "", errors.New("storage repository not found")
	}
	repo.Region = normalizedStorageRegion(repo.Type, repo.Region)
	if sourceClusterID == "" {
		sourceClusterID = clusterID
	}
	bslName := storageDomainBSLName(repo, sourceClusterID)
	objectPrefix := storageDomainPrefix(repo.TenantID, sourceClusterID)
	config := storageConfigWithPrefix(repo, objectPrefix)
	binding, err := r.store.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID:       clusterID,
		StorageRepoID:   repo.ID,
		SourceClusterID: sourceClusterID,
		BSLName:         bslName,
		ObjectPrefix:    objectPrefix,
		Status:          "configuring",
		RetryCount:      retryAttempt,
		RepoUpdatedAt:   repo.UpdatedAt,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	commandID := store.NewPublicID()
	payload := map[string]any{
		"repositoryId":    repo.ID,
		"name":            bslName,
		"displayName":     repo.Name,
		"sourceClusterId": sourceClusterID,
		"objectPrefix":    objectPrefix,
		"type":            repo.Type,
		"endpoint":        repo.Endpoint,
		"bucket":          repo.Bucket,
		"region":          repo.Region,
		"tlsEnabled":      repo.TLSEnabled,
		"secretRef":       repo.SecretRef,
		"hasSecret":       len(repo.Secret) > 0,
		"config":          config,
		"bindingId":       binding.ID,
	}
	if activationAttempt != "" {
		payload["activationAttempt"] = activationAttempt
		payload["retryAttempt"] = retryAttempt
		payload["maxAttempts"] = storageSyncMaxAttempts
	}
	if activationRole != "" {
		payload["activationRole"] = activationRole
	}
	if reconfigureStorage {
		payload["reconfigureStorage"] = true
	}
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		ProtectionPlanID: protectionPlanID,
		Type:             "storage-sync",
		Status:           "queued",
		CommandID:        commandID,
		Payload:          payload,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected",
		})
		return task, "agent is offline; task remains queued", nil
	}
	dispatch := protocol.Message[protocol.TaskDispatchPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformTaskDispatch,
		ClusterID:   clusterID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.TaskDispatchPayload{
			TaskID:    task.ID,
			CommandID: commandID,
			Type:      "storage-sync",
			Deadline:  time.Now().UTC().Add(10 * time.Minute),
			StorageSync: &protocol.StorageSyncCommand{
				RepositoryID: repo.ID,
				Name:         bslName,
				Type:         repo.Type,
				Endpoint:     repo.Endpoint,
				Bucket:       repo.Bucket,
				Region:       repo.Region,
				TLSEnabled:   repo.TLSEnabled,
				SecretRef:    repo.SecretRef,
				Credentials:  storageCredentials(repo),
				Config:       config,
			},
		},
	}
	if err := conn.WriteJSON(dispatch); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		return task, "task created but dispatch failed", nil
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	return task, "", nil
}

func (r *Router) ensureStorageSynced(ctx context.Context, clusterID string, storageName string, repositoryID string, sourceClusterID string) (store.Task, error) {
	if repositoryID == "" {
		repo, ok, err := r.findStorageRepositoryByName(storageName)
		if err != nil {
			return store.Task{}, err
		}
		if !ok {
			return store.Task{}, errors.New("storage repository " + storageName + " not found")
		}
		repositoryID = repo.ID
	}
	if sourceClusterID == "" {
		sourceClusterID = clusterID
	}
	task, warning, err := r.dispatchStorageSyncTaskForPlanActivationAttempt(clusterID, repositoryID, "", sourceClusterID, "", "", false, 1)
	if err != nil {
		return task, err
	}
	if warning != "" {
		return task, errors.New(warning)
	}
	if err := r.waitForTaskSucceeded(ctx, task.ID, 2*time.Minute); err != nil {
		return task, err
	}
	return task, nil
}

func (r *Router) findStorageRepositoryByName(name string) (store.StorageRepository, bool, error) {
	repos, err := r.store.ListStorageRepositories()
	if err != nil {
		return store.StorageRepository{}, false, err
	}
	for _, repo := range repos {
		if repo.Name == name {
			return repo, true, nil
		}
	}
	return store.StorageRepository{}, false, nil
}
