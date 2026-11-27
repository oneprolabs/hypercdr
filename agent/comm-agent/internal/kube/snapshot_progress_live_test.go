package kube

import (
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestSnapshotProgressLiveReadOnly(t *testing.T) {
	name := os.Getenv("HYPERCDR_PROGRESS_TEST_BACKUP")
	path := os.Getenv("HYPERCDR_PROGRESS_TEST_KUBECONFIG")
	if name == "" || path == "" {
		t.Skip("requires an explicit backup and test kubeconfig")
	}
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal(err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	progress, err := NewDynamicManifestApplierWithClient(client).GetBackupVolumeProgress(ctx, "hypercdr-agent", name)
	if err != nil {
		t.Fatal(err)
	}
	if !progress.DiscoveryComplete || progress.DiscoveryCount != 3 || len(progress.Items) != 3 {
		t.Fatalf("unexpected test inventory: %+v", progress)
	}
	for _, item := range progress.Items {
		t.Logf("%s/%s phase=%s snapshotReady=%t temporaryVolumeReady=%t message=%s", item.Namespace, item.Volume, item.Phase, item.SnapshotReady, item.TemporaryVolumeReady, item.PreparationMessage)
	}
}
