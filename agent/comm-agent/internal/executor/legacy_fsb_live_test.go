package executor

import (
	"context"
	"crypto/sha256"
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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedLegacyFSBRestore(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_LEGACY_FSB_TEST") != "1" {
		t.Skip("requires explicit isolated legacy FSB acceptance opt-in")
	}
	path := os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("legacy acceptance is restricted to authorized 123")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	applier, err := kube.NewDynamicManifestApplier(path)
	if err != nil {
		t.Fatal(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	const source, target, agent = "hcdr-consistency-test", "hcdr-consistency-restored-legacy-fsb", "hcdr-consistency-velero"
	for _, name := range []string{source, target, agent} {
		namespace, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil || namespace.Labels["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatal("required isolated namespace label missing")
		}
	}
	targetPods, err := client.CoreV1().Pods(target).List(ctx, metav1.ListOptions{})
	if err != nil || len(targetPods.Items) != 0 {
		t.Fatal("legacy restore target must be empty")
	}
	nodeAgents, err := client.CoreV1().Pods(agent).List(ctx, metav1.ListOptions{LabelSelector: "role=node-agent"})
	if err != nil || len(nodeAgents.Items) != 1 || nodeAgents.Items[0].Spec.NodeName != "k8s-124" || nodeAgents.Items[0].Status.Phase != corev1.PodRunning {
		t.Fatal("legacy FSB requires a discoverable running node-agent on isolated worker 124")
	}
	workload, err := client.AppsV1().Deployments(source).Get(ctx, "writer", metav1.GetOptions{})
	if err != nil || workload.Labels["hypercdr.io/consistency-fixture"] != "writer" || len(workload.Spec.Template.Spec.Containers) != 1 {
		t.Fatal("cannot identify isolated fixture image")
	}
	id := fmt.Sprintf("isolated-fsb-%d", time.Now().UnixNano())
	digest := sha256.Sum256([]byte(id))
	taskID := fmt.Sprintf("%x", digest[:16])
	script := `import pathlib,time
path=pathlib.Path('/data/marker')
expected='` + id + `'
if path.exists():
 assert path.read_text()==expected
 print('legacy-fsb-restored '+expected,flush=True)
else:
 path.write_text(expected)
 print('legacy-fsb-created '+expected,flush=True)
time.sleep(600)
`
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: source, Labels: map[string]string{"hypercdr.io/legacy-fsb-test": id}, Annotations: map[string]string{"backup.velero.io/backup-volumes": "data"}}, Spec: corev1.PodSpec{
		NodeSelector: map[string]string{"kubernetes.io/hostname": "k8s-124"}, RestartPolicy: corev1.RestartPolicyNever,
		Containers: []corev1.Container{{Name: "fixture", Image: workload.Spec.Template.Spec.Containers[0].Image, Command: []string{"python3", "-u", "-c", script}, VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}}}},
		Volumes:    []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
	}}
	if _, err := client.CoreV1().Pods(source).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitLog := func(namespace, message string) {
		t.Helper()
		for {
			logs, err := client.CoreV1().Pods(namespace).GetLogs(id, &corev1.PodLogOptions{Container: "fixture"}).DoRaw(ctx)
			if err == nil && strings.Contains(string(logs), message+" "+id) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("fixture %s did not confirm %s: %v", namespace, message, ctx.Err())
			case <-time.After(time.Second):
			}
		}
	}
	waitLog(source, "legacy-fsb-created")
	backupExecutor := KubernetesBackupExecutor{agentNamespace: agent, applier: applier}
	backup, err := backupExecutor.BuildBackupManifest(protocol.TaskDispatchPayload{TaskID: taskID, CommandID: taskID, Type: "backup", Backup: &protocol.BackupCommand{
		SourceNamespace: source, VeleroBackupName: id, StorageRepo: "consistency-test", IncludedResources: []string{"pods"},
		LabelSelector: protocol.LabelSelector{MatchLabels: map[string]string{"hypercdr.io/legacy-fsb-test": id}},
	}})
	if err != nil || backup.Spec.DefaultVolumesToFsBackup == nil || !*backup.Spec.DefaultVolumesToFsBackup || backup.Spec.SnapshotVolumes == nil || *backup.Spec.SnapshotVolumes {
		t.Fatalf("legacy omitted protection mode no longer builds FSB: %v", err)
	}
	if err := backupExecutor.SubmitBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	t.Logf("legacy FSB backup %s submitted; no consistency mode/evidence", id)
	waitPhase := func(resource, name string) {
		t.Helper()
		for {
			object, err := dynamicClient.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: resource}).Namespace(agent).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
			if phase == "Completed" {
				return
			}
			if phase == "Failed" || phase == "PartiallyFailed" || phase == "FailedValidation" {
				t.Fatalf("legacy %s failed: %v", resource, object.Object["status"])
			}
			select {
			case <-ctx.Done():
				t.Fatal("observation expired; inspect the same objects before retrying")
			case <-time.After(time.Second):
			}
		}
	}
	waitPhase("backups", id)
	restoreExecutor := KubernetesRestoreExecutor{agentNamespace: agent, applier: applier}
	restore, err := restoreExecutor.BuildRestoreManifest(protocol.TaskDispatchPayload{TaskID: taskID, CommandID: taskID, Type: "restore", Restore: &protocol.RestoreCommand{VeleroBackupName: id, SourceNamespace: source, TargetNamespace: target, ConflictPolicy: "skip"}})
	if err != nil || restore.ConsistencyEvidence != nil {
		t.Fatalf("legacy restore unexpectedly requires consistency evidence: %v", err)
	}
	if err := restoreExecutor.SubmitRestore(ctx, restore); err != nil {
		t.Fatal(err)
	}
	waitPhase("restores", restore.Metadata.Name)
	waitLog(target, "legacy-fsb-restored")
	t.Logf("legacy FSB backup %s and restore %s completed; exact file marker restored without new consistency evidence", id, restore.Metadata.Name)
}
