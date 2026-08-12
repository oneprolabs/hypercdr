package wsclient

import (
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestSizeMetricsV2PreservesKnownZeroIncrement(t *testing.T) {
	metrics := sizeMetricsV2(protocol.TaskDispatchPayload{Type: "backup"}, map[string]any{
		"restorePointSize": map[string]any{
			"totalBytes": float64(1024), "metadataBytes": float64(24), "volumeBytes": float64(1000),
			"uploadedBytes": float64(24), "uploadedMetadataBytes": float64(24), "uploadedVolumeBytes": float64(0),
			"volumeCount": float64(1), "incrementalKnownCount": float64(1), "allIncrementalKnown": true,
		},
	})
	if metrics == nil || !metrics.NewData.Known || metrics.NewData.TotalBytes != 24 || metrics.NewData.VolumeBytes != 0 {
		t.Fatalf("known zero incremental metrics not preserved: %#v", metrics)
	}
	if metrics.Reuse.Status != "available" || metrics.Reuse.Ratio != 0.9765625 {
		t.Fatalf("reuse = %#v, want overall reuse 0.9765625", metrics.Reuse)
	}
}

func TestSizeMetricsV2ReuseIncludesMetadataAndVolumeData(t *testing.T) {
	metrics := sizeMetricsV2(protocol.TaskDispatchPayload{Type: "backup"}, map[string]any{
		"restorePointSize": map[string]any{
			"totalBytes": float64(210451553), "metadataBytes": float64(23777), "volumeBytes": float64(210427776),
			"uploadedBytes": float64(23777), "uploadedMetadataBytes": float64(23777), "uploadedVolumeBytes": float64(0),
			"volumeCount": float64(1), "incrementalKnownCount": float64(1), "allIncrementalKnown": true,
		},
	})
	want := 1 - float64(23777)/float64(210451553)
	if metrics == nil || metrics.Reuse.Status != "available" || metrics.Reuse.Ratio != want {
		t.Fatalf("reuse = %#v, want overall ratio %.12f", metrics, want)
	}
}

func TestSizeMetricsV2DoesNotPresentUnknownAsZero(t *testing.T) {
	metrics := sizeMetricsV2(protocol.TaskDispatchPayload{Type: "backup"}, map[string]any{
		"restorePointSize": map[string]any{
			"totalBytes": float64(1024), "volumeBytes": float64(1000),
			"uploadedVolumeBytes": float64(0), "volumeCount": float64(1), "incrementalKnownCount": float64(0),
		},
	})
	if metrics == nil || metrics.NewData.Known || metrics.Reuse.Status != "unavailable" {
		t.Fatalf("unknown incremental metrics were presented as known: %#v", metrics)
	}
}

func TestSizeMetricsV2MetadataOnlyBackupIsKnown(t *testing.T) {
	metrics := sizeMetricsV2(protocol.TaskDispatchPayload{Type: "backup"}, map[string]any{
		"restorePointSize": map[string]any{
			"totalBytes": float64(17721), "metadataBytes": float64(17721), "volumeBytes": float64(0),
			"uploadedBytes": float64(17721), "uploadedMetadataBytes": float64(17721), "uploadedVolumeBytes": float64(0),
			"volumeCount": float64(0), "incrementalKnownCount": float64(0), "allIncrementalKnown": true,
		},
	})
	if metrics == nil || !metrics.NewData.Known || metrics.NewData.TotalBytes != 17721 {
		t.Fatalf("metadata-only new data = %#v, want known 17721 bytes", metrics)
	}
	if metrics.Reuse.Status != "available" || metrics.Reuse.Ratio != 0 {
		t.Fatalf("metadata-only reuse = %#v, want available 0", metrics.Reuse)
	}
}
