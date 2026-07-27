package velero

import (
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestBuildBackupStorageLocationManifestMapsPathStyleConfig(t *testing.T) {
	manifest, err := BuildBackupStorageLocationManifest(StorageBuildInput{
		TaskID:         "task-a",
		CommandID:      "command-a",
		AgentNamespace: "hypercdr-agent",
		Command: protocol.StorageSyncCommand{
			RepositoryID: "repo-a",
			Name:         "my-minio",
			Type:         "S3-Compatible",
			Endpoint:     "192.168.8.171:9000",
			Bucket:       "wangjunfeng-k8s",
			TLSEnabled:   false,
			SecretRef:    "hypercdr-repo-a",
			Config: map[string]any{
				"urlStyle": "path",
				"prefix":   "clusters/source-a",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := manifest.Spec.Config["s3Url"]; got != "http://192.168.8.171:9000" {
		t.Fatalf("unexpected s3Url %q", got)
	}
	if got := manifest.Spec.Config["s3ForcePathStyle"]; got != "true" {
		t.Fatalf("unexpected s3ForcePathStyle %q", got)
	}
	if _, ok := manifest.Spec.Config["urlStyle"]; ok {
		t.Fatalf("urlStyle must not be passed through to Velero config: %#v", manifest.Spec.Config)
	}
	if _, ok := manifest.Spec.Config["prefix"]; ok {
		t.Fatalf("prefix must not be passed through to Velero config: %#v", manifest.Spec.Config)
	}
	if got := manifest.Spec.ObjectStorage.Prefix; got != "clusters/source-a" {
		t.Fatalf("unexpected object storage prefix %q", got)
	}
}

func TestBuildBackupStorageLocationManifestRejectsDisplayRegionPlaceholder(t *testing.T) {
	manifest, err := BuildBackupStorageLocationManifest(StorageBuildInput{
		Command: protocol.StorageSyncCommand{
			Name: "minio", Type: "S3-Compatible", Bucket: "backups", Region: "N/A",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Spec.Config["region"]; got != "us-east-1" {
		t.Fatalf("expected safe S3 region, got %q", got)
	}
}

func TestBuildAzureStorageManifests(t *testing.T) {
	manifests, err := BuildStorageManifests(StorageBuildInput{Command: protocol.StorageSyncCommand{Name: "azure", Type: "Azure", Bucket: "backups", SecretRef: "azure-secret", Credentials: &protocol.S3Credentials{AccountName: "account1", AccountKey: "secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	if manifests.BackupStorageLocation.Spec.Provider != "azure" {
		t.Fatalf("provider=%q", manifests.BackupStorageLocation.Spec.Provider)
	}
	if got := manifests.BackupStorageLocation.Spec.Config["storageAccount"]; got != "account1" {
		t.Fatalf("storageAccount=%q", got)
	}
	if manifests.Secret == nil || !strings.Contains(manifests.Secret.StringData["cloud"], "AZURE_STORAGE_ACCOUNT_ACCESS_KEY=secret") {
		t.Fatalf("unexpected Azure secret: %#v", manifests.Secret)
	}
}

func TestBuildGCSStorageManifests(t *testing.T) {
	key := `{"type":"service_account"}`
	manifests, err := BuildStorageManifests(StorageBuildInput{Command: protocol.StorageSyncCommand{Name: "gcs", Type: "Google Cloud", Bucket: "backups", SecretRef: "gcs-secret", Credentials: &protocol.S3Credentials{ServiceAccountKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	if manifests.BackupStorageLocation.Spec.Provider != "gcp" {
		t.Fatalf("provider=%q", manifests.BackupStorageLocation.Spec.Provider)
	}
	if manifests.Secret == nil || manifests.Secret.StringData["cloud"] != key {
		t.Fatalf("unexpected GCS secret: %#v", manifests.Secret)
	}
}
