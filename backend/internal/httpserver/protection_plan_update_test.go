package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestEditProtectionPlanKeepsIdentityAndHistory(t *testing.T) {
	repo := newTestStore(t)
	source := seedSchedulerCluster(t, repo)
	target := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, source, "edit-plan")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "edit", Type: "s3", Endpoint: "http://minio:9000", Bucket: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	input := store.ProtectionPlanInput{SourceClusterID: source, AppID: app.ID, StorageRepoID: storage.ID, TargetClusterID: source, Status: "active"}
	plan, err := repo.CreateProtectionPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err = repo.GetProtectionPlan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := repo.CreateTask(store.TaskInput{ClusterID: source, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{ProtectionPlanID: plan.ID, Enabled: true, NextFireAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
	input.TargetClusterID = target
	input.ResourceSelection = store.ResourceSelection{Mode: "exclude", NamespaceScoped: []string{"events"}}
	raw, _ := json.Marshal(input)
	save := func(actor store.User) *httptest.ResponseRecorder {
		req := tenantRequest(httptest.NewRequest("PUT", "/api/v1/protection-plans/"+plan.ID, strings.NewReader(string(raw))), actor)
		req.SetPathValue("id", plan.ID)
		res := httptest.NewRecorder()
		r.tenantGuard("plan", r.createProtectionPlan)(res, req)
		return res
	}
	res := save(store.User{TenantID: store.DefaultTenantID})
	if res.Code != 200 {
		t.Fatalf("save: %d %s", res.Code, res.Body.String())
	}
	updated, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok || updated.TargetClusterID != target || !updated.CreatedAt.Equal(plan.CreatedAt) || updated.ResourceSelection.Mode != "exclude" {
		t.Fatalf("updated=%+v ok=%v err=%v", updated, ok, err)
	}
	schedule, found, err := repo.GetProtectionPlanSchedule(plan.ID)
	if err != nil || !found || schedule.Enabled {
		t.Fatalf("previous schedule not paused: %+v %v", schedule, err)
	}
	plans, _ := repo.ListProtectionPlans(source)
	if len(plans) != 1 {
		t.Fatalf("created duplicate: %d", len(plans))
	}
	task, ok, err := repo.GetTask(history.ID)
	if err != nil || !ok || task.ProtectionPlanID != plan.ID {
		t.Fatalf("history lost: %+v %v", task, err)
	}
	foreign, _ := repo.CreateTenant(store.TenantInput{Name: "other", Status: "active"})
	if res := save(store.User{TenantID: foreign.ID}); res.Code != 404 {
		t.Fatalf("foreign edit: %d %s", res.Code, res.Body.String())
	}
	// Once configuration is ready, an active backup must still block editing.
	_, _, _ = repo.UpdateProtectionPlanStatus(plan.ID, "active")
	_, err = repo.CreateTask(store.TaskInput{ClusterID: source, ProtectionPlanID: plan.ID, Type: "backup", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if res := save(store.User{TenantID: store.DefaultTenantID}); res.Code != 409 {
		t.Fatalf("active work edit: %d %s", res.Code, res.Body.String())
	}
	unchanged, _, _ := repo.GetProtectionPlan(plan.ID)
	if unchanged.Status != "active" {
		t.Fatalf("rejected edit changed status: %s", unchanged.Status)
	}
}
