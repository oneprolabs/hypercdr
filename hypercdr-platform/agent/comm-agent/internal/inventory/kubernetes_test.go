package inventory

import (
	"context"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
)

func TestKubernetesCollectorCollectsNamespaceApplications(t *testing.T) {
	collectedAt := time.Date(2026, 6, 5, 2, 30, 0, 0, time.UTC)
	collector := NewKubernetesCollector(config.Config{ClusterName: "fallback"}, fakeClusterReader{
		state: kube.ClusterState{
			Name:        "prod-a",
			KubeVersion: "v1.30.1",
			CollectedAt: collectedAt,
			Nodes: []kube.Node{
				{
					Name:           "cp-0",
					Ready:          true,
					KubeletVersion: "v1.30.1",
					Labels:         map[string]string{"node-role.kubernetes.io/control-plane": ""},
					Capacity:       map[string]string{"cpu": "4", "memory": "16Gi"},
				},
				{Name: "worker-0", Ready: false},
			},
			Namespaces: []kube.Namespace{
				{Name: "payments", Phase: "Active", Labels: map[string]string{"team": "pay"}},
				{Name: "auth", Phase: "Terminating", Labels: map[string]string{"team": "iam"}},
			},
			Workloads: []kube.Workload{
				{Namespace: "payments", Name: "api", Kind: "Deployment", Ready: true},
				{Namespace: "payments", Name: "worker", Kind: "Deployment", Ready: true},
				{Namespace: "auth", Name: "db", Kind: "StatefulSet", Ready: true},
				{Namespace: "auth", Name: "node-agent", Kind: "DaemonSet", Ready: true},
			},
			Services:   []kube.NamespacedResource{{Namespace: "payments", Name: "api"}, {Namespace: "auth", Name: "db"}},
			Ingresses:  []kube.NamespacedResource{{Namespace: "payments", Name: "api"}},
			ConfigMaps: []kube.NamespacedResource{{Namespace: "payments", Name: "config"}},
			Secrets:    []kube.NamespacedResource{{Namespace: "payments", Name: "secret"}, {Namespace: "auth", Name: "secret"}},
			PVCs: []kube.PVC{
				{Namespace: "auth", Name: "db-0", CapacityBytes: 10},
				{Namespace: "auth", Name: "db-1", CapacityBytes: 20},
			},
			Velero: kube.VeleroState{
				BackupStorageLocations: []map[string]any{{"name": "default", "phase": "Available"}},
				RecentBackups:          []map[string]any{{"name": "backup-1", "phase": "Completed"}},
			},
		},
	})

	snapshot, err := collector.Collect()
	if err != nil {
		t.Fatal(err)
	}
	report := snapshot.Report
	if report.InventoryHash == "" || snapshot.Hash != report.InventoryHash {
		t.Fatal("expected inventory hash to be set consistently")
	}
	if report.Cluster.Name != "prod-a" || report.Cluster.KubeVersion != "v1.30.1" {
		t.Fatalf("unexpected cluster summary: %#v", report.Cluster)
	}
	if report.Cluster.NodeCount != 2 || report.Cluster.NamespaceCount != 2 {
		t.Fatalf("unexpected cluster counts: %#v", report.Cluster)
	}
	if len(report.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(report.Nodes))
	}
	if report.Nodes[0].Role != "control-plane" || report.Nodes[0].Status != "ready" {
		t.Fatalf("unexpected control-plane node: %#v", report.Nodes[0])
	}
	if report.Nodes[1].Status != "notReady" {
		t.Fatalf("unexpected worker node: %#v", report.Nodes[1])
	}

	if len(report.Apps) != 2 {
		t.Fatalf("expected 2 applications, got %d", len(report.Apps))
	}
	auth := report.Apps[0]
	payments := report.Apps[1]
	if auth.Namespace != "auth" || auth.Resources.StatefulSets != 1 || auth.Resources.DaemonSets != 1 {
		t.Fatalf("unexpected auth resources: %#v", auth)
	}
	if auth.Resources.PVCs != 2 || auth.Resources.PVCapacityBytes != 30 {
		t.Fatalf("unexpected auth pvc summary: %#v", auth.Resources)
	}
	if payments.Namespace != "payments" || payments.Resources.Deployments != 2 || payments.Resources.Services != 1 {
		t.Fatalf("unexpected payments resources: %#v", payments)
	}
	if payments.Resources.Ingresses != 1 || payments.Resources.ConfigMaps != 1 || payments.Resources.Secrets != 1 {
		t.Fatalf("unexpected payments secondary resources: %#v", payments.Resources)
	}
	if len(report.Velero.BackupStorageLocations) != 1 || len(report.Velero.RecentBackups) != 1 {
		t.Fatalf("unexpected velero inventory: %#v", report.Velero)
	}
}

type fakeClusterReader struct {
	state kube.ClusterState
}

func (r fakeClusterReader) ReadCluster(ctx context.Context) (kube.ClusterState, error) {
	return r.state, nil
}
