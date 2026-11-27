package httpserver

import (
	"strings"
	"testing"
)

func TestBackupFailureResultsReachUserFacingDetails(t *testing.T) {
	for _, errors := range []any{[]string{"error to list DataUpload: API timeout"}, []any{"error to list DataUpload: API timeout"}} {
		details := map[string]any{"velero": map[string]any{"backupResults": map[string]any{"errors": errors}}}
		message := detailedTaskFailureMessage("velero backup failed", details)
		if message != "error to list DataUpload: API timeout" {
			t.Fatalf("missing original failure: %s", message)
		}
		patch := taskFailurePayloadPatch(details)
		if !strings.Contains(strings.Join(stringArrayFromAny(patch["failureDetails"]), "\n"), "API timeout") {
			t.Fatalf("failure evidence not persisted: %+v", patch)
		}
	}
}
