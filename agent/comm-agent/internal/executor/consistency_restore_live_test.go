package executor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedProductConsistencyRestore(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_PRODUCT_RESTORE_TEST") != "1" {
		t.Skip("requires explicit isolated product restore opt-in")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG"))
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("product restore acceptance is restricted to authorized 123")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	const agent = "hcdr-consistency-velero"
	target := os.Getenv("HYPERCDR_CONSISTENCY_PRODUCT_RESTORE_NAMESPACE")
	if !strings.HasPrefix(target, "hcdr-consistency-restored-product-") || len(validation.IsDNS1123Label(target)) != 0 {
		t.Fatal("requires an explicitly named isolated product restore target")
	}
	for _, name := range []string{target, agent, "hcdr-consistency-test"} {
		namespace, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil || namespace.Labels["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatal("isolated namespace safety label is missing")
		}
	}
	claims, err := client.CoreV1().PersistentVolumeClaims(target).List(ctx, metav1.ListOptions{})
	if err != nil || len(claims.Items) != 0 {
		t.Fatal("restore target must have no existing PVCs")
	}
	deployments, err := client.AppsV1().Deployments(target).List(ctx, metav1.ListOptions{})
	if err != nil || len(deployments.Items) != 0 {
		t.Fatal("restore target must have no existing Deployments")
	}
	var evidence *protocol.ConsistencyRestoreEvidence
	if path := os.Getenv("HYPERCDR_CONSISTENCY_PUBLISHED_EVIDENCE_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &evidence); err != nil || evidence == nil || !strings.HasPrefix(evidence.Backup.VeleroBackupName, "isolated-platform-") || evidence.Backup.SourceNamespace != "hcdr-consistency-test" {
			t.Fatal("requires isolated platform publication evidence")
		}
	} else {
		data, err := os.ReadFile(os.Getenv("HYPERCDR_CONSISTENCY_ARCHIVE_TASK_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		var backupTask protocol.TaskDispatchPayload
		if err := json.Unmarshal(data, &backupTask); err != nil || backupTask.Backup == nil || !strings.HasPrefix(backupTask.Backup.VeleroBackupName, "isolated-ws-") {
			t.Fatal("requires isolated accepted backup task")
		}
		data, err = os.ReadFile(os.Getenv("HYPERCDR_CONSISTENCY_BACKUP_RESULT_FILE"))
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
					Consistency protocol.ConsistencyRecord `json:"consistency"`
				} `json:"velero"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &event); err != nil || event.Type != "agent.task.completed" || event.Payload.Status != "succeeded" || event.Payload.TaskID != backupTask.TaskID || event.Payload.CommandID != backupTask.CommandID || event.Payload.Velero.Name != backupTask.Backup.VeleroBackupName {
			t.Fatal("completion does not match source task")
		}
		evidence = &protocol.ConsistencyRestoreEvidence{TaskID: backupTask.TaskID, CommandID: backupTask.CommandID, Backup: *backupTask.Backup, Record: event.Payload.Velero.Consistency}
	}
	record := evidence.Record
	var proof struct {
		Sequence int64 `json:"sequence"`
	}
	if record.QuiescenceProof == nil || json.Unmarshal(record.QuiescenceProof.Evidence, &proof) != nil || proof.Sequence <= 0 {
		t.Fatal("captured paired-volume sequence is unknown")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", target, time.Now().UnixNano())))
	id := fmt.Sprintf("%x", digest[:16])
	task := protocol.TaskDispatchPayload{TaskID: id, CommandID: id, Type: "restore", Restore: &protocol.RestoreCommand{VeleroBackupName: evidence.Backup.VeleroBackupName, SourceNamespace: "hcdr-consistency-test", TargetNamespace: target, ConflictPolicy: "skip", ConsistencyEvidence: evidence}}
	applier := kube.NewDynamicManifestApplierWithClient(dynamicClient)
	executor := KubernetesRestoreExecutor{agentNamespace: agent, applier: applier}
	manifest, err := executor.BuildRestoreManifest(task)
	if err != nil || manifest.ConsistencyEvidence == nil {
		t.Fatalf("wire evidence did not reach product restore builder: %v", err)
	}
	t.Logf("submitting normal product executor Restore %s into %s", manifest.Metadata.Name, target)
	if err := executor.SubmitRestore(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	for {
		restore, err := dynamicClient.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}).Namespace(agent).Get(ctx, manifest.Metadata.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		phase, _, _ := unstructured.NestedString(restore.Object, "status", "phase")
		if phase == "Completed" {
			t.Logf("Velero restore completed; retained status: %v", restore.Object["status"])
			break
		}
		if phase == "Failed" || phase == "PartiallyFailed" || phase == "FailedValidation" {
			t.Fatalf("product restore reached unsuccessful phase %s: %v", phase, restore.Object["status"])
		}
		select {
		case <-ctx.Done():
			t.Fatalf("restore observation timed out; inspect same Restore before retrying: %v", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	for {
		pods, err := client.CoreV1().Pods(target).List(ctx, metav1.ListOptions{LabelSelector: "app=hcdr-consistency-writer"})
		if err == nil && len(pods.Items) == 1 && pods.Items[0].Status.Phase == corev1.PodRunning {
			logs, err := client.CoreV1().Pods(target).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{}).DoRaw(ctx)
			if err == nil {
				for _, line := range strings.Split(string(logs), "\n") {
					var entry struct {
						Event    string `json:"event"`
						Sequence int64  `json:"sequence"`
					}
					if json.Unmarshal([]byte(line), &entry) == nil && entry.Event == "validated-start" {
						if entry.Sequence != proof.Sequence {
							t.Fatalf("restored sequence %d differs from captured %d", entry.Sequence, proof.Sequence)
						}
						claims, err := client.CoreV1().PersistentVolumeClaims(target).List(ctx, metav1.ListOptions{})
						if err != nil || len(claims.Items) != 2 || claims.Items[0].Status.Phase != corev1.ClaimBound || claims.Items[1].Status.Phase != corev1.ClaimBound {
							t.Fatal("restored paired PVCs are not both Bound")
						}
						t.Logf("normal executor restored both volumes at exact captured sequence %d and resumed the workload", entry.Sequence)
						return
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("restored paired-volume invariant was not verified")
		case <-time.After(time.Second):
		}
	}
}
