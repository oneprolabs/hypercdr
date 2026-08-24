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
