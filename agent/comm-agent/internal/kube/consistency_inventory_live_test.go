package kube

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedConsistencyResourceInventory(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_CONSISTENCY_INVENTORY_TEST") != "1" {
		t.Skip("requires explicit authorized read-only 123 inventory acceptance")
	}
	directory := os.Getenv("HYPERCDR_CONSISTENCY_INVENTORY_EVIDENCE_DIR")
	if !strings.HasPrefix(filepath.Clean(directory), "/data/hypercdr-runtime/") {
		t.Fatal("requires runtime evidence directory")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG"))
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("read-only acceptance restricted to authorized 123")
	}
	config.Timeout = 10 * time.Second
	reader, err := NewKubernetesClusterReaderForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const namespace = "hcdr-consistency-test"
	ns, err := reader.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil || ns.Labels["hypercdr.io/isolated-test"] != "application-consistency" {
		t.Fatal("isolated namespace safety label required")
	}
	workloads, err := reader.ReadConsistencyWorkloadInventory(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	volumes, err := reader.ReadSnapshotVolumePrerequisites(ctx, namespace)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]bool{}
	for _, volume := range volumes {
		claims[volume.Name] = volume.Fields["uid"] != ""
	}
	if len(workloads) == 0 || !claims["ledger"] || !claims["commits"] {
		t.Fatal("expected real isolated writer and both ledger PVC identities")
	}
	collectedAt := time.Now().UTC()
	items := append(workloads, volumes...)
	items = append(items,
		NamedCapability{Type: "ConsistencyNamespaceInventory", Name: namespace, Fields: map[string]string{"namespace": namespace, "complete": "true", "workloadCount": strconv.Itoa(len(workloads))}},
		NamedCapability{Type: "CSISnapshotNamespaceInventory", Name: namespace, Fields: map[string]string{"namespace": namespace, "complete": "true", "volumeCount": strconv.Itoa(len(volumes))}},
	)
	capabilities := make([]protocol.NamedCapabilityInventory, 0, len(items))
	for _, item := range items {
		item.Fields["collectedAt"] = collectedAt.Format(time.RFC3339Nano)
		capabilities = append(capabilities, protocol.NamedCapabilityInventory{Type: item.Type, Name: item.Name, Driver: item.Driver, Fields: item.Fields})
	}
	data, err := json.MarshalIndent(map[string]any{"collectedAt": collectedAt, "capabilities": capabilities}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "capabilities.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("read-only inventory captured %d workloads and %d volumes; no qualification inferred", len(workloads), len(volumes))
}
