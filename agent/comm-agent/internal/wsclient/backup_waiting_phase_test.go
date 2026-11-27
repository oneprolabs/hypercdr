package wsclient

import (
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
)

func TestBackupWaitingForDataMovementIsNotReportedAsNotStarted(t *testing.T) {
	for _, phase := range []string{"WaitingForPluginOperations", "WaitingForPluginOperationsPartiallyFailed"} {
		terminal, success, _, message, code := backupStatusResult(kube.ManifestStatus{Phase: phase}, 0)
		if terminal || success || code != "" || !strings.Contains(message, "volume preparation") || strings.Contains(message, "operation to start") {
			t.Fatalf("incorrect transfer wait status for %s: %s", phase, message)
		}
	}
}
