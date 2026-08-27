package kube

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPodTerminalReadinessFailureObservesRecentImagePull(t *testing.T) {
	object := map[string]any{
		"metadata": map[string]any{"name": "demo-web-abc", "creationTimestamp": time.Now().Add(-time.Minute).Format(time.RFC3339)},
		"status": map[string]any{"containerStatuses": []any{map[string]any{
			"name": "web", "state": map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff"}},
		}}},
	}
	if code, message := podTerminalReadinessFailure(object); code != "" || message != "" {
		t.Fatalf("recent image pull must remain observable, got %q %q", code, message)
	}
}

func TestPodTerminalReadinessFailureReportsPersistentImagePull(t *testing.T) {
	object := map[string]any{
		"metadata": map[string]any{"name": "demo-web-abc", "creationTimestamp": time.Now().Add(-restoreWorkloadStartupGrace - time.Minute).Format(time.RFC3339)},
		"status": map[string]any{"containerStatuses": []any{map[string]any{
			"name": "web", "state": map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff"}},
		}}},
	}
	if code, _ := podTerminalReadinessFailure(object); code != "RESTORE_WORKLOAD_IMAGE_PULL_FAILED" {
		t.Fatalf("persistent image pull code = %q", code)
	}
}

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

func TestImagePullFailureEventMessageReturnsDetailedKubeletCause(t *testing.T) {
	events := []unstructured.Unstructured{
		{Object: map[string]any{"message": "Back-off pulling image nginx"}},
		{Object: map[string]any{"message": `Failed to pull image "nginx": failed to do request: Head "https://registry-1.docker.io/v2/library/nginx/manifests/latest": dial tcp 199.59.150.39:443: i/o timeout`}},
	}
	message := imagePullFailureEventMessage(events)
	for _, expected := range []string{"registry-1.docker.io", "443", "i/o timeout"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected %q in %q", expected, message)
		}
	}
}

func TestPersistentStorageFailureEventMessageReportsUncorrectableFilesystem(t *testing.T) {
	events := []unstructured.Unstructured{{Object: map[string]any{
		"reason":  "FailedMount",
		"message": "MountVolume.MountDevice failed: 'fsck' found errors on device /dev/longhorn/pvc-1 but could not correct them",
	}}}
	code, message := persistentStorageFailureEventMessage(events, "demo-mysql-abc")
	if code != "RESTORE_WORKLOAD_VOLUME_MOUNT_FAILED" {
		t.Fatalf("unexpected code %q", code)
	}
	for _, expected := range []string{"demo-mysql-abc", "fsck", "could not correct"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected %q in %q", expected, message)
		}
	}
}

func TestPersistentStorageFailureEventMessageIgnoresTransientAttach(t *testing.T) {
	events := []unstructured.Unstructured{{Object: map[string]any{
		"reason":  "FailedAttachVolume",
		"message": "AttachVolume.Attach failed: volume is not ready for workloads",
	}}}
	if code, message := persistentStorageFailureEventMessage(events, "demo-mysql-abc"); code != "" || message != "" {
		t.Fatalf("transient attach must remain retryable, got %q %q", code, message)
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

func TestPodTerminalReadinessFailureReportsCrashLoop(t *testing.T) {
	object := map[string]any{
		"metadata": map[string]any{"name": "demo-mysql"},
		"status": map[string]any{"containerStatuses": []any{map[string]any{
			"name": "mysql", "restartCount": int64(4),
			"state":     map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
			"lastState": map[string]any{"terminated": map[string]any{"exitCode": int64(1), "reason": "Error"}},
		}}},
	}
	code, message := podTerminalReadinessFailure(object)
	if code != "RESTORE_WORKLOAD_CRASH_LOOP" {
		t.Fatalf("unexpected code %q", code)
	}
	for _, expected := range []string{"demo-mysql", "mysql", "4 restarts", "exit code 1"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected message to contain %q, got %q", expected, message)
		}
	}
}
