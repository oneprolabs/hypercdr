package httpserver

import "testing"

func TestVolumeStateChangesAreNotDeduplicatedByBackupResourceVersion(t *testing.T) {
	observation := func(phase string, elapsed int) map[string]any {
		return map[string]any{
			"name": "backup-a", "kind": "Backup", "resourceVersion": "42", "phase": "InProgress",
			"volumeProgress": map[string]any{
				"bytesDone": 0, "totalBytes": 0,
				"items": []map[string]any{{"name": "pvb-a", "phase": phase, "elapsedSeconds": elapsed}},
			},
		}
	}
	if sameVeleroEventPayload(observation("New", 1), observation("InProgress", 2)) {
		t.Fatal("volume start must be recorded even with unchanged aggregate bytes and backup resource version")
	}
	if !sameVeleroEventPayload(observation("InProgress", 2), observation("InProgress", 3)) {
		t.Fatal("elapsed time alone must not create a new event")
	}
}
