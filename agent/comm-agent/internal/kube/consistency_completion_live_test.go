package kube

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedConsistencyCompletionReadOnly(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_COMPLETION_READ_TEST") != "1" {
		t.Skip("requires explicit read-only isolated completion opt-in")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG"))
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("completion read test is restricted to authorized cluster 123")
	}
	config.Timeout = 10 * time.Second
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, namespace := range []string{"hcdr-consistency-test", "hcdr-consistency-velero"} {
		object, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(ctx, namespace, metav1.GetOptions{})
		if err != nil || object.GetLabels()["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatalf("namespace isolation guard failed for %s: %v", namespace, err)
		}
	}
	encoded, err := os.ReadFile(os.Getenv("HYPERCDR_CONSISTENCY_RESULT_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var record consistency.Record
	if err := json.Unmarshal(encoded, &record); err != nil || !strings.HasPrefix(record.ID, "isolated-runner-") || record.ApplicationProfile != "paired-ledger-v1" || len(record.Volumes) != 2 {
		t.Fatal("expected existing isolated runner completion evidence")
	}
	var requirements []SnapshotVolumeRequirement
	for _, volume := range record.Volumes {
		if volume.Namespace != "hcdr-consistency-test" || (volume.Name != "ledger" && volume.Name != "commits") {
			t.Fatal("evidence references a nonfixture volume")
		}
		requirements = append(requirements, SnapshotVolumeRequirement{Namespace: volume.Namespace, Name: volume.Name, UID: volume.UID, Class: "hcdr-test-longhorn"})
	}
	backups, err := client.Resource(consistencyBackups).Namespace("hcdr-consistency-velero").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	name := ""
	for _, backup := range backups.Items {
		if backup.GetAnnotations()["hypercdr.io/consistency-run"] == record.ID {
			if name != "" {
				t.Fatal("multiple backups claim the consistency run")
			}
			name = backup.GetName()
		}
	}
	verified, err := NewDynamicManifestApplierWithClient(client).ReadConsistencyBackupCompletion(ctx, "hcdr-consistency-velero", name, record, requirements)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("read-only verification bound run %s to backup %s UID %s and %d journaled Kopia snapshots", record.ID, verified.GetName(), verified.GetUID(), len(record.Snapshots))
}
