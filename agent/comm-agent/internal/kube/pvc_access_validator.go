package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// PVCAccessValidator enforces a backend qualification boundary before a
// backup is accepted. OADP phase one qualifies only ReadWriteOnce volumes.
type PVCAccessValidator interface {
	RequireRWO(ctx context.Context, namespaces []string) error
}

type KubernetesPVCAccessValidator struct{ client dynamic.Interface }

func NewKubernetesPVCAccessValidator(kubeconfigPath string) (*KubernetesPVCAccessValidator, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubernetesPVCAccessValidator{client: client}, nil
}

func NewKubernetesPVCAccessValidatorWithClient(client dynamic.Interface) *KubernetesPVCAccessValidator {
	return &KubernetesPVCAccessValidator{client: client}
}

func (v *KubernetesPVCAccessValidator) RequireRWO(ctx context.Context, namespaces []string) error {
	if v == nil || v.client == nil {
		return fmt.Errorf("PVC access-mode validator is not configured")
	}
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	var unsupported []string
	seen := map[string]struct{}{}
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		list, err := v.client.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list PVCs in namespace %s: %w", namespace, err)
		}
		for _, pvc := range list.Items {
			modes, _, _ := unstructuredStringSlice(pvc.Object, "spec", "accessModes")
			if len(modes) != 1 || modes[0] != "ReadWriteOnce" {
				unsupported = append(unsupported, namespace+"/"+pvc.GetName()+" ("+strings.Join(modes, ",")+")")
			}
		}
	}
	sort.Strings(unsupported)
	if len(unsupported) > 0 {
		return fmt.Errorf("OADP phase one supports ReadWriteOnce PVCs only; unsupported PVCs: %s", strings.Join(unsupported, "; "))
	}
	return nil
}

func unstructuredStringSlice(object map[string]any, fields ...string) ([]string, bool, error) {
	current := any(object)
	for _, field := range fields {
		values, ok := current.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		current, ok = values[field]
		if !ok {
			return nil, false, nil
		}
	}
	raw, ok := current.([]any)
	if !ok {
		return nil, false, nil
	}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if !ok {
			return nil, false, fmt.Errorf("accessModes contains a non-string value")
		}
		result = append(result, text)
	}
	return result, true, nil
}
