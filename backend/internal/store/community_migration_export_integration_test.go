package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCommunityMigrationExportAndBackupPostgres(t *testing.T) {
	dsn := os.Getenv("HCDR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HCDR_TEST_DATABASE_URL is not set")
	}
	repo, err := NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = repo.ConfigureSecretKey(context.Background(), "migration-export-integration-secret"); err != nil {
		t.Fatal(err)
	}
	settings, found, err := repo.GetPlatformSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		settings, err = repo.UpsertPlatformSettings(PlatformSettingsInput{AgentNamespace: "hypercdr-agent", InstanceID: NewPublicID()})
		if err != nil {
			t.Fatal(err)
		}
	}
	users, err := repo.ListUsers()
	if err != nil || len(users) == 0 {
		t.Fatalf("default user unavailable: %v", err)
	}
	authorization, err := repo.CreateCommunityMigrationAuthorization(users[0].ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.ConsumeCommunityMigrationAuthorization(authorization.Token, settings.InstanceID, "hypercdr-community-migration/v1", "integration-public-key", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := repo.CommunityMigrationManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FormatVersion != CommunityMigrationExportVersion || len(manifest.Tables) == 0 || len(manifest.SchemaVersions) == 0 {
		t.Fatalf("incomplete manifest: %#v", manifest)
	}
	batch, err := repo.CommunityMigrationBatch(context.Background(), "clusters", 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Total != int64(len(batch.Rows)) {
		t.Fatalf("batch total=%d rows=%d", batch.Total, len(batch.Rows))
	}
	backup, err := repo.CreateCommunityMigrationBackup(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if backup.SHA256 != manifest.SHA256 {
		t.Fatalf("backup manifest changed: %s != %s", backup.SHA256, manifest.SHA256)
	}
}
