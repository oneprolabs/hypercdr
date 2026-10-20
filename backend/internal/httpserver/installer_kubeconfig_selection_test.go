package httpserver

import (
	"bytes"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandKubeconfigSelection(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script is needed for terminal interaction")
	}
	shared, err := installerModuleFS.ReadFile("installers/kubeconfig-selection.sh")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	mock := filepath.Join(base, "kubectl")
	// Reject every mutating command; validate that context and merged config are
	// preserved on every remote request, not just printed in the summary.
	source := `#!/usr/bin/env bash
context=""
while [[ $# -gt 0 ]]; do
 case "$1" in
 --context) context="$2"; shift 2 ;;
 --request-timeout=*) shift ;;
 *) break ;;
 esac
done
case "$*" in
 "config get-contexts -o name")
   [[ "${KUBECONFIG:-}" == *missing* ]] && exit 0
   printf 'selected\nother\n' ;;
 "config current-context") printf 'selected\n' ;;
 config\ view*)
   [[ "$context" == selected ]] || exit 20
   if [[ "$*" == *context.user* ]]; then printf admin; else printf https://api.example.test:6443; fi ;;
 "get --raw=/api/v1/namespaces?limit=1"|"auth can-i * * --all-namespaces"|"get --raw=/apis/config.openshift.io/v1/clusterversions/version")
   [[ "$context" == selected && "${KUBECONFIG:-}" == "$EXPECTED_CONFIG" ]] || exit 21
   [[ "${API_UNAVAILABLE:-}" != true && -z "${API_ERROR:-}" ]] || { echo "${API_ERROR:-Unauthorized}" >&2; exit 1; }
   if [[ "$1" == auth ]]; then printf '%s\n' "${ACCESS:-yes}"; else printf namespace/default; fi ;;
 *) echo "Unexpected/mutating command: $*" >&2; exit 22 ;;
esac
`
	if err := os.WriteFile(mock, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(base, "config")
	if err := os.WriteFile(config, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(base, "select.sh")
	program := "#!/usr/bin/env bash\nset -euo pipefail\nfail() { echo \"ERROR: $*\" >&2; exit 1; }\n" + string(shared) + "\nKUBECTL_BIN=\"" + mock + "\"\nKUBECONFIG_PATH=\"${KUBECONFIG_PATH:-}\"\nKUBECTL_CONTEXT=\"${KUBECTL_CONTEXT:-}\"\nINTERACTIVE=\"${INTERACTIVE:-true}\"\nselect_registration_kubeconfig\nverify_registration_access\nprintf 'RESULT:%s:%s\\n' \"$KUBECONFIG_PATH\" \"$KUBECTL_CONTEXT\"\n"
	if err := os.WriteFile(fixture, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, config, input, extra, want string
		terminal, success                bool
	}{
		{"existing context", config, "y\n", "", "RESULT:" + config + ":selected", true, true},
		{"merged config", config + ":" + config, "y\n", "", "RESULT:" + config + ":" + config + ":selected", true, true},
		{"cancel", config, "n\n", "", "Registration canceled", true, false},
		{"denied permissions", config, "", "ACCESS=no", "administrator permissions are required", true, false},
		{"expired credentials", config, "", "API_UNAVAILABLE=true", "credentials are invalid or expired", true, false},
		{"forbidden namespace access", config, "", "API_ERROR=Forbidden", "cannot list namespaces", true, false},
		{"invalid CA", config, "", "API_ERROR=x509 certificate invalid", "certificate verification failed", true, false},
		{"network unreachable", config, "", "API_ERROR=no route to host", "Check the API address, network", true, false},
		{"missing terminal", config, "", "", "Open an interactive terminal", false, false},
		{"platform direct", config, "", "INTERACTIVE=false", "RESULT:" + config + ":selected", false, true},
		{"missing background", "missing", "", "INTERACTIVE=false", "No cluster configuration found", false, false},
		{"missing prompt", "missing", config + "\ny\n", "", "RESULT:" + config + ":selected", true, true},
		{"openshift", config, "y\n", "CLUSTER_TYPE=openshift", "OpenShift API verified", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", fixture)
			if tc.terminal {
				cmd = exec.Command("script", "-q", "-e", "-c", "bash "+fixture, "/dev/null")
			}
			expected := tc.config
			if tc.name == "missing prompt" {
				expected = config
			}
			cmd.Env = append(os.Environ(), "KUBECONFIG="+tc.config, "EXPECTED_CONFIG="+expected, "KUBECONFIG_PATH=", "KUBECTL_CONTEXT=", "INTERACTIVE=true", "ACCESS=yes", "API_UNAVAILABLE=false", "API_ERROR=", "CLUSTER_TYPE=native-kubernetes")
			if tc.name == "platform direct" {
				cmd.Env = append(cmd.Env, "KUBECONFIG=missing", "KUBECONFIG_PATH="+config, "KUBECTL_CONTEXT=selected")
			}
			if tc.extra != "" {
				cmd.Env = append(cmd.Env, tc.extra)
			}
			cmd.Stdin = strings.NewReader(tc.input)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.success || !bytes.Contains(out, []byte(tc.want)) {
				t.Fatalf("err=%v\n%s\nwant %s", err, out, tc.want)
			}
		})
	}
}

func TestClusterAccessScriptAsset(t *testing.T) {
	for _, name := range []string{"installers/kubeconfig-selection.sh", "installers/check-cluster.sh"} {
		data, err := installerModuleFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "-n")
		cmd.Stdin = bytes.NewReader(data)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", name, err, out)
		}
	}
}

func TestClusterAccessScriptDownload(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Router{}).clusterAccessScript(recorder, httptest.NewRequest("GET", "/check-cluster.sh", nil))
	if recorder.Code != 200 || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/x-shellscript") {
		t.Fatal(recorder.Code, recorder.Header())
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(recorder.Body.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid served script: %v %s", err, out)
	}
	if !strings.Contains(recorder.Body.String(), "verify_registration_access") {
		t.Fatal("missing shared access check")
	}
}
