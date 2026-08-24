package kube

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type workloadImageResource struct {
	gvr     schema.GroupVersionResource
	podSpec []string
}

var workloadImageResources = []workloadImageResource{
	{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, podSpec: []string{"spec", "template", "spec"}},
	{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, podSpec: []string{"spec", "template", "spec"}},
	{gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, podSpec: []string{"spec", "template", "spec"}},
	{gvr: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, podSpec: []string{"spec", "template", "spec"}},
	{gvr: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, podSpec: []string{"spec", "jobTemplate", "spec", "template", "spec"}},
}

// ApplyWorkloadImageMappings updates controller PodTemplates only after Velero
// and PodVolumeRestore have completed. This deliberately causes any required
// rollout after persistent data is safely restored.
func (a *DynamicManifestApplier) ApplyWorkloadImageMappings(ctx context.Context, namespace string, mappings map[string]string) (int, error) {
	if strings.TrimSpace(namespace) == "" {
		return 0, fmt.Errorf("namespace is required")
	}
	if len(mappings) == 0 {
		return 0, nil
	}
	updated := 0
	for _, resource := range workloadImageResources {
		items, err := a.client.Resource(resource.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return updated, fmt.Errorf("list %s: %w", resource.gvr.Resource, err)
		}
		for _, listed := range items.Items {
			changed, err := a.updateWorkloadImageMappings(ctx, namespace, resource, listed.GetName(), mappings)
			if err != nil {
				return updated, err
			}
			updated += changed
		}
	}
	return updated, nil
}

func (a *DynamicManifestApplier) updateWorkloadImageMappings(ctx context.Context, namespace string, resource workloadImageResource, name string, mappings map[string]string) (int, error) {
	for attempt := 0; attempt < 3; attempt++ {
		object, err := a.client.Resource(resource.gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return 0, fmt.Errorf("get %s %s/%s: %w", resource.gvr.Resource, namespace, name, err)
		}
		changed := mapPodSpecImages(object.Object, resource.podSpec, mappings)
		if changed == 0 {
			return 0, nil
		}
		if _, err = a.client.Resource(resource.gvr).Namespace(namespace).Update(ctx, object, metav1.UpdateOptions{}); err == nil {
			return changed, nil
		} else if !apierrors.IsConflict(err) || attempt == 2 {
			return 0, fmt.Errorf("update %s %s/%s: %w", resource.gvr.Resource, namespace, name, err)
		}
	}
	return 0, nil
}

func mapPodSpecImages(object map[string]any, podSpecPath []string, mappings map[string]string) int {
	changed := 0
	for _, field := range []string{"containers", "initContainers"} {
		path := append(append([]string{}, podSpecPath...), field)
		containers, found, _ := unstructured.NestedSlice(object, path...)
		if !found {
			continue
		}
		for index, raw := range containers {
			container, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			image, _ := container["image"].(string)
			mapped := strings.TrimSpace(mappings[image])
			if mapped == "" || mapped == image {
				continue
			}
			container["image"] = mapped
			containers[index] = container
			changed++
		}
		if changed > 0 {
			_ = unstructured.SetNestedSlice(object, containers, path...)
		}
	}
	return changed
}
