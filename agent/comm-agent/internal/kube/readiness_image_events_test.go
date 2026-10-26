package kube

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestReadinessImageEventsUseActualContainerImage(t *testing.T) {
	pods := []ReadinessObject{{Kind: "Pod", UID: "pod-1", Name: "nextcloud-123", DisplayName: "nextcloud", Images: map[string]string{"app": "mapped.registry/nextcloud:30", "sidecar": "registry/sidecar:v1", "restore-wait": "registry/velero:v1"}}}
	event := func(uid, container, reason, message string) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"uid": container + reason, "creationTimestamp": "2026-10-23T09:00:00Z"}, "involvedObject": map[string]any{"uid": uid, "fieldPath": "spec.containers{" + container + "}"}, "reason": reason, "message": message, "count": int64(3)}}
	}
	observed := readinessImageEvents(pods, []unstructured.Unstructured{
		event("pod-1", "app", "Pulling", `Pulling image "mapped.registry/nextcloud:30"`),
		event("pod-1", "sidecar", "Pulled", `Container image "registry/sidecar:v1" already present on machine`),
		event("pod-1", "app", "Failed", `Failed to pull image "mapped.registry/nextcloud:30": timeout`),
		event("old-pod", "app", "Pulling", `Pulling image "mapped.registry/nextcloud:30"`),
		event("pod-1", "restore-wait", "Pulled", `Container image "registry/velero:v1" already present on machine`),
		event("pod-1", "app", "FailedMount", `Unable to mount volume`),
	})
	if len(observed) != 3 {
		t.Fatalf("unexpected events: %+v", observed)
	}
	for _, item := range observed {
		if item.Reason == "ImageCached" && item.Image != "registry/sidecar:v1" {
			t.Fatalf("incorrect sidecar image: %+v", item)
		}
		if item.Container == "app" && item.Image != "mapped.registry/nextcloud:30" {
			t.Fatalf("incorrect mapped image: %+v", item)
		}
		if item.Count != 3 || item.FirstAt.IsZero() {
			t.Fatalf("missing observation evidence: %+v", item)
		}
	}
}

func TestReadinessImageEventsDoNotGuessAmbiguousContainer(t *testing.T) {
	pods := []ReadinessObject{{Kind: "Pod", UID: "pod-1", Images: map[string]string{"first": "registry/app:v1", "second": "registry/app:v1"}}}
	event := unstructured.Unstructured{Object: map[string]any{"involvedObject": map[string]any{"uid": "pod-1"}, "reason": "Pulling", "message": `Pulling image "registry/app:v1"`}}
	if events := readinessImageEvents(pods, []unstructured.Unstructured{event}); len(events) != 0 {
		t.Fatalf("ambiguous containers must not be guessed: %+v", events)
	}
}

func TestPodReadinessObjectKeepsImagesWhenAlreadyReady(t *testing.T) {
	pod := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"uid": "pod-1", "name": "app-123", "labels": map[string]any{"app": "app"}},
		"spec":     map[string]any{"containers": []any{map[string]any{"name": "app", "image": "registry/app:v1"}, map[string]any{"name": "sidecar", "image": "registry/sidecar:v2"}}},
		"status":   map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}}
	object := podReadinessObject(pod)
	if object.DisplayName != "app" || object.State != "Ready" || object.Images["sidecar"] != "registry/sidecar:v2" {
		t.Fatalf("missing ready-container image evidence: %+v", object)
	}
}
