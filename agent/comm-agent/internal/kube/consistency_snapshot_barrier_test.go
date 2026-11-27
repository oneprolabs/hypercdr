package kube

import (
	"context"
	"encoding/json"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestConsistencySnapshotBarrierRequiresStableSourceAndCorrectOwnership(t *testing.T) {
	for _, phase := range []string{"Prepared", "Accepted"} {
		t.Run(phase, func(t *testing.T) {
			for _, scenario := range []string{"valid", "completed", "completed-no-id", "completed-wrong-class", "temporary-pending", "temporary-pending-unready", "temporary-pending-wrong-owner", "temporary-lost", "temporary-unknown", "temporary-wrong-source", "temporary-wrong-owner", "fsb", "wrong-backup", "wrong-pvc", "wrong-namespace", "wrong-owner", "duplicate-upload", "mover-pending", "mover-failed", "snapshot-pending", "content-pending", "wrong-content-ref", "wrong-driver", "missing-volume", "wrong-upload-class", "wrong-snapshot-class", "wrong-content-class"} {
				t.Run(scenario, func(t *testing.T) {
					fixtures := []string{
						`{"apiVersion":"velero.io/v1","kind":"Backup","metadata":{"name":"backup-test","namespace":"agent","uid":"backup-uid"},"spec":{"snapshotVolumes":true,"snapshotMoveData":true,"defaultVolumesToFsBackup":false}}`,
						`{"apiVersion":"v1","kind":"PersistentVolumeClaim","metadata":{"name":"data","namespace":"app","uid":"pvc-uid"}}`,
						`{"apiVersion":"velero.io/v2alpha1","kind":"DataUpload","metadata":{"name":"upload-test","namespace":"agent","labels":{"velero.io/backup-uid":"backup-uid","velero.io/pvc-uid":"pvc-uid"},"ownerReferences":[{"apiVersion":"velero.io/v1","kind":"Backup","name":"backup-test","uid":"backup-uid","controller":true}]},"spec":{"snapshotType":"CSI","sourceNamespace":"app","sourcePVC":"data","csiSnapshot":{"volumeSnapshot":"snapshot-test","driver":"driver.test"}},"status":{"phase":"Prepared"}}`,
						`{"apiVersion":"snapshot.storage.k8s.io/v1","kind":"VolumeSnapshot","metadata":{"name":"snapshot-test","namespace":"app","uid":"snapshot-uid"},"spec":{"source":{"persistentVolumeClaimName":"data"}},"status":{"readyToUse":true,"boundVolumeSnapshotContentName":"content-test"}}`,
						`{"apiVersion":"snapshot.storage.k8s.io/v1","kind":"VolumeSnapshotContent","metadata":{"name":"content-test"},"spec":{"driver":"driver.test","volumeSnapshotRef":{"namespace":"app","name":"snapshot-test","uid":"snapshot-uid"}},"status":{"readyToUse":true,"snapshotHandle":"stable-handle"}}`,
					}
					var objects []runtime.Object
					for _, fixture := range fixtures {
						var object map[string]any
						if err := json.Unmarshal([]byte(fixture), &object); err != nil {
							t.Fatal(err)
						}
						objects = append(objects, &unstructured.Unstructured{Object: object})
					}
					backup := objects[0].(*unstructured.Unstructured)
					pvc := objects[1].(*unstructured.Unstructured)
					upload := objects[2].(*unstructured.Unstructured)
					_ = unstructured.SetNestedField(upload.Object, phase, "status", "phase")
					snapshot := objects[3].(*unstructured.Unstructured)
					content := objects[4].(*unstructured.Unstructured)
					_ = unstructured.SetNestedField(upload.Object, "snapshot-test", "spec", "csiSnapshot", "snapshotClass")
					_ = unstructured.SetNestedField(snapshot.Object, "snapshot-test", "spec", "volumeSnapshotClassName")
					_ = unstructured.SetNestedField(content.Object, "snapshot-test", "spec", "volumeSnapshotClassName")
					upload.SetUID("upload-uid")
					snapshot.SetName("upload-test")
					snapshot.SetNamespace("agent")
					_ = unstructured.SetNestedMap(snapshot.Object, map[string]any{"volumeSnapshotContentName": "upload-test"}, "spec", "source")
					_ = unstructured.SetNestedField(snapshot.Object, "upload-test", "status", "boundVolumeSnapshotContentName")
					content.SetName("upload-test")
					_ = unstructured.SetNestedField(content.Object, "stable-handle", "spec", "source", "snapshotHandle")
					_ = unstructured.SetNestedField(content.Object, "agent", "spec", "volumeSnapshotRef", "namespace")
					_ = unstructured.SetNestedField(content.Object, "upload-test", "spec", "volumeSnapshotRef", "name")
					var temporary map[string]any
					if err := json.Unmarshal([]byte(`{"apiVersion":"v1","kind":"PersistentVolumeClaim","metadata":{"name":"upload-test","namespace":"agent","ownerReferences":[{"apiVersion":"velero.io/v2alpha1","kind":"DataUpload","name":"upload-test","uid":"upload-uid"}]},"spec":{"dataSource":{"apiGroup":"snapshot.storage.k8s.io","kind":"VolumeSnapshot","name":"upload-test"}},"status":{"phase":"Bound"}}`), &temporary); err != nil {
						t.Fatal(err)
					}
					objects = append(objects, &unstructured.Unstructured{Object: temporary})
					volumes := []consistency.Volume{{Namespace: "app", Name: "data", UID: "pvc-uid"}}
					switch scenario {
					case "wrong-upload-class", "completed-wrong-class":
						_ = unstructured.SetNestedField(upload.Object, "other-class", "spec", "csiSnapshot", "snapshotClass")
						if scenario == "completed-wrong-class" {
							_ = unstructured.SetNestedField(upload.Object, "Completed", "status", "phase")
							_ = unstructured.SetNestedField(upload.Object, "stable-handle", "status", "snapshotID")
							objects = objects[:3]
						}
					case "wrong-snapshot-class":
						_ = unstructured.SetNestedField(snapshot.Object, "other-class", "spec", "volumeSnapshotClassName")
					case "wrong-content-class":
						_ = unstructured.SetNestedField(content.Object, "other-class", "spec", "volumeSnapshotClassName")
					case "completed":
						_ = unstructured.SetNestedField(upload.Object, "Completed", "status", "phase")
						_ = unstructured.SetNestedField(upload.Object, "stable-handle", "status", "snapshotID")
						objects = objects[:3]
					case "completed-no-id":
						_ = unstructured.SetNestedField(upload.Object, "Completed", "status", "phase")
					case "temporary-pending", "temporary-pending-unready", "temporary-pending-wrong-owner":
						_ = unstructured.SetNestedField(temporary, "Pending", "status", "phase")
						if scenario == "temporary-pending-unready" {
							_ = unstructured.SetNestedField(snapshot.Object, false, "status", "readyToUse")
						}
						if scenario == "temporary-pending-wrong-owner" {
							objects[5].(*unstructured.Unstructured).SetOwnerReferences(nil)
						}
					case "temporary-lost":
						_ = unstructured.SetNestedField(temporary, "Lost", "status", "phase")
					case "temporary-unknown":
						unstructured.RemoveNestedField(temporary, "status", "phase")
					case "temporary-wrong-source":
						_ = unstructured.SetNestedField(temporary, "other-snapshot", "spec", "dataSource", "name")
					case "temporary-wrong-owner":
						objects[5].(*unstructured.Unstructured).SetOwnerReferences(nil)
					case "fsb":
						_ = unstructured.SetNestedField(backup.Object, true, "spec", "defaultVolumesToFsBackup")
					case "wrong-backup":
						backup.SetUID("replacement-backup")
					case "wrong-pvc":
						pvc.SetUID("replacement-pvc")
					case "wrong-namespace":
						_ = unstructured.SetNestedField(upload.Object, "other-app", "spec", "sourceNamespace")
					case "wrong-owner":
						upload.SetOwnerReferences(nil)
					case "duplicate-upload":
						duplicate := upload.DeepCopy()
						duplicate.SetName("duplicate")
						objects = append(objects, duplicate)
					case "mover-pending":
						_ = unstructured.SetNestedField(upload.Object, "New", "status", "phase")
					case "mover-failed":
						_ = unstructured.SetNestedField(upload.Object, "Failed", "status", "phase")
					case "snapshot-pending":
						_ = unstructured.SetNestedField(snapshot.Object, false, "status", "readyToUse")
					case "content-pending":
						_ = unstructured.SetNestedField(content.Object, false, "status", "readyToUse")
					case "wrong-content-ref":
						_ = unstructured.SetNestedField(content.Object, "another-snapshot", "spec", "volumeSnapshotRef", "uid")
					case "wrong-driver":
						_ = unstructured.SetNestedField(content.Object, "other-driver", "spec", "driver")
					case "missing-volume":
						volumes = append(volumes, consistency.Volume{Namespace: "another-app", Name: "data", UID: "second-pvc"})
					}
					client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}: "DataUploadList"}, objects...)
					classes := []SnapshotVolumeRequirement{{Namespace: "app", Name: "data", UID: "pvc-uid", Class: "snapshot-test"}}
					if scenario == "missing-volume" {
						classes = append(classes, SnapshotVolumeRequirement{Namespace: "another-app", Name: "data", UID: "second-pvc", Class: "snapshot-test"})
					}
					result, err := NewDynamicManifestApplierWithClient(client).ReadConsistencySnapshotBarrier(context.Background(), "agent", "backup-test", "backup-uid", volumes, classes)
					if scenario == "valid" || scenario == "completed" || scenario == "temporary-pending" {
						if err != nil || len(result) != 1 || !result[0].Secured || result[0].Handle != "stable-handle" {
							t.Fatalf("result=%#v err=%v", result, err)
						}
					} else if err == nil {
						t.Fatalf("unsafe barrier accepted %s: %#v", scenario, result)
					}
					for _, action := range client.Actions() {
						if action.GetVerb() != "get" && action.GetVerb() != "list" {
							t.Fatal("barrier reader mutated cluster")
						}
					}
				})
			}
		})
	}
}
