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
			"namespace":        "demo",
			"stage":            "restoring",
			"technicalDetails": map[string]any{"large": true},
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
