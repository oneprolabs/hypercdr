package httpserver

import (
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestStorageBindingActivationActionRedispatchesFailedBinding(t *testing.T) {
	repo := store.NewMemoryStore()
	clusterID := seedSchedulerCluster(t, repo)
	storageRepo, err := repo.CreateStorageRepository(store.StorageRepositoryInput{
		Name: "minio", Type: "s3", Endpoint: "http://minio:9000", Bucket: "bucket",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID: clusterID, StorageRepoID: storageRepo.ID, SourceClusterID: clusterID,
		BSLName:      storageDomainBSLName(storageRepo, clusterID),
		ObjectPrefix: storageDomainPrefix(storageRepo.TenantID, clusterID),
		Status:       "configuring", RepoUpdatedAt: storageRepo.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
		ClusterID: clusterID, StorageRepoID: storageRepo.ID, SourceClusterID: clusterID,
		Status: "failed", LastSyncedAt: time.Now().UTC(),
		LastErrorCode: "BSL_UNAVAILABLE", LastErrorMessage: "previous storage validation failed",
		RepoUpdatedAt: storageRepo.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	router := &Router{store: repo}
	for _, role := range []string{"source", "target"} {
		action, warning, err := router.storageBindingActivationAction(clusterID, storageRepo.ID, clusterID, role)
		if err != nil {
			t.Fatalf("role %s returned stale failure instead of retrying: %v", role, err)
		}
		if action != "dispatch" || warning != "" {
			t.Fatalf("role %s action=%q warning=%q, want dispatch with no stale warning", role, action, warning)
		}
	}
}
