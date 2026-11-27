package kube

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestConsistencyRestoreChecksDestinationBackupBeforeMutation(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-command", "wrong-source", "wrong-proof", "wrong-replicas", "partially-failed", "fsb", "missing-scope", "missing-class"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC()
			record := consistency.Record{ID: "run", GroupID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", StartedAt: now.Add(-time.Minute), Deadline: now.Add(time.Minute), Phase: consistency.Completed, Resumed: true, LocksReleased: true,
				Workloads: []consistency.WorkloadState{{Namespace: "app", Name: "writer", Kind: "Deployment", UID: "writer-uid", Generation: 1, Replicas: 2}},
				Volumes:   []consistency.Volume{{Namespace: "app", Name: "data", UID: "pvc-uid"}}, Snapshots: []consistency.Snapshot{{Source: "kopia", DataUploadUID: "upload", VolumeUID: "pvc-uid", Handle: "durable", Secured: true}}}
			proof, err := consistency.NewQuiescenceProof(record, now, []byte(`{"sequence":42}`))
			if err != nil {
				t.Fatal(err)
			}
			record.QuiescenceProof = proof
			encodedProof, _ := json.Marshal(proof)
			encodedWorkloads, _ := json.Marshal(record.Workloads)
			group := &protocol.ConsistencyGroup{ID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", MaxPauseSeconds: 120, Workloads: []protocol.ConsistencyWorkload{{Namespace: "app", Name: "writer", Kind: "Deployment", UID: "writer-uid"}}, Volumes: []protocol.ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc-uid", SnapshotClass: "class"}}}
			evidence := protocol.ConsistencyRestoreEvidence{TaskID: "run", CommandID: "command", Backup: protocol.BackupCommand{VeleroBackupName: "backup", SourceNamespace: "app", SourceClusterID: "source", DataProtectionMode: "application-consistent", ConsistencyGroup: group}, Record: record}
			backup := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "metadata": map[string]any{"name": "backup", "namespace": "agent", "uid": "synced-uid", "resourceVersion": "1"}, "spec": map[string]any{"snapshotMoveData": true, "snapshotVolumes": true, "defaultVolumesToFsBackup": false, "includedNamespaces": []any{"app"}}, "status": map[string]any{"phase": "Completed"}}}
			labels := map[string]string{"hypercdr.io/task-id": "run", "hypercdr.io/command-id": "command", "hypercdr.io/source-cluster-id": "source"}
			annotations := map[string]string{"hypercdr.io/consistency-run": "run", "hypercdr.io/consistency-requested": "application-consistent", "hypercdr.io/consistency-quiescence-proof": string(encodedProof), "hypercdr.io/consistency-original-workloads": string(encodedWorkloads), "velero.io/csi-volumesnapshot-class_driver.test": "class"}
			switch scenario {
			case "wrong-command":
				labels["hypercdr.io/command-id"] = "other"
			case "wrong-source":
				labels["hypercdr.io/source-cluster-id"] = "other"
			case "wrong-proof":
				annotations["hypercdr.io/consistency-quiescence-proof"] = "{}"
			case "wrong-replicas":
				annotations["hypercdr.io/consistency-original-workloads"] = "[]"
			case "partially-failed":
				_ = unstructured.SetNestedField(backup.Object, "PartiallyFailed", "status", "phase")
			case "fsb":
				_ = unstructured.SetNestedField(backup.Object, true, "spec", "defaultVolumesToFsBackup")
			case "missing-scope":
				unstructured.RemoveNestedField(backup.Object, "spec", "includedNamespaces")
			case "missing-class":
				delete(annotations, "velero.io/csi-volumesnapshot-class_driver.test")
			}
			backup.SetLabels(labels)
			backup.SetAnnotations(annotations)
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), backup)
			err = NewDynamicManifestApplierWithClient(client).verifyConsistencyRestoreBackupMetadata(context.Background(), "agent", evidence)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected destination verification: %v", err)
			}
			if scenario == "valid" {
				testConsistencyRestoreArchive(t, evidence)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("destination verification mutated the cluster")
				}
			}
		})
	}
}

func testConsistencyRestoreArchive(t *testing.T, evidence protocol.ConsistencyRestoreEvidence) {
	for _, scenario := range []string{"valid", "missing", "wrong-handle", "wrong-uid", "pending", "wrong-class", "wrong-owner", "mixed-fsb", "duplicate", "version-mismatch", "truncated", "corrupt-gzip"} {
		t.Run("archive-"+scenario, func(t *testing.T) {
			upload := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v2alpha1", "kind": "DataUpload", "metadata": map[string]any{"name": "upload", "namespace": "agent", "uid": "upload", "labels": map[string]any{"velero.io/backup-uid": "source-backup-uid", "velero.io/pvc-uid": "pvc-uid"}, "ownerReferences": []any{map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "name": "backup", "uid": "source-backup-uid", "controller": true}}}, "spec": map[string]any{"snapshotType": "CSI", "sourceNamespace": "app", "sourcePVC": "data", "csiSnapshot": map[string]any{"snapshotClass": "class"}}, "status": map[string]any{"phase": "Completed", "snapshotID": "durable"}}}
			switch scenario {
			case "wrong-handle":
				_ = unstructured.SetNestedField(upload.Object, "other", "status", "snapshotID")
			case "wrong-uid":
				upload.SetUID("other")
			case "pending":
				_ = unstructured.SetNestedField(upload.Object, "InProgress", "status", "phase")
			case "wrong-class":
				_ = unstructured.SetNestedField(upload.Object, "other", "spec", "csiSnapshot", "snapshotClass")
			case "wrong-owner":
				upload.SetOwnerReferences(nil)
			}
			var buffer bytes.Buffer
			compressed := gzip.NewWriter(&buffer)
			archive := tar.NewWriter(compressed)
			write := func(name string, object *unstructured.Unstructured) {
				encoded, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				if err := archive.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(encoded))}); err != nil {
					t.Fatal(err)
				}
				if _, err := archive.Write(encoded); err != nil {
					t.Fatal(err)
				}
			}
			const canonical = "resources/datauploads.velero.io/namespaces/agent/upload.json"
			if scenario != "missing" {
				write(canonical, upload)
			}
			switch scenario {
			case "mixed-fsb":
				write("resources/podvolumebackups.velero.io/namespaces/agent/fsb.json", upload)
			case "duplicate":
				write(canonical, upload)
			case "version-mismatch":
				_ = unstructured.SetNestedField(upload.Object, "other", "status", "snapshotID")
				write("resources/datauploads.velero.io/v2alpha1-preferredversion/namespaces/agent/upload.json", upload)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := compressed.Close(); err != nil {
				t.Fatal(err)
			}
			data := buffer.Bytes()
			if scenario == "truncated" {
				data = data[:len(data)/2]
			}
			if scenario == "corrupt-gzip" {
				data[len(data)-1] ^= 1
			}
			err := verifyConsistencyRestoreArchive(context.Background(), bytes.NewReader(data), evidence)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected archive acceptance: %v", err)
			}
		})
	}
}
