package main

import "testing"

func TestValidateClusterBackupBackendKeepsProvidersIsolated(t *testing.T) {
	for _, valid := range [][2]string{
		{"native-kubernetes", "velero"},
		{"huaweicloud-cce", "velero"},
		{"openshift", "oadp"},
	} {
		if err := validateClusterBackupBackend(valid[0], valid[1]); err != nil {
			t.Fatalf("valid combination %q/%q rejected: %v", valid[0], valid[1], err)
		}
	}
	for _, invalid := range [][2]string{
		{"native-kubernetes", "oadp"},
		{"huaweicloud-cce", "oadp"},
		{"openshift", "velero"},
	} {
		if err := validateClusterBackupBackend(invalid[0], invalid[1]); err == nil {
			t.Fatalf("invalid combination %q/%q accepted", invalid[0], invalid[1])
		}
	}
}
