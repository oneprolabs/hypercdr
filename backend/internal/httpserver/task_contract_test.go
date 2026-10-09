package httpserver

import (
	"context"
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTaskContractsCoverAllMountedTaskRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		_, path, _ := strings.Cut(route.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/tasks") && !strings.HasSuffix(path, "/latest-sync") && !strings.HasSuffix(path, "/latest-recovery") {
			continue
		}
		count++
		c, _, ok := taskPayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatalf("missing task contract %s", route.Pattern)
		}
		op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
		applyTaskPayloadContract(route.Pattern, op)
		if _, found := op["responses"].(map[string]any)["2XX"]; found {
			t.Fatal("placeholder remains")
		}
		if c.Request != nil {
			if _, found := wireSchema(c.Request)["properties"].(map[string]any)["tenantId"]; found {
				t.Fatal("client tenant exposed")
			}
		}
	}
	if count != 12 {
		t.Fatalf("task route coverage %d", count)
	}
}

func taskContractRun(t *testing.T, r *Router, actor store.User, pattern, body, id string, handler http.HandlerFunc, status int) map[string]any {
	t.Helper()
	method, path, _ := strings.Cut(pattern, " ")
	req := tenantRequest(httptest.NewRequest(method, strings.ReplaceAll(path, "{id}", id), strings.NewReader(body)), actor)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != status {
		t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
	}
	var value map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
	if !applyTaskPayloadContract(pattern, op) {
		t.Fatal("missing contract")
	}
	response, ok := op["responses"].(map[string]any)[strconv.Itoa(status)].(map[string]any)
	if !ok {
		t.Fatal("undocumented status", status)
	}
	validateWireObject(t, value, response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any))
	return value
}

func TestTaskActualReadCancelRetryCleanupMatchContracts(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	workerContext, stop := context.WithCancel(context.Background())
	r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workerContext: workerContext, stopWorkers: stop}
	h := &managedRouter{router: r}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	run := func(pattern, body, id string, handler http.HandlerFunc, status int) map[string]any {
		return taskContractRun(t, r, actor, pattern, body, id, handler, status)
	}
	empty := run("GET /api/v1/protection-plans/{id}/latest-sync", "", plan.ID, r.latestPlanTask("backup"), 200)
	if empty["task"] != nil {
		t.Fatal("new plan has unexpected task")
	}
	run("GET /api/v1/protection-plans/{id}/latest-recovery", "", plan.ID, r.latestPlanRecoveryTask, 200)
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, AppID: plan.AppID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running", CommandID: store.NewPublicID()})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AddTaskEvent(store.TaskEventInput{TaskID: backup.ID, Level: "info", Reason: "contract", Message: "Persisted event"}); err != nil {
		t.Fatal(err)
	}
	run("GET /api/v1/tasks", "", "", r.listTasks, 200)
	run("GET /api/v1/tasks/{id}", "", backup.ID, r.tenantGuard("task", r.getTask), 200)
	run("GET /api/v1/tasks/{id}/events", "", backup.ID, r.tenantGuard("task", r.listTaskEvents), 200)
	run("GET /api/v1/protection-plans/{id}/latest-sync", "", plan.ID, r.latestPlanTask("backup"), 200)
	run("POST /api/v1/tasks/{id}/cancel", "", backup.ID, r.tenantGuard("task", r.cancelTask), 202)
	run("POST /api/v1/tasks/{id}/cancel", "", backup.ID, r.tenantGuard("task", r.cancelTask), 200)
	drill, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, AppID: plan.AppID, ProtectionPlanID: plan.ID, Type: "drill", Status: "failed", CommandID: store.NewPublicID(), Payload: map[string]any{"sourceNamespace": "demo", "targetNamespace": "demo-drill"}})
	if err != nil {
		t.Fatal(err)
	}
	run("GET /api/v1/protection-plans/{id}/latest-recovery", "", plan.ID, r.latestPlanRecoveryTask, 200)
	run("POST /api/v1/tasks/{id}/cleanup-drill", "", drill.ID, r.tenantGuard("task", r.cleanupDrillTask), 202)
	run("POST /api/v1/tasks/{id}/cleanup-drill", "", drill.ID, r.tenantGuard("task", r.cleanupDrillTask), 200)
	run("POST /api/v1/tasks/{id}/retry", "", drill.ID, r.tenantGuard("task", r.retryRecoveryTask), 201)
}

func TestTaskForeignTenantActionsAreInvisibleAndDoNotMutate(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "task-foreign", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	plan := testTenantPlan(t, repo, tenant.ID)
	task, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, AppID: plan.AppID, Type: "backup", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub()}
	for _, handler := range []http.HandlerFunc{r.getTask, r.listTaskEvents, r.cancelTask, r.retryRecoveryTask, r.cleanupDrillTask} {
		req := tenantRequest(httptest.NewRequest("POST", "/api/v1/tasks/"+task.ID, nil), actor)
		req.SetPathValue("id", task.ID)
		w := httptest.NewRecorder()
		r.tenantGuard("task", handler)(w, req)
		if w.Code != 404 {
			t.Fatalf("foreign task visible: %d %s", w.Code, w.Body.String())
		}
	}
	persisted, _, err := repo.GetTask(task.ID)
	if err != nil || persisted.Status != "running" {
		t.Fatal("foreign task mutated", err)
	}
	tasks, err := repo.ListTasks(plan.SourceClusterID)
	if err != nil || len(tasks) != 1 {
		t.Fatal("foreign action created task", err)
	}
}

func TestBackupContractRejectsAmbiguousOrInvalidResponses(t *testing.T) {
	op := map[string]any{"parameters": []any{}, "responses": map[string]any{}}
	applyTaskPayloadContract("POST /api/v1/tasks/backup", op)
	schema := op["responses"].(map[string]any)["201"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	for _, value := range []map[string]any{{}, {"task": nil}, {"tasks": []any{}, "task": nil}, {"tasks": []any{}, "reused": "true"}, {"tasks": []any{}, "reused": float64(0)}} {
		if wireContractError(value, schema) == nil {
			t.Fatalf("invalid backup response accepted: %#v", value)
		}
	}
}

func TestTaskCreationActualResponsesMatchContracts(t *testing.T) {
	for _, kind := range []string{"backup-single", "backup-multiple", "restore", "drill", "takeover"} {
		t.Run(kind, func(t *testing.T) {
			repo := newTestStore(t)
			actor := testAdmin(t, repo)
			workerContext, stop := context.WithCancel(context.Background())
			r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workerContext: workerContext, stopWorkers: stop}
			h := &managedRouter{router: r}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := h.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			source := testTenantCluster(t, repo, store.DefaultTenantID, "contract-source")
			app := seedSchedulerApplication(t, repo, source.ID, "demo")
			appIDs := []string{app.ID}
			if kind == "backup-multiple" {
				second := seedSchedulerApplication(t, repo, source.ID, "demo-second")
				if _, _, err := repo.ApplyInventory(store.InventoryInput{ClusterID: source.ID, Apps: []store.Application{app, second}, CollectedAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
				appIDs = append(appIDs, second.ID)
			}
			// Legacy persisted plans without a repository remain supported by task
			// dispatch. These fixtures test API response shapes without external S3.
			plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: source.ID, AppID: app.ID, AppIDs: appIDs, Status: "active"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(kind, "backup-") {
				body := `{"clusterId":"` + source.ID + `","protectionPlanId":"` + plan.ID + `"`
				if kind == "backup-multiple" {
					body += `,"sourceNamespace":"demo"`
				}
				body += "}"
				created := taskContractRun(t, r, actor, "POST /api/v1/tasks/backup", body, "", r.createBackupTask, 201)
				reused := taskContractRun(t, r, actor, "POST /api/v1/tasks/backup", body, "", r.createBackupTask, 200)
				if kind == "backup-multiple" {
					if len(created["tasks"].([]any)) != 2 || reused["reused"] != float64(2) {
						t.Fatalf("multi-task shape %#v %#v", created, reused)
					}
				} else if reused["reused"] != true {
					t.Fatal("single reuse not boolean")
				}
				return
			}
			backup, err := repo.CreateTask(store.TaskInput{ClusterID: source.ID, AppID: app.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
			if err != nil {
				t.Fatal(err)
			}
			point, err := repo.CreateRestorePoint(store.RestorePointInput{ProtectionPlanID: plan.ID, BackupTaskID: backup.ID, SourceClusterID: source.ID, VeleroBackupName: "contract-backup", Status: "available", SourceNamespace: "demo"})
			if err != nil {
				t.Fatal(err)
			}
			body := `{"clusterId":"` + source.ID + `","restorePointId":"` + point.ID + `","targetNamespace":"demo-recovered"}`
			handlers := map[string]http.HandlerFunc{"restore": r.createRestoreTask, "drill": r.createDrillTask, "takeover": r.createTakeoverTask}
			value := taskContractRun(t, r, actor, "POST /api/v1/tasks/"+kind, body, "", handlers[kind], 201)
			if value["restorePointId"] != point.ID || value["protectionPlanId"] != plan.ID {
				t.Fatal("recovery references lost")
			}
		})
	}
}
