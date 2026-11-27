package velero

import "fmt"

func configureDataProtection(spec *BackupManifestSpec, mode string) error {
	switch mode {
	case "", "live-filesystem":
		spec.SnapshotVolumes = boolPtr(false)
		spec.DefaultVolumesToFsBackup = boolPtr(true)
		spec.SnapshotMoveData = nil
	case "snapshot-crash-consistent":
		spec.SnapshotVolumes = boolPtr(true)
		spec.DefaultVolumesToFsBackup = boolPtr(false)
		spec.SnapshotMoveData = boolPtr(true)
	case "application-consistent":
		return fmt.Errorf("application-consistent backups require a qualified consistency coordinator; refusing uncoordinated capture")
	default:
		return fmt.Errorf("unsupported data protection mode %q", mode)
	}
	return nil
}
