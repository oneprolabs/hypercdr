package store

import (
	"context"
	"hypercdr-platform/platform/backend/internal/testdb"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	repo, err := NewPostgresStore(context.Background(), testdb.New(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := repo.ConfigureSecretKey(context.Background(), "postgres-test-secret"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func seedPlanApplication(t *testing.T, repo *PostgresStore) (Cluster, Application) {
	t.Helper()
	token, err := repo.CreateAgentToken(DefaultTenantID, "", "plan-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "plan-test"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = repo.ApplyInventory(InventoryInput{ClusterID: cluster.ID, Apps: []Application{{Namespace: "demo", Name: "demo", Status: "active", ProtectionStatus: "protected"}}})
	if err != nil {
		t.Fatal(err)
	}
	apps, err := repo.ListApplications(cluster.ID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("seed applications: %v %v", apps, err)
	}
	return cluster, apps[0]
}
func seedProtectionPlan(t *testing.T, repo *PostgresStore) (Application, ProtectionPlan) {
	t.Helper()
	cluster, app := seedPlanApplication(t, repo)
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: cluster.ID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	status := "protected"
	if _, _, err := repo.UpdateApplication(ApplicationUpdateInput{ID: app.ID, ProtectionStatus: status}); err != nil {
		t.Fatal(err)
	}
	return app, plan
}

func testAdmin(t *testing.T, repo *PostgresStore) User {
	t.Helper()
	users, err := repo.ListUsers()
	if err != nil || len(users) == 0 {
		t.Fatalf("test admin: %v", err)
	}
	return users[0]
}
