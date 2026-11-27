package exposer

import (
	"testing"

	snapshotapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	snapshotfake "github.com/kubernetes-csi/external-snapshotter/client/v8/clientset/versioned/fake"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCSISnapshotCleanupScopeAndIdempotence(t *testing.T) {
	ctx := t.Context()
	owner := corev1.ObjectReference{Name: "task-upload", Namespace: "velero"}
	snapshots := snapshotfake.NewSimpleClientset(
		&snapshotapi.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace}},
		&snapshotapi.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: "task-snapshot", Namespace: "source"}},
		&snapshotapi.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: "user-snapshot", Namespace: "source"}},
		&snapshotapi.VolumeSnapshot{ObjectMeta: metav1.ObjectMeta{Name: "task-snapshot", Namespace: "other"}},
	)
	kubernetes := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "writer", Namespace: "source"}},
	)
	exposer := &csiSnapshotExposer{kubeClient: kubernetes, csiSnapshotClient: snapshots.SnapshotV1(), log: logrus.New()}
	for range 2 {
		exposer.CleanUp(ctx, owner, "task-snapshot", "source")
		_, err := snapshots.SnapshotV1().VolumeSnapshots("source").Get(ctx, "task-snapshot", metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err))
		_, err = snapshots.SnapshotV1().VolumeSnapshots(owner.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err))
		_, err = kubernetes.CoreV1().Pods(owner.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err))
		_, err = snapshots.SnapshotV1().VolumeSnapshots("source").Get(ctx, "user-snapshot", metav1.GetOptions{})
		require.NoError(t, err)
		_, err = snapshots.SnapshotV1().VolumeSnapshots("other").Get(ctx, "task-snapshot", metav1.GetOptions{})
		require.NoError(t, err)
		_, err = kubernetes.CoreV1().Pods("source").Get(ctx, "writer", metav1.GetOptions{})
		require.NoError(t, err)
	}
}
