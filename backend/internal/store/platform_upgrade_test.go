package store

import "testing"

func platformReleaseInput(version string) PlatformReleaseInput {
	return PlatformReleaseInput{
		Version:               version,
		APIImage:              "registry/platform-api:" + version,
		APIImageDigest:        "sha256:api-" + version,
		FrontendImage:         "registry/platform-frontend:" + version,
		FrontendImageDigest:   "sha256:frontend-" + version,
		DatabaseSchemaVersion: "000009",
		RollbackSupported:     true,
	}
}

func TestPlatformReleaseActivationKeepsOneActiveRelease(t *testing.T) {
	repo := NewMemoryStore()
	firstInput := platformReleaseInput("v1")
	firstInput.Status = "active"
	first, err := repo.UpsertPlatformRelease(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.UpsertPlatformRelease(platformReleaseInput("v2"))
	if err != nil {
		t.Fatal(err)
	}
	activated, ok, err := repo.ActivatePlatformRelease(second.ID, "admin")
	if err != nil || !ok {
		t.Fatalf("activate platform release: ok=%v err=%v", ok, err)
	}
	if activated.Status != "active" || activated.PublishedBy != "admin" {
		t.Fatalf("unexpected activated release: %#v", activated)
	}
	items, err := repo.ListPlatformReleases()
	if err != nil {
		t.Fatal(err)
	}
	activeCount := 0
	for _, item := range items {
		if item.Status == "active" {
			activeCount++
		}
		if item.ID == first.ID && item.Status != "retired" {
			t.Fatalf("previous release was not retired: %#v", item)
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected one active platform release, got %d", activeCount)
	}
}

func TestPlatformUpgradeAllowsOnlyOneActiveJob(t *testing.T) {
	repo := NewMemoryStore()
	release, err := repo.UpsertPlatformRelease(platformReleaseInput("v2"))
	if err != nil {
		t.Fatal(err)
	}
	input := PlatformUpgradeJobInput{Release: release, FromVersion: "v1", RequestedBy: "admin"}
	first, err := repo.CreatePlatformUpgradeJob(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreatePlatformUpgradeJob(input); err == nil {
		t.Fatal("expected a second active platform upgrade job to be rejected")
	}
	if _, ok, err := repo.UpdatePlatformUpgradeJob(PlatformUpgradeJobUpdate{ID: first.ID, Status: "succeeded", Step: "completed", Progress: 100, MarkDone: true}); err != nil || !ok {
		t.Fatalf("complete first upgrade: ok=%v err=%v", ok, err)
	}
	if _, err = repo.CreatePlatformUpgradeJob(input); err != nil {
		t.Fatalf("create upgrade after terminal job: %v", err)
	}
}
