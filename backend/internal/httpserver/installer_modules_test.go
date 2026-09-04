package httpserver

import (
	"strings"
	"testing"
)

func TestInstallerProviderModulesExposeContractWithoutNativeCCELeakage(t *testing.T) {
	modules, err := assembledInstallerModules()
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{
		"provider_select_context", "provider_verify", "provider_prepare_platform_trust",
		"provider_prepare_preflight", "provider_run_preflight", "provider_install_platform_trust",
		"provider_install_backup_backend",
		"provider_rollback_backup_backend",
	} {
		if !strings.Contains(modules, hook+"()") {
			t.Fatalf("provider contract is missing %s", hook)
		}
	}
	nativeStart := strings.Index(modules, "# Native Kubernetes provider")
	cceStart := strings.Index(modules, "# Huawei Cloud CCE provider")
	openshiftStart := strings.Index(modules, "# OpenShift provider")
	if nativeStart < 0 || cceStart <= nativeStart || openshiftStart <= cceStart {
		t.Fatal("provider modules are not assembled in the declared order")
	}
	native := modules[nativeStart:cceStart]
	for _, forbidden := range []string{"kubeconfig", "dynamic_pvc", "platform_connectivity", "huaweicloud"} {
		if strings.Contains(strings.ToLower(native), forbidden) {
			t.Fatalf("Native provider contains CCE-specific behavior %q", forbidden)
		}
	}
	openShift := modules[openshiftStart:]
	for _, required := range []string{"DataProtectionApplication", "noDefaultBackupLocation: true", "backupImages: false", "defaultVolumesToFSBackup: true", "uploaderType: kopia", "oadp-comm-agent", `NAMESPACE="openshift-adp"`, "CatalogSource readiness", "existing OADP installation"} {
		if !strings.Contains(openShift, required) {
			t.Fatalf("OpenShift provider is missing %q", required)
		}
	}
}
