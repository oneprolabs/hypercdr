package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	testclocks "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/builder"
	velerotest "github.com/vmware-tanzu/velero/pkg/test"
)

func TestGCPlatformRetentionOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		labels  map[string]string
		deleted bool
	}{
		{name: "managed plan skips expired TTL", labels: map[string]string{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/plan-id": "plan"}},
		{name: "external backup keeps upstream TTL", deleted: true},
		{name: "unassociated backup keeps upstream TTL", labels: map[string]string{"hypercdr.io/managed-by": "hypercdr"}, deleted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := testclocks.NewFakeClock(time.Now())
			backup := builder.ForBackup(velerov1api.DefaultNamespace, "retention-test").Expiration(clock.Now().Add(-time.Hour)).StorageLocation("default").Result()
			backup.Labels = test.labels
			location := builder.ForBackupStorageLocation(velerov1api.DefaultNamespace, "default").Phase(velerov1api.BackupStorageLocationPhaseAvailable).Result()
			client := velerotest.NewFakeControllerRuntimeClient(t, backup, location)
			reconciler := NewGCReconciler(velerotest.NewLogger(), client, time.Minute)
			reconciler.clock = clock
			if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}}); err != nil {
				t.Fatal(err)
			}
			var requests velerov1api.DeleteBackupRequestList
			if err := client.List(t.Context(), &requests); err != nil {
				t.Fatal(err)
			}
			if (len(requests.Items) > 0) != test.deleted {
				t.Fatalf("deletion requests=%d want deletion=%v", len(requests.Items), test.deleted)
			}
			var retained velerov1api.Backup
			if err := client.Get(t.Context(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, &retained); err != nil {
				t.Fatal(err)
			}
			if !retained.Status.Expiration.Before(&metav1.Time{Time: clock.Now()}) {
				t.Fatal("test backup must already be expired")
			}
		})
	}
}
