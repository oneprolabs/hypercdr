package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBackupPVCMappingRefreshDoesNotRequireRestart(t *testing.T) {
	config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "configuration", Namespace: "agent"}, Data: map[string]string{"node-agent-config.json": `{"backupPVC":{"source":{"storageClass":"first"}}}`}}
	client := fake.NewSimpleClientset(config)
	reconciler := &DataUploadReconciler{kubeClient: client}
	reconciler.SetBackupPVCConfigMap("configuration")
	initial, err := reconciler.currentBackupPVCConfig("agent")
	if err != nil || initial["source"].StorageClass != "first" {
		t.Fatalf("initial mapping: %v %v", initial, err)
	}
	config.Data["node-agent-config.json"] = `{"backupPVC":{"source":{"storageClass":"second","readOnly":true}}}`
	if _, err := client.CoreV1().ConfigMaps("agent").Update(context.Background(), config, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	updated, err := reconciler.currentBackupPVCConfig("agent")
	if err != nil || updated["source"].StorageClass != "second" || initial["source"].StorageClass != "first" {
		t.Fatalf("refresh mutated existing upload or missed new mapping: %v %v", updated, err)
	}
	config.Data["node-agent-config.json"] = `{invalid`
	_, _ = client.CoreV1().ConfigMaps("agent").Update(context.Background(), config, metav1.UpdateOptions{})
	if _, err := reconciler.currentBackupPVCConfig("agent"); err == nil {
		t.Fatal("invalid current configuration silently accepted")
	}
}
