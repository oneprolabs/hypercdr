package store

import "testing"

func TestCreateClusterlessTaskUsesExplicitTenant(t *testing.T) {
	repo := NewMemoryStore()
	task, err := repo.CreateTask(TaskInput{TenantID: "tenant-registration", Type: "cluster-registration", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if task.TenantID != "tenant-registration" {
		t.Fatalf("expected explicit tenant, got %q", task.TenantID)
	}
	items, err := repo.ListTasksFiltered(TaskFilter{TenantID: "tenant-registration", Types: []string{"cluster-registration"}})
	if err != nil || len(items) != 1 || items[0].ID != task.ID {
		t.Fatalf("tenant-filtered registration task missing: %#v err=%v", items, err)
	}
}

func TestClaimQueuedTaskClaimsExactlyOnce(t *testing.T) {
	repo := NewMemoryStore()
	created, err := repo.CreateTask(TaskInput{TenantID: "tenant-registration", Type: "cluster-registration", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := repo.ClaimQueuedTask("cluster-registration", "executor-a")
	if err != nil || !ok || claimed.ID != created.ID || claimed.Status != "running" {
		t.Fatalf("unexpected claim: %#v ok=%v err=%v", claimed, ok, err)
	}
	if claimed.Payload["executorId"] != "executor-a" {
		t.Fatalf("executor ownership missing: %#v", claimed.Payload)
	}
	if _, ok, err = repo.ClaimQueuedTask("cluster-registration", "executor-b"); err != nil || ok {
		t.Fatalf("task was claimed twice: ok=%v err=%v", ok, err)
	}
}

func TestClaimQueuedTaskByIDCannotClaimSibling(t *testing.T) {
	repo := NewMemoryStore()
	first, _ := repo.CreateTask(TaskInput{TenantID: "tenant-a", Type: "cluster-registration", Status: "queued"})
	second, _ := repo.CreateTask(TaskInput{TenantID: "tenant-a", Type: "cluster-registration", Status: "queued"})
	claimed, ok, err := repo.ClaimQueuedTaskByID(second.ID, "cluster-registration", "job-second")
	if err != nil || !ok || claimed.ID != second.ID {
		t.Fatalf("wrong task claimed: %#v ok=%v err=%v", claimed, ok, err)
	}
	unchanged, _, _ := repo.GetTask(first.ID)
	if unchanged.Status != "queued" {
		t.Fatalf("sibling task changed: %#v", unchanged)
	}
}
