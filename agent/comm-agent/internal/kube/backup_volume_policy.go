package kube

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
)

var backupStorageClasses = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}

type backupVolumeAdapter func(parameters map[string]string) bool

var backupVolumeAdapters = map[string]backupVolumeAdapter{
	"driver.longhorn.io": adaptLonghornBackupVolume,
}

func (a *DynamicManifestApplier) ConfigureBackupVolumes(ctx context.Context, namespace string, namespaces []string) error {
	workloads := a.client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}).Namespace(namespace)
	workload, err := workloads.Get(ctx, "node-agent", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read backup volume configuration: %w", err)
	}
	annotations, _, _ := unstructured.NestedStringMap(workload.Object, "spec", "template", "metadata", "annotations")
	containers, _, _ := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
	configured := false
	for _, container := range containers {
		fields, ok := container.(map[string]any)
		if !ok || fields["name"] != "node-agent" {
			continue
		}
		if annotations["hypercdr.io/backup-pvc-refresh-supported"] != "v1" || annotations["hypercdr.io/backup-pvc-refresh-image"] != fields["image"] {
			return fmt.Errorf("runtime lacks verified backupPVC refresh support; retaining existing configuration")
		}
		args, _, _ := unstructured.NestedStringSlice(fields, "args")
		for index, argument := range args {
			if argument == "--node-agent-configmap=node-agent-config" || (argument == "--node-agent-configmap" && index+1 < len(args) && args[index+1] == "node-agent-config") {
				configured = true
			}
		}
	}
	if !configured {
		return nil
	}
	sources := map[string]bool{}
	for _, sourceNamespace := range namespaces {
		claims, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(sourceNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, claim := range claims.Items {
			name, _, _ := unstructured.NestedString(claim.Object, "spec", "storageClassName")
			if name != "" {
				sources[name] = true
			}
		}
	}
	for source := range sources {
		if err := a.configureBackupVolume(ctx, namespace, source); err != nil {
			return fmt.Errorf("temporary volume optimization for %s: %w", source, err)
		}
	}
	return nil
}

func supportsBackupPVCRefreshImage(image string) bool {
	return strings.Contains(image, "backup-volume-policy-v1")
}

func (a *DynamicManifestApplier) configureBackupVolume(ctx context.Context, namespace, source string) error {
	configMaps := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		configMap, err := configMaps.Get(ctx, "node-agent-config", metav1.GetOptions{})
		if err != nil {
			return err
		}
		raw, _, _ := unstructured.NestedString(configMap.Object, "data", "node-agent-config.json")
		configuration := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(raw), &configuration); err != nil || configuration == nil {
			return fmt.Errorf("invalid node-agent configuration; leaving it unchanged")
		}
		mappings := map[string]json.RawMessage{}
		if existing := configuration["backupPVC"]; len(existing) > 0 {
			if err := json.Unmarshal(existing, &mappings); err != nil || mappings == nil {
				return fmt.Errorf("invalid backupPVC configuration; leaving it unchanged")
			}
		}
		if _, exists := mappings[source]; exists {
			return nil
		}
		original, err := a.client.Resource(backupStorageClasses).Get(ctx, source, metav1.GetOptions{})
		if err != nil {
			return err
		}
		candidate := backupVolumeStorageClass(original)
		if candidate == nil {
			return nil
		}
		created, err := a.client.Resource(backupStorageClasses).Create(ctx, candidate, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			created, err = a.client.Resource(backupStorageClasses).Get(ctx, candidate.GetName(), metav1.GetOptions{})
		}
		if err != nil {
			return err
		}
		if created.GetDeletionTimestamp() != nil || !reflect.DeepEqual(created.GetLabels(), candidate.GetLabels()) {
			return fmt.Errorf("temporary storage class ownership mismatch; not adopting %s", candidate.GetName())
		}
		for _, field := range []string{"provisioner", "parameters", "reclaimPolicy", "volumeBindingMode", "allowedTopologies", "allowVolumeExpansion", "mountOptions"} {
			if !reflect.DeepEqual(created.Object[field], candidate.Object[field]) {
				return fmt.Errorf("temporary storage class configuration mismatch; not adopting %s", candidate.GetName())
			}
		}
		mappings[source], err = json.Marshal(map[string]any{"storageClass": candidate.GetName(), "readOnly": false})
		if err != nil {
			return err
		}
		configuration["backupPVC"], err = json.Marshal(mappings)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(configuration)
		if err != nil {
			return err
		}
		if err := unstructured.SetNestedField(configMap.Object, string(encoded), "data", "node-agent-config.json"); err != nil {
			return err
		}
		_, err = configMaps.Update(ctx, configMap, metav1.UpdateOptions{})
		return err
	})
}

func backupVolumeStorageClass(source *unstructured.Unstructured) *unstructured.Unstructured {
	provisioner, _, _ := unstructured.NestedString(source.Object, "provisioner")
	parameters, _, _ := unstructured.NestedStringMap(source.Object, "parameters")
	if source.GetDeletionTimestamp() != nil {
		return nil
	}
	if source.GetLabels()["hypercdr.io/managed-by"] == "backup-volume-policy" {
		return nil
	}
	adapter, supported := backupVolumeAdapters[provisioner]
	if !supported || !adapter(parameters) {
		return nil
	}
	candidate := source.DeepCopy()
	delete(candidate.Object, "metadata")
	delete(candidate.Object, "status")
	_ = unstructured.SetNestedStringMap(candidate.Object, parameters, "parameters")
	candidate.Object["reclaimPolicy"] = "Delete"
	if candidate.Object["volumeBindingMode"] == nil {
		candidate.Object["volumeBindingMode"] = "Immediate"
	}
	encoded, _ := json.Marshal(map[string]any{"uid": source.GetUID(), "source": source.GetName(), "configuration": candidate.Object})
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))[:20]
	candidate.SetName("hypercdr-backup-" + digest)
	candidate.SetLabels(map[string]string{"hypercdr.io/managed-by": "backup-volume-policy", "hypercdr.io/source-class-hash": digest})
	candidate.SetAnnotations(map[string]string{"hypercdr.io/source-storage-class": source.GetName()})
	return candidate
}
