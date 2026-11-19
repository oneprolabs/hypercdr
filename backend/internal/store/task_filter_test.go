package store

import "testing"

func TestPlanTaskBatchKeepsLatestPointersAndAllActiveTasks(t *testing.T) {
	repo := newTestStore(t)
	planIDs := []string{}
	want := map[string]bool{}
	activeIDs := []string{}
	for planIndex := 0; planIndex < 3; planIndex++ {
		cluster, app := seedPlanApplication(t, repo)
		plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, Status: "active"})
		if err != nil {
			t.Fatal(err)
		}
		if planIndex < 2 {
			planIDs = append(planIDs, plan.ID)
		}
		for _, taskType := range []string{"backup", "drill"} {
			for taskIndex := 0; taskIndex < 8; taskIndex++ {
				task, err := repo.CreateTask(TaskInput{TenantID: DefaultTenantID, ProtectionPlanID: plan.ID, Type: taskType, Status: "succeeded", SuppressLatestPointer: taskIndex == 7})
				if err != nil {
					t.Fatal(err)
				}
				if planIndex < 2 && (taskIndex < 2 || taskIndex >= 6) {
					want[task.ID] = true
				}
				if taskIndex < 2 {
					activeIDs = append(activeIDs, task.ID)
				}
			}
		}
	}
	for _, taskID := range activeIDs {
		if _, err := repo.db.Exec(`update tasks set status='running' where id=$1`, taskID); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.ListTasksFiltered(TaskFilter{TenantID: DefaultTenantID, ProtectionPlanIDs: planIDs, LatestPerPlanType: true, Summary: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(want) {
		t.Fatalf("got %d tasks, want %d", len(items), len(want))
	}
	for _, item := range items {
		if !want[item.ID] {
			t.Fatalf("unexpected historical or unrelated task %s", item.ID)
		}
	}
	items, err = repo.ListTasksFiltered(TaskFilter{TenantID: NewPublicID(), ProtectionPlanIDs: planIDs, LatestPerPlanType: true, Summary: true})
	if err != nil || len(items) != 0 {
		t.Fatalf("foreign tenant sees %d tasks: %v", len(items), err)
	}
}

func TestPostgresStoreListTasksFilteredScopesBeforeLimit(t *testing.T) {
	repo := newTestStore(t)
	tenantA, err := repo.CreateTenant(TenantInput{Name: "A", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := repo.CreateTenant(TenantInput{Name: "B", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []TaskInput{
		{TenantID: tenantB.ID, Type: "drill", Status: "failed"},
		{TenantID: tenantA.ID, Type: "backup", Status: "failed"},
		{TenantID: tenantA.ID, Type: "drill", Status: "succeeded"},
	} {
		if _, err := repo.CreateTask(input); err != nil {
			t.Fatal(err)
		}
	}
	match, err := repo.CreateTask(TaskInput{TenantID: tenantA.ID, Type: "drill", Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListTasksFiltered(TaskFilter{
		TenantID: tenantA.ID,
		Types:    []string{"drill"},
		Statuses: []string{"failed"},
		Limit:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != match.ID {
		t.Fatalf("items=%v, want only matching tenant/type/status task", items)
	}
}

func TestPostgresStoreTaskSummaryKeepsListFieldsAndGetTaskKeepsFullPayload(t *testing.T) {
	repo := newTestStore(t)
	task, err := repo.CreateTask(TaskInput{
		TenantID: DefaultTenantID,
		Type:     "drill",
		Payload: map[string]any{
			"namespace":         "demo",
			"sourceNamespaces":  []string{"demo", "database"},
			"targetNamespace":   "demo-drill",
			"targetNamespaces":  map[string]any{"demo": "demo-drill"},
			"targetClusterName": "cluster-b",
			"stage":             "restoring",
			"recoveryStages":    []any{map[string]any{"id": "waiting_for_workloads", "status": "running"}},
			"technicalDetails":  map[string]any{"large": true},
			"sizeMetricsV2": map[string]any{
				"logical": map[string]any{"known": true, "totalBytes": int64(1000)},
				"newData": map[string]any{"known": true, "totalBytes": int64(250)},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListTasksFiltered(TaskFilter{TenantID: DefaultTenantID, Summary: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Payload["namespace"] != "demo" || items[0].Payload["stage"] != "restoring" {
		t.Fatalf("summary payload=%v, want list fields", items)
	}
	if items[0].Payload["targetNamespace"] != "demo-drill" || items[0].Payload["targetClusterName"] != "cluster-b" {
		t.Fatalf("summary payload=%v, drill cleanup boundary fields must be retained", items[0].Payload)
	}
	if targets, ok := items[0].Payload["targetNamespaces"].(map[string]any); !ok || targets["demo"] != "demo-drill" {
		t.Fatalf("summary payload=%v, target namespace mapping must be retained", items[0].Payload)
	}
	if sources, ok := items[0].Payload["sourceNamespaces"].([]any); !ok || len(sources) != 2 || sources[1] != "database" {
		t.Fatalf("summary payload=%v, selected source namespaces must be retained", items[0].Payload)
	}
	if _, ok := items[0].Payload["recoveryStages"]; !ok {
		t.Fatalf("summary payload=%v, authoritative recovery stage snapshot must be retained", items[0].Payload)
	}
	if _, ok := items[0].Payload["technicalDetails"]; ok {
		t.Fatalf("summary payload=%v, technical details must be omitted", items[0].Payload)
	}
	metrics, ok := items[0].Payload["sizeMetricsV2"].(map[string]any)
	if !ok || metrics["logical"] == nil || metrics["newData"] == nil {
		t.Fatalf("summary payload=%v, data reuse metrics must be retained", items[0].Payload)
	}

	item, ok, err := repo.GetTask(task.ID)
	if err != nil || !ok {
		t.Fatalf("GetTask: ok=%v err=%v", ok, err)
	}
	if _, ok := item.Payload["technicalDetails"]; !ok {
		t.Fatalf("detail payload=%v, want full technical details", item.Payload)
	}
}
