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

func TestNamespaceReadinessPreservesPVCOwnership(t *testing.T) {
	actual := namespaceReadinessFromCatalog([]protocol.BackupResourceSummary{
		{Kind: "Deployment", Namespace: "mysql", Name: "mysql"},
		{Kind: "PersistentVolumeClaim", Namespace: "mysql", Name: "data"},
		{Kind: "PersistentVolumeClaim", Namespace: "nextcloud", Name: "data"},
		{Kind: "PersistentVolumeClaim", Namespace: "nextcloud", Name: "postgres"},
		{Kind: "ConfigMap", Namespace: "config", Name: "settings"},
		{Kind: "PersistentVolumeClaim", Namespace: "unselected", Name: "ignored"},
	}, []string{"mysql", "nextcloud", "config"})
	if len(actual) != 3 || !actual["mysql"].RuntimeWorkloadsExpected || actual["config"].RuntimeWorkloadsExpected || !reflect.DeepEqual(actual["mysql"].ExpectedPVCs, []string{"data"}) || !reflect.DeepEqual(actual["nextcloud"].ExpectedPVCs, []string{"data", "postgres"}) || len(actual["config"].ExpectedPVCs) != 0 {
		t.Fatalf("incorrect namespace expectations: %+v", actual)
	}
}
