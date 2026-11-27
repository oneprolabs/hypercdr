package kube

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func enableVeleroCSI(args []string) []string {
	withCSI := func(features string) string {
		for _, feature := range strings.Split(features, ",") {
			if strings.TrimSpace(feature) == "EnableCSI" {
				return features
			}
		}
		if strings.TrimSpace(features) == "" {
			return "EnableCSI"
		}
		return features + ",EnableCSI"
	}
	for index, arg := range args {
		if strings.HasPrefix(arg, "--features=") {
			args[index] = "--features=" + withCSI(strings.TrimPrefix(arg, "--features="))
			return args
		}
		if arg == "--features" && index+1 < len(args) {
			args[index+1] = withCSI(args[index+1])
			return args
		}
	}
	return append(args, "--features=EnableCSI")
}

func (a *DynamicManifestApplier) CheckSnapshotRuntime(ctx context.Context, namespace string) error {
	if namespace == "" || namespace == "*" {
		return fmt.Errorf("CSI runtime check requires an explicit Agent namespace")
	}
	deployments, err := a.client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("cannot inspect Velero CSI runtime: %w", err)
	}
	for _, deployment := range deployments.Items {
		if deployment.GetDeletionTimestamp() != nil {
			continue
		}
		containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
		for _, value := range containers {
			container, ok := value.(map[string]any)
			if !ok || container["name"] != "velero" {
				continue
			}
			args, _, _ := unstructured.NestedStringSlice(container, "args")
			for index, arg := range args {
				features := strings.TrimPrefix(arg, "--features=")
				if arg == "--features" && index+1 < len(args) {
					features = args[index+1]
				} else if !strings.HasPrefix(arg, "--features=") {
					continue
				}
				for _, feature := range strings.Split(features, ",") {
					if strings.TrimSpace(feature) == "EnableCSI" {
						ready, _, _ := unstructured.NestedInt64(deployment.Object, "status", "availableReplicas")
						observed, _, _ := unstructured.NestedInt64(deployment.Object, "status", "observedGeneration")
						updated, _, _ := unstructured.NestedInt64(deployment.Object, "status", "updatedReplicas")
						desired, exists, _ := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
						if !exists {
							desired = 1
						}
						if desired > 0 && ready >= desired && updated >= desired && observed >= deployment.GetGeneration() {
							return nil
						}
					}
				}
			}
		}
	}
	return fmt.Errorf("CSI snapshot execution is unavailable: enable --features=EnableCSI on Velero and wait for its rollout to complete; no file-level fallback was used")
}
