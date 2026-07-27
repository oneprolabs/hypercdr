package kube

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type backupStorageLocationInfo struct {
	Bucket     string
	Prefix     string
	Endpoint   string
	Region     string
	Secure     bool
	Credential credentialRef
	Provider   string
}

type credentialRef struct {
	Name string
	Key  string
}

type s3Credentials struct {
	AccessKey string
	SecretKey string
}

func (a *DynamicManifestApplier) GetBackupObjectStats(ctx context.Context, namespace string, storageLocation string, backupName string) (BackupObjectStats, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" || strings.TrimSpace(backupName) == "" {
		return BackupObjectStats{}, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return BackupObjectStats{}, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return BackupObjectStats{}, fmt.Errorf("backup object stats only support aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return BackupObjectStats{}, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return BackupObjectStats{}, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return BackupObjectStats{}, err
	}
	prefix := backupObjectPrefix(bsl.Prefix, backupName)
	stats := BackupObjectStats{Prefix: prefix}
	for object := range client.ListObjects(ctx, bsl.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return BackupObjectStats{}, object.Err
		}
		stats.MetadataPackageBytes += object.Size
		stats.ObjectCount++
	}
	return stats, nil
}

func (a *DynamicManifestApplier) GetBackupVolumeInfoStats(ctx context.Context, namespace string, storageLocation string, backupName string) (BackupVolumeInfoStats, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" || strings.TrimSpace(backupName) == "" {
		return BackupVolumeInfoStats{}, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return BackupVolumeInfoStats{}, fmt.Errorf("backup volume info stats only support aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	key := backupVolumeInfoKey(bsl.Prefix, backupName)
	object, err := client.GetObject(ctx, bsl.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	defer object.Close()
	reader, err := gzip.NewReader(object)
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return BackupVolumeInfoStats{}, err
	}
	var infos []map[string]any
	if err := json.Unmarshal(data, &infos); err != nil {
		return BackupVolumeInfoStats{}, err
	}
	stats := BackupVolumeInfoStats{Key: key}
	for _, info := range infos {
		size := backupVolumeInfoSize(info)
		if size <= 0 {
			continue
		}
		stats.VolumeBytes += size
		stats.VolumeCount++
	}
	stats.Accurate = len(infos) > 0 && stats.VolumeCount > 0
	return stats, nil
}

func (a *DynamicManifestApplier) GetPlanObjectStorageStats(ctx context.Context, namespace string, storageLocation string, backupNamePrefix string, repositoryNamespaces []string) (PlanObjectStorageStats, error) {
	backupNamePrefix = strings.TrimSpace(backupNamePrefix)
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" || backupNamePrefix == "" {
		return PlanObjectStorageStats{}, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return PlanObjectStorageStats{}, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return PlanObjectStorageStats{}, fmt.Errorf("plan object storage stats only support aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return PlanObjectStorageStats{}, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return PlanObjectStorageStats{}, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return PlanObjectStorageStats{}, err
	}

	backupPrefix := strings.TrimSuffix(backupObjectPrefix(bsl.Prefix, backupNamePrefix), "/")
	stats := PlanObjectStorageStats{
		BackupNamePrefix: backupNamePrefix,
		BackupPrefix:     backupPrefix,
		KopiaNamespaces:  uniqueNonEmptyStrings(repositoryNamespaces),
	}
	for object := range client.ListObjects(ctx, bsl.Bucket, minio.ListObjectsOptions{Prefix: backupPrefix, Recursive: true}) {
		if object.Err != nil {
			return PlanObjectStorageStats{}, object.Err
		}
		stats.MetadataBytes += object.Size
		stats.BackupObjectCount++
	}

	for _, repoNamespace := range stats.KopiaNamespaces {
		prefix := kopiaRepositoryPrefix(bsl.Prefix, repoNamespace)
		stats.KopiaPrefixes = append(stats.KopiaPrefixes, prefix)
		for object := range client.ListObjects(ctx, bsl.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if object.Err != nil {
				return PlanObjectStorageStats{}, object.Err
			}
			stats.KopiaBytes += object.Size
			stats.KopiaObjectCount++
		}
	}
	stats.TotalBytes = stats.MetadataBytes + stats.KopiaBytes
	return stats, nil
}

func (a *DynamicManifestApplier) GetRestoreResultSummary(ctx context.Context, namespace string, storageLocation string, restoreName string) (RestoreResultSummary, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" || strings.TrimSpace(restoreName) == "" {
		return RestoreResultSummary{}, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return RestoreResultSummary{}, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return RestoreResultSummary{}, fmt.Errorf("restore result summary only supports aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return RestoreResultSummary{}, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return RestoreResultSummary{}, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return RestoreResultSummary{}, err
	}
	key := restoreResultsKey(bsl.Prefix, restoreName)
	object, err := client.GetObject(ctx, bsl.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return RestoreResultSummary{}, err
	}
	defer object.Close()
	reader, err := gzip.NewReader(object)
	if err != nil {
		return RestoreResultSummary{}, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return RestoreResultSummary{}, err
	}
	var resultMap map[string]restoreResult
	if err := json.Unmarshal(data, &resultMap); err != nil {
		return RestoreResultSummary{}, err
	}
	errors := flattenRestoreResult(resultMap["errors"])
	warnings := flattenRestoreResult(resultMap["warnings"])
	return RestoreResultSummary{
		Key:          key,
		ErrorCount:   len(errors),
		WarningCount: len(warnings),
		Errors:       limitStrings(errors, 12),
		Warnings:     limitStrings(warnings, 8),
	}, nil
}

func (a *DynamicManifestApplier) DeleteKopiaRepositories(ctx context.Context, namespace string, storageLocation string, repositoryNamespaces []string) ([]string, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" {
		return nil, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return nil, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return nil, fmt.Errorf("kopia repository cleanup only supports aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return nil, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return nil, err
	}
	deleted := []string{}
	for _, repoNamespace := range uniqueNonEmptyStrings(repositoryNamespaces) {
		prefix := kopiaRepositoryPrefix(bsl.Prefix, repoNamespace)
		removed := int64(0)
		for object := range client.ListObjects(ctx, bsl.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if object.Err != nil {
				return deleted, object.Err
			}
			if err := client.RemoveObject(ctx, bsl.Bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
				return deleted, err
			}
			removed++
		}
		deleted = append(deleted, fmt.Sprintf("%s (%d objects)", prefix, removed))
	}
	return deleted, nil
}

func (a *DynamicManifestApplier) DeleteBackupObjectsByNamePrefix(ctx context.Context, namespace string, storageLocation string, backupNamePrefix string) ([]string, error) {
	backupNamePrefix = strings.TrimSpace(backupNamePrefix)
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" || backupNamePrefix == "" {
		return nil, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return nil, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return nil, fmt.Errorf("backup object cleanup only supports aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return nil, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return nil, err
	}
	prefix := backupObjectPrefix(bsl.Prefix, backupNamePrefix)
	prefix = strings.TrimSuffix(prefix, "/")
	removed := int64(0)
	for object := range client.ListObjects(ctx, bsl.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return nil, object.Err
		}
		if err := client.RemoveObject(ctx, bsl.Bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return nil, err
		}
		removed++
	}
	return []string{fmt.Sprintf("%s (%d objects)", prefix, removed)}, nil
}

func (a *DynamicManifestApplier) DeleteRestoreObjects(ctx context.Context, namespace string, storageLocation string, restoreNames []string) ([]string, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(storageLocation) == "" {
		return nil, nil
	}
	names := uniqueNonEmptyStrings(restoreNames)
	if len(names) == 0 {
		return nil, nil
	}
	bsl, err := a.readBackupStorageLocation(ctx, namespace, storageLocation)
	if err != nil {
		return nil, err
	}
	if bsl.Provider != "" && bsl.Provider != "aws" {
		return nil, fmt.Errorf("restore object cleanup only supports aws-compatible storage, got %s", bsl.Provider)
	}
	creds, err := a.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return nil, err
	}
	endpoint, secure, err := normalizeObjectStoreEndpoint(bsl.Endpoint, bsl.Secure)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: bsl.Region,
	})
	if err != nil {
		return nil, err
	}
	deleted := make([]string, 0, len(names))
	for _, restoreName := range names {
		prefix, err := restoreObjectPrefix(bsl.Prefix, restoreName)
		if err != nil {
			return deleted, err
		}
		removed := int64(0)
		for object := range client.ListObjects(ctx, bsl.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
			if object.Err != nil {
				return deleted, object.Err
			}
			if err := client.RemoveObject(ctx, bsl.Bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
				return deleted, err
			}
			removed++
		}
		deleted = append(deleted, fmt.Sprintf("%s (%d objects)", prefix, removed))
	}
	return deleted, nil
}

func (a *DynamicManifestApplier) readBackupStorageLocation(ctx context.Context, namespace string, name string) (backupStorageLocationInfo, error) {
	resource := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"}
	item, err := a.client.Resource(resource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return backupStorageLocationInfo{}, err
	}
	provider, _, _ := unstructured.NestedString(item.Object, "spec", "provider")
	bucket, _, _ := unstructured.NestedString(item.Object, "spec", "objectStorage", "bucket")
	prefix, _, _ := unstructured.NestedString(item.Object, "spec", "objectStorage", "prefix")
	config, _, _ := unstructured.NestedStringMap(item.Object, "spec", "config")
	credentialName, _, _ := unstructured.NestedString(item.Object, "spec", "credential", "name")
	credentialKey, _, _ := unstructured.NestedString(item.Object, "spec", "credential", "key")
	if credentialKey == "" {
		credentialKey = "cloud"
	}
	endpoint := config["s3Url"]
	if endpoint == "" {
		endpoint = "https://s3.amazonaws.com"
	}
	secure := !strings.EqualFold(config["insecureSkipTLSVerify"], "true")
	return backupStorageLocationInfo{
		Bucket:     bucket,
		Prefix:     prefix,
		Endpoint:   endpoint,
		Region:     config["region"],
		Secure:     secure,
		Provider:   provider,
		Credential: credentialRef{Name: credentialName, Key: credentialKey},
	}, nil
}

func (a *DynamicManifestApplier) readS3Credentials(ctx context.Context, namespace string, ref credentialRef) (s3Credentials, error) {
	if ref.Name == "" {
		return s3Credentials{}, fmt.Errorf("backup storage location has no credential secret")
	}
	secret, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return s3Credentials{}, err
	}
	data, _, _ := unstructured.NestedStringMap(secret.Object, "data")
	cloud := data[ref.Key]
	if decoded, err := base64.StdEncoding.DecodeString(cloud); err == nil {
		cloud = string(decoded)
	}
	if cloud == "" {
		stringData, _, _ := unstructured.NestedStringMap(secret.Object, "stringData")
		cloud = stringData[ref.Key]
	}
	creds := parseAWSCredentialsFile(cloud)
	if creds.AccessKey == "" || creds.SecretKey == "" {
		return s3Credentials{}, fmt.Errorf("credential secret %s/%s does not contain AWS access key and secret key", namespace, ref.Name)
	}
	return creds, nil
}

func parseAWSCredentialsFile(content string) s3Credentials {
	var creds s3Credentials
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "aws_access_key_id":
			creds.AccessKey = value
		case "aws_secret_access_key":
			creds.SecretKey = value
		}
	}
	return creds
}

func normalizeObjectStoreEndpoint(endpoint string, defaultSecure bool) (string, bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", defaultSecure, fmt.Errorf("object storage endpoint is empty")
	}
	secure := defaultSecure
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", secure, err
		}
		if parsed.Scheme == "http" {
			secure = false
		}
		if parsed.Scheme == "https" {
			secure = true
		}
		endpoint = parsed.Host
	}
	return endpoint, secure, nil
}

func backupObjectPrefix(rootPrefix string, backupName string) string {
	parts := []string{}
	if trimmed := strings.Trim(rootPrefix, "/"); trimmed != "" {
		parts = append(parts, trimmed)
	}
	parts = append(parts, "backups", backupName)
	return strings.Join(parts, "/") + "/"
}

func backupVolumeInfoKey(rootPrefix string, backupName string) string {
	return backupObjectPrefix(rootPrefix, backupName) + backupName + "-volumeinfo.json.gz"
}

func restoreResultsKey(rootPrefix string, restoreName string) string {
	parts := []string{}
	if trimmed := strings.Trim(rootPrefix, "/"); trimmed != "" {
		parts = append(parts, trimmed)
	}
	parts = append(parts, "restores", restoreName, "restore-"+restoreName+"-results.gz")
	return strings.Join(parts, "/")
}

func restoreObjectPrefix(rootPrefix string, restoreName string) (string, error) {
	restoreName = strings.TrimSpace(restoreName)
	if restoreName == "" || strings.Contains(restoreName, "/") || restoreName == "." || restoreName == ".." {
		return "", fmt.Errorf("invalid restore name %q", restoreName)
	}
	parts := []string{}
	if trimmed := strings.Trim(rootPrefix, "/"); trimmed != "" {
		parts = append(parts, trimmed)
	}
	parts = append(parts, "restores", restoreName)
	return strings.Join(parts, "/") + "/", nil
}

func kopiaRepositoryPrefix(rootPrefix string, namespace string) string {
	parts := []string{}
	if trimmed := strings.Trim(rootPrefix, "/"); trimmed != "" {
		parts = append(parts, trimmed)
	}
	parts = append(parts, "kopia", namespace)
	return strings.Join(parts, "/") + "/"
}

type restoreResult struct {
	Velero     []string            `json:"velero,omitempty"`
	Cluster    []string            `json:"cluster,omitempty"`
	Namespaces map[string][]string `json:"namespaces,omitempty"`
}

func flattenRestoreResult(result restoreResult) []string {
	lines := make([]string, 0, len(result.Velero)+len(result.Cluster))
	for _, message := range result.Velero {
		if trimmed := strings.TrimSpace(message); trimmed != "" {
			lines = append(lines, "Velero: "+trimmed)
		}
	}
	for _, message := range result.Cluster {
		if trimmed := strings.TrimSpace(message); trimmed != "" {
			lines = append(lines, "Cluster: "+trimmed)
		}
	}
	namespaces := make([]string, 0, len(result.Namespaces))
	for namespace := range result.Namespaces {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		for _, message := range result.Namespaces[namespace] {
			if trimmed := strings.TrimSpace(message); trimmed != "" {
				lines = append(lines, "Namespace "+namespace+": "+trimmed)
			}
		}
	}
	return lines
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func backupVolumeInfoSize(info map[string]any) int64 {
	for _, path := range [][]string{
		{"pvbInfo", "size"},
		{"snapshotDataMovementInfo", "size"},
		{"csiSnapshotInfo", "size"},
	} {
		if size := nestedInt64FromMap(info, path...); size > 0 {
			return size
		}
	}
	return 0
}

func nestedInt64FromMap(value map[string]any, fields ...string) int64 {
	var current any = value
	for _, field := range fields {
		asMap, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = asMap[field]
	}
	return int64FromAny(current)
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
