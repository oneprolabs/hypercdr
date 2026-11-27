package csi

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/builder"
	velerotest "github.com/vmware-tanzu/velero/pkg/test"
)

func TestHyperCDRSnapshotOwnershipRequiresManagedBackupIdentity(t *testing.T) {
	for _, scenario := range []string{"managed", "external", "missing-task", "missing-uid"} {
		t.Run(scenario, func(t *testing.T) {
			storageClass := builder.ForStorageClass("storage").Provisioner("test.csi").Result()
			snapshotClass := builder.ForVolumeSnapshotClass("snapshot").Driver("test.csi").ObjectMeta(builder.WithLabels(velerov1.VolumeSnapshotClassSelectorLabel, "true")).Result()
			client := velerotest.NewFakeControllerRuntimeClient(t, storageClass, snapshotClass)
			action := &pvcBackupItemAction{crClient: client, log: logrus.New()}
			backup := &velerov1.Backup{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "velero", UID: "backup-uid", Labels: map[string]string{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/task-id": "task"}}}
			switch scenario {
			case "external":
				backup.Labels = nil
			case "missing-task":
				delete(backup.Labels, "hypercdr.io/task-id")
			case "missing-uid":
				backup.UID = ""
			}
			className := "storage"
			claim := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "source", Labels: map[string]string{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/task-id": "inherited-not-proof"}}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &className}}
			snapshot, err := action.createVolumeSnapshot(claim, backup)
			require.NoError(t, err)
			if scenario == "managed" {
				require.Equal(t, "backup-uid", snapshot.Annotations["hypercdr.io/snapshot-backup-uid"])
				require.Equal(t, "task", snapshot.Annotations["hypercdr.io/snapshot-task-id"])
				require.Equal(t, "backup", snapshot.Annotations["hypercdr.io/snapshot-backup-name"])
				require.Equal(t, "velero", snapshot.Annotations["hypercdr.io/snapshot-backup-namespace"])
			} else {
				require.Empty(t, snapshot.Annotations)
			}
			require.Empty(t, snapshot.Annotations["hypercdr.io/snapshot-cleanup-approved-uid"])
		})
	}
}
