package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestApplyInventoryPreservesProtectionStatus is a regression test for the
// bug where agent inventory reports reset applications to
// protection_status='unprotected' after an operator had moved the namespace
// to stage 2 (pending_protection) or stage 3 (protected).
//
// The fix: ApplyInventory now uses INSERT ... ON CONFLICT (cluster_id, namespace)
// DO UPDATE SET ..., which only refreshes inventory-derived fields and does
// NOT touch protection_status or protection_score.
func TestApplyInventoryPreservesProtectionStatus(t *testing.T) {
	dsn := os.Getenv("HCDR_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://hypercdr:hypercdr@127.0.0.1:5432/hypercdr?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Skipf("postgres not available, skipping: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()

	// Mint a fresh install token, then register a throwaway cluster.
	tok, err := store.CreateAgentToken("regression-test-apply-inventory", time.Hour)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}
	cluster, _, err := store.RegisterCluster(RegisterClusterInput{
		Token:        tok.Token,
		ClusterName:  "regression-test-cluster",
		KubeVersion:  "v1.29.0",
		AgentVersion: "test",
		VeleroStatus: "Ready",
	})
	if err != nil {
		t.Fatalf("RegisterCluster: %v", err)
	}

	// First inventory establishes the application row.
	first := InventoryInput{
		ClusterID:      cluster.ID,
		KubeVersion:    "v1.29.0",
		VeleroStatus:   "Ready",
		NodeCount:      1,
		NamespaceCount: 1,
		Apps: []Application{
			{ID: "00000000-0000-0000-0000-00000000c001", Namespace: "kasten-io", Name: "kasten-io", Status: "Active",
				WorkloadCount: 3, ServiceCount: 1, PVCCount: 1, PVCapacityBytes: 1 << 30},
		},
		CollectedAt: now,
		Hash:        "h1",
	}
	if _, _, err := store.ApplyInventory(first); err != nil {
		t.Fatalf("first ApplyInventory: %v", err)
	}

	// Operator moves the namespace to stage 2.
	if _, _, err := store.UpdateApplication(ApplicationUpdateInput{
		ID:               "00000000-0000-0000-0000-00000000c001",
		ProtectionStatus: "pending_protection",
	}); err != nil {
		t.Fatalf("UpdateApplication pending_protection: %v", err)
	}

	// A new agent heartbeat arrives later with the same namespace.
	second := first
	second.Hash = "h2"
	second.CollectedAt = now.Add(30 * time.Second)
	if _, _, err := store.ApplyInventory(second); err != nil {
		t.Fatalf("second ApplyInventory: %v", err)
	}

	// The application must still be in stage 2.
	apps, err := store.ListApplications(cluster.ID)
	if err != nil {
		t.Fatalf("ListApplications: %v", err)
	}
	var kasten *Application
	for i := range apps {
		if apps[i].Namespace == "kasten-io" {
			kasten = &apps[i]
			break
		}
	}
	if kasten == nil {
		t.Fatalf("kasten-io application row missing after second inventory")
	}
	if kasten.ProtectionStatus != "pending_protection" {
		t.Fatalf("protection_status regression: got %q, want %q", kasten.ProtectionStatus, "pending_protection")
	}

	// And inventory-derived fields (workloadCount) must have been refreshed.
	if kasten.WorkloadCount != 3 {
		t.Fatalf("inventory refresh regression: workloadCount=%d, want 3", kasten.WorkloadCount)
	}

	// Cleanup so we don't leave a synthetic cluster behind.
	if _, err := store.DeleteCluster(cluster.ID); err != nil {
		t.Logf("cleanup DeleteCluster: %v", err)
	}
}
