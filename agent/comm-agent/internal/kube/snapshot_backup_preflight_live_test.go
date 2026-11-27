package kube

import (
	"context"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedSnapshotBackupReadOnlyPreflight(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_SNAPSHOT_PREFLIGHT_TEST") != "1" {
		t.Skip("requires explicit isolated read-only preflight opt-in")
	}
	path := os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("preflight acceptance is restricted to authorized cluster 123")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const namespace = "hcdr-consistency-restored-runner"
	isolated, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil || isolated.Labels["hypercdr.io/isolated-test"] != "application-consistency" {
		t.Fatal("isolated namespace safety label is missing")
	}
	claims, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(claims.Items) != 2 {
		t.Fatal("expected the isolated restored two-volume fixture")
	}
	applier, err := NewDynamicManifestApplier(path)
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := applier.PrepareSnapshotBackup(ctx, []string{namespace})
	if err != nil {
		t.Fatal(err)
	}
	if annotations["velero.io/csi-volumesnapshot-class_driver.longhorn.io"] != "hcdr-test-longhorn" {
		t.Fatalf("unexpected read-only snapshot class resolution: %v", annotations)
	}
	t.Logf("read-only preflight matched %d isolated PVCs: %v; no snapshot, pause or backup was requested", len(claims.Items), annotations)
}
