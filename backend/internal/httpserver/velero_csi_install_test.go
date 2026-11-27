package httpserver

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAgentInstallerEnablesVeleroCSI(t *testing.T) {
	if !strings.Contains(installScriptTemplate, "            - --features=EnableCSI\n") {
		t.Fatal("Velero installer must enable CSI snapshot plugins")
	}
}

func TestAgentInstallerDefaultTransferConcurrency(t *testing.T) {
	if !strings.Contains(installScriptTemplate, `\"globalConfig\":4`) || !strings.Contains(installScriptTemplate, `\"prepareQueueLength\":4`) || !strings.Contains(installScriptTemplate, `if [[ -z "$existing_node_config" ]]`) {
		t.Fatal("installer must configure four transfers with preparation queue capacity four")
	}
}

func TestInstallerRejectsInconsistentVeleroRuntime(t *testing.T) {
	start := strings.Index(installScriptTemplate, "verify_velero_runtime_contract() {")
	if start < 0 {
		t.Fatal("installer runtime verification is missing")
	}
	end := strings.Index(installScriptTemplate[start:], "\n}\n")
	if end < 0 {
		t.Fatal("installer verification function is incomplete")
	}
	function := installScriptTemplate[start : start+end+3]
	for _, scenario := range []string{"valid", "wrong-image", "wrong-node-image", "stale-capability", "missing-capability", "missing-csi", "stale-node-cache", "missing-node-refresh", "api-error"} {
		t.Run(scenario, func(t *testing.T) {
			script := `set -euo pipefail
NAMESPACE=agent
VELERO_IMAGE=velero:new
STORAGE_ON_DEMAND_SUPPORTED=v1
CACHE_PERSISTENCE_SUPPORTED=v1
CACHE_ISOLATION_SUPPORTED=v1
BACKUP_PVC_REFRESH_SUPPORTED=v1
log_error() { echo "$*" >&2; }
kubectl() {
  case "$SCENARIO:$*" in
    api-error:*) return 1 ;;
    wrong-image:*containers*image*) echo velero:old ;;
    wrong-node-image:*daemonset*containers*image*) echo velero:old ;;
    stale-capability:*storage-on-demand-image*) echo velero:old ;;
    missing-capability:*storage-on-demand-supported*) echo '' ;;
    stale-node-cache:*cache-persistence-image*) echo velero:old ;;
    missing-node-refresh:*backup-pvc-refresh-supported*) echo '' ;;
    missing-csi:*args*) echo server ;;
    *storage-on-demand-image*|*containers*image*) echo velero:new ;;
    *storage-on-demand-supported*) echo v1 ;;
    *cache-persistence-image*|*backup-pvc-refresh-image*) echo velero:new ;;
    *cache-persistence-supported*|*cache-isolation-supported*|*backup-pvc-refresh-supported*) echo v1 ;;
    *args*) echo 'server --features=EnableCSI' ;;
    *) return 1 ;;
  esac
}
` + function + "\nverify_velero_runtime_contract\n"
			command := exec.Command("bash", "-c", "SCENARIO="+scenario+"\n"+script)
			output, err := command.CombinedOutput()
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected runtime admission: %v %s", err, output)
			}
		})
	}
}
