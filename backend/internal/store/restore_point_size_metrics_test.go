package store

import "testing"

func TestRestorePointSizeMetricsV2RoundTripAndUpsert(t *testing.T) {
	repo := NewMemoryStore()
	point, err := repo.CreateRestorePoint(RestorePointInput{
		SourceClusterID: "cluster-size", VeleroBackupName: "backup-size",
		SizeMetricsV2: map[string]any{
			"schemaVersion": float64(2),
			"newData":       map[string]any{"totalBytes": float64(0), "known": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	newData := point.SizeMetricsV2["newData"].(map[string]any)
	if known, _ := newData["known"].(bool); !known {
		t.Fatal("known zero incremental data was not preserved")
	}
	updated, err := repo.CreateRestorePoint(RestorePointInput{
		SourceClusterID: "cluster-size", VeleroBackupName: "backup-size",
		SizeMetricsV2: map[string]any{"measurementStatus": "partial"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.SizeMetricsV2["measurementStatus"]; got != "partial" {
		t.Fatalf("measurementStatus = %v, want partial", got)
	}
}
