package httpserver

import (
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestCSIAdministratorRepairScript(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Router{}).clusterCSIUpgradeScript(recorder, httptest.NewRequest("GET", "/upgrade-cluster-csi-rbac.sh", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "--dry-run=server") || !strings.Contains(recorder.Body.String(), "/metadata/resourceVersion") || strings.Contains(recorder.Body.String(), "--from-literal") {
		t.Fatalf("unexpected repair script response: %d", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("repair script must not be served from a stale cache")
	}
}

func TestSourceAddressedReleaseInstallerCapabilities(t *testing.T) {
	start := strings.Index(installScriptTemplate, "CACHE_PERSISTENCE_SUPPORTED=\"\"")
	end := strings.Index(installScriptTemplate, "VELERO_GCP_PLUGIN_IMAGE=")
	if start < 0 || end <= start {
		t.Fatal("installer capability section missing")
	}
	script := installScriptTemplate[start:end] + "\nprintf '%s/%s/%s/%s' \"$CACHE_PERSISTENCE_SUPPORTED\" \"$STORAGE_ON_DEMAND_SUPPORTED\" \"$CACHE_ISOLATION_SUPPORTED\" \"$BACKUP_PVC_REFRESH_SUPPORTED\""
	image := "registry.example/hypercdr/velero:v1.18.2-hcdr.8-backup-volume-policy-v1-" + strings.Repeat("a", 64) + "-isolation-cache-v1"
	for _, reference := range []string{image, image + "@sha256:" + strings.Repeat("b", 64)} {
		output, err := exec.Command("bash", "-c", "VELERO_IMAGE=\"$1\"; "+script, "test", reference).CombinedOutput()
		if err != nil || string(output) != "v1/v1/v1/v1" {
			t.Fatalf("release capabilities: %q %v", output, err)
		}
	}
}
