package kube

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (a *DynamicManifestApplier) ApplyServiceNodePortMappings(ctx context.Context, namespace string, mappings map[string]int) (int, error) {
	if len(mappings) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(namespace) == "" {
		return 0, fmt.Errorf("namespace is required")
	}
	resource := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "services"}).Namespace(namespace)
	updated := 0
	for name, port := range mappings {
		if port < 30000 || port > 32767 {
			return updated, fmt.Errorf("service %s NodePort %d is outside 30000-32767", name, port)
		}
		service, err := resource.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return updated, fmt.Errorf("get service %s/%s: %w", namespace, name, err)
		}
		ports, found, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
		if !found || len(ports) == 0 {
			return updated, fmt.Errorf("service %s has no ports", name)
		}
		first, ok := ports[0].(map[string]any)
		if !ok {
			return updated, fmt.Errorf("service %s has invalid ports", name)
		}
		if value, ok := first["nodePort"].(int64); ok && int(value) == port {
			continue
		}
		first["nodePort"] = int64(port)
		if service.Object["spec"] == nil {
			return updated, fmt.Errorf("service %s has no spec", name)
		}
		if err := unstructured.SetNestedSlice(service.Object, ports, "spec", "ports"); err != nil {
			return updated, err
		}
		if _, err := resource.Update(ctx, service, metav1.UpdateOptions{}); err != nil {
			return updated, fmt.Errorf("update service %s: %w", name, err)
		}
		updated++
	}
	return updated, nil
}
