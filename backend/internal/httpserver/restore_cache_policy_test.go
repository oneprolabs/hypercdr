package httpserver

import (
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestDeriveRestoreCachePolicyEnablesCompatibleDefaultStorageClass(t *testing.T) {
	policy := deriveRestoreCachePolicy([]store.ClusterStorageClass{{
		Name: "longhorn", Default: true, Provisioner: "driver.longhorn.io", ReclaimPolicy: "Delete",
	}})
	if !policy.Enabled || policy.StorageClass != "longhorn" || policy.ResidentThresholdMB != 1024 || policy.CacheLimitMB != 5120 {
		t.Fatalf("unexpected automatic cache policy: %+v", policy)
	}
}

func TestDeriveRestoreCachePolicyDisablesRetainStorageClass(t *testing.T) {
	policy := deriveRestoreCachePolicy([]store.ClusterStorageClass{{
		Name: "retained", Default: true, Provisioner: "csi.example.test", ReclaimPolicy: "Retain",
	}})
	if policy.Enabled || policy.StorageClass != "" || policy.Reason == "" {
		t.Fatalf("expected safe fallback policy: %+v", policy)
	}
}
