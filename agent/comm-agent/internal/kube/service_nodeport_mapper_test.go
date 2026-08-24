package kube

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"testing"
)

func TestApplyServiceNodePortMappings(t *testing.T) {
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "services"}
	service := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": "web", "namespace": "demo"}, "spec": map[string]any{"type": "NodePort", "ports": []any{map[string]any{"port": int64(80), "protocol": "TCP"}}}}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "ServiceList"}, service)
	applier := NewDynamicManifestApplierWithClient(client)
	if changed, err := applier.ApplyServiceNodePortMappings(context.Background(), "demo", map[string]int{"web": 30081}); err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	got, err := client.Resource(gvr).Namespace("demo").Get(context.Background(), "web", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ports, _, _ := unstructured.NestedSlice(got.Object, "spec", "ports")
	port := ports[0].(map[string]any)["nodePort"].(int64)
	if port != 30081 {
		t.Fatalf("nodePort=%d", port)
	}
}
