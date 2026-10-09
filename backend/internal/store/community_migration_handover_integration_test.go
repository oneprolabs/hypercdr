package store

import (
	"testing"
	"time"
)

func TestMigrationHandoverTokenPreservesClusterIdentityPostgres(t *testing.T) {
	repo := newTestStore(t)
	staged, _ := seedPlanApplication(t, repo)
	token, err := repo.CreateAgentToken(DefaultTenantID, "", "community-migration-handover:test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec("update agent_tokens set cluster_id=$1 where id=$2", staged.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	cluster, credential, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "Migrated cluster reconnected", KubeVersion: "v1.30.0", AgentVersion: "migration-test", VeleroVersion: "v1.18.2", VeleroStatus: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ID != staged.ID {
		t.Fatalf("cluster identity changed: got %s want %s", cluster.ID, staged.ID)
	}
	if credential == "" {
		t.Fatal("target Agent credential was not issued")
	}
	if _, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token}); err != ErrTokenUsed {
		t.Fatalf("handover token reuse: %v", err)
	}
}
