package httpserver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandKubeconfigSelection(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script is needed for the terminal interaction test")
	}
	module, err := installerModuleFS.ReadFile("installers/kubeconfig-selection.sh")
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	directory := filepath.Join(base, "kubeconfigs")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{"a.yaml": 0600, "b.yaml": 0600, "unsafe.yaml": 0644} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("test"), mode); err != nil {
			t.Fatal(err)
		}
	}
	mock := filepath.Join(base, "kubectl")
	mockSource := `#!/usr/bin/env bash
file=""; context=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --kubeconfig) file="$2"; shift 2 ;;
    --context) context="$2"; shift 2 ;;
    *) break ;;
  esac
done
case "$*" in
  "config get-contexts -o name")
    if [[ "$file" == *b.yaml ]]; then printf 'one\ntwo\n'; else printf 'only\n'; fi ;;
  "config current-context") printf 'only\n' ;;
  config\ view*) printf 'https://api.example.test:6443' ;;
esac
`
	if err := os.WriteFile(mock, []byte(mockSource), 0700); err != nil {
		t.Fatal(err)
	}
	shared := strings.Replace(string(module), `HCDR_COMMAND_KUBECONFIG_DIR="/etc/hypercdr/kubeconfigs"`, `HCDR_COMMAND_KUBECONFIG_DIR="`+directory+`"`, 1)
	shared = strings.Replace(shared, `(( EUID == 0 )) || fail "Run the registration command from a root shell so it can read ${HCDR_COMMAND_KUBECONFIG_DIR}."`, `: # test fixture owns its private directory`, 1)
	fixture := filepath.Join(base, "select.sh")
	fixtureSource := `#!/usr/bin/env bash
set -euo pipefail
fail() { echo "ERROR: $*" >&2; exit 1; }
` + shared + `
KUBECTL_BIN="` + mock + `"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-}"
INTERACTIVE="${INTERACTIVE:-true}"
select_registration_kubeconfig
printf 'RESULT:%s:%s\n' "${KUBECONFIG_PATH##*/}" "$KUBECTL_CONTEXT"
`
	if err := os.WriteFile(fixture, []byte(fixtureSource), 0700); err != nil {
		t.Fatal(err)
	}

	terminal := exec.Command("script", "-q", "-e", "-c", "bash "+fixture, "/dev/null")
	terminal.Stdin = strings.NewReader("2\n2\n")
	output, err := terminal.CombinedOutput()
	if err != nil {
		t.Fatalf("interactive selection failed: %v\n%s", err, output)
	}
	for _, expected := range []string{"a.yaml", "b.yaml", "Select a kubeconfig", "Select a context", "RESULT:b.yaml:two", "https://api.example.test:6443"} {
		if !bytes.Contains(output, []byte(expected)) {
			t.Fatalf("missing %q in output:\n%s", expected, output)
		}
	}
	if bytes.Contains(output, []byte("3) unsafe.yaml")) {
		t.Fatalf("unsafe file was offered:\n%s", output)
	}

	background := exec.Command("bash", fixture)
	background.Env = append(os.Environ(), "INTERACTIVE=false", "KUBECONFIG_PATH="+filepath.Join(directory, "a.yaml"), "KUBECTL_CONTEXT=only")
	output, err = background.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("RESULT:a.yaml:only")) {
		t.Fatalf("platform-direct bypass failed: %v\n%s", err, output)
	}

	noTTY := exec.Command("bash", fixture)
	noTTY.Env = append(os.Environ(), "INTERACTIVE=false", "KUBECONFIG_PATH=", "KUBECTL_CONTEXT=")
	output, err = noTTY.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("needs an interactive terminal")) {
		t.Fatalf("expected noninteractive rejection: %v\n%s", err, output)
	}
}
