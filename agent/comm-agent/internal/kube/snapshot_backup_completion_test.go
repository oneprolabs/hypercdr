package kube

import (
	"context"
	"encoding/json"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestSnapshotBackupCompletionRejectsIncompleteAndMixedCapture(t *testing.T) {
	for _, scenario := range []string{"valid", "partially-failed-backup", "missing-inventory", "missing-upload", "pending", "no-durable-id", "wrong-owner", "mixed-fsb"} {
		t.Run(scenario, func(t *testing.T) {
			inventory := protocol.SnapshotBackupInventory{Namespaces: []string{"app"}, Volumes: []protocol.ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc", SnapshotClass: "class"}}}
			encoded, _ := json.Marshal(inventory)
			fixtures := []string{
				`{"apiVersion":"velero.io/v1","kind":"Backup","metadata":{"name":"backup","namespace":"agent","uid":"backup-uid","resourceVersion":"1","labels":{"hypercdr.io/task-id":"task","hypercdr.io/command-id":"command"}},"spec":{"includedNamespaces":["app"],"snapshotVolumes":true,"snapshotMoveData":true,"defaultVolumesToFsBackup":false},"status":{"phase":"Completed"}}`,
				`{"apiVersion":"v1","kind":"PersistentVolumeClaim","metadata":{"name":"data","namespace":"app","uid":"pvc"}}`,
				`{"apiVersion":"velero.io/v2alpha1","kind":"DataUpload","metadata":{"name":"upload","namespace":"agent","uid":"upload-uid","labels":{"velero.io/backup-uid":"backup-uid","velero.io/pvc-uid":"pvc"},"ownerReferences":[{"apiVersion":"velero.io/v1","kind":"Backup","name":"backup","uid":"backup-uid","controller":true}]},"spec":{"snapshotType":"CSI","sourceNamespace":"app","sourcePVC":"data","csiSnapshot":{"snapshotClass":"class"}},"status":{"phase":"Completed","snapshotID":"durable-kopia"}}`,
			}
			var objects []runtime.Object
			for _, fixture := range fixtures {
				var object unstructured.Unstructured
				if err := json.Unmarshal([]byte(fixture), &object); err != nil {
					t.Fatal(err)
				}
				objects = append(objects, &object)
			}
			backup := objects[0].(*unstructured.Unstructured)
			backup.SetAnnotations(map[string]string{protocol.SnapshotInventoryAnnotation: string(encoded)})
			upload := objects[2].(*unstructured.Unstructured)
			switch scenario {
			case "partially-failed-backup":
				_ = unstructured.SetNestedField(backup.Object, "PartiallyFailed", "status", "phase")
			case "missing-inventory":
				backup.SetAnnotations(nil)
			case "missing-upload":
				objects = objects[:2]
			case "pending":
				_ = unstructured.SetNestedField(upload.Object, "Pending", "status", "phase")
			case "no-durable-id":
				unstructured.RemoveNestedField(upload.Object, "status", "snapshotID")
			case "wrong-owner":
				upload.SetOwnerReferences(nil)
			case "mixed-fsb":
				objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "PodVolumeBackup", "metadata": map[string]any{"name": "fsb", "namespace": "agent", "labels": map[string]any{"velero.io/backup-name": "backup"}}}})
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
				{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}:  "PodVolumeBackupList",
				{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}: "DataUploadList",
			}, objects...)
			result, err := NewDynamicManifestApplierWithClient(client).ReadSnapshotBackupCompletion(context.Background(), "agent", "backup")
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected completion: %v", err)
			}
			if err == nil {
				task := protocol.TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "backup", Backup: &protocol.BackupCommand{SourceNamespace: "app", VeleroBackupName: "backup", DataProtectionMode: "snapshot-crash-consistent"}}
				if err := protocol.ValidateSnapshotBackupResult(task, result); err != nil {
					t.Fatal(err)
				}
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatal("completion mutated source cluster")
				}
			}
		})
	}
}
