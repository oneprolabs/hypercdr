package httpserver

import (
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestCapacityScheduleUsesUTCAndBoundedStagger(t *testing.T) {
	utc := time.Date(2026, 11, 6, 0, 5, 0, 0, time.UTC)
	for _, cluster := range []string{"one", "two", "three"} {
		if !capacityDue(utc, cluster) || !capacityDue(utc.In(time.FixedZone("other", 8*3600)), cluster) {
			t.Fatal("daily schedule depends on display timezone")
		}
		if capacityDue(utc.Add(-5*time.Minute), cluster) {
			t.Fatal("expected cluster staggering")
		}
	}
}

func TestInitialCapacityOnlyAfterSuccessfulBackupAndOnlyOnce(t *testing.T) {
	for _, test := range []struct {
		name         string
		size         map[string]any
		backupStatus string
		wantTask     bool
	}{
		{name: "no backup"},
		{name: "failed backup", backupStatus: "failed"},
		{name: "missing capacity", backupStatus: "succeeded", wantTask: true},
		{name: "previous empty repository", size: map[string]any{"totalBytes": 0, "collectedAt": "2026-11-06T00:00:00Z"}, backupStatus: "succeeded", wantTask: true},
		{name: "existing valid capacity", size: map[string]any{"totalBytes": 100, "collectedAt": "2026-11-06T00:00:00Z"}, backupStatus: "succeeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newTestStore(t)
			clusterID := seedSchedulerCluster(t, repo)
			app := seedSchedulerApplication(t, repo, clusterID, "initial-capacity")
			repository, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: store.DefaultTenantID, Name: "capacity", Type: "S3", Endpoint: "localhost:9000", Bucket: "test"})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, StorageRepoID: repository.ID, Status: "active"})
			if err != nil {
				t.Fatal(err)
			}
			plan.PlanStorageSize = test.size
			var backup store.Task
			if test.backupStatus != "" {
				status := test.backupStatus
				if test.name == "missing capacity" {
					status = "running"
				}
				backup, err = repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "backup", Status: status})
				if err != nil {
					t.Fatal(err)
				}
			}
			router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), workersClosing: true, contentIndexing: map[string]struct{}{}}
			connection := &websocket.Conn{}
			router.hub.set(clusterID, connection)
			router.hub.setCapacitySupport(clusterID, connection, true)
			if test.name == "missing capacity" {
				result := protocol.TaskCompletedPayload{TaskID: backup.ID, Status: "succeeded", Velero: map[string]any{"kind": "Backup", "name": "first-backup"}, SizeMetricsV2: &protocol.SizeMetricsV2{SchemaVersion: 2, Logical: protocol.SizeValueV2{Known: true, TotalBytes: 1024}, NewData: protocol.SizeValueV2{Known: true, TotalBytes: 123}}}
				if err := router.acceptBackupCompletion(backup, result); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := router.collectInitialPlanCapacity(plan, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			tasks, err := repo.ListTasksFiltered(store.TaskFilter{ClusterID: clusterID, Types: []string{"plan-capacity"}, CapacityTrigger: "first-backup", CapacityPlanID: plan.ID, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if (len(tasks) == 1) != test.wantTask {
				t.Fatalf("initial tasks: %v", tasks)
			}
			if !test.wantTask {
				return
			}
			if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: tasks[0].ID, Status: "failed", MarkDone: true}); err != nil {
				t.Fatal(err)
			}
			if err := router.collectInitialPlanCapacity(plan, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			all, err := repo.ListTasksFiltered(store.TaskFilter{ClusterID: clusterID, Types: []string{"plan-capacity"}})
			if err != nil || len(all) != 1 {
				t.Fatalf("repeated automatic scan: %v %v", all, err)
			}
		})
	}
}

func TestCapacityDailyBatchManualReuseAndNextDay(t *testing.T) {
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	repository, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: store.DefaultTenantID, Name: "capacity", Type: "S3", Endpoint: "localhost:9000", Bucket: "test"})
	if err != nil {
		t.Fatal(err)
	}
	plans := []store.ProtectionPlan{}
	if _, _, err := repo.ApplyInventory(store.InventoryInput{ClusterID: clusterID, Apps: []store.Application{{Namespace: "first", Name: "first"}, {Namespace: "second", Name: "second"}}}); err != nil {
		t.Fatal(err)
	}
	apps, err := repo.ListApplications(clusterID)
	if err != nil || len(apps) != 2 {
		t.Fatalf("applications: %v %v", apps, err)
	}
	for _, app := range apps {
		plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, StorageRepoID: repository.ID, Status: "active"})
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), workersClosing: true}
	now := time.Now().UTC()
	connection := &websocket.Conn{}
	router.hub.set(clusterID, connection)
	if _, _, err := router.createCapacityTask(clusterID, plans, now, true); err == nil {
		t.Fatal("old agent accepted new command")
	}
	router.hub.setCapacitySupport(clusterID, connection, true)
	first, reused, err := router.createCapacityTask(clusterID, plans, now, true)
	if err != nil || reused {
		t.Fatalf("create batch: %+v %v", first, err)
	}
	command, err := capacityCommand(first.Payload)
	if err != nil || len(command.Plans) != 2 {
		t.Fatalf("not a cluster batch: %+v %v", command, err)
	}
	manual, reused, err := router.createCapacityTask(clusterID, plans[:1], now, false)
	if err != nil || !reused || manual.ID != first.ID {
		t.Fatal("manual refresh repeated active collection")
	}
	if _, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plans[0].ID, Type: "backup", Status: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	router.collectInitialPlanCapacityAfterBackup(plans[0].ID)
	initial, err := repo.ListTasksFiltered(store.TaskFilter{ClusterID: clusterID, Types: []string{"plan-capacity"}, CapacityTrigger: "first-backup"})
	if err != nil || len(initial) != 0 {
		t.Fatalf("initial collection did not reuse daily batch: %v %v", initial, err)
	}
	collected := time.Now().UTC()
	report := &protocol.PlanCapacityReport{Plans: []protocol.PlanCapacityResult{{PlanID: plans[0].ID, Status: "succeeded", MetadataBytes: 10, KopiaBytes: 100, TotalBytes: 110, CollectedAt: collected}, {PlanID: plans[1].ID, Status: "failed", ErrorMessage: "Unavailable", CollectedAt: collected}}}
	if err := router.persistCapacityReport(first, report); err != nil {
		t.Fatal(err)
	}
	got, _, _ := repo.GetProtectionPlan(plans[0].ID)
	if got.PlanStorageSize["totalBytes"] != float64(110) {
		t.Fatalf("result not saved %+v", got.PlanStorageSize)
	}
	failed, _, _ := repo.GetProtectionPlan(plans[1].ID)
	if failed.PlanStorageSize["status"] != "failed" {
		t.Fatal("partial failure missing")
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: first.ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	same, reused, err := router.createCapacityTask(clusterID, plans, now, true)
	if err != nil || !reused || same.ID != first.ID {
		t.Fatal("repeated daily run")
	}
	next, reused, err := router.createCapacityTask(clusterID, plans, now.Add(24*time.Hour), true)
	if err != nil || reused || next.ID == first.ID {
		t.Fatal("next daily run not created")
	}
}

func TestCapacityNoPlanOrUnsupportedAgentCreatesNoTask(t *testing.T) {
	repo := newTestStore(t)
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	router.schedulePlanCapacity(time.Now().UTC())
	if tasks, err := repo.ListTasksFiltered(store.TaskFilter{Types: []string{"plan-capacity"}}); err != nil || len(tasks) != 0 {
		t.Fatalf("unexpected tasks %v %v", tasks, err)
	}
	if _, _, err := router.createCapacityTask("unknown", nil, time.Now(), true); err == nil {
		t.Fatal("empty task accepted")
	}
}

func TestCapacityAPIHidesForeignPlan(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	for _, method := range []string{"GET", "POST"} {
		req := tenantRequest(httptest.NewRequest(method, "/api/v1/protection-plans/"+store.NewPublicID()+"/capacity", nil), actor)
		req.SetPathValue("id", store.NewPublicID())
		response := httptest.NewRecorder()
		if method == "GET" {
			router.getPlanCapacity(response, req)
		} else {
			router.refreshPlanCapacity(response, req)
		}
		if response.Code != 404 {
			t.Fatalf("unexpected response %d", response.Code)
		}
	}
}

func TestCapacityReportRejectsUnexpectedOrDuplicatePlan(t *testing.T) {
	router := &Router{}
	task := store.Task{Payload: map[string]any{"planCapacity": protocol.PlanCapacityCommand{Namespace: "agent", Plans: []protocol.PlanCapacityTarget{{PlanID: "expected"}}}}}
	if err := router.persistCapacityReport(task, nil); err == nil {
		t.Fatal("nil result accepted")
	}
	if err := router.persistCapacityReport(task, &protocol.PlanCapacityReport{Plans: []protocol.PlanCapacityResult{{PlanID: "foreign"}}}); err == nil {
		t.Fatal("foreign plan accepted")
	}
}
