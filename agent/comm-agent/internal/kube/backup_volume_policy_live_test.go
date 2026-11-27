package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

func TestLiveIsolatedBackupVolumePolicy(t *testing.T) {
	path := os.Getenv("HYPERCDR_BACKUP_POLICY_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("explicit isolated backup policy test kubeconfig required")
	}
	config, err := BuildRESTConfig(path)
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("isolated backup policy validation is restricted to cluster 123")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := fmt.Sprintf("hcdr-backup-policy-%d", time.Now().Unix())
	original, err := client.StorageV1().StorageClasses().Get(ctx, "longhorn", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fixture := original.DeepCopy()
	fixture.ObjectMeta = metav1.ObjectMeta{Name: name, Labels: map[string]string{"hypercdr.io/isolated-test": "backup-volume-policy"}}
	mode := storagev1.VolumeBindingWaitForFirstConsumer
	fixture.VolumeBindingMode = &mode
	if _, err := client.StorageV1().StorageClasses().Create(ctx, fixture, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.StorageV1().StorageClasses().Delete(context.Background(), name, metav1.DeleteOptions{})
	})
	if _, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"hypercdr.io/isolated-test": "backup-volume-policy"}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{}) })
	if _, err := client.CoreV1().PersistentVolumeClaims(name).Create(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &name, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	config.Impersonate.UserName = "system:serviceaccount:hypercdr-agent:hypercdr-agent"
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewDynamicManifestApplierWithClient(dynamicClient).ConfigureBackupVolumes(ctx, "hypercdr-agent", []string{name}); err != nil {
		t.Fatal(err)
	}
	configMap, err := client.CoreV1().ConfigMaps("hypercdr-agent").Get(ctx, "node-agent-config", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var configuration struct {
		BackupPVC map[string]struct {
			StorageClass string `json:"storageClass"`
			ReadOnly     bool   `json:"readOnly"`
		} `json:"backupPVC"`
	}
	if err := json.Unmarshal([]byte(configMap.Data["node-agent-config.json"]), &configuration); err != nil {
		t.Fatal(err)
	}
	selection := configuration.BackupPVC[name]
	if selection.StorageClass == "" || selection.ReadOnly {
		t.Fatalf("unsafe generated mapping: %+v", selection)
	}
	t.Cleanup(func() {
		_ = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, err := client.CoreV1().ConfigMaps("hypercdr-agent").Get(context.Background(), "node-agent-config", metav1.GetOptions{})
			if err != nil {
				return err
			}
			var contents map[string]json.RawMessage
			if err := json.Unmarshal([]byte(current.Data["node-agent-config.json"]), &contents); err != nil {
				return err
			}
			var mappings map[string]json.RawMessage
			if err := json.Unmarshal(contents["backupPVC"], &mappings); err != nil {
				return err
			}
			delete(mappings, name)
			contents["backupPVC"], _ = json.Marshal(mappings)
			encoded, _ := json.Marshal(contents)
			current.Data["node-agent-config.json"] = string(encoded)
			_, err = client.CoreV1().ConfigMaps("hypercdr-agent").Update(context.Background(), current, metav1.UpdateOptions{})
			return err
		})
		_ = client.StorageV1().StorageClasses().Delete(context.Background(), selection.StorageClass, metav1.DeleteOptions{})
	})
	generated, err := client.StorageV1().StorageClasses().Get(ctx, selection.StorageClass, metav1.GetOptions{})
	if err != nil || generated.Parameters["numberOfReplicas"] != "1" || generated.Provisioner != original.Provisioner || generated.Labels["hypercdr.io/managed-by"] != "backup-volume-policy" {
		t.Fatalf("invalid generated class: %v %v", generated, err)
	}
	if err := NewDynamicManifestApplierWithClient(dynamicClient).ConfigureBackupVolumes(ctx, "hypercdr-agent", []string{name}); err != nil {
		t.Fatal(err)
	}
	t.Logf("Agent service account created and reused %s, readOnly=false; isolated PVC has no consumer and allocates no data volume", selection.StorageClass)
}
