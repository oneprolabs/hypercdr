package wsclient

import (
	"context"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
)

type backupSizeStatsReader struct {
	metadataBytes int64
	volumeBytes   int64
}

func (r backupSizeStatsReader) GetBackupObjectStats(context.Context, string, string, string) (kube.BackupObjectStats, error) {
	return kube.BackupObjectStats{MetadataPackageBytes: r.metadataBytes}, nil
}

func (r backupSizeStatsReader) GetBackupVolumeInfoStats(context.Context, string, string, string) (kube.BackupVolumeInfoStats, error) {
	return kube.BackupVolumeInfoStats{VolumeBytes: r.volumeBytes, Accurate: true}, nil
}

func (backupSizeStatsReader) GetPlanObjectStorageStats(context.Context, string, string, string, []string) (kube.PlanObjectStorageStats, error) {
	return kube.PlanObjectStorageStats{}, nil
}

func (backupSizeStatsReader) GetRestoreResultSummary(context.Context, string, string, string) (kube.RestoreResultSummary, error) {
	return kube.RestoreResultSummary{}, nil
}

func TestAttachBackupSizeStatsPreservesKnownZeroIncrementalUpload(t *testing.T) {
	const (
		metadataBytes = int64(24_001)
		volumeBytes   = int64(210_427_776)
	)
	client := &Client{statsReader: backupSizeStatsReader{
		metadataBytes: metadataBytes,
		volumeBytes:   volumeBytes,
	}}
	payload := map[string]any{
		"storageLocation": "default",
		"volumeProgress": map[string]any{
			"volumeBytes":      volumeBytes,
			"incrementalBytes": int64(0),
			"incrementalCount": int64(1),
		},
	}

	client.attachBackupSizeStats(context.Background(), kube.AppliedObject{
		Kind:      "Backup",
		Namespace: "hypercdr-agent",
		Name:      "backup-1",
	}, payload)

	size := wsMapFromAny(payload["restorePointSize"])
	if got := int64FromAny(size["totalBytes"]); got != metadataBytes+volumeBytes {
		t.Fatalf("totalBytes = %d, want %d", got, metadataBytes+volumeBytes)
	}
	if got := int64FromAny(size["uploadedVolumeBytes"]); got != 0 {
		t.Fatalf("uploadedVolumeBytes = %d, want 0", got)
	}
	if got := int64FromAny(size["uploadedBytes"]); got != metadataBytes {
		t.Fatalf("uploadedBytes = %d, want %d", got, metadataBytes)
	}
}
