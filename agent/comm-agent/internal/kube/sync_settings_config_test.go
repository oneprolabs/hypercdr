package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSyncSettingsPreserveCacheAndSnapshotConfiguration(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "node-agent-config", Namespace: "agent"}, Data: map[string]string{"node-agent-config.json": `{"cachePVC":{"storageClass":"custom-cache","residentThresholdInMB":2048},"backupPVC":{"source":{"storageClass":"temporary"}}}`, "extra": "keep"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "backup-repository-config", Namespace: "agent"}, Data: map[string]string{"kopia": `{"cacheLimitMB":1234,"description":"keep"}`, "extra": "keep"}},
	)
	runtime := &KubernetesAgentRuntime{client: client}
	if err := runtime.ensureVeleroPerformanceConfig(context.Background(), "agent", VeleroUpgradeOptions{NodeAgentConcurrency: 6, PrepareQueueLength: 4}); err != nil {
		t.Fatal(err)
	}
	config, err := client.CoreV1().ConfigMaps("agent").Get(context.Background(), "node-agent-config", metav1.GetOptions{})
	if err != nil || !strings.Contains(config.Data["node-agent-config.json"], `"storageClass":"custom-cache"`) || !strings.Contains(config.Data["node-agent-config.json"], `"storageClass":"temporary"`) || !strings.Contains(config.Data["node-agent-config.json"], `"globalConfig":6`) || config.Data["extra"] != "keep" {
		t.Fatalf("lost node configuration: %+v %v", config, err)
	}
	repository, err := client.CoreV1().ConfigMaps("agent").Get(context.Background(), "backup-repository-config", metav1.GetOptions{})
	if err != nil || repository.Data["kopia"] != `{"cacheLimitMB":1234,"description":"keep"}` || repository.Data["extra"] != "keep" {
		t.Fatalf("lost repository config: %+v %v", repository, err)
	}
}
