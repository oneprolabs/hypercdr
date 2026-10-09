package store

import (
	"errors"
	"testing"
	"time"
)

func TestProtectionPlanRejectsCrossTenantReferences(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	tenant, err := repo.CreateTenant(TenantInput{Name: "Other", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.CreateAgentToken(tenant.ID, "", "other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	otherCluster, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "other"})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := repo.CreateStorageRepository(StorageRepositoryInput{TenantID: tenant.ID, Name: "other", Type: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreatePolicy(PolicyInput{TenantID: tenant.ID, Name: "other", ScheduleType: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []ProtectionPlanInput{
		{TenantID: tenant.ID, SourceClusterID: cluster.ID},
		{SourceClusterID: cluster.ID, TargetClusterID: otherCluster.ID},
		{SourceClusterID: cluster.ID, StorageRepoID: storage.ID},
		{SourceClusterID: cluster.ID, PolicyID: policy.ID},
		{TenantID: tenant.ID, SourceClusterID: otherCluster.ID, AppID: app.ID},
	}
	for _, input := range cases {
		if _, err := repo.CreateProtectionPlan(input); !errors.Is(err, ErrTenantResourceMismatch) {
			t.Fatalf("cross-tenant plan accepted: %+v err=%v", input, err)
		}
	}
	plans, err := repo.ListProtectionPlans("")
	if err != nil || len(plans) != 0 {
		t.Fatalf("invalid plans persisted: %v %v", plans, err)
	}
	if _, err := repo.CreateTask(TaskInput{TenantID: tenant.ID, ClusterID: cluster.ID, Type: "backup"}); !errors.Is(err, ErrTenantResourceMismatch) {
		t.Fatalf("cross-tenant task accepted: %v", err)
	}
}
