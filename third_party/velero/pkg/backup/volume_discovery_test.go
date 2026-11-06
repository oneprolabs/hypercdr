package backup

import (
	"context"
	"testing"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	velerotest "github.com/vmware-tanzu/velero/pkg/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kbclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPublishVolumeDiscovery(t *testing.T) {
	client := velerotest.NewFakeControllerRuntimeClient(t)
	backup := &velerov1api.Backup{ObjectMeta: metav1.ObjectMeta{Name: "backup-a", Namespace: "velero", UID: "uid-a"}}
	if err := client.Create(context.Background(), backup); err != nil {
		t.Fatal(err)
	}
	volume := &velerov1api.PodVolumeBackup{ObjectMeta: metav1.ObjectMeta{Name: "pvb-a", Namespace: "velero", Labels: map[string]string{"velero.io/backup-name": "backup-a", "velero.io/backup-uid": "uid-a"}}}
	if err := client.Create(context.Background(), volume); err != nil {
		t.Fatal(err)
	}
	backupper := &kubernetesBackupper{kbClient: client}
	if err := backupper.publishVolumeDiscovery(backup); err != nil {
		t.Fatal(err)
	}
	current := &velerov1api.Backup{}
	if err := client.Get(context.Background(), kbclient.ObjectKeyFromObject(backup), current); err != nil {
		t.Fatal(err)
	}
	if current.Annotations["hypercdr.io/pod-volume-discovery"] != "uid-a:1" {
		t.Fatalf("unexpected discovery marker: %v", current.Annotations)
	}
	backup.UID = "stale-uid"
	if err := backupper.publishVolumeDiscovery(backup); err == nil {
		t.Fatal("stale backup must not publish a discovery marker")
	}
}
