package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestCacheUsageJobIsReadOnlyAndDoesNotCreateDirectories(t *testing.T) {
	job, paths, err := cacheUsageJob("agent", "node-a", "agent@sha256:digest", nil, []string{"/data/index-cache", "/other/drill-cache"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Spec.Template.Spec.NodeName != "node-a" || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("scan job is not node-local or has credentials")
	}
	if *job.Spec.ActiveDeadlineSeconds != 240 || *job.Spec.BackoffLimit != 0 {
		t.Fatal("scan job has unbounded execution or retries")
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Command[0] != "/comm-agent" || container.Command[1] != "cache-usage" || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("scan job does not use the bounded read-only agent command")
	}
	if paths[0] != "/scan/cache-0/index-cache" || paths[1] != "/scan/cache-1/drill-cache" {
		t.Fatalf("independent paths were changed: %#v", paths)
	}
	for _, mount := range container.VolumeMounts {
		if !mount.ReadOnly {
			t.Fatal("cache was mounted writable")
		}
	}
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.HostPath == nil || *volume.HostPath.Type != corev1.HostPathDirectory {
			t.Fatal("scan job can create host directories")
		}
	}
	if _, _, err := cacheUsageJob("agent", "node-a", "image", nil, []string{"/"}); err == nil {
		t.Fatal("root scan was allowed")
	}
}

func TestCacheUsageConfigurationUsesEachActualNodeMount(t *testing.T) {
	pod := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Env: []corev1.EnvVar{
		{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_BUDGET_BYTES", Value: "5368709120"}, {Name: "HCDR_CACHE_INDEX_PATH", Value: "/old/index"},
		{Name: "HCDR_DRILL_DATA_CACHE", Value: "false"}, {Name: "HCDR_DRILL_DATA_CACHE_BUDGET_BYTES", Value: "53687091200"}, {Name: "HCDR_DRILL_DATA_CACHE_PATH", Value: "/drill/actual"},
	}, VolumeMounts: []corev1.VolumeMount{{Name: "index", MountPath: cacheRoot}}}}, Volumes: []corev1.Volume{{Name: "index", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/index/actual"}}}}}}
	index, drill := cacheUsageConfiguration(pod, false), cacheUsageConfiguration(pod, true)
	if index.Path != "/index/actual" || index.Enabled == nil || !*index.Enabled || index.MaxBytes != 5<<30 {
		t.Fatalf("index cache configuration: %#v", index)
	}
	if drill.Path != "/drill/actual" || drill.Enabled == nil || *drill.Enabled || drill.MaxBytes != 50<<30 {
		t.Fatalf("disabled drill cache configuration was lost: %#v", drill)
	}
}
