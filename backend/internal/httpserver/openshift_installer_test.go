package httpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImagePullPreflightUsesRestrictedPodSecurity(t *testing.T) {
	for _, required := range []string{
		"seccompProfile:",
		"type: RuntimeDefault",
		"allowPrivilegeEscalation: false",
		"drop:\n            - ALL",
		"runAsNonRoot: true",
		"runAsUser: 1000",
	} {
		if !strings.Contains(installScriptTemplate, required) {
			t.Fatalf("image pull preflight is missing restricted security setting %q", required)
		}
	}
}

func TestOpenShiftProviderInstallsOADPAndKopiaWithoutCommunityVelero(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "applied.yaml")
	script := `set -euo pipefail
log_section() { :; }; log_info() { :; }; log_ok() { :; }; fail() { echo "$*" >&2; return 1; }
kubectl_apply_retry() { tee -a "$CAPTURE" >/dev/null; }
kubectl() {
  if [[ "$*" == "get clusterversion version -o jsonpath={.status.desired.version}" ]]; then printf '4.15.31'; return; fi
  if [[ "$*" == "-n openshift-adp get subscription hypercdr-oadp-operator -o jsonpath={.status.installedCSV}" ]]; then printf 'oadp-operator.v1.3.10'; return; fi
  if [[ "$*" == "-n openshift-adp get clusterserviceversion oadp-operator.v1.3.10 -o jsonpath={.status.phase}" ]]; then printf 'Succeeded'; return; fi
  if [[ "$*" == "get crd dataprotectionapplications.oadp.openshift.io" ]]; then return 0; fi
  if [[ "$*" == "create namespace openshift-adp --dry-run=client -o yaml" ]]; then printf 'apiVersion: v1\nkind: Namespace\n'; return; fi
  if [[ "$*" == *"rollout status"* ]]; then return 0; fi
  return 0
}
AGENT_IMAGE=registry.cn-hangzhou.aliyuncs.com/hypercdr/oadp-comm-agent:v1
OADP_CATALOG_IMAGE=registry.cn-hangzhou.aliyuncs.com/hypercdr/oadp-catalog:v1
OADP_RUNTIME_IMAGES="one two three four five six"
OADP_CHANNEL=stable-1.3
REGISTRY_SERVER=""; REGISTRY_USERNAME=""; REGISTRY_PASSWORD=""; REGISTRY_EMAIL=""; IMAGE_PULL_SECRET=hypercdr-registry
` + string(module) + `
provider_openshift_install_backup_backend
`
	path := filepath.Join(dir, "test.sh")
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", path)
	cmd.Env = append(os.Environ(), "CAPTURE="+capture)
	if output, runErr := cmd.CombinedOutput(); runErr != nil {
		t.Fatalf("provider failed: %v\n%s", runErr, output)
	}
	applied, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	text := string(applied)
	for _, required := range []string{"kind: CatalogSource", "kind: Subscription", "kind: DataProtectionApplication", "noDefaultBackupLocation: true", "backupImages: false", "defaultVolumesToFSBackup: true", "uploaderType: kopia"} {
		if !strings.Contains(text, required) {
			t.Fatalf("applied manifests are missing %q:\n%s", required, text)
		}
	}
	if strings.Contains(text, "kind: Deployment\nmetadata:\n  name: velero") {
		t.Fatal("OpenShift provider must not install a community Velero deployment")
	}
}

func TestOpenShiftProviderWaitsForSuccessfulOperatorCSV(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{"status.installedCSV", "get clusterserviceversion", `csv_phase" == "Succeeded"`, "DPA API is unavailable"} {
		if !strings.Contains(text, required) {
			t.Fatalf("OpenShift provider does not enforce %q", required)
		}
	}
}

func TestOpenShiftProviderWaitsForDPAWorkloadsToExist(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{
		`get deployment velero`,
		`rollout status deployment/velero`,
		`get daemonset node-agent`,
		`rollout status daemonset/node-agent`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("OpenShift provider does not wait for DPA workload %q", required)
		}
	}
}

func TestOpenShiftProviderSelectsUploadedKubeconfig(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{
		`provider_openshift_select_context()`,
		`Detected OpenShift kubeconfig files:`,
		`"$HOME/.kube/config"`,
		`"$PWD/kubeconfig"`,
		`Select an OpenShift kubeconfig`,
		`export KUBECONFIG="$KUBECONFIG_PATH"`,
		`OpenShift registration requires a kubeconfig`,
		`--kubeconfig "$KUBECONFIG_PATH" config get-contexts`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("OpenShift kubeconfig selection is missing %q", required)
		}
	}
}

func TestOpenShiftProviderGrantsManagedOADPCleanupRBAC(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{"catalogsources", "subscriptions", "operatorgroups", "clusterserviceversions", "dataprotectionapplications", `"delete"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("OpenShift cleanup RBAC is missing %q", required)
		}
	}
}

func TestOpenShiftProviderDetectsExistingOADPClusterWide(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{
		"get subscriptions.operators.coreos.com -A",
		"get dataprotectionapplications.oadp.openshift.io -A",
		"get clusterserviceversions.operators.coreos.com -A",
		"does not reuse existing OADP",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("cluster-wide existing OADP detection is missing %q", required)
		}
	}
}

func TestOpenShiftProviderBindsPrivateRegistrySecret(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{"openshift-adp create secret docker-registry", "patch serviceaccount default", "patch serviceaccount openshift-adp-controller-manager", "patch serviceaccount velero", "imagePullSecrets"} {
		if !strings.Contains(text, required) {
			t.Fatalf("private registry binding is missing %q", required)
		}
	}
}

func TestInstallTemplateKeepsProviderNamespacesIsolated(t *testing.T) {
	if !strings.Contains(installScriptTemplate, `if [[ "$CLUSTER_TYPE" != "openshift" && "$NAMESPACE" != "hypercdr-agent" ]]`) {
		t.Fatal("install template must retain the canonical namespace guard for native Kubernetes and CCE")
	}
	if !strings.Contains(installScriptTemplate, `if [[ "$CLUSTER_TYPE" == "openshift" && "$NAMESPACE" != "openshift-adp" ]]`) {
		t.Fatal("install template must enforce openshift-adp for OpenShift")
	}
}

func TestOpenShiftProviderUsesRestrictedSCCCompatibleAgentContext(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(module)
	for _, required := range []string{
		"seccompProfile:\\n          type: RuntimeDefault",
		"allowPrivilegeEscalation: false",
		"capabilities:\\n              drop:\\n                - ALL",
		"runAsNonRoot: true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("OpenShift Agent security context is missing %q", required)
		}
	}
	if !strings.Contains(installScriptTemplate, "${AGENT_POD_SECURITY_CONTEXT_BLOCK}") || !strings.Contains(installScriptTemplate, "${AGENT_CONTAINER_SECURITY_CONTEXT_BLOCK}") {
		t.Fatal("install template does not render the provider-specific Agent security context")
	}
}

func TestOpenShiftProviderDoesNotPrePullRuntimeImages(t *testing.T) {
	module, err := installerModuleFS.ReadFile("installers/openshift.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := `set -euo pipefail
preflight_image_pull() { echo "unexpected runtime image preflight" >&2; exit 1; }
log_info() { printf '%s\n' "$1"; }
OADP_CATALOG_IMAGE=registry.cn-hangzhou.aliyuncs.com/hypercdr/oadp-catalog:stable-1.3
OADP_RUNTIME_IMAGES="registry/oadp-bundle:v1 registry/two:v1 registry/three:v1 registry/four:v1 registry/five:v1 registry/six:v1"
` + string(module) + `
provider_openshift_run_preflight
`
	path := filepath.Join(dir, "test.sh")
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", path)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("preflight failed: %v\n%s", runErr, output)
	}
	if !strings.Contains(string(output), "pulled once during formal installation") {
		t.Fatalf("missing concise formal-installation message: %s", output)
	}
}
