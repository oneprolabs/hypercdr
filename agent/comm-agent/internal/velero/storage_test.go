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

func TestBuildStorageManifestsUsesOpenShiftOADPNamespace(t *testing.T) {
	manifests, err := BuildStorageManifests(StorageBuildInput{
		TaskID: "task-oadp", CommandID: "command-oadp", AgentNamespace: "openshift-adp",
		Command: protocol.StorageSyncCommand{Name: "aliyun-s3", Type: "S3-Compatible", Bucket: "hypercdr", SecretRef: "oadp-object-storage", Credentials: &protocol.S3Credentials{AccessKey: "access", SecretKey: "secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifests.BackupStorageLocation.Metadata.Namespace != "openshift-adp" {
		t.Fatalf("BSL namespace = %q", manifests.BackupStorageLocation.Metadata.Namespace)
	}
	if manifests.Secret == nil || manifests.Secret.Metadata.Namespace != "openshift-adp" {
		t.Fatalf("credential secret was not created in openshift-adp: %#v", manifests.Secret)
	}
	if manifests.BackupStorageLocation.Spec.Provider != "aws" {
		t.Fatalf("provider = %q", manifests.BackupStorageLocation.Spec.Provider)
	}
}

func TestBuildBackupStorageLocationManifestMapsVirtualHostStyleConfig(t *testing.T) {
	manifest, err := BuildBackupStorageLocationManifest(StorageBuildInput{Command: protocol.StorageSyncCommand{
		Name: "my-obs", Type: "S3-Compatible", Endpoint: "obs.cn-north-9.myhuaweicloud.com", Bucket: "backups", Region: "cn-north-9", TLSEnabled: true,
		Config: map[string]any{"urlStyle": "virtual"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Spec.Config["s3ForcePathStyle"]; got != "false" {
		t.Fatalf("unexpected s3ForcePathStyle for virtual host: %q", got)
	}
	if got, ok := manifest.Spec.Config["checksumAlgorithm"]; !ok || got != "" {
		t.Fatalf("Huawei OBS must disable payload checksum, config=%#v", manifest.Spec.Config)
	}
}

func TestS3VendorCompatibilityDoesNotAffectOtherProviders(t *testing.T) {
	tests := []struct{ name, endpoint string }{
		{name: "minio", endpoint: "minio.example.internal:9000"},
		{name: "aws", endpoint: "s3.cn-north-1.amazonaws.com.cn"},
		{name: "generic-s3", endpoint: "objects.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := BuildBackupStorageLocationManifest(StorageBuildInput{Command: protocol.StorageSyncCommand{
				Name: tt.name, Type: "S3-Compatible", Endpoint: tt.endpoint, Bucket: "backups", TLSEnabled: true,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := manifest.Spec.Config["checksumAlgorithm"]; ok {
				t.Fatalf("vendor-specific OBS config leaked into %s: %#v", tt.name, manifest.Spec.Config)
			}
		})
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
