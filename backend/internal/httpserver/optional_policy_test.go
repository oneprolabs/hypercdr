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

func TestNoPolicyDisablesStaleScheduleAndAllowsManualBackup(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "no-policy")
	app := seedSchedulerApplication(t, repo, cluster.ID, "manual-app")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: actor.TenantID, SourceClusterID: cluster.ID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
	if _, scheduled, err := router.protectionPlanSchedulePolicy(plan); err != nil || scheduled {
		t.Fatalf("no policy must not schedule: %v %v", scheduled, err)
	}
	if task, warning, err := router.dispatchScheduleSyncTask(plan); err != nil || task.ID != "" || warning != "" {
		t.Fatalf("unexpected agent schedule sync: %+v %s %v", task, warning, err)
	}
	now := time.Now().UTC()
	for _, nextFire := range []time.Time{now.Add(time.Hour), now.Add(-time.Hour)} {
		schedule, err := repo.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{ProtectionPlanID: plan.ID, Enabled: true, NextFireAt: nextFire})
		if err != nil {
			t.Fatal(err)
		}
		if nextFire.Before(now) {
			router.fireProtectionPlanSchedule(schedule, now)
		} else {
			router.reconcilePlatformSchedules(now)
		}
		actual, found, err := repo.GetProtectionPlanSchedule(plan.ID)
		if err != nil || !found || actual.Enabled {
			t.Fatalf("stale schedule still enabled: %+v %v", actual, err)
		}
	}
	router.reconcileRetention(plan.ID, "")
	tasks, err := repo.ListTasks(cluster.ID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("unexpected automatic task: %+v %v", tasks, err)
	}
	body := `{"clusterId":"` + cluster.ID + `","protectionPlanId":"` + plan.ID + `"}`
	taskContractRun(t, router, actor, "POST /api/v1/tasks/backup", body, "", router.createBackupTask, 201)
}

func TestClearPolicyPreservesRestorePointAndDisablesSchedule(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "clear-policy")
	point := seedContractRestorePoint(t, repo, cluster, "keep-manual-history")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: actor.TenantID, Name: "manual-repo", Type: "s3", Endpoint: "http://minio:9000", Bucket: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreatePolicy(store.PolicyInput{TenantID: actor.TenantID, Name: "previous-policy", ScheduleType: "interval", IntervalValue: 1, IntervalUnit: "hour", Composition: "combined", RetentionCount: 1, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	input := store.ProtectionPlanInput{TenantID: actor.TenantID, SourceClusterID: cluster.ID, AppID: point.AppID, StorageRepoID: storage.ID, PolicyID: policy.ID, Status: "active"}
	if _, err := repo.UpdateProtectionPlan(point.ProtectionPlanID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{ProtectionPlanID: point.ProtectionPlanID, Enabled: true, NextFireAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	input.PolicyID = ""
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
	req := tenantRequest(httptest.NewRequest("PUT", "/api/v1/protection-plans/"+point.ProtectionPlanID, strings.NewReader(string(raw))), actor)
	req.SetPathValue("id", point.ProtectionPlanID)
	response := httptest.NewRecorder()
	router.tenantGuard("plan", router.createProtectionPlan)(response, req)
	if response.Code != 200 {
		t.Fatalf("clear policy: %d %s", response.Code, response.Body.String())
	}
	plan, found, err := repo.GetProtectionPlan(point.ProtectionPlanID)
	if err != nil || !found || plan.PolicyID != "" {
		t.Fatalf("policy not cleared: %+v %v", plan, err)
	}
	schedule, found, err := repo.GetProtectionPlanSchedule(plan.ID)
	if err != nil || !found || schedule.Enabled {
		t.Fatalf("previous schedule remains active: %+v %v", schedule, err)
	}
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, AppID: point.AppID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRestorePoint(store.RestorePointInput{SourceClusterID: cluster.ID, ProtectionPlanID: plan.ID, AppID: point.AppID, BackupTaskID: backup.ID, VeleroBackupName: "second-manual-point", SourceNamespace: "demo", Status: "available"}); err != nil {
		t.Fatalf("old retention quota still applies: %v", err)
	}
	router.reconcileRetention(plan.ID, "")
	preserved, found, err := repo.GetRestorePoint(point.ID)
	if err != nil || !found || preserved.Status != "available" {
		t.Fatalf("historical point changed: %+v %v", preserved, err)
	}
	points, err := repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 2 {
		t.Fatalf("manual points not preserved: %+v %v", points, err)
	}
	tasks, err := repo.ListTasks(cluster.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Type == "retention-cleanup" {
			t.Fatalf("unexpected cleanup after removing policy: %+v", task)
		}
	}
}
