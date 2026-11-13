package httpserver

import (
	"errors"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestMissingSourceBlocksNewBackupNotRecoveryAssets(t *testing.T) {
	repo := newTestStore(t)
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "missing-source")
	if _, _, err := repo.ApplyInventory(store.InventoryInput{ClusterID: cluster.ID, Apps: []store.Application{{Namespace: "missing-source", Status: "Active"}}}); err != nil {
		t.Fatal(err)
	}
	apps, err := repo.ListApplications(cluster.ID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("applications: %v %v", apps, err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: cluster.ID, AppID: apps[0].ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyInventory(store.InventoryInput{ClusterID: cluster.ID}); err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo}
	_, err = router.createPendingBackupTask(backupTaskRequest{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, SourceNamespace: apps[0].Namespace}, apps[0].ID)
	if !errors.Is(err, errSourceNamespaceMissing) {
		t.Fatalf("missing source backup: %v", err)
	}
	if _, ok, err := repo.GetProtectionPlan(plan.ID); err != nil || !ok {
		t.Fatalf("plan lost: %v", err)
	}
	tasks, err := repo.ListTasks(cluster.ID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("unexpected task created: %+v %v", tasks, err)
	}
}
