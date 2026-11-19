package wsclient

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type namespacedReadinessReader struct {
	results map[string]kube.NamespaceReadiness
	calls   map[string]kube.ReadinessExpectations
	fail    string
}

func (reader *namespacedReadinessReader) GetNamespaceReadiness(ctx context.Context, namespace string, expectations kube.ReadinessExpectations) (kube.NamespaceReadiness, error) {
	reader.calls[namespace] = expectations
	if namespace == reader.fail {
		return kube.NamespaceReadiness{}, fmt.Errorf("read denied")
	}
	return reader.results[namespace], nil
}

func TestRestoredNamespacesReadinessScopes(t *testing.T) {
	for _, clone := range []bool{false, true} {
		t.Run(fmt.Sprintf("clone=%v", clone), func(t *testing.T) {
			first, second := "mysql", "nextcloud"
			mapping := map[string]string{}
			if clone {
				first, second = "mysql-copy", "nextcloud-copy"
				mapping = map[string]string{"mysql": first, "nextcloud": second, "unselected": "untouched"}
			}
			reader := &namespacedReadinessReader{calls: map[string]kube.ReadinessExpectations{}, results: map[string]kube.NamespaceReadiness{
				first:  {NamespacePhase: "Active", Ready: true, WorkloadCount: 2, ReadyWorkloads: 2, PodCount: 2, ReadyPodCount: 2, Objects: []kube.ReadinessObject{{Kind: "PersistentVolumeClaim", Name: "data", State: "Bound"}, {Kind: "Pod", Name: "app", State: "Ready"}}},
				second: {NamespacePhase: "Active", Ready: false, WorkloadCount: 2, ReadyWorkloads: 1, PodCount: 2, ReadyPodCount: 1, Objects: []kube.ReadinessObject{{Kind: "PersistentVolumeClaim", Name: "data", State: "Missing"}, {Kind: "Pod", Name: "app", State: "Starting"}}, UnreadyWorkload: []string{"PersistentVolumeClaim/data (not restored)"}},
			}}
			client := Client{readiness: reader}
			task := protocol.TaskDispatchPayload{Restore: &protocol.RestoreCommand{SourceNamespaces: []string{"mysql", "nextcloud"}, TargetNamespace: first, TargetNamespaces: mapping, ReadinessExpectationsKnown: true, RuntimeWorkloadsExpected: true, NamespaceReadiness: map[string]protocol.RestoreNamespaceReadiness{"mysql": {RuntimeWorkloadsExpected: true, ExpectedPVCs: []string{"data"}}, "nextcloud": {RuntimeWorkloadsExpected: true, ExpectedPVCs: []string{"data"}}}}}
			result, err := client.restoredNamespacesReadiness(context.Background(), task)
			if err != nil || result.Ready || result.WorkloadCount != 4 || result.ReadyWorkloads != 3 || len(reader.calls) != 2 || result.PVCCount != 2 || result.ReadyPVCCount != 1 {
				t.Fatalf("unexpected aggregate: %+v err=%v calls=%v", result, err, reader.calls)
			}
			if result.Objects[2].Namespace != second || !reflect.DeepEqual(reader.calls[second].PVCNames, []string{"data"}) {
				t.Fatalf("namespace ownership lost: %+v", result)
			}
			ready := reader.results[second]
			ready.Ready = true
			ready.ReadyWorkloads = 2
			ready.ReadyPodCount = 2
			ready.UnreadyWorkload = nil
			ready.Objects[0].State = "Bound"
			ready.Objects[1].State = "Ready"
			reader.results[second] = ready
			result, err = client.restoredNamespacesReadiness(context.Background(), task)
			if err != nil || !result.Ready || result.ReadyWorkloads != 4 || result.ReadyPVCCount != 2 {
				t.Fatalf("all namespaces should be ready: %+v err=%v", result, err)
			}
			reader.fail = second
			if _, err := client.restoredNamespacesReadiness(context.Background(), task); err == nil {
				t.Fatal("second namespace read failure must not be ignored")
			}
		})
	}
}

func TestLegacyReadinessDoesNotSearchSecondNamespacePVCOnlyInFirst(t *testing.T) {
	reader := &namespacedReadinessReader{calls: map[string]kube.ReadinessExpectations{}, results: map[string]kube.NamespaceReadiness{
		"mysql":     {NamespacePhase: "Active", WorkloadCount: 2, ReadyWorkloads: 2, Objects: []kube.ReadinessObject{{Kind: "PersistentVolumeClaim", Name: "mysql-data", State: "Bound"}, {Kind: "PersistentVolumeClaim", Name: "nextcloud-data", State: "Missing"}}, UnreadyWorkload: []string{"PersistentVolumeClaim/nextcloud-data (not restored)"}},
		"nextcloud": {NamespacePhase: "Active", WorkloadCount: 2, ReadyWorkloads: 2, Objects: []kube.ReadinessObject{{Kind: "PersistentVolumeClaim", Name: "nextcloud-data", State: "Bound"}, {Kind: "PersistentVolumeClaim", Name: "mysql-data", State: "Missing"}}, UnreadyWorkload: []string{"PersistentVolumeClaim/mysql-data (not restored)"}},
	}}
	client := Client{readiness: reader}
	task := protocol.TaskDispatchPayload{Restore: &protocol.RestoreCommand{SourceNamespaces: []string{"mysql", "nextcloud"}, TargetNamespace: "mysql", RuntimeWorkloadsExpected: true, ReadinessExpectationsKnown: true, ExpectedPVCs: []string{"mysql-data", "nextcloud-data"}}}
	result, err := client.restoredNamespacesReadiness(context.Background(), task)
	if err != nil || !result.Ready || result.PVCCount != 2 || result.ReadyPVCCount != 2 || len(result.Objects) != 2 {
		t.Fatalf("legacy scope is incorrect: %+v err=%v", result, err)
	}
	missing := reader.results["nextcloud"]
	missing.Objects[0].State = "Missing"
	reader.results["nextcloud"] = missing
	result, err = client.restoredNamespacesReadiness(context.Background(), task)
	if err != nil || result.Ready {
		t.Fatalf("genuinely missing PVC must block readiness: %+v err=%v", result, err)
	}
}
