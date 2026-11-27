package controller

import (
	"context"
	"time"

	"github.com/vmware-tanzu/velero/pkg/nodeagent"
	velerotypes "github.com/vmware-tanzu/velero/pkg/types"
)

func (r *DataUploadReconciler) SetBackupPVCConfigMap(name string) {
	r.backupPVCConfigMap = name
}

func (r *DataUploadReconciler) currentBackupPVCConfig(namespace string) (map[string]velerotypes.BackupPVC, error) {
	if r.backupPVCConfigMap == "" {
		return r.backupPVCConfig, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	configuration, err := nodeagent.GetConfigs(ctx, namespace, r.kubeClient, r.backupPVCConfigMap)
	if err != nil {
		return nil, err
	}
	if configuration == nil {
		return nil, nil
	}
	return configuration.BackupPVCConfig, nil
}
