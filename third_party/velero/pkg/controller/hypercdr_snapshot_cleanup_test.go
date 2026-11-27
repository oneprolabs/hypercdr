package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	velerov2 "github.com/vmware-tanzu/velero/pkg/apis/velero/v2alpha1"
	"github.com/vmware-tanzu/velero/pkg/datapath"
	"github.com/vmware-tanzu/velero/pkg/exposer"
)

type recordingCSICleanup struct {
	exposer.SnapshotExposer
	owners     []corev1.ObjectReference
	snapshots  []string
	namespaces []string
}

func (recorder *recordingCSICleanup) CleanUp(ctx context.Context, owner corev1.ObjectReference, snapshot, namespace string) {
	recorder.owners = append(recorder.owners, owner)
	recorder.snapshots = append(recorder.snapshots, snapshot)
	recorder.namespaces = append(recorder.namespaces, namespace)
}

func TestCSITerminalCallbacksCleanOnlyReferencedSnapshot(t *testing.T) {
	for _, phase := range []velerov2.DataUploadPhase{velerov2.DataUploadPhaseCompleted, velerov2.DataUploadPhaseFailed, velerov2.DataUploadPhaseCanceled} {
		t.Run(string(phase), func(t *testing.T) {
			reconciler, err := initDataUploaderReconciler()
			require.NoError(t, err)
			upload := dataUploadBuilder().Result()
			upload.UID = "upload-uid"
			upload.Spec.SnapshotType = velerov2.SnapshotTypeCSI
			upload.Spec.SourceNamespace = "isolated-source"
			upload.Spec.CSISnapshot = &velerov2.CSISnapshotSpec{VolumeSnapshot: "this-task-snapshot"}
			require.NoError(t, reconciler.client.Create(t.Context(), upload))
			recorder := &recordingCSICleanup{}
			reconciler.snapshotExposerList = map[velerov2.SnapshotType]exposer.SnapshotExposer{velerov2.SnapshotTypeCSI: recorder}
			switch phase {
			case velerov2.DataUploadPhaseCompleted:
				reconciler.OnDataUploadCompleted(t.Context(), upload.Namespace, upload.Name, datapath.Result{Backup: datapath.BackupResult{SnapshotID: "durable-kopia-snapshot"}})
			case velerov2.DataUploadPhaseFailed:
				reconciler.OnDataUploadFailed(t.Context(), upload.Namespace, upload.Name, fmt.Errorf("injected data movement failure"))
			case velerov2.DataUploadPhaseCanceled:
				reconciler.OnDataUploadCancelled(t.Context(), upload.Namespace, upload.Name)
			}
			require.Equal(t, []string{"this-task-snapshot"}, recorder.snapshots)
			require.Equal(t, []string{"isolated-source"}, recorder.namespaces)
			require.Len(t, recorder.owners, 1)
			require.Equal(t, upload.UID, recorder.owners[0].UID)
			require.Equal(t, upload.Name, recorder.owners[0].Name)
			require.Equal(t, upload.Namespace, recorder.owners[0].Namespace)
			var final velerov2.DataUpload
			require.NoError(t, reconciler.client.Get(t.Context(), types.NamespacedName{Namespace: upload.Namespace, Name: upload.Name}, &final))
			require.Equal(t, phase, final.Status.Phase)
		})
	}
}
