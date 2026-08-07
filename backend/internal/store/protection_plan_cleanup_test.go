package store

import "testing"

func TestCleanupProtectionPlanRecordsReturnsApplicationsToUnprotected(t *testing.T) {
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
	if app.ProtectionStatus != "unprotected" {
		t.Fatalf("application protection status = %q, want unprotected", app.ProtectionStatus)
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
