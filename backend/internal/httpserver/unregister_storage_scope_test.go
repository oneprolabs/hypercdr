package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestUnregisterStorageScopeExcludesHistoricalBindingsAndPoints(t *testing.T) {
	repo := newTestStore(t)
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "current-storage-only")
	current, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: cluster.TenantID, Name: "current", Type: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: cluster.TenantID, Name: "historical", Type: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	appID := testApplicationID(t, repo, cluster.ID)
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: cluster.TenantID, SourceClusterID: cluster.ID, AppID: appID, StorageRepoID: current.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{ClusterID: cluster.ID, SourceClusterID: cluster.ID, StorageRepoID: old.ID, BSLName: "old-binding", ObjectPrefix: storageDomainPrefix(cluster.TenantID, cluster.ID), Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, AppID: appID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRestorePoint(store.RestorePointInput{SourceClusterID: cluster.ID, AppID: appID, ProtectionPlanID: plan.ID, StorageRepoID: old.ID, BackupTaskID: backup.ID, VeleroBackupName: "historical-backup", Status: "available"}); err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	audit, err := router.auditClusterUnregister(cluster.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit.StorageRepositoryIDs) != 1 || audit.StorageRepositoryIDs[0] != current.ID || audit.RestorePointCount != 1 {
		t.Fatalf("unexpected scope: %#v", audit)
	}
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	calls := 0
	cleanObjectStoragePrefix = func(_ context.Context, repository store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		calls++
		if repository.ID != current.ID || prefix != storageDomainPrefix(cluster.TenantID, cluster.ID)+"/" {
			t.Fatalf("unsafe cleanup: %s %s", repository.ID, prefix)
		}
		return objectStorageCleanupResult{}, errors.New("stop before uninstall")
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "unregister", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	router.cleanupAndDispatchUnregister(task, []string{old.ID, current.ID}, false)
	if calls != 1 {
		t.Fatalf("expected only current repository access, got %d", calls)
	}
}

func TestUnregisterMissingBucketIsIdempotentButAccessDeniedFails(t *testing.T) {
	for _, code := range []string{"NoSuchBucket", "AccessDenied"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet {
					t.Errorf("unexpected destructive request: %s", request.Method)
				}
				response.Header().Set("Content-Type", "application/xml")
				if code == "NoSuchBucket" {
					response.WriteHeader(404)
				} else {
					response.WriteHeader(403)
				}
				fmt.Fprintf(response, "<Error><Code>%s</Code><Message>test failure</Message></Error>", code)
			}))
			defer server.Close()
			repository := store.StorageRepository{Endpoint: server.URL, Bucket: "test", Region: "us-east-1", Config: map[string]any{"urlStyle": "path"}, Secret: map[string]string{"accessKey": "test-access", "secretKey": "test-secret"}}
			_, err := deleteObjectStoragePrefix(context.Background(), repository, storageDomainPrefix(store.DefaultTenantID, store.NewPublicID())+"/")
			if (err == nil) != (code == "NoSuchBucket") {
				t.Fatalf("unexpected result for %s: %v", code, err)
			}
		})
	}
}
