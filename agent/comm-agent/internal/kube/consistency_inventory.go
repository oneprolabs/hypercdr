package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *KubernetesClusterReader) ReadConsistencyWorkloadInventory(ctx context.Context, namespace string) ([]NamedCapability, error) {
	if namespace == "" || namespace == "*" {
		return nil, fmt.Errorf("consistency workload inventory requires an explicit namespace")
	}
	deployments, err := r.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list consistency Deployments: %w", err)
	}
	statefulSets, err := r.clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list consistency StatefulSets: %w", err)
	}
	items := make([]NamedCapability, 0, len(deployments.Items)+len(statefulSets.Items))
	appendWorkload := func(kind string, metadata metav1.Object, replicas *int32) error {
		if metadata.GetDeletionTimestamp() != nil {
			return nil
		}
		if metadata.GetUID() == "" || metadata.GetResourceVersion() == "" || metadata.GetGeneration() < 1 || metadata.GetNamespace() != namespace || replicas == nil {
			return fmt.Errorf("consistency workload inventory has an incomplete resource identity")
		}
		items = append(items, NamedCapability{Type: "ConsistencyWorkload", Name: metadata.GetName(), Fields: map[string]string{
			"namespace": namespace, "kind": kind, "uid": string(metadata.GetUID()), "resourceVersion": metadata.GetResourceVersion(),
			"generation": strconv.FormatInt(metadata.GetGeneration(), 10), "replicas": strconv.FormatInt(int64(*replicas), 10),
			"offlineOptIn":      strconv.FormatBool(metadata.GetAnnotations()["hypercdr.io/allow-offline-backup"] == "true"),
			"controllerManaged": strconv.FormatBool(metav1.GetControllerOf(metadata) != nil), "verification": "read-only-inventory",
		}})
		return nil
	}
	for _, deployment := range deployments.Items {
		if err := appendWorkload("Deployment", &deployment, deployment.Spec.Replicas); err != nil {
			return nil, err
		}
	}
	for _, statefulSet := range statefulSets.Items {
		if err := appendWorkload("StatefulSet", &statefulSet, statefulSet.Spec.Replicas); err != nil {
			return nil, err
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].Fields["kind"]+"/"+items[left].Name < items[right].Fields["kind"]+"/"+items[right].Name
	})
	return items, nil
}
