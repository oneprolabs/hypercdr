package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestCacheDirectoryStageWaitsForAllNodesBeforeRollback(t *testing.T) {
	failed := &batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}}}}
	pending := &batchv1.Job{}
	if done, err := cacheDirectoryStageResult("isolate", []*batchv1.Job{failed, pending}); done || err != nil {
		t.Fatal("isolation returned while another node could still rename its cache")
	}
	pending.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if done, err := cacheDirectoryStageResult("isolate", []*batchv1.Job{failed, pending}); done || err == nil {
		t.Fatal("terminal node failure was not reported")
	}
	if done, err := cacheDirectoryStageResult("isolate", []*batchv1.Job{pending}); !done || err != nil {
		t.Fatal("successful isolation was not reported")
	}
}

func TestCacheDirectoryStagesUseStableNamesAndParentMount(t *testing.T) {
	isolationID := strings.Repeat("a", 64)
	previousName := ""
	for _, operation := range []string{"isolate", "rollback", "delete-isolated"} {
		job, err := cacheDirectoryStageJob("hypercdr-agent", "node-one", "velero-isolation", operation, isolationID, "/data/cache/drill")
		if err != nil {
			t.Fatal(err)
		}
		if job.Name == previousName {
			t.Fatal("different operations reused a Job name")
		}
		previousName = job.Name
		repeated, err := cacheDirectoryStageJob("hypercdr-agent", "node-one", "velero-isolation", operation, isolationID, "/data/cache/drill")
		if err != nil || job.Name != repeated.Name {
			t.Fatal("stage Job identity changed on restart")
		}
		pod := job.Spec.Template.Spec
		if pod.Volumes[0].HostPath.Path != "/data/cache" || pod.Containers[0].Command[2] != "/cache-parent/drill" || pod.Containers[0].Command[3] != isolationID {
			t.Fatalf("incorrect isolation mount or arguments: %+v", pod)
		}
		if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
			t.Fatal("directory stage must not receive an API token")
		}
		expectedBackoff := int32(0)
		if operation == "delete-isolated" {
			expectedBackoff = 5
		}
		if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != expectedBackoff {
			t.Fatalf("incorrect retry policy for %s", operation)
		}
	}
}

func TestCacheDirectoryStagesRejectUnsafeArguments(t *testing.T) {
	isolationID := strings.Repeat("b", 64)
	for _, path := range []string{"/", "/cache", "relative", "/cache/../other"} {
		if _, err := cacheDirectoryStageJob("namespace", "node", "image", "isolate", isolationID, path); err == nil {
			t.Fatalf("accepted unsafe path %s", path)
		}
	}
	for _, operation := range []string{"", "clean", "isolate;invalid"} {
		if _, err := cacheDirectoryStageJob("namespace", "node", "image", operation, isolationID, "/data/cache"); err == nil {
			t.Fatalf("accepted invalid operation %s", operation)
		}
	}
	if _, err := cacheDirectoryStageJob("namespace", "node", "image", "isolate", "../invalid", "/data/cache"); err == nil {
		t.Fatal("accepted invalid isolation ID")
	}
}

func TestCacheTransitionRejectsOverlappingDirectoriesBeforeClusterAccess(t *testing.T) {
	runtime := &KubernetesAgentRuntime{}
	for _, paths := range [][]string{{"/data/cache", "/data/cache/drill"}, {"/data/cache/drill", "/data/cache"}, {"/data/cache", "/data/cache"}} {
		if err := runtime.ValidateCacheDirectoryTransition(context.Background(), "namespace", strings.Repeat("a", 64), paths, []string{"node"}); err == nil {
			t.Fatalf("accepted overlapping isolation targets: %v", paths)
		}
	}
}

func TestCacheCleanupManualRetryUsesNewJobsWithoutChangingIsolation(t *testing.T) {
	stage, err := cacheDirectoryStageJob("namespace", "node", "image", "delete-isolated", strings.Repeat("c", 64), "/data/cache")
	if err != nil {
		t.Fatal(err)
	}
	first := cacheDirectoryCleanupJobName(stage.Name, "task-one")
	if first != cacheDirectoryCleanupJobName(stage.Name, "task-one") {
		t.Fatal("restart changed cleanup Job identity")
	}
	if first == cacheDirectoryCleanupJobName(stage.Name, "manual-retry-task") {
		t.Fatal("manual retry reused exhausted Job")
	}
	if stage.Spec.Template.Spec.Containers[0].Command[3] != strings.Repeat("c", 64) {
		t.Fatal("cleanup changed fixed isolation identity")
	}
}

func TestIsolatedCleanupReportsProgressAndRetainsIdentity(t *testing.T) {
	image := "custom/velero:test-isolation-cache-v1"
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "namespace"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-isolation-supported": "v1", "hypercdr.io/cache-persistence-image": image}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: image}}},
	}}}
	client := fake.NewSimpleClientset(daemon)
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
		job.Status.Failed = 2
		job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
		return false, nil, nil
	})
	runtime := &KubernetesAgentRuntime{client: client}
	var samples [][3]int
	err := runtime.RunCacheDirectoryStageWithProgress(context.Background(), "namespace", "delete-isolated", strings.Repeat("d", 64), []string{"/data/cache"}, []string{"node-one", "node-two"}, "task-one", func(completed, total, attempts int) {
		samples = append(samples, [3]int{completed, total, attempts})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0] != [3]int{2, 2, 4} {
		t.Fatalf("missing node completion and retry progress: %v", samples)
	}
}

func TestIsolationPreflightFailureOnlyReleasesGateWhenNoJobsExist(t *testing.T) {
	image := "custom/velero:test-isolation-cache-v1"
	isolationID := strings.Repeat("e", 64)
	for _, existing := range []bool{false, true} {
		daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "namespace"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-isolation-supported": "v1", "hypercdr.io/cache-persistence-image": image}},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: image, Env: []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_INDEX_PATH", Value: "/data/cache"}}}}},
		}}}
		client := fake.NewSimpleClientset(daemon)
		if existing {
			job, err := cacheDirectoryStageJob("namespace", "node", image, "isolate", isolationID, "/data/cache/old")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.BatchV1().Jobs("namespace").Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		runtime := &KubernetesAgentRuntime{client: client}
		err := runtime.RunCacheDirectoryStage(context.Background(), "namespace", "isolate", isolationID, []string{"/data/cache/old"}, []string{"node"})
		if err == nil || errors.Is(err, ErrCacheDirectoryStageFailed) == existing {
			t.Fatalf("unsafe preflight classification with existing=%v: %v", existing, err)
		}
	}
}
