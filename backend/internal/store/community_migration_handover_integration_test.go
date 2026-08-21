package store

import (
	"context"
	"os"
	"testing"
)

func TestMigrationHandoverTokenPreservesClusterIdentityPostgres(t *testing.T) {
	dsn := os.Getenv("HCDR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HCDR_TEST_DATABASE_URL is not set")
	}
	repo, err := NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	var token, expectedClusterID string
	err = repo.db.QueryRow(`select token_hash,cluster_id::text from agent_tokens where description like 'community-migration-handover:%' and used_at is null order by created_at desc limit 1`).Scan(&token, &expectedClusterID)
	if err != nil {
		t.Fatal(err)
	}
	cluster, credential, err := repo.RegisterCluster(RegisterClusterInput{Token: token, ClusterName: "Migrated cluster reconnected", KubeVersion: "v1.30.0", AgentVersion: "migration-test", VeleroVersion: "v1.18.2", VeleroStatus: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ID != expectedClusterID {
		t.Fatalf("cluster identity changed: got %s want %s", cluster.ID, expectedClusterID)
	}
	if credential == "" {
		t.Fatal("target Agent credential was not issued")
	}
}
