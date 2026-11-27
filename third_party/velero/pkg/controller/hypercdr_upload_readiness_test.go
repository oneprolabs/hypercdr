package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	velerov2 "github.com/vmware-tanzu/velero/pkg/apis/velero/v2alpha1"
	"github.com/vmware-tanzu/velero/pkg/datapath"
	"github.com/vmware-tanzu/velero/pkg/exposer"
)

func TestUploadReadinessWithoutPodEvent(t *testing.T) {
	for _, scenario := range []string{"ready", "pending", "foreign-owner", "foreign-node", "patch-error"} {
		t.Run(scenario, func(t *testing.T) {
			reconciler, err := initDataUploaderReconciler()
			require.NoError(t, err)
			upload := dataUploadBuilder().Phase(velerov2.DataUploadPhaseAccepted).Result()
			upload.UID = types.UID("current-upload")
			upload.Spec.SnapshotType = fakeSnapshotType
			reconciler.snapshotExposerList[fakeSnapshotType] = &fakeSnapshotExposer{kubeClient: reconciler.client}
			upload.Finalizers = []string{DataUploadDownloadFinalizer}
			require.NoError(t, reconciler.client.Create(t.Context(), upload))
			controller := true
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: upload.Namespace, Name: upload.Name,
					Labels:          map[string]string{velerov1.DataUploadLabel: upload.Name},
					OwnerReferences: []metav1.OwnerReference{{APIVersion: velerov2.SchemeGroupVersion.String(), Kind: "DataUpload", Name: upload.Name, UID: upload.UID, Controller: &controller}},
				},
				Spec:   corev1.PodSpec{NodeName: reconciler.nodeName},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			}
			switch scenario {
			case "pending":
				pod.Status.Phase = corev1.PodPending
			case "foreign-owner":
				pod.OwnerReferences[0].UID = "previous-upload"
			case "foreign-node":
				pod.Spec.NodeName = "another-node"
			}
			require.NoError(t, reconciler.client.Create(t.Context(), pod))
			if scenario == "patch-error" {
				reconciler.client.(*FakeClient).patchError = errors.New("temporary API failure")
			}
			result, err := reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: upload.Namespace, Name: upload.Name}})
			if scenario == "patch-error" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			stored := &velerov2.DataUpload{}
			require.NoError(t, reconciler.client.Get(t.Context(), types.NamespacedName{Namespace: upload.Namespace, Name: upload.Name}, stored))
			if scenario == "ready" {
				require.Equal(t, velerov2.DataUploadPhasePrepared, stored.Status.Phase)
				require.True(t, result.Requeue)
			} else {
				require.Equal(t, velerov2.DataUploadPhaseAccepted, stored.Status.Phase)
				if scenario != "patch-error" {
					require.Equal(t, 5*time.Second, result.RequeueAfter)
				}
			}
		})
	}
}

type uploadCleanupBarrier struct {
	fakeSnapshotExposer
	check func()
}

func (barrier *uploadCleanupBarrier) CleanUp(_ context.Context, _ corev1.ObjectReference, _, _ string) {
	barrier.check()
}

func TestCompletedUploadReleasesSlotBeforeCleanup(t *testing.T) {
	reconciler, err := initDataUploaderReconciler()
	require.NoError(t, err)
	upload := dataUploadBuilder().Phase(velerov2.DataUploadPhaseInProgress).Result()
	upload.Spec.SnapshotType = fakeSnapshotType
	require.NoError(t, reconciler.client.Create(t.Context(), upload))
	watcher, err := reconciler.dataPathMgr.CreateMicroServiceBRWatcher(t.Context(), nil, nil, nil, "test", upload.Name, upload.Namespace, "pod", "container", upload.Name, datapath.Callbacks{}, false, nil)
	require.NoError(t, err)
	checked := false
	reconciler.snapshotExposerList = map[velerov2.SnapshotType]exposer.SnapshotExposer{fakeSnapshotType: &uploadCleanupBarrier{check: func() {
		checked = true
		require.Same(t, watcher, reconciler.dataPathMgr.GetAsyncBR(upload.Name))
		stored := &velerov2.DataUpload{}
		require.NoError(t, reconciler.client.Get(t.Context(), types.NamespacedName{Namespace: upload.Namespace, Name: upload.Name}, stored))
		require.Equal(t, velerov2.DataUploadPhaseInProgress, stored.Status.Phase)
		_, err := reconciler.dataPathMgr.CreateFileSystemBR("next-upload", "test", t.Context(), nil, upload.Namespace, datapath.Callbacks{}, nil)
		require.NoError(t, err)
	}}}
	reconciler.OnDataUploadCompleted(t.Context(), upload.Namespace, upload.Name, datapath.Result{Backup: datapath.BackupResult{SnapshotID: "snapshot"}})
	require.True(t, checked)
	require.Nil(t, reconciler.dataPathMgr.GetAsyncBR(upload.Name))
	stored := &velerov2.DataUpload{}
	require.NoError(t, reconciler.client.Get(t.Context(), types.NamespacedName{Namespace: upload.Namespace, Name: upload.Name}, stored))
	require.Equal(t, velerov2.DataUploadPhaseCompleted, stored.Status.Phase)
}
