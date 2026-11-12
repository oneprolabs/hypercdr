package httpserver

import (
	"os/exec"
	"strings"
	"testing"
)

func platformIPTestScript(t *testing.T, endpoint string) string {
	t.Helper()
	prefix, _, found := strings.Cut(installScriptTemplate, `if [[ -z "$TOKEN" ]]; then`)
	if !found {
		t.Fatal("installer argument validation boundary missing")
	}
	return strings.NewReplacer(
		"{{INSTALLER_PROVIDER_MODULES}}", "",
		"{{AGENT_WS_ENDPOINT}}", endpoint,
		"{{TOKEN_VALIDATE_URL}}", "https://platform.example:12443/api/v1/agent-tokens/validate",
		"{{AGENT_UNINSTALL_URL}}", "https://platform.example:12443/uninstall-agent.sh",
		"{{VELERO_CRDS_URL}}", "https://platform.example:12443/assets/velero/crds.yaml",
		"{{REGISTRY_CA_URL}}", "https://platform.example:12443/assets/registry/ca.crt",
		"{{PLATFORM_CA_URL}}", "https://platform.example:12443/assets/platform/ca.crt",
	).Replace(prefix) + "\nprintf '%s\\n' \"$ENDPOINT\" \"$TOKEN_VALIDATE_URL\" \"$AGENT_UNINSTALL_URL\" \"$VELERO_CRDS_URL\" \"$REGISTRY_CA_URL\" \"$PLATFORM_CA_URL\"\n"
}

func TestInstallerPlatformIPOverridesPlatformHosts(t *testing.T) {
	for _, endpoint := range []string{"wss://embedded.example:9443/custom/agent", "ws://embedded.example:18080/ws/agent", "wss://embedded.example/ws/agent", "wss://[2001:db8::1]:12443/ws/agent"} {
		t.Run(endpoint, func(t *testing.T) {
			command := exec.Command("bash", "-s", "--", "--platform-ip", "192.168.8.149")
			command.Stdin = strings.NewReader(platformIPTestScript(t, endpoint))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("script failed: %v: %s", err, output)
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) != 6 {
				t.Fatalf("unexpected platform addresses: %q", output)
			}
			for _, address := range lines {
				if !strings.Contains(address, "://192.168.8.149") || strings.Contains(address, "platform.example") || strings.Contains(address, "embedded.example") {
					t.Fatalf("platform host not overridden: %q", address)
				}
			}
			wantedEndpoint := strings.NewReplacer("embedded.example", "192.168.8.149", "[2001:db8::1]", "192.168.8.149").Replace(endpoint)
			if lines[0] != wantedEndpoint || lines[1] != "https://192.168.8.149:12443/api/v1/agent-tokens/validate" || lines[3] != "https://192.168.8.149:12443/assets/velero/crds.yaml" {
				t.Fatalf("protocol, port or path changed: %q", output)
			}
		})
	}
}

func TestInstallerPlatformIPRejectsInvalidAndConflictingArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--platform-ip"}, {"--platform-ip", ""}, {"--platform-ip", "999.1.1.1"},
		{"--platform-ip", "01.2.3.4"}, {"--platform-ip", "1.2.3.4:443"},
		{"--platform-ip", "1.2.3.4; echo invalid"}, {"--platform-ip", "$(echo invalid)"},
		{"--platform-ip", "https://1.2.3.4"}, {"--platform-ip", "--token", "example"},
		{"--platform-ip", "1.2.3.4", "--endpoint", "wss://legacy/ws/agent"},
		{"--endpoint", "wss://legacy/ws/agent", "--platform-ip", "1.2.3.4"},
		{"--platform-ip", "1.2.3.4", "--endpoint-private", "wss://legacy/ws/agent"},
		{"--endpoint-public", "wss://legacy/ws/agent", "--platform-ip", "1.2.3.4"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			command := exec.Command("bash", append([]string{"-s", "--"}, args...)...)
			command.Stdin = strings.NewReader(platformIPTestScript(t, "wss://embedded.example:12443/ws/agent"))
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "--platform-ip") {
				t.Fatalf("invalid arguments not rejected explicitly: %q, %v", output, err)
			}
		})
	}
}

func TestInstallerPlatformIPDefaultAndLegacyCompatibility(t *testing.T) {
	for _, args := range [][]string{nil, {"--endpoint", "ws://legacy:18080/custom"}} {
		command := exec.Command("bash", append([]string{"-s", "--"}, args...)...)
		command.Stdin = strings.NewReader(platformIPTestScript(t, "wss://embedded.example:12443/ws/agent"))
		output, err := command.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "https://platform.example:12443/api/v1/agent-tokens/validate") {
			t.Fatalf("existing installation behavior changed: %q, %v", output, err)
		}
		if len(args) > 0 && !strings.HasPrefix(string(output), "ws://legacy:18080/custom\n") {
			t.Fatalf("legacy endpoint not retained: %q", output)
		}
	}
}
