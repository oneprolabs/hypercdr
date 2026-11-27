package kube

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedConsistencyRestoreArchiveReadOnly(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_RESTORE_ARCHIVE_TEST") != "1" {
		t.Skip("requires explicit isolated read-only archive verification opt-in")
	}
	configPath := os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("archive verification is restricted to authorized 123")
	}
	taskData, err := os.ReadFile(os.Getenv("HYPERCDR_CONSISTENCY_ARCHIVE_TASK_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var task protocol.TaskDispatchPayload
	if err := json.Unmarshal(taskData, &task); err != nil || task.Backup == nil || !strings.HasPrefix(task.Backup.VeleroBackupName, "isolated-ws-") {
		t.Fatal("requires an isolated websocket backup task")
	}
	completionData, err := os.ReadFile(os.Getenv("HYPERCDR_CONSISTENCY_BACKUP_RESULT_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type    string `json:"type"`
		Payload struct {
			TaskID    string `json:"taskId"`
			CommandID string `json:"commandId"`
			Status    string `json:"status"`
			Velero    struct {
				Name        string                     `json:"name"`
				Namespace   string                     `json:"namespace"`
				Consistency protocol.ConsistencyRecord `json:"consistency"`
			} `json:"velero"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(completionData, &event); err != nil || event.Type != "agent.task.completed" || event.Payload.Status != "succeeded" || event.Payload.TaskID != task.TaskID || event.Payload.CommandID != task.CommandID || event.Payload.Velero.Name != task.Backup.VeleroBackupName || event.Payload.Velero.Namespace != "hcdr-consistency-velero" {
		t.Fatal("completion is not bound to the isolated task")
	}
	evidence := protocol.ConsistencyRestoreEvidence{TaskID: task.TaskID, CommandID: task.CommandID, Backup: *task.Backup, Record: event.Payload.Velero.Consistency}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range []string{"hcdr-consistency-test", "hcdr-consistency-velero"} {
		namespace, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil || namespace.Labels["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatal("isolated namespace safety label is missing")
		}
	}
	applier, err := NewDynamicManifestApplier(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := applier.VerifyConsistencyRestoreBackup(ctx, "hcdr-consistency-velero", evidence); err != nil {
		t.Fatal(err)
	}
	t.Log("actual completed Backup and archived DataUpload/Kopia identities match the isolated accepted completion; no restore or pause requested")
}
