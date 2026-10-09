package store

import (
	"errors"
	"testing"
	"time"
)

func TestCleanupProtectionPlanRecordsReturnsApplicationsToPendingProtection(t *testing.T) {
	repo := newTestStore(t)
	appFixture, planFixture := seedProtectionPlan(t, repo)
	appID, planID := appFixture.ID, planFixture.ID

	if _, ok, err := repo.CleanupProtectionPlanRecords(planID); err != nil || !ok {
		t.Fatalf("cleanup protection plan: ok=%v err=%v", ok, err)
	}
	app, ok, err := repo.GetApplication(appID)
	if err != nil || !ok {
		t.Fatalf("get application after cleanup: ok=%v err=%v", ok, err)
	}
	if app.ProtectionStatus != "pending_protection" {
		t.Fatalf("application protection status = %q, want pending_protection", app.ProtectionStatus)
	}
}

func TestDeleteProtectionPlanReturnsApplicationsToUnprotected(t *testing.T) {
	repo := newTestStore(t)
	appFixture, planFixture := seedProtectionPlan(t, repo)
	appID, planID := appFixture.ID, planFixture.ID

	if _, ok, err := repo.DeleteProtectionPlan(planID); err != nil || !ok {
		t.Fatalf("delete protection plan: ok=%v err=%v", ok, err)
	}
	app, ok, err := repo.GetApplication(appID)
	if err != nil || !ok {
		t.Fatalf("get application after delete: ok=%v err=%v", ok, err)
	}
	if app.ProtectionStatus != "unprotected" {
		t.Fatalf("application protection status = %q, want unprotected", app.ProtectionStatus)
	}
}

func TestCreateProtectionPlanRejectsDuplicateApplication(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	first, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppIDs: []string{app.ID}})
	var conflict *ApplicationAlreadyProtectedError
	if !errors.As(err, &conflict) || conflict.ProtectionPlanID != first.ID || conflict.ApplicationID != app.ID {
		t.Fatalf("duplicate error=%v, want plan %s application %s", err, first.ID, app.ID)
	}
	otherCluster, otherApp := seedPlanApplication(t, repo)
	if _, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: otherCluster.ID, AppID: otherApp.ID}); err != nil {
		t.Fatalf("separate cluster/application should be allowed: %v", err)
	}
	tenant, err := repo.CreateTenant(TenantInput{Name: "Independent tenant", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.CreateAgentToken(tenant.ID, "", "other-tenant", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	separate, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "plan-test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyInventory(InventoryInput{ClusterID: separate.ID, Apps: []Application{{Namespace: app.Namespace, Name: app.Name}}}); err != nil {
		t.Fatal(err)
	}
	apps, err := repo.ListApplications(separate.ID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("tenant applications: %v %v", apps, err)
	}
	if _, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: tenant.ID, SourceClusterID: separate.ID, AppID: apps[0].ID}); err != nil {
		t.Fatalf("same namespace/name in independent tenant: %v", err)
	}

}

func TestCleanupProtectionPlanKeepsApplicationProtectedWhenAnotherPlanOwnsIt(t *testing.T) {
	repo := newTestStore(t)
	appFixture, oldPlan := seedProtectionPlan(t, repo)
	appID := appFixture.ID
	remainingID := newID()
	// Simulate legacy duplicate ownership; the public API correctly rejects new duplicates.
	if _, err := repo.db.Exec(`insert into protection_plans(id,tenant_id,source_cluster_id,app_id,scope_type,status) select $1,tenant_id,source_cluster_id,app_id,scope_type,status from protection_plans where id=$2`, remainingID, oldPlan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`insert into protection_plan_apps(plan_id,app_id) values($1,$2)`, remainingID, appID); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := repo.CleanupProtectionPlanRecords(oldPlan.ID); err != nil || !ok {
		t.Fatalf("cleanup duplicate plan: ok=%v err=%v", ok, err)
	}
	app, _, _ := repo.GetApplication(appID)
	if app.ProtectionStatus != "protected" {
		t.Fatalf("application protection status = %q, want protected", app.ProtectionStatus)
	}
}
