package kube

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestCacheCleanupTargetProtectsEnabledCache(t *testing.T) {
	environment := []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_INDEX_PATH", Value: "/cache/index"}}
	for _, path := range []string{"/", "/cache", "/cache/index", "/cache/index/child", "relative", "/cache/../index"} {
		if err := validateCacheCleanupTarget(path, environment); err == nil {
			t.Errorf("unsafe target accepted: %s", path)
		}
	}
	if err := validateCacheCleanupTarget("/cache/old-index", environment); err != nil {
		t.Fatal(err)
	}
	environment[0].Value = "false"
	if err := validateCacheCleanupTarget("/cache/index", environment); err != nil {
		t.Fatal(err)
	}
}

func TestCacheHostPathsAreNotContainerMountPaths(t *testing.T) {
	daemon := &appsv1.DaemonSet{Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "node-agent", Env: []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_INDEX_PATH", Value: cacheRoot}, {Name: "HCDR_DRILL_DATA_CACHE", Value: "true"}, {Name: "HCDR_DRILL_DATA_CACHE_PATH", Value: cacheRoot + "/drill-data"}}, VolumeMounts: []corev1.VolumeMount{{Name: "index", MountPath: cacheRoot}, {Name: "drill", MountPath: cacheRoot + "/drill-data"}}}},
		Volumes:    []corev1.Volume{{Name: "index", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/node/index"}}}, {Name: "drill", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/node/drill"}}}},
	}}}}
	environment := cacheHostEnvironment(daemon)
	for _, path := range []string{"/node/index", "/node/drill", "/node"} {
		if err := validateCacheCleanupTarget(path, environment); err == nil {
			t.Errorf("allowed cleanup of active host directory %s", path)
		}
	}
	if err := validateCacheCleanupTarget("/node/old-drill", environment); err != nil {
		t.Fatal(err)
	}
	if daemon.Spec.Template.Spec.Containers[0].Env[1].Value != cacheRoot {
		t.Fatal("host path resolution mutated container environment")
	}
}

func TestCacheCleanupReportsCompletedNodeDirectories(t *testing.T) {
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-agent"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "velero-cache-v1"}}}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "node-agent-one", Namespace: "hypercdr-agent", Labels: map[string]string{"app": "node-agent"}}, Spec: corev1.PodSpec{NodeName: "node-one"}}
	duplicatePod := pod.DeepCopy()
	duplicatePod.Name = "node-agent-replacement"
	client := fake.NewSimpleClientset(daemon, pod, duplicatePod)
	jobLabels := map[string]string{}
	createdJobs := 0
	jobName := ""
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
		createdJobs++
		if jobName != "" && job.Name != jobName {
			t.Error("resuming a task must reuse the same Job name")
		}
		jobName = job.Name
		if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever || job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 5 {
			t.Error("cleanup must use bounded Job retries with failed Pod accounting")
		}
		jobLabels = job.Labels
		if createdJobs > 1 {
			return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "batch", Resource: "jobs"}, job.Name)
		}
		return true, job, nil
	})
	client.PrependReactor("list", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &batchv1.JobList{Items: []batchv1.Job{{ObjectMeta: metav1.ObjectMeta{Labels: jobLabels}, Status: batchv1.JobStatus{Succeeded: 1}}}}, nil
	})
	manager := &KubernetesAgentRuntime{client: client}
	reported := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := manager.ScheduleCacheCleanupForTask(ctx, "hypercdr-agent", "cleanup-task-one", []string{"/cache/old"}, func(completed, total, failed int) {
		reported = true
		if completed != 1 || total != 1 || failed != 0 {
			t.Errorf("unexpected progress: %d/%d, failed=%d", completed, total, failed)
		}
	})
	if err != nil || !reported {
		t.Fatalf("cleanup error=%v reported=%v", err, reported)
	}
	if createdJobs != 1 {
		t.Fatalf("expected one cleanup per node, got %d", createdJobs)
	}
	if err := manager.ScheduleCacheCleanupForTask(ctx, "hypercdr-agent", "cleanup-task-one", []string{"/cache/old"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCacheCleanupProtectsLegacyIndexPath(t *testing.T) {
	for _, hostRoot := range []string{"", "/legacy/cache"} {
		environment := []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_HOST_ROOT", Value: hostRoot}}
		activePath := hostRoot
		if activePath == "" {
			activePath = defaultCacheHostRoot
		}
		if err := validateCacheCleanupTarget(activePath, environment); err == nil {
			t.Fatalf("accepted active legacy cache path %s", activePath)
		}
	}
}

func TestCacheCleanupReportsExhaustedJobRetries(t *testing.T) {
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-agent"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "velero-cache-v1"}}}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "node-agent-one", Namespace: "hypercdr-agent", Labels: map[string]string{"app": "node-agent"}}, Spec: corev1.PodSpec{NodeName: "node-one"}}
	client := fake.NewSimpleClientset(daemon, pod)
	var cleanupJob *batchv1.Job
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		cleanupJob = action.(ktesting.CreateAction).GetObject().(*batchv1.Job).DeepCopy()
		return true, cleanupJob, nil
	})
	client.PrependReactor("list", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		cleanupJob.Status = batchv1.JobStatus{Failed: 6, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"}}}
		return true, &batchv1.JobList{Items: []batchv1.Job{*cleanupJob}}, nil
	})
	manager := &KubernetesAgentRuntime{client: client}
	err := manager.ScheduleCacheCleanupForTask(context.Background(), "hypercdr-agent", "exhausted-task", []string{"/cache/old"}, nil)
	if err == nil || !strings.Contains(err.Error(), "BackoffLimitExceeded") {
		t.Fatalf("expected exhausted retry failure, got %v", err)
	}
}

func TestConfigureCacheDefaultsDisableAndCapability(t *testing.T) {
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1", "hypercdr.io/cache-persistence-image": "velero-cache-v1"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "velero-cache-v1"}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1}}
	client := fake.NewSimpleClientset(daemon)
	runtime := &KubernetesAgentRuntime{client: client}
	for _, enabled := range []bool{true, false} {
		if err := runtime.ConfigureCache(context.Background(), "", "", "", "", enabled, 5, false, 50, false); err != nil {
			t.Fatal(err)
		}
		got, err := client.AppsV1().DaemonSets("hypercdr-agent").Get(context.Background(), "node-agent", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Spec.Template.Spec.Volumes) != 2 || len(got.Spec.Template.Spec.Containers[0].VolumeMounts) != 2 {
			t.Fatal("missing cache mount or duplicate mounts")
		}
		env := got.Spec.Template.Spec.Containers[0].Env
		if len(env) < 7 || env[1].Value != "5368709120" || (env[0].Value == "true") != enabled {
			t.Fatalf("wrong settings: %+v", env)
		}
	}
	if err := runtime.ConfigureCache(context.Background(), "", "", "", "", true, 0, false, 50, false); err == nil {
		t.Fatal("invalid size accepted")
	}
	daemon.Spec.Template.Spec.Containers[0].Image = "old-image"
	if _, err := client.AppsV1().DaemonSets("hypercdr-agent").Update(context.Background(), daemon, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ConfigureCache(context.Background(), "", "", "", "", true, 5, false, 50, false); err == nil {
		t.Fatal("unsupported image accepted")
	}
}

func TestOfficialCacheImageRepairsMissingCapability(t *testing.T) {
	image := "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5"
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: image}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1}}
	client := fake.NewSimpleClientset(daemon)
	manager := &KubernetesAgentRuntime{client: client}
	if err := manager.ConfigureCache(context.Background(), "", "", "", "", true, 5, false, 50, false); err != nil {
		t.Fatal(err)
	}
	updated, err := client.AppsV1().DaemonSets("hypercdr-agent").Get(context.Background(), "node-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] != "v1" || updated.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != image || len(updated.Spec.Template.Spec.Volumes) != 2 {
		t.Fatal("official image capability and cache mount were not repaired")
	}
}

func TestSupportsPersistentCache(t *testing.T) {
	for _, test := range []struct {
		image string
		want  bool
	}{
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5", true},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6", true},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5@sha256:abcd", true},
		{"custom/velero:test-cache-v1", true},
		{"custom/velero:test-cache-v1@sha256:abcd", true},
		{"registry.example/hypercdr/velero:v1.18.2-hcdr.6-fix3-drill-cache-20261110", true},
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.4", false},
		{"custom/velero:velero-v1.18.2-hcdr.5", false},
		{"custom/velero:test-cache-v1-other", false},
	} {
		if got := supportsPersistentCache(test.image); got != test.want {
			t.Errorf("supportsPersistentCache(%q) = %v, want %v", test.image, got, test.want)
		}
	}
}

func TestCacheIsolationImageCapabilities(t *testing.T) {
	for _, image := range []string{"custom/velero:dev-isolation-cache-v1", "custom/velero:dev-isolation-cache-v1@sha256:abcd", "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.7", "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.7@sha256:abcd"} {
		if !supportsCacheIsolationImage(image) || !supportsPersistentCache(image) || !supportsOnDemandImage(image) {
			t.Fatalf("isolation image missing required capabilities: %s", image)
		}
	}
	for _, image := range []string{"custom/velero:dev-cache-v1", "custom/velero:dev-isolation-cache-v1-other"} {
		if supportsCacheIsolationImage(image) {
			t.Fatalf("unqualified isolation image: %s", image)
		}
	}
}

func TestObservedCacheStateRequiresActualRolloutAndMount(t *testing.T) {
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1", "hypercdr.io/cache-persistence-image": "image"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "image", Env: []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_BUDGET_BYTES", Value: "5368709120"}}, VolumeMounts: []corev1.VolumeMount{{Name: "cache", MountPath: cacheRoot}}}}, Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/hypercdr/kopia-cache"}}}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, UpdatedNumberScheduled: 2, NumberReady: 2, NumberAvailable: 2}}
	state := observedCacheState(daemon)
	if state["phase"] != "applied" || state["enabled"] != true || state["maxGiB"] != 5 || state["indexPath"] != "/var/lib/hypercdr/kopia-cache" {
		t.Fatalf("wrong actual settings: %+v", state)
	}
	daemon.Status.UpdatedNumberScheduled = 1
	state = observedCacheState(daemon)
	if state["phase"] != "applying" || state["enabled"] != nil {
		t.Fatalf("incomplete rollout reported as applied: %+v", state)
	}
	daemon.Status.UpdatedNumberScheduled = 2
	daemon.Spec.Template.Spec.Containers[0].VolumeMounts = nil
	if state = observedCacheState(daemon); state["phase"] != "unknown" {
		t.Fatalf("missing mount reported as applied: %+v", state)
	}
	daemon.Spec.Template.Spec.Containers[0].Env[0].Value = "false"
	state = observedCacheState(daemon)
	if state["phase"] != "applied" || state["enabled"] != false || state["maxGiB"] != 5 {
		t.Fatalf("disabled cache not reflected: %+v", state)
	}
	daemon.Spec.Template.Spec.Containers[0].Env = nil
	if state = observedCacheState(daemon); state["phase"] != "unconfigured" || state["enabled"] != nil {
		t.Fatalf("missing configuration reported as enabled: %+v", state)
	}
}

func TestCacheCleanupUsesLocalRootWithoutServiceAccountToken(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "node-agent"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1", "hypercdr.io/cache-persistence-image": "image"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "image"}}}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent", Labels: map[string]string{"role": "node-agent"}}, Spec: corev1.PodSpec{NodeName: "node-one", Containers: []corev1.Container{{Image: "image"}}}},
	)
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batchv1.Job)
		pod := job.Spec.Template.Spec
		if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || pod.SecurityContext == nil || pod.SecurityContext.RunAsUser == nil || *pod.SecurityContext.RunAsUser != 0 {
			t.Fatal("unexpected cleanup identity")
		}
		if pod.Volumes[0].HostPath.Path != "/var/lib/hypercdr/cache/index" || pod.NodeName != "node-one" {
			t.Fatal("cleanup targets wrong node/path")
		}
		job.Name = "cleanup-test"
		job.Status.Succeeded = 1
		return false, nil, nil
	})
	runtimeManager := &KubernetesAgentRuntime{client: client}
	if err := runtimeManager.ConfigureCache(context.Background(), "", "", "", "", true, 5, false, 50, true); err != nil {
		t.Fatal(err)
	}
}
