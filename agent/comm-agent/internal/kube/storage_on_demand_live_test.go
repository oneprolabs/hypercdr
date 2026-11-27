package kube

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveOnDemandStoragePreflight(t *testing.T) {
	path := os.Getenv("HYPERCDR_STORAGE_PREFLIGHT_KUBECONFIG")
	location := os.Getenv("HYPERCDR_STORAGE_PREFLIGHT_LOCATION")
	if path == "" || location == "" {
		t.Skip("explicit kubeconfig and storage location required")
	}
	applier, err := NewDynamicManifestApplier(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if supported, err := applier.supportsOnDemandStorage(ctx, "hypercdr-agent"); err != nil || !supported {
		t.Fatalf("runtime capability contract failed: supported=%v error=%v", supported, err)
	}
	if err := applier.PrepareObjectStorage(ctx, "hypercdr-agent", location, ""); err != nil {
		t.Fatal(err)
	}
}
