package backup

import (
	"context"
	"fmt"
	"time"

	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/label"
	kbclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (kb *kubernetesBackupper) publishVolumeDiscovery(backup *velerov1api.Backup) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	volumes := &velerov1api.PodVolumeBackupList{}
	if err := kb.kbClient.List(ctx, volumes, kbclient.InNamespace(backup.Namespace), kbclient.MatchingLabels{velerov1api.BackupNameLabel: label.GetValidName(backup.Name), velerov1api.BackupUIDLabel: string(backup.UID)}); err != nil {
		return err
	}
	current := &velerov1api.Backup{}
	if err := kb.kbClient.Get(ctx, kbclient.ObjectKeyFromObject(backup), current); err != nil {
		return err
	}
	if current.UID == "" || current.UID != backup.UID {
		return fmt.Errorf("backup identity changed during discovery")
	}
	base := current.DeepCopy()
	if current.Annotations == nil {
		current.Annotations = map[string]string{}
	}
	current.Annotations["hypercdr.io/pod-volume-discovery"] = fmt.Sprintf("%s:%d", current.UID, len(volumes.Items))
	return kb.kbClient.Patch(ctx, current, kbclient.MergeFrom(base))
}
