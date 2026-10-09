package store

import "testing"

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
			"targetNamespace":   "demo-drill",
			"targetNamespaces":  map[string]any{"demo": "demo-drill"},
			"targetClusterName": "cluster-b",
			"stage":             "restoring",
			"recoveryStages":    []any{map[string]any{"id": "waiting_for_workloads", "status": "running"}},
			"technicalDetails":  map[string]any{"large": true},
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
	if _, ok := items[0].Payload["recoveryStages"]; !ok {
		t.Fatalf("summary payload=%v, authoritative recovery stage snapshot must be retained", items[0].Payload)
	}
	if _, ok := items[0].Payload["technicalDetails"]; ok {
		t.Fatalf("summary payload=%v, technical details must be omitted", items[0].Payload)
	}

	item, ok, err := repo.GetTask(task.ID)
	if err != nil || !ok {
		t.Fatalf("GetTask: ok=%v err=%v", ok, err)
	}
	if _, ok := item.Payload["technicalDetails"]; !ok {
		t.Fatalf("detail payload=%v, want full technical details", item.Payload)
	}
}
