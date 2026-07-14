package velero

import (
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
