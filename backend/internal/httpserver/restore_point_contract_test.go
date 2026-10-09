package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestRestorePointContractsCoverMountedRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		_, path, _ := strings.Cut(route.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/restore-points") {
			continue
		}
		count++
		c, _, ok := restorePointPayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatalf("missing contract: %s", route.Pattern)
		}
		op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
		applyRestorePointPayloadContract(route.Pattern, op)
		if _, found := op["responses"].(map[string]any)["2XX"]; found {
			t.Fatal("placeholder remains")
		}
		if c.Request != nil {
			if _, exists := wireSchema(c.Request)["properties"].(map[string]any)["tenantId"]; exists {
				t.Fatal("client tenant exposed")
			}
		}
	}
	if count != 3 {
		t.Fatalf("route coverage %d", count)
	}
}

func restorePointContractRun(t *testing.T, r *Router, actor store.User, pattern, path, body, id string, handler http.HandlerFunc, status int) map[string]any {
	t.Helper()
	method, _, _ := strings.Cut(pattern, " ")
	req := tenantRequest(httptest.NewRequest(method, path, strings.NewReader(body)), actor)
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
	applyRestorePointPayloadContract(pattern, op)
	if status >= 400 {
		schema := map[string]any{"type": "object", "required": []string{"error"}}
		validateWireObject(t, value, schema)
	} else {
		response := op["responses"].(map[string]any)[strconv.Itoa(status)].(map[string]any)
		validateWireObject(t, value, response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any))
	}
	return value
}
func seedContractRestorePoint(t *testing.T, repo *store.PostgresStore, cluster store.Cluster, name string) store.RestorePoint {
	t.Helper()
	app := seedSchedulerApplication(t, repo, cluster.ID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: cluster.TenantID, SourceClusterID: cluster.ID, AppID: app.ID, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, AppID: app.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	point, err := repo.CreateRestorePoint(store.RestorePointInput{SourceClusterID: cluster.ID, ProtectionPlanID: plan.ID, AppID: app.ID, BackupTaskID: backup.ID, VeleroBackupName: name, Status: "available", SourceNamespace: "demo", Metadata: map[string]any{"contentIndex": restorePointIndex{Status: "ready", SchemaVersion: restorePointContentIndexSchemaVersion, IndexedAt: time.Now().UTC(), Resources: []protocol.BackupResourceSummary{{Kind: "ConfigMap", Name: "demo-config", Namespace: "demo"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return point
}
func TestRestorePointReadAndDeleteActualResponsesAndTenantBatchIsolation(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "restore-contract")
	otherTenant, err := repo.CreateTenant(store.TenantInput{Name: "other-restore", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreignCluster := testTenantCluster(t, repo, otherTenant.ID, "foreign-restore")
	own := seedContractRestorePoint(t, repo, cluster, "own-backup")
	foreign := seedContractRestorePoint(t, repo, foreignCluster, "foreign-backup")
	r := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	run := func(pattern, path, body, id string, handler http.HandlerFunc, status int) map[string]any {
		return restorePointContractRun(t, r, actor, pattern, path, body, id, handler, status)
	}
	for _, query := range []string{"", "?view=summary&page=1&pageSize=1", "?clusterId=" + cluster.ID} {
		result := run("GET /api/v1/restore-points", "/api/v1/restore-points"+query, "", "", r.listRestorePoints, 200)
		items := result["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != own.ID {
			t.Fatalf("foreign collection entry: %#v", items)
		}
	}
	empty := run("GET /api/v1/restore-points", "/api/v1/restore-points?clusterId="+foreignCluster.ID, "", "", r.listRestorePoints, 200)
	if len(empty["items"].([]any)) != 0 {
		t.Fatal("foreign query leaked points")
	}
	content := run("GET /api/v1/restore-points/{id}/contents", "/api/v1/restore-points/"+own.ID+"/contents", "", own.ID, r.getRestorePointContents, 200)
	if content["source"] != "index" || len(content["resources"].([]any)) != 1 {
		t.Fatal("durable catalog not returned")
	}
	run("GET /api/v1/restore-points/{id}/contents", "/api/v1/restore-points/"+foreign.ID+"/contents", "", foreign.ID, r.getRestorePointContents, 404)
	// Validate the entire batch before creating tasks or marking earlier points deleting.
	for _, invalid := range []string{foreign.ID, store.NewPublicID()} {
		run("POST /api/v1/restore-points/delete", "/api/v1/restore-points/delete", `{"restorePointIds":["`+own.ID+`","`+invalid+`"]}`, "", r.deleteRestorePoints, 404)
		tasks, err := repo.ListTasksFiltered(store.TaskFilter{ClusterID: cluster.ID, Types: []string{"retention-cleanup"}})
		if err != nil || len(tasks) != 0 {
			t.Fatalf("partial delete task: %#v %v", tasks, err)
		}
		point, found, err := repo.GetRestorePoint(own.ID)
		if err != nil || !found || point.Metadata["retentionState"] != nil {
			t.Fatal("partial retention mutation")
		}
	}
	run("POST /api/v1/restore-points/delete", "/api/v1/restore-points/delete", `{}`, "", r.deleteRestorePoints, 400)
	result := run("POST /api/v1/restore-points/delete", "/api/v1/restore-points/delete", `{"restorePointIds":["`+own.ID+`","`+own.ID+`"],"restorePointId":"`+own.ID+`"}`, "", r.deleteRestorePoints, 202)
	if len(result["tasks"].([]any)) != 1 || result["task"] == nil || result["warning"] == nil {
		t.Fatal("queued single-task shape changed")
	}
	taskID := result["task"].(map[string]any)["id"].(string)
	task, found, err := repo.GetTask(taskID)
	if err != nil || !found || task.TenantID != actor.TenantID || task.Status != "queued" {
		t.Fatalf("delete ownership/state: %#v %v", task, err)
	}
	point, _, err := repo.GetRestorePoint(own.ID)
	if err != nil || point.Metadata["retentionState"] != "deleting" {
		t.Fatal("deletion metadata missing")
	}
	run("POST /api/v1/restore-points/delete", "/api/v1/restore-points/delete", `{"restorePointId":"`+own.ID+`"}`, "", r.deleteRestorePoints, 409)
}

func TestRestorePointMultiClusterDeleteResponseMatchesContract(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	firstCluster := testTenantCluster(t, repo, actor.TenantID, "multi-delete-one")
	secondCluster := testTenantCluster(t, repo, actor.TenantID, "multi-delete-two")
	first := seedContractRestorePoint(t, repo, firstCluster, "first-backup")
	second := seedContractRestorePoint(t, repo, secondCluster, "second-backup")
	r := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	result := restorePointContractRun(t, r, actor, "POST /api/v1/restore-points/delete", "/api/v1/restore-points/delete", `{"restorePointIds":["`+first.ID+`","`+second.ID+`"]}`, "", r.deleteRestorePoints, 202)
	if len(result["tasks"].([]any)) != 2 || result["task"] != nil {
		t.Fatal("multi-cluster response must contain tasks without single-task alias")
	}
	for _, value := range result["tasks"].([]any) {
		task, found, err := repo.GetTask(value.(map[string]any)["id"].(string))
		if err != nil || !found || task.TenantID != actor.TenantID || task.Status != "queued" {
			t.Fatalf("wrong queued task: %#v %v", task, err)
		}
	}
}
