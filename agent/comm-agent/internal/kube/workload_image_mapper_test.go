package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestApplyWorkloadImageMappingsUpdatesControllerAfterRestore(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	deployment := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "demo", "namespace": "demo-drill"},
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{
			map[string]any{"name": "app", "image": "old.example/app:v1"},
		}}}},
	}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		gvr: "DeploymentList",
		{Group: "apps", Version: "v1", Resource: "statefulsets"}: "StatefulSetList",
		{Group: "apps", Version: "v1", Resource: "daemonsets"}:   "DaemonSetList",
		{Group: "batch", Version: "v1", Resource: "jobs"}:        "JobList",
		{Group: "batch", Version: "v1", Resource: "cronjobs"}:    "CronJobList",
	}, deployment)
	applier := NewDynamicManifestApplierWithClient(client)
	changed, err := applier.ApplyWorkloadImageMappings(context.Background(), "demo-drill", map[string]string{"old.example/app:v1": "new.example/app:v2"})
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	got, err := client.Resource(gvr).Namespace("demo-drill").Get(context.Background(), "demo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	image, _, _ := unstructured.NestedString(got.Object, "spec", "template", "spec", "containers", "0", "image")
	if image == "" {
		containers, _, _ := unstructured.NestedSlice(got.Object, "spec", "template", "spec", "containers")
		image = containers[0].(map[string]any)["image"].(string)
	}
	if image != "new.example/app:v2" {
		t.Fatalf("image=%q", image)
	}
}
