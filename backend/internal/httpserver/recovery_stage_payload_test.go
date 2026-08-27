package httpserver

import (
	"strings"
	"testing"
)

func TestRecoveryStagesArePersistedFromProgressAndFailure(t *testing.T) {
	stages := []any{map[string]any{"id": "restoring_resources", "status": "succeeded"}}
	progressPatch := backupTaskPayloadPatch(map[string]any{"recoveryStages": stages})
	if got := sliceFromAny(progressPatch["recoveryStages"]); len(got) != 1 {
		t.Fatalf("progress recoveryStages = %#v, want one stage", progressPatch["recoveryStages"])
	}
	failurePatch := taskFailurePayloadPatch(map[string]any{"velero": map[string]any{"recoveryStages": stages}})
	if got := sliceFromAny(failurePatch["recoveryStages"]); len(got) != 1 {
		t.Fatalf("failure recoveryStages = %#v, want one stage", failurePatch["recoveryStages"])
	}
}

func TestRestoreDataPathFailureIsPersistedWithTechnicalEvidence(t *testing.T) {
	details := map[string]any{"velero": map[string]any{"dataPathFailure": map[string]any{
		"pod": "restore-pod", "node": "worker-1", "logDetail": "open /restore/file: read-only file system",
	}}}
	patch := taskFailurePayloadPatch(details)
	failure := mapFromAny(patch["dataPathFailure"])
	if failure["pod"] != "restore-pod" || failure["node"] != "worker-1" {
		t.Fatalf("dataPathFailure = %#v", failure)
	}
	messages := taskFailureMessagesFromDetails(details)
	for _, expected := range []string{"Restore data-path Pod: restore-pod", "Target node: worker-1", "read-only file system"} {
		found := false
		for _, message := range messages {
			if message == expected || strings.Contains(message, expected) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q in %#v", expected, messages)
		}
	}
}

func TestValidImageDigest(t *testing.T) {
	valid := "sha256:648daf2d76ab9c52e3f5ee58b320a06082c78ba0ed1abefd5ebaa91cd9404e85"
	if !validImageDigest(valid) {
		t.Fatalf("expected digest to be valid: %s", valid)
	}
	for _, invalid := range []string{"", "sha256:agent", "sha256:648DAF2D76AB9C52E3F5EE58B320A06082C78BA0ED1ABEF", "md5:648daf"} {
		if validImageDigest(invalid) {
			t.Errorf("expected digest to be invalid: %s", invalid)
		}
	}
}
