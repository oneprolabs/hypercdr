package store

import "testing"

func TestRestorePointDisplayNameIsDeprecated(t *testing.T) {
	repo := NewMemoryStore()
	point, err := repo.CreateRestorePoint(RestorePointInput{
		SourceClusterID:  "cluster-1",
		VeleroBackupName: "backup-1",
		DisplayName:      "RP-custom",
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint() error = %v", err)
	}
	if point.DisplayName != "" {
		t.Fatalf("CreateRestorePoint() displayName = %q, want empty", point.DisplayName)
	}
}
