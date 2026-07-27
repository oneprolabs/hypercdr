package store

import "testing"

func TestComponentReleaseActivationKeepsOneActiveTarget(t *testing.T) {
	repo := NewMemoryStore()
	first, err := repo.UpsertComponentRelease(ComponentReleaseInput{Component: "comm-agent", Version: "v1", Image: "registry/comm-agent:v1", ImageDigest: "sha256:first", Status: "active", PublishedBy: "system"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.UpsertComponentRelease(ComponentReleaseInput{Component: "comm-agent", Version: "v2", Image: "registry/comm-agent:v2", ImageDigest: "sha256:second", Status: "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	activated, ok, err := repo.ActivateComponentRelease(second.ID, "admin")
	if err != nil || !ok {
		t.Fatalf("activate release: ok=%v err=%v", ok, err)
	}
	if activated.Status != "active" || activated.PublishedBy != "admin" {
		t.Fatalf("unexpected activated release: %#v", activated)
	}
	active, ok, err := repo.GetActiveComponentRelease("comm-agent")
	if err != nil || !ok || active.ID != second.ID {
		t.Fatalf("unexpected active release: %#v ok=%v err=%v", active, ok, err)
	}
	items, err := repo.ListComponentReleases("comm-agent")
	if err != nil {
		t.Fatal(err)
	}
	activeCount := 0
	for _, item := range items {
		if item.Status == "active" {
			activeCount++
		}
		if item.ID == first.ID && item.Status != "retired" {
			t.Fatalf("previous target was not retired: %#v", item)
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected one active release, got %d", activeCount)
	}
}
