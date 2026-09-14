package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strings"
	"time"
)

func (r *Router) listStorageRepositories(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListStorageRepositories()
	if err != nil {
		r.logger.Error("failed to list storage repositories", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_storage_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) createStorageRepository(w http.ResponseWriter, req *http.Request) {
	var input store.StorageRepositoryInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if input.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required"})
		return
	}
	if actor, ok := requestUser(req); ok {
		input.TenantID = actor.TenantID
	}
	if code, message := validateCloudStorageInput(input, true); code != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": code, "message": message})
		return
	}
	input.Region = normalizedStoredRegion(input.Region)
	item, err := r.store.CreateStorageRepository(input)
	if err != nil {
		r.logger.Error("failed to create storage repository", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_storage_failed"})
		return
	}
	validationStatus := "connected"
	if _, testErr := probeStorageRepository(item, 5*time.Second); testErr != nil {
		validationStatus = "warning"
		r.logger.Warn("new storage repository connection test failed", "repository_id", item.ID, "error", testErr)
	}
	validatedAt := time.Now().UTC()
	updated, ok, updateErr := r.store.SetStorageRepositoryStatus(item.ID, validationStatus, validatedAt)
	if updateErr != nil {
		r.logger.Error("failed to persist new storage repository status", "repository_id", item.ID, "error", updateErr)
	} else if ok {
		item = updated
	}
	writeJSON(w, http.StatusCreated, item)
}

func (r *Router) updateStorageRepository(w http.ResponseWriter, req *http.Request) {
	var input store.StorageRepositoryInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required", "message": "Storage repository name is required."})
		return
	}
	if code, message := validateCloudStorageInput(input, false); code != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": code, "message": message})
		return
	}
	input.Region = normalizedStoredRegion(input.Region)
	item, ok, err := r.store.UpdateStorageRepository(req.PathValue("id"), input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_storage_repository_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}
	validationStatus := "connected"
	if _, testErr := probeStorageRepository(item, 5*time.Second); testErr != nil {
		validationStatus = "warning"
		r.logger.Warn("updated storage repository connection test failed", "repository_id", item.ID, "error", testErr)
	}
	validatedAt := time.Now().UTC()
	validated, statusOK, statusErr := r.store.SetStorageRepositoryStatus(item.ID, validationStatus, validatedAt)
	if statusErr != nil {
		r.logger.Error("failed to persist updated storage repository status", "repository_id", item.ID, "error", statusErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_storage_status_failed"})
		return
	}
	if statusOK {
		item = validated
	}
	writeJSON(w, http.StatusOK, item)
}

func (r *Router) deleteStorageRepository(w http.ResponseWriter, req *http.Request) {
	deleted, inUse, err := r.store.DeleteStorageRepository(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_storage_repository_failed"})
		return
	}
	if inUse {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "storage_repository_in_use", "message": "This storage repository is used by a DR configuration and cannot be deleted."})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (r *Router) testStorageRepositoryDraft(w http.ResponseWriter, req *http.Request) {
	var input store.StorageRepositoryInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if code, message := validateCloudStorageInput(input, true); code != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": code, "message": message})
		return
	}
	if input.Endpoint == "" && !strings.EqualFold(input.Type, "Google Cloud") && !strings.EqualFold(input.Type, "GCS") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "endpoint_required"})
		return
	}
	if input.Bucket == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bucket_required"})
		return
	}
	input.Region = normalizedStorageRegion(input.Type, input.Region)
	repo := store.StorageRepository{
		Name:       input.Name,
		Type:       input.Type,
		Endpoint:   input.Endpoint,
		Bucket:     input.Bucket,
		Region:     input.Region,
		TLSEnabled: input.TLSEnabled,
		Config:     input.Config,
		Secret: map[string]string{
			"accessKey":         input.AccessKey,
			"secretKey":         input.SecretKey,
			"accountName":       input.AccountName,
			"accountKey":        input.AccountKey,
			"serviceAccountKey": input.ServiceAccountKey,
		},
	}
	probe, testErr := probeStorageRepository(repo, 5*time.Second)
	status := "connected"
	detail := "S3 bucket is reachable"
	if testErr != nil {
		status = "warning"
		detail = testErr.Error()
	}
	body := map[string]any{
		"status":     status,
		"detail":     detail,
		"reachable":  testErr == nil,
		"testedAt":   time.Now().UTC().Format(time.RFC3339Nano),
		"probe":      probe,
		"repository": repo,
	}
	if testErr != nil {
		body["error"] = detail
	}
	writeJSON(w, http.StatusOK, body)
}

func validateCloudStorageInput(input store.StorageRepositoryInput, requireCredentials bool) (string, string) {
	switch strings.ToLower(strings.TrimSpace(input.Type)) {
	case "azure", "azure blob":
		if strings.TrimSpace(input.AccountName) == "" {
			return "azure_account_name_required", "Azure storage account name is required."
		}
		if requireCredentials && strings.TrimSpace(input.AccountKey) == "" {
			return "azure_account_key_required", "Azure storage account key is required."
		}
	case "google cloud", "gcs":
		key := strings.TrimSpace(input.ServiceAccountKey)
		if key == "" {
			if requireCredentials {
				return "gcs_service_account_required", "Google Cloud service account JSON is required."
			}
			return "", ""
		}
		var credentials struct {
			Type        string `json:"type"`
			ClientEmail string `json:"client_email"`
			PrivateKey  string `json:"private_key"`
		}
		if json.Unmarshal([]byte(key), &credentials) != nil || credentials.Type != "service_account" || strings.TrimSpace(credentials.ClientEmail) == "" || strings.TrimSpace(credentials.PrivateKey) == "" {
			return "gcs_service_account_invalid", "Provide a valid Google Cloud service account JSON key."
		}
	}
	return "", ""
}

func (r *Router) testStorageRepository(w http.ResponseWriter, req *http.Request) {
	repositoryID := req.PathValue("id")
	if repositoryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "repository_id_required"})
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

	probe, testErr := probeStorageRepository(repo, 5*time.Second)
	status := "connected"
	detail := "S3 bucket is reachable"
	if testErr != nil {
		status = "warning"
		detail = testErr.Error()
	}
	_ = probe
	updated, _, err := r.store.SetStorageRepositoryStatus(repositoryID, status, time.Now().UTC())
	if err != nil {
		r.logger.Error("failed to update storage status", "repository_id", repositoryID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_storage_status_failed"})
		return
	}

	body := map[string]any{
		"status":          status,
		"detail":          detail,
		"repository":      updated,
		"testedAt":        time.Now().UTC().Format(time.RFC3339Nano),
		"reachable":       testErr == nil,
		"checkedType":     repo.Type,
		"checkedBucket":   repo.Bucket,
		"checkedEndpoint": repo.Endpoint,
	}
	if testErr != nil {
		body["error"] = detail
	}
	writeJSON(w, http.StatusOK, body)
}

func (r *Router) listPolicies(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPolicies()
	if err != nil {
		r.logger.Error("failed to list policies", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_policies_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) createPolicy(w http.ResponseWriter, req *http.Request) {
	var input store.PolicyInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if actor, ok := requestUser(req); ok {
		input.TenantID = actor.TenantID
	}
	if input.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required"})
		return
	}
	item, err := r.store.CreatePolicy(input)
	if err != nil {
		r.logger.Error("failed to create policy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_policy_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (r *Router) updatePolicy(w http.ResponseWriter, req *http.Request) {
	var input store.PolicyInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required", "message": "Policy name is required."})
		return
	}
	item, ok, err := r.store.UpdatePolicy(req.PathValue("id"), input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_policy_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (r *Router) deletePolicy(w http.ResponseWriter, req *http.Request) {
	deleted, inUse, err := r.store.DeletePolicy(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_policy_failed"})
		return
	}
	if inUse {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "policy_in_use", "message": "This policy is used by a DR configuration and cannot be deleted."})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func probeStorageRepository(repo store.StorageRepository, timeout time.Duration) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	typeName := strings.ToLower(strings.TrimSpace(repo.Type))
	if repo.Endpoint == "" && typeName != "google cloud" && typeName != "gcs" {
		return nil, errors.New("endpoint is empty")
	}
	if repo.Bucket == "" {
		return nil, errors.New("bucket is empty")
	}
	if typeName == "s3" || typeName == "s3-compatible" || typeName == "s3 compatible" {
		creds := storageCredentials(repo)
		if creds == nil || strings.TrimSpace(creds.AccessKey) == "" || strings.TrimSpace(creds.SecretKey) == "" {
			return nil, errors.New("S3 access key and secret key are required")
		}
		endpoint, secure := minioEndpoint(repo)
		client, err := minio.New(endpoint, &minio.Options{
			Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
			Secure:       secure,
			Region:       normalizedStorageRegion(repo.Type, repo.Region),
			BucketLookup: storageBucketLookup(repo),
		})
		if err != nil {
			return map[string]any{"endpoint": endpoint, "bucket": repo.Bucket}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		for object := range client.ListObjects(ctx, repo.Bucket, minio.ListObjectsOptions{Recursive: false}) {
			if object.Err != nil {
				return map[string]any{"endpoint": endpoint, "bucket": repo.Bucket}, object.Err
			}
			break
		}
		return map[string]any{"endpoint": endpoint, "bucket": repo.Bucket, "authenticated": true}, nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(repo.Endpoint), "/")
	if typeName == "google cloud" || typeName == "gcs" {
		endpoint = "storage.googleapis.com"
	}
	scheme := "http"
	if repo.TLSEnabled {
		scheme = "https"
	}
	if strings.HasPrefix(endpoint, "http://") {
		scheme = "http"
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		scheme = "https"
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	urlStyle, _ := repo.Config["urlStyle"].(string)
	if urlStyle == "" {
		urlStyle = "path"
	}
	var probeURL string
	if typeName == "azure" {
		probeURL = scheme + "://" + endpoint + "/" + repo.Bucket + "?restype=container"
	} else if typeName == "google cloud" || typeName == "gcs" {
		probeURL = "https://storage.googleapis.com/" + repo.Bucket
	} else if urlStyle == "virtual" {
		probeURL = scheme + "://" + repo.Bucket + "." + endpoint + "/?probe=1"
	} else {
		probeURL = scheme + "://" + endpoint + "/" + repo.Bucket + "?probe=1"
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Head(probeURL)
	if err != nil {
		return map[string]any{"url": probeURL, "style": urlStyle}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusForbidden {
		return map[string]any{"url": probeURL, "style": urlStyle, "statusCode": resp.StatusCode}, nil
	}
	return map[string]any{"url": probeURL, "style": urlStyle, "statusCode": resp.StatusCode},
		errors.New("unexpected status " + resp.Status)
}

type objectStorageCleanupResult struct {
	RepositoryID   string
	RepositoryName string
	Prefix         string
	ObjectsDeleted int
	BytesDeleted   int64
}
