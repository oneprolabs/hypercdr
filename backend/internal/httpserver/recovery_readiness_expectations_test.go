package httpserver

import (
	"reflect"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestReadinessExpectationsFromCatalog(t *testing.T) {
	runtime, pvcs := readinessExpectationsFromCatalog([]protocol.BackupResourceSummary{
		{Kind: "PersistentVolumeClaim", Namespace: "app", Name: "data"},
		{Kind: "ConfigMap", Namespace: "app", Name: "config"},
		{Kind: "Deployment", Namespace: "other", Name: "ignored"},
	}, []string{"app"})
	if runtime || !reflect.DeepEqual(pvcs, []string{"data"}) {
		t.Fatalf("runtime=%v pvcs=%v, want no runtime workload and data PVC", runtime, pvcs)
	}

	runtime, _ = readinessExpectationsFromCatalog([]protocol.BackupResourceSummary{
		{Kind: "DeploymentConfig", Namespace: "app", Name: "web"},
	}, []string{"app"})
	if !runtime {
		t.Fatal("OpenShift DeploymentConfig must require workload readiness")
	}
}
