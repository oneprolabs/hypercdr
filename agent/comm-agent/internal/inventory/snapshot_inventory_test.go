package inventory

import (
	"context"
	"fmt"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
)

type snapshotInventoryReader struct {
	fakeClusterReader
	err       error
	namespace string
}

type consistencyInventoryReader struct {
	snapshotInventoryReader
	workloadError error
}

func (reader *consistencyInventoryReader) ReadConsistencyWorkloadInventory(_ context.Context, namespace string) ([]kube.NamedCapability, error) {
	return []kube.NamedCapability{{Type: "ConsistencyWorkload", Name: "writer", Fields: map[string]string{"namespace": namespace, "uid": "workload-uid"}}}, reader.workloadError
}

func TestCapabilitiesKeepWorkloadAndVolumeCompletenessSeparate(t *testing.T) {
	for _, failed := range []bool{false, true} {
		reader := &consistencyInventoryReader{}
		if failed {
			reader.workloadError = fmt.Errorf("forbidden")
		}
		snapshot, err := NewKubernetesCollector(config.Config{}, reader).CollectCapabilities("isolated")
		if err != nil {
			t.Fatal(err)
		}
		markers, workloads := 0, 0
		for _, item := range snapshot.Report.Capabilities {
			switch item.Type {
			case "CSISnapshotNamespaceInventory":
				if item.Fields["volumeCount"] != "1" {
					t.Fatal("workloads contaminated the PVC count")
				}
			case "ConsistencyNamespaceInventory":
				markers++
				if (item.Fields["complete"] == "true") == failed || !failed && item.Fields["workloadCount"] != "1" {
					t.Fatal("workload completeness marker is incorrect")
				}
			case "ConsistencyWorkload":
				workloads++
			}
		}
		if markers != 1 || failed && workloads != 0 || !failed && workloads != 1 {
			t.Fatal("partial workload inventory was advertised")
		}
	}
}

func (reader *snapshotInventoryReader) ReadNamespaceAPIs(context.Context, string) ([]kube.NamespaceAPI, error) {
	return nil, nil
}

func (reader *snapshotInventoryReader) ReadSnapshotVolumePrerequisites(_ context.Context, namespace string) ([]kube.NamedCapability, error) {
	reader.namespace = namespace
	return []kube.NamedCapability{{Type: "CSISnapshotVolumePrerequisite", Name: "data", Fields: map[string]string{"namespace": namespace, "uid": "current-pvc", "status": "configured"}}}, reader.err
}

func TestCapabilitiesCarryNamespaceSnapshotPrerequisites(t *testing.T) {
	reader := &snapshotInventoryReader{}
	collector := NewKubernetesCollector(config.Config{}, reader)
	snapshot, err := collector.CollectCapabilities("isolated")
	if err != nil {
		t.Fatal(err)
	}
	if reader.namespace != "isolated" || len(snapshot.Report.Capabilities) != 2 || snapshot.Report.Capabilities[0].Fields["uid"] != "current-pvc" || snapshot.Report.Capabilities[1].Fields["complete"] != "true" {
		t.Fatalf("namespace identity or completion evidence lost: %+v", snapshot.Report)
	}
	reader.err = fmt.Errorf("PVC listing forbidden")
	if _, err := collector.CollectCapabilities("isolated"); err == nil {
		t.Fatal("failed snapshot inventory was reported as successful")
	}
}
