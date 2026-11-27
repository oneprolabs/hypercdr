package kube

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestConsistencyCompletionRequiresBackupProofAndExactDurableInventory(t *testing.T) {
	for _, scenario := range []string{"valid", "formatted-proof", "unresumed", "locked", "missing-proof", "changed-run", "changed-proof", "not-completed", "fsb", "different-handle", "different-upload", "different-backup", "wrong-class", "no-requirements", "changed-during-check"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC()
			record := consistency.Record{ID: "run", GroupID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", StartedAt: now.Add(-time.Minute), Deadline: now.Add(time.Minute), Phase: consistency.Completed, Resumed: true, LocksReleased: true,
				Workloads: []consistency.WorkloadState{{Namespace: "app", Name: "writer", Kind: "Deployment", UID: "writer-uid"}},
				Volumes:   []consistency.Volume{{Namespace: "app", Name: "data", UID: "pvc-uid"}},
				Snapshots: []consistency.Snapshot{{Source: "kopia", DataUploadUID: "upload-uid", VolumeUID: "pvc-uid", Handle: "durable", Secured: true}}}
			proof, err := consistency.NewQuiescenceProof(record, now, []byte(`{"sequence":42}`))
			if err != nil {
				t.Fatal(err)
			}
			record.QuiescenceProof = proof
			encodedProof, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			backup := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "metadata": map[string]any{"name": "backup", "namespace": "agent", "uid": "backup-uid", "annotations": map[string]any{"hypercdr.io/consistency-run": "run", "hypercdr.io/consistency-quiescence-proof": string(encodedProof)}}, "spec": map[string]any{"snapshotMoveData": true, "snapshotVolumes": true, "defaultVolumesToFsBackup": false}, "status": map[string]any{"phase": "Completed"}}}
			pvc := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": "data", "namespace": "app", "uid": "pvc-uid"}}}
			upload := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v2alpha1", "kind": "DataUpload", "metadata": map[string]any{"name": "upload", "namespace": "agent", "uid": "upload-uid", "labels": map[string]any{"velero.io/backup-uid": "backup-uid", "velero.io/pvc-uid": "pvc-uid"}, "ownerReferences": []any{map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "name": "backup", "uid": "backup-uid", "controller": true}}}, "spec": map[string]any{"snapshotType": "CSI", "sourceNamespace": "app", "sourcePVC": "data", "csiSnapshot": map[string]any{"snapshotClass": "class", "driver": "driver.test"}}, "status": map[string]any{"phase": "Completed", "snapshotID": "durable"}}}
			requirements := []SnapshotVolumeRequirement{{Namespace: "app", Name: "data", UID: "pvc-uid", Class: "class"}}
			switch scenario {
			case "formatted-proof":
				record.QuiescenceProof.Evidence = json.RawMessage("{\n  \"sequence\": 42\n}")
			case "unresumed":
				record.Resumed = false
			case "locked":
				record.LocksReleased = false
			case "missing-proof":
				record.QuiescenceProof = nil
			case "changed-run":
				annotations := backup.GetAnnotations()
				annotations["hypercdr.io/consistency-run"] = "other"
				backup.SetAnnotations(annotations)
			case "changed-proof":
				annotations := backup.GetAnnotations()
				annotations["hypercdr.io/consistency-quiescence-proof"] = "{}"
				backup.SetAnnotations(annotations)
			case "not-completed":
				_ = unstructured.SetNestedField(backup.Object, "InProgress", "status", "phase")
			case "fsb":
				_ = unstructured.SetNestedField(backup.Object, true, "spec", "defaultVolumesToFsBackup")
			case "different-handle":
				_ = unstructured.SetNestedField(upload.Object, "other", "status", "snapshotID")
			case "different-upload":
				upload.SetUID("other")
			case "different-backup":
				backup.SetUID("replacement")
			case "wrong-class":
				requirements[0].Class = "other"
			case "no-requirements":
				requirements = nil
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}: "DataUploadList"}, backup, pvc, upload)
			if scenario == "changed-during-check" {
				reads := 0
				client.PrependReactor("get", "backups", func(clienttesting.Action) (bool, runtime.Object, error) {
					reads++
					if reads == 4 {
						changed := backup.DeepCopy()
						changed.SetResourceVersion("changed")
						return true, changed, nil
					}
					return false, nil, nil
				})
			}
			applier := NewDynamicManifestApplierWithClient(client)
			result, err := applier.ReadConsistencyBackupCompletion(context.Background(), "agent", "backup", record, requirements)
			if (err == nil) != (scenario == "valid" || scenario == "formatted-proof") {
				t.Fatalf("scenario=%s: %v", scenario, err)
			}
			if err == nil && result.GetUID() != "backup-uid" {
				t.Fatal("completion did not retain the verified backup identity")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatal("completion verification mutated the cluster")
				}
			}
		})
	}
}
