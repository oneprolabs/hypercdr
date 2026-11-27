package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func backupPolicySource() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "storage.k8s.io/v1", "kind": "StorageClass", "metadata": map[string]any{"name": "business", "uid": "source-uid", "annotations": map[string]any{"storageclass.kubernetes.io/is-default-class": "true"}}, "provisioner": "driver.longhorn.io", "parameters": map[string]any{"dataEngine": "v1", "numberOfReplicas": "3", "diskSelector": "fast"}, "reclaimPolicy": "Retain", "volumeBindingMode": "WaitForFirstConsumer", "allowVolumeExpansion": true}}
}

func TestBackupVolumeStorageClassPolicy(t *testing.T) {
	source := backupPolicySource()
	candidate := backupVolumeStorageClass(source)
	if candidate == nil || candidate.Object["reclaimPolicy"] != "Delete" || candidate.GetAnnotations()["storageclass.kubernetes.io/is-default-class"] != "" || candidate.Object["volumeBindingMode"] != "WaitForFirstConsumer" {
		t.Fatalf("unsafe generated class: %v", candidate)
	}
	parameters, _, _ := unstructured.NestedStringMap(candidate.Object, "parameters")
	original, _, _ := unstructured.NestedStringMap(source.Object, "parameters")
	if parameters["numberOfReplicas"] != "1" || parameters["diskSelector"] != "fast" || original["numberOfReplicas"] != "3" || candidate.GetName() != backupVolumeStorageClass(source).GetName() {
		t.Fatal("source changed, parameters lost or unstable name")
	}
	_ = unstructured.SetNestedField(source.Object, "another-pool", "parameters", "diskSelector")
	if candidate.GetName() == backupVolumeStorageClass(source).GetName() {
		t.Fatal("different backend configurations share a name")
	}
	for _, engine := range []string{"", "v3", "unknown"} {
		_ = unstructured.SetNestedField(source.Object, engine, "parameters", "dataEngine")
		if backupVolumeStorageClass(source) != nil {
			t.Fatal("unverified engine optimized")
		}
	}
	source.Object["provisioner"] = "other.csi"
	if backupVolumeStorageClass(source) != nil {
		t.Fatal("unknown driver optimized")
	}
}

func TestLonghornBackupVolumeEngines(t *testing.T) {
	for _, engine := range []string{"v1", "v2"} {
		t.Run(engine, func(t *testing.T) {
			source := backupPolicySource()
			_ = unstructured.SetNestedField(source.Object, engine, "parameters", "dataEngine")
			_ = unstructured.SetNestedField(source.Object, "blockdev", "parameters", "frontend")
			_ = unstructured.SetNestedField(source.Object, "storage-pool-a", "parameters", "nodeSelector")
			original := source.DeepCopy()
			candidate := backupVolumeStorageClass(source)
			if candidate == nil {
				t.Fatal("supported engine skipped")
			}
			parameters, _, _ := unstructured.NestedStringMap(candidate.Object, "parameters")
			if parameters["dataEngine"] != engine || parameters["numberOfReplicas"] != "1" || parameters["frontend"] != "blockdev" || parameters["nodeSelector"] != "storage-pool-a" {
				t.Fatalf("engine or backend parameters changed: %v", parameters)
			}
			if !reflect.DeepEqual(source.Object, original.Object) {
				t.Fatal("business storage class mutated")
			}
			otherEngine := source.DeepCopy()
			if engine == "v1" {
				_ = unstructured.SetNestedField(otherEngine.Object, "v2", "parameters", "dataEngine")
			} else {
				_ = unstructured.SetNestedField(otherEngine.Object, "v1", "parameters", "dataEngine")
			}
			if candidate.GetName() == backupVolumeStorageClass(otherEngine).GetName() {
				t.Fatal("different engines share a generated storage class")
			}
			_ = unstructured.SetNestedField(source.Object, "backup://existing", "parameters", "fromBackup")
			if backupVolumeStorageClass(source) != nil {
				t.Fatal("backup restore source optimized")
			}
		})
	}
}

func TestBackupVolumeMappingSafety(t *testing.T) {
	for _, engine := range []string{"v1", "v2"} {
		for _, scenario := range []string{"create", "existing", "collision", "forbidden", "conflict", "unknown", "invalid"} {
			t.Run(engine+"/"+scenario, func(t *testing.T) {
				source := backupPolicySource()
				_ = unstructured.SetNestedField(source.Object, engine, "parameters", "dataEngine")
				raw := `{"loadConcurrency":{"globalConfig":4},"cachePVC":{"storageClass":"cache"},"backupPVC":{"other":{"storageClass":"manual","readOnly":true}}}`
				if scenario == "existing" {
					raw = `{"backupPVC":{"business":{"storageClass":"manual","readOnly":true}}}`
				}
				if scenario == "invalid" {
					raw = `null`
				}
				if scenario == "unknown" {
					source.Object["provisioner"] = "unknown.csi"
				}
				config := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"namespace": "agent", "name": "node-agent-config"}, "data": map[string]any{"node-agent-config.json": raw, "extra": "keep"}}}
				objects := []runtime.Object{source, config}
				if scenario == "collision" {
					collision := backupVolumeStorageClass(source)
					collision.SetLabels(nil)
					objects = append(objects, collision)
				}
				client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{backupStorageClasses: "StorageClassList"}, objects...)
				if scenario == "forbidden" {
					client.PrependReactor("create", "storageclasses", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("forbidden") })
				}
				if scenario == "conflict" {
					attempts := 0
					client.PrependReactor("update", "configmaps", func(clienttesting.Action) (bool, runtime.Object, error) {
						attempts++
						if attempts == 1 {
							return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "node-agent-config", fmt.Errorf("concurrent change"))
						}
						return false, nil, nil
					})
				}
				applier := NewDynamicManifestApplierWithClient(client)
				err := applier.configureBackupVolume(context.Background(), "agent", "business")
				if (err != nil) != (scenario == "collision" || scenario == "forbidden" || scenario == "invalid") {
					t.Fatalf("unexpected result: %v", err)
				}
				updated, _ := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("agent").Get(context.Background(), "node-agent-config", metav1.GetOptions{})
				result, _, _ := unstructured.NestedString(updated.Object, "data", "node-agent-config.json")
				if scenario != "create" && scenario != "conflict" {
					if result != raw {
						t.Fatal("configuration unexpectedly modified")
					}
					return
				}
				var decoded map[string]any
				if err := json.Unmarshal([]byte(result), &decoded); err != nil {
					t.Fatal(err)
				}
				mappings := decoded["backupPVC"].(map[string]any)
				if mappings["business"].(map[string]any)["readOnly"] != false || mappings["other"].(map[string]any)["readOnly"] != true || decoded["loadConcurrency"] == nil || decoded["cachePVC"] == nil {
					t.Fatal("lost configuration or enabled unverified readonly")
				}
				if err := applier.configureBackupVolume(context.Background(), "agent", "business"); err != nil {
					t.Fatal(err)
				}
				classes, _ := client.Resource(backupStorageClasses).List(context.Background(), metav1.ListOptions{})
				if len(classes.Items) != 2 {
					t.Fatal("repeated calls created extra classes")
				}
			})
		}
	}
}

func TestBackupVolumePolicyUsesVerifiedRuntimeAndMultipleNamespaces(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(fmt.Sprint(supported), func(t *testing.T) {
			var workload unstructured.Unstructured
			if err := json.Unmarshal([]byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"namespace":"agent","name":"node-agent"},"spec":{"template":{"metadata":{"annotations":{"hypercdr.io/backup-pvc-refresh-supported":"v1","hypercdr.io/backup-pvc-refresh-image":"current"}},"spec":{"containers":[{"name":"node-agent","image":"current","args":["node-agent","server","--node-agent-configmap=node-agent-config"]}]}}}}`), &workload); err != nil {
				t.Fatal(err)
			}
			if !supported {
				unstructured.RemoveNestedField(workload.Object, "spec", "template", "metadata", "annotations")
			}
			source := backupPolicySource()
			other := backupPolicySource()
			other.SetName("business-two")
			config := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"namespace": "agent", "name": "node-agent-config"}, "data": map[string]any{"node-agent-config.json": `{}`}}}
			objects := []runtime.Object{&workload, source, other, config}
			for namespace, class := range map[string]string{"first": "business", "second": "business-two"} {
				objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"namespace": namespace, "name": "data"}, "spec": map[string]any{"storageClassName": class}}})
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Version: "v1", Resource: "persistentvolumeclaims"}: "PersistentVolumeClaimList", backupStorageClasses: "StorageClassList"}, objects...)
			err := NewDynamicManifestApplierWithClient(client).ConfigureBackupVolumes(context.Background(), "agent", []string{"first", "second"})
			if (err == nil) != supported {
				t.Fatalf("runtime verification failed: %v", err)
			}
			classes, err := client.Resource(backupStorageClasses).List(context.Background(), metav1.ListOptions{})
			if err != nil || (supported && len(classes.Items) != 4) || (!supported && len(classes.Items) != 2) {
				t.Fatalf("unexpected number of classes: %v %v", classes, err)
			}
		})
	}
}
