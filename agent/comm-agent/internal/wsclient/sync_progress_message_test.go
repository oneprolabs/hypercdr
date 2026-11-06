package wsclient

import (
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
)

func TestBackupProgressMessageDoesNotClaimOverallPercentage(t *testing.T) {
	for _, known := range []bool{true, false} {
		message := volumeProgressMessage("backup", kube.VolumeProgress{BytesDone: 512, TotalBytes: 1024, KnownTotal: known}, 100, 5, 50)
		if !strings.Contains(message, "processed 512 B") || strings.Contains(message, "%") || strings.Contains(message, "remaining") {
			t.Fatalf("misleading aggregate backup progress: %s", message)
		}
	}
}
