package httpserver

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInstallerCacheCapability(t *testing.T) {
	start := strings.Index(installScriptTemplate, "CACHE_PERSISTENCE_SUPPORTED=\"\"")
	if start < 0 {
		t.Fatal("missing capability detection")
	}
	end := strings.Index(installScriptTemplate[start:], "esac")
	if end < 0 {
		t.Fatal("missing capability case terminator")
	}
	script := installScriptTemplate[start:start+end+len("esac")] + "\nprintf '%s' \"$CACHE_PERSISTENCE_SUPPORTED\""
	for _, test := range []struct {
		image string
		want  string
	}{
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5", "v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6", "v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6@sha256:abcd", "v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5@sha256:abcd", "v1"},
		{"custom/velero:test-cache-v1", "v1"},
		{"custom/velero:dev-isolation-cache-v1", "v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.7", "v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.7@sha256:abcd", "v1"},
		{"custom/velero:test-cache-v1@sha256:abcd", "v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.4", ""},
		{"custom/velero:velero-v1.18.2-hcdr.5", ""},
		{"custom/velero:test-cache-v1-other", ""},
	} {
		command := exec.Command("bash", "-c", "VELERO_IMAGE=\"$1\"; "+script, "test", test.image)
		output, err := command.CombinedOutput()
		if err != nil || string(output) != test.want {
			t.Errorf("image %q: output %q, error %v; want %q", test.image, output, err, test.want)
		}
	}
}

func TestInstallerPublishedCacheCapabilities(t *testing.T) {
	start := strings.Index(installScriptTemplate, "CACHE_PERSISTENCE_SUPPORTED=\"\"")
	end := strings.Index(installScriptTemplate, "VELERO_AWS_PLUGIN_IMAGE=")
	if start < 0 || end <= start {
		t.Fatal("missing installer cache capabilities")
	}
	script := installScriptTemplate[start:end] + "\nprintf '%s/%s/%s' \"$CACHE_PERSISTENCE_SUPPORTED\" \"$STORAGE_ON_DEMAND_SUPPORTED\" \"$CACHE_ISOLATION_SUPPORTED\""
	for _, test := range []struct{ image, want string }{
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.7", "v1/v1/v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.7@sha256:abcd", "v1/v1/v1"},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6", "v1/v1/"},
		{"custom/velero:v1.18.2-hcdr.7", "//"},
	} {
		output, err := exec.Command("bash", "-c", "VELERO_IMAGE=\"$1\"; "+script, "test", test.image).CombinedOutput()
		if err != nil || string(output) != test.want {
			t.Errorf("image %q: output %q, error %v; want %q", test.image, output, err, test.want)
		}
	}
}
