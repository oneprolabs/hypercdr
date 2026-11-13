package httpserver

import "testing"

func TestCacheHitChangesAreNotDeduplicated(t *testing.T) {
	observation := func(hit int) map[string]any {
		return map[string]any{"name": "backup-cache", "kind": "Backup", "resourceVersion": "42", "volumeProgress": map[string]any{"items": []map[string]any{{"name": "pvb", "cacheStats": map[string]any{"version": 1, "indexHitBytes": hit, "indexReadBytes": 100}}}}}
	}
	if sameVeleroEventPayload(observation(30), observation(50)) {
		t.Fatal("cache statistics change was lost")
	}
	if !sameVeleroEventPayload(observation(30), observation(30)) {
		t.Fatal("same statistics not deduplicated")
	}
}

func TestCacheCleanupRetryChangesAreNotDeduplicated(t *testing.T) {
	previous := map[string]any{"kind": "CacheCleanup", "completed": 0, "total": 2, "failedAttempts": 0}
	unchanged := map[string]any{"kind": "CacheCleanup", "completed": float64(0), "total": float64(2), "failedAttempts": float64(0)}
	if !sameVeleroEventPayload(previous, unchanged) {
		t.Fatal("unchanged cleanup progress should be deduplicated")
	}
	for _, key := range []string{"completed", "total", "failedAttempts"} {
		changed := map[string]any{"kind": "CacheCleanup", "completed": 0, "total": 2, "failedAttempts": 0}
		changed[key] = 3
		if sameVeleroEventPayload(previous, changed) {
			t.Fatalf("cleanup change in %s was lost", key)
		}
	}
}

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
