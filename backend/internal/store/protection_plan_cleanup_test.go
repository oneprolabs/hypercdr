package store

import (
	"errors"
	"testing"
)

func TestCleanupProtectionPlanRecordsReturnsApplicationsToPendingProtection(t *testing.T) {
	repo := NewMemoryStore()
	const appID = "app-1"
	const planID = "plan-1"
	repo.applications[appID] = Application{ID: appID, ProtectionStatus: "protected"}
	repo.plans[planID] = ProtectionPlan{ID: planID, AppID: appID, AppIDs: []string{appID}}

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
	repo := NewMemoryStore()
	const appID = "app-1"
	const planID = "plan-1"
	repo.applications[appID] = Application{ID: appID, ProtectionStatus: "protected"}
	repo.plans[planID] = ProtectionPlan{ID: planID, AppID: appID, AppIDs: []string{appID}}

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
	repo := NewMemoryStore()
	first, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: "app-1"})
	if err != nil {
		t.Fatalf("create first plan: %v", err)
	}
	_, err = repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-1", AppIDs: []string{"app-2", "app-1"}})
	var conflict *ApplicationAlreadyProtectedError
	if !errors.As(err, &conflict) || conflict.ProtectionPlanID != first.ID || conflict.ApplicationID != "app-1" {
		t.Fatalf("duplicate error = %#v, want conflict with plan %q and app-1", err, first.ID)
	}
	if _, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-2", SourceClusterID: "cluster-1", AppID: "app-1"}); err != nil {
		t.Fatalf("same app identity in another tenant should be allowed: %v", err)
	}
	if _, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-2", AppID: "app-1"}); err != nil {
		t.Fatalf("same app identity in another source cluster should be allowed: %v", err)
	}
}

func TestCleanupProtectionPlanKeepsApplicationProtectedWhenAnotherPlanOwnsIt(t *testing.T) {
	repo := NewMemoryStore()
	const appID = "app-1"
	repo.applications[appID] = Application{ID: appID, ProtectionStatus: "protected"}
	repo.plans["old-plan"] = ProtectionPlan{ID: "old-plan", TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: appID, AppIDs: []string{appID}}
	repo.plans["remaining-plan"] = ProtectionPlan{ID: "remaining-plan", TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: appID, AppIDs: []string{appID}}

	if _, ok, err := repo.CleanupProtectionPlanRecords("old-plan"); err != nil || !ok {
		t.Fatalf("cleanup duplicate plan: ok=%v err=%v", ok, err)
	}
	app, _, _ := repo.GetApplication(appID)
	if app.ProtectionStatus != "protected" {
		t.Fatalf("application protection status = %q, want protected", app.ProtectionStatus)
	}
}
