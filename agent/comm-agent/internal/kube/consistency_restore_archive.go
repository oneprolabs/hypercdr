package kube

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"

	"github.com/minio/minio-go/v7"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (applier *DynamicManifestApplier) VerifyConsistencyRestoreBackup(ctx context.Context, namespace string, evidence protocol.ConsistencyRestoreEvidence) error {
	if err := applier.verifyConsistencyRestoreBackupMetadata(ctx, namespace, evidence); err != nil {
		return err
	}
	name := evidence.Backup.VeleroBackupName
	backup, err := applier.client.Resource(consistencyBackups).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	location, _, _ := unstructured.NestedString(backup.Object, "spec", "storageLocation")
	if location == "" {
		return fmt.Errorf("destination backup storage location is unknown")
	}
	bsl, err := applier.readBackupStorageLocation(ctx, namespace, location)
	if err != nil {
		return err
	}
	if bsl.Provider != "aws" {
		return fmt.Errorf("consistency archive verification requires S3-compatible storage")
	}
	credentials, err := applier.readS3Credentials(ctx, namespace, bsl.Credential)
	if err != nil {
		return err
	}
	client, err := newObjectStoreClient(bsl, credentials)
	if err != nil {
		return err
	}
	object, err := client.GetObject(ctx, bsl.Bucket, backupObjectPrefix(bsl.Prefix, name)+name+".tar.gz", minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer object.Close()
	if err := verifyConsistencyRestoreArchive(ctx, object, evidence); err != nil {
		return err
	}
	if err := applier.verifyConsistencyRestoreBackupMetadata(ctx, namespace, evidence); err != nil {
		return err
	}
	final, err := applier.client.Resource(consistencyBackups).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if final.GetUID() != backup.GetUID() || final.GetResourceVersion() != backup.GetResourceVersion() {
		return fmt.Errorf("destination backup changed during archive verification")
	}
	return nil
}

func verifyConsistencyRestoreArchive(ctx context.Context, reader io.Reader, evidence protocol.ConsistencyRestoreEvidence) error {
	command := protocol.RestoreCommand{VeleroBackupName: evidence.Backup.VeleroBackupName, SourceNamespace: evidence.Backup.SourceNamespace, SourceNamespaces: evidence.Backup.SourceNamespaces, ConsistencyEvidence: &evidence}
	if err := protocol.ValidateConsistencyRestoreEvidence(command); err != nil {
		return err
	}
	compressed := &io.LimitedReader{R: reader, N: 64<<20 + 1}
	archive, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer archive.Close()
	expanded := &io.LimitedReader{R: archive, N: 256<<20 + 1}
	entries := tar.NewReader(expanded)
	expected := map[string]protocol.ConsistencySnapshot{}
	volumes := map[string]protocol.ConsistencyVolume{}
	for _, snapshot := range evidence.Record.Snapshots {
		expected[snapshot.DataUploadUID] = snapshot
	}
	for _, volume := range evidence.Backup.ConsistencyGroup.Volumes {
		volumes[volume.UID] = volume
	}
	seen := map[string]bool{}
	paths := map[string]bool{}
	backupUID := ""
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count > 100000 || compressed.N <= 1 || expanded.N <= 1 {
			return fmt.Errorf("consistency archive exceeds verification limits")
		}
		header, err := entries.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		item, role := classifyVeleroArchivePath(header.Name)
		if role == veleroArchiveOther {
			continue
		}
		if item.groupResource == "podvolumebackups.velero.io" {
			return fmt.Errorf("consistency archive contains file-level backup evidence")
		}
		if item.groupResource != "datauploads.velero.io" {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 8<<20 || paths[header.Name] {
			return fmt.Errorf("invalid or duplicate archived DataUpload entry")
		}
		paths[header.Name] = true
		data, err := io.ReadAll(entries)
		if err != nil {
			return err
		}
		var upload unstructured.Unstructured
		if err := json.Unmarshal(data, &upload); err != nil {
			return err
		}
		snapshot, found := expected[string(upload.GetUID())]
		volume := volumes[snapshot.VolumeUID]
		if !found || upload.GetAPIVersion() != "velero.io/v2alpha1" || upload.GetKind() != "DataUpload" || upload.GetNamespace() != item.namespace || path.Base(header.Name) != upload.GetName()+".json" {
			return fmt.Errorf("archived DataUpload identity differs from accepted evidence")
		}
		ownerUID := upload.GetLabels()["velero.io/backup-uid"]
		if ownerUID == "" || (backupUID != "" && ownerUID != backupUID) {
			return fmt.Errorf("archived DataUploads belong to different backups")
		}
		backupUID = ownerUID
		if err := verifyConsistencyUploadIdentity(&upload, evidence.Backup.VeleroBackupName, ownerUID, protocol.ConsistencyVolumeIdentity{Namespace: volume.Namespace, Name: volume.Name, UID: volume.UID}); err != nil {
			return err
		}
		phase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
		handle, _, _ := unstructured.NestedString(upload.Object, "status", "snapshotID")
		class, _, _ := unstructured.NestedString(upload.Object, "spec", "csiSnapshot", "snapshotClass")
		if phase != "Completed" || handle != snapshot.Handle || class != volume.SnapshotClass || upload.GetLabels()["velero.io/pvc-uid"] != volume.UID {
			return fmt.Errorf("archived DataUpload durable snapshot differs from accepted evidence")
		}
		if role == veleroArchiveCanonicalResource {
			if seen[snapshot.DataUploadUID] {
				return fmt.Errorf("duplicate canonical archived DataUpload")
			}
			seen[snapshot.DataUploadUID] = true
		}
	}
	if len(seen) != len(expected) || len(expected) == 0 {
		return fmt.Errorf("consistency archive does not cover the complete snapshot group")
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return err
	}
	if compressed.N <= 1 || expanded.N <= 1 {
		return fmt.Errorf("consistency archive exceeds verification limits")
	}
	return nil
}
