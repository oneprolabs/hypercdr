package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestConfigureCacheDefaultsDisableAndCapability(t *testing.T) {
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1", "hypercdr.io/cache-persistence-image": "velero:cache-v1"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "velero:cache-v1"}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1}}
	client := fake.NewSimpleClientset(daemon)
	runtime := &KubernetesAgentRuntime{client: client}
	for _, enabled := range []bool{true, false} {
		if err := runtime.ConfigureCache(context.Background(), "", enabled, 5, false); err != nil {
			t.Fatal(err)
		}
		got, err := client.AppsV1().DaemonSets("hypercdr-agent").Get(context.Background(), "node-agent", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Spec.Template.Spec.Volumes) != 1 || len(got.Spec.Template.Spec.Containers[0].VolumeMounts) != 1 {
			t.Fatal("missing cache mount or duplicate mounts")
		}
		env := got.Spec.Template.Spec.Containers[0].Env
		if len(env) != 2 || env[1].Value != "5368709120" || (env[0].Value == "true") != enabled {
			t.Fatalf("wrong settings: %+v", env)
		}
	}
	if err := runtime.ConfigureCache(context.Background(), "", true, 0, false); err == nil {
		t.Fatal("invalid size accepted")
	}
	daemon.Spec.Template.Spec.Containers[0].Image = "old-image"
	if _, err := client.AppsV1().DaemonSets("hypercdr-agent").Update(context.Background(), daemon, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ConfigureCache(context.Background(), "", true, 5, false); err == nil {
		t.Fatal("unsupported image accepted")
	}
}

func TestOfficialCacheImageRepairsMissingCapability(t *testing.T) {
	image := "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5"
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: image}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1}}
	client := fake.NewSimpleClientset(daemon)
	manager := &KubernetesAgentRuntime{client: client}
	if err := manager.ConfigureCache(context.Background(), "", true, 5, false); err != nil {
		t.Fatal(err)
	}
	updated, err := client.AppsV1().DaemonSets("hypercdr-agent").Get(context.Background(), "node-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] != "v1" || updated.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != image || len(updated.Spec.Template.Spec.Volumes) != 1 {
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
		{"registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.4", false},
		{"custom/velero:velero-v1.18.2-hcdr.5", false},
		{"custom/velero:test-cache-v1-other", false},
	} {
		if got := supportsPersistentCache(test.image); got != test.want {
			t.Errorf("supportsPersistentCache(%q) = %v, want %v", test.image, got, test.want)
		}
	}
}

func TestObservedCacheStateRequiresActualRolloutAndMount(t *testing.T) {
	daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1", "hypercdr.io/cache-persistence-image": "image"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "image", Env: []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_BUDGET_BYTES", Value: "5368709120"}}, VolumeMounts: []corev1.VolumeMount{{Name: "cache", MountPath: cacheRoot}}}}, Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/hypercdr/kopia-cache"}}}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, UpdatedNumberScheduled: 2, NumberReady: 2, NumberAvailable: 2}}
	state := observedCacheState(daemon)
	if state["phase"] != "applied" || state["enabled"] != true || state["maxGiB"] != 5 {
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
		if pod.Volumes[0].HostPath.Path != "/var/lib/hypercdr/kopia-cache" || pod.NodeName != "node-one" {
			t.Fatal("cleanup targets wrong node/path")
		}
		job.Name = "cleanup-test"
		job.Status.Succeeded = 1
		return false, nil, nil
	})
	runtimeManager := &KubernetesAgentRuntime{client: client}
	if err := runtimeManager.ConfigureCache(context.Background(), "", true, 5, true); err != nil {
		t.Fatal(err)
	}
}
