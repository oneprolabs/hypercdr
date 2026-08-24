package kube

import (
	"context"
	"fmt"
	"strconv"
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
	for key, port := range mappings {
		name, servicePort, protocol, err := parseServiceNodePortKey(key)
		if err != nil {
			return updated, err
		}
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
		index := -1
		for i, raw := range ports {
			candidate, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			value, _, _ := unstructured.NestedInt64(candidate, "port")
			proto, _, _ := unstructured.NestedString(candidate, "protocol")
			if proto == "" {
				proto = "TCP"
			}
			if value == servicePort && strings.EqualFold(proto, protocol) {
				index = i
				break
			}
		}
		if index < 0 {
			return updated, fmt.Errorf("service %s does not contain port %d/%s", name, servicePort, protocol)
		}
		selected := ports[index].(map[string]any)
		if value, ok := selected["nodePort"].(int64); ok && int(value) == port {
			continue
		}
		selected["nodePort"] = int64(port)
		ports[index] = selected
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

func parseServiceNodePortKey(key string) (string, int64, string, error) {
	parts := strings.Split(key, "|")
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" {
		return "", 0, "", fmt.Errorf("invalid service port mapping key %q", key)
	}
	value, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || value <= 0 {
		return "", 0, "", fmt.Errorf("invalid service port mapping key %q", key)
	}
	protocol := strings.ToUpper(strings.TrimSpace(parts[2]))
	if protocol != "TCP" && protocol != "UDP" && protocol != "SCTP" {
		return "", 0, "", fmt.Errorf("unsupported protocol %q for service %s", parts[2], parts[0])
	}
	return parts[0], value, protocol, nil
}
