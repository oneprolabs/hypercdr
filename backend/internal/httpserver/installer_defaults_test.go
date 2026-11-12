package httpserver

import (
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
)

func TestInstallerEndpointDefaults(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"private", config.Config{AgentPrivateWSEndpoint: " wss://private/ws/agent ", AgentWSEndpoint: "wss://configured/ws/agent"}, "wss://private/ws/agent"},
		{"configured", config.Config{AgentWSEndpoint: "wss://configured/ws/agent"}, "wss://configured/ws/agent"},
		{"https", config.Config{PublicBaseURL: "https://platform:12443"}, "wss://platform:12443/ws/agent"},
		{"http", config.Config{PublicBaseURL: "http://platform:18080"}, "ws://platform:18080/ws/agent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := &Router{cfg: test.cfg}
			if got := router.installerAgentWSEndpoint(httptest.NewRequest("GET", "http://platform/install.sh", nil)); got != test.want {
				t.Fatalf("endpoint = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInstallerArgumentsOverrideEmbeddedDefaults(t *testing.T) {
	prefix, _, found := strings.Cut(installScriptTemplate, `if [[ -z "$TOKEN" ]]; then`)
	if !found {
		t.Fatal("installer argument validation boundary missing")
	}
	prefix = strings.ReplaceAll(prefix, "{{INSTALLER_PROVIDER_MODULES}}", "")
	prefix = strings.ReplaceAll(prefix, "{{AGENT_WS_ENDPOINT}}", "wss://embedded/ws/agent")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"defaults", nil, "wss://embedded/ws/agent|false"},
		{"override", []string{"--endpoint", "wss://private/ws/agent", "--install-registry-ca", "true"}, "wss://private/ws/agent|true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"-s", "--"}, test.args...)
			command := exec.Command("bash", args...)
			command.Stdin = strings.NewReader(prefix + "\nprintf '%s|%s' \"$ENDPOINT\" \"$INSTALL_REGISTRY_CA\"\n")
			output, err := command.CombinedOutput()
			if err != nil || string(output) != test.want {
				t.Fatalf("arguments produced %q, error %v; want %q", output, err, test.want)
			}
		})
	}
}
