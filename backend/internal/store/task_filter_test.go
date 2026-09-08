package store

import "testing"

func TestMemoryStoreListTasksFilteredScopesBeforeLimit(t *testing.T) {
	repo := NewMemoryStore()
	repo.mu.Lock()
	repo.tasks["other-tenant"] = Task{ID: "other-tenant", TenantID: "tenant-b", Type: "drill", Status: "failed"}
	repo.tasks["wrong-type"] = Task{ID: "wrong-type", TenantID: "tenant-a", Type: "backup", Status: "failed"}
	repo.tasks["wrong-status"] = Task{ID: "wrong-status", TenantID: "tenant-a", Type: "drill", Status: "succeeded"}
	repo.tasks["match"] = Task{ID: "match", TenantID: "tenant-a", Type: "drill", Status: "failed"}
	repo.mu.Unlock()

	items, err := repo.ListTasksFiltered(TaskFilter{
		TenantID: "tenant-a",
		Types:    []string{"drill"},
		Statuses: []string{"failed"},
		Limit:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "match" {
		t.Fatalf("items=%v, want only matching tenant/type/status task", items)
	}
}

func TestMemoryStoreTaskSummaryKeepsListFieldsAndGetTaskKeepsFullPayload(t *testing.T) {
	repo := NewMemoryStore()
	repo.mu.Lock()
	repo.tasks["task-1"] = Task{
		ID:       "task-1",
		TenantID: "tenant-a",
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
	}
	repo.mu.Unlock()

	items, err := repo.ListTasksFiltered(TaskFilter{TenantID: "tenant-a", Summary: true})
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

	item, ok, err := repo.GetTask("task-1")
	if err != nil || !ok {
		t.Fatalf("GetTask: ok=%v err=%v", ok, err)
	}
	if _, ok := item.Payload["technicalDetails"]; !ok {
		t.Fatalf("detail payload=%v, want full technical details", item.Payload)
	}
}
