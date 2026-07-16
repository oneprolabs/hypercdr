package kube

import (
	"strings"
	"testing"
)

func TestPodTerminalReadinessFailureReportsImagePull(t *testing.T) {
	object := map[string]any{
		"metadata": map[string]any{"name": "demo-web-abc"},
		"status": map[string]any{
			"containerStatuses": []any{map[string]any{
				"name":  "web",
				"image": "registry.local/baseimage/demo-web:v2",
				"state": map[string]any{"waiting": map[string]any{
					"reason":  "ImagePullBackOff",
					"message": "repository does not exist",
				}},
			}},
		},
	}
	code, message := podTerminalReadinessFailure(object)
	if code != "RESTORE_WORKLOAD_IMAGE_PULL_FAILED" {
		t.Fatalf("unexpected code %q", code)
	}
	for _, expected := range []string{"demo-web-abc", "web", "ImagePullBackOff", "registry.local/baseimage/demo-web:v2", "repository does not exist"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected message to contain %q, got %q", expected, message)
		}
	}
}

func TestPodTerminalReadinessFailureAllowsOrdinaryStartup(t *testing.T) {
	object := map[string]any{
		"metadata": map[string]any{"name": "demo-web-abc"},
		"status": map[string]any{
			"containerStatuses": []any{map[string]any{
				"name":  "web",
				"image": "registry.local/baseimage/demo-web:v2",
				"state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}},
			}},
		},
	}
	if code, message := podTerminalReadinessFailure(object); code != "" || message != "" {
		t.Fatalf("ordinary startup must remain retryable, got %q %q", code, message)
	}
}
