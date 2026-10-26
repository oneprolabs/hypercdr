package kube

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
)

func TestPodReadinessObject(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		status        map[string]any
		state, reason string
	}{
		{"ready", map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}, "Ready", ""},
		{"scheduling", map[string]any{"conditions": []any{map[string]any{"type": "PodScheduled", "status": "False", "reason": "Unschedulable"}}}, "Scheduling", "Unschedulable"},
		{"image", map[string]any{"containerStatuses": []any{map[string]any{"name": "app", "state": map[string]any{"waiting": map[string]any{"reason": "ImagePullBackOff"}}}}}, "Starting", "ImagePullBackOff"},
		{"init", map[string]any{"initContainerStatuses": []any{map[string]any{"name": "restore-wait", "state": map[string]any{"waiting": map[string]any{"reason": "PodInitializing"}}}}}, "Initializing", "PodInitializing"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			object := podReadinessObject(unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "app", "uid": "uid-1"}, "status": scenario.status}})
			if object.State != scenario.state || object.Reason != scenario.reason || object.UID != "uid-1" {
				t.Fatalf("unexpected object: %+v", object)
			}
		})
	}
}
