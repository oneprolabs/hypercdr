package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestVeleroImageVersionPreservesReleaseIdentity(t *testing.T) {
	for image, expected := range map[string]string{
		"registry/hypercdr:velero-v1.18.2-hcdr.9-build.abcdef-backup-volume-policy-v1-isolation-cache-v1": "v1.18.2-hcdr.9",
		"registry/hypercdr:velero-v1.18.2-hcdr.9-build.abcdef123456":                                      "v1.18.2-hcdr.9",
		"registry:5000/hypercdr:velero-v1.18.2-hcdr.8-backup-volume-policy-v1":                            "v1.18.2-hcdr.8-backup-volume-policy-v1",
		"registry/velero:v1.18.2-hcdr.8":                                                                  "v1.18.2-hcdr.8",
		"registry/velero:v1.18.2":                                                                         "v1.18.2",
		"registry/hypercdr:velero-v1.18.2-hcdr.8@sha256:target":                                           "v1.18.2-hcdr.8",
		"registry:5000/velero@sha256:target":                                                              "",
	} {
		if actual := imageTag(image); actual != expected {
			t.Errorf("imageTag(%q) = %q, want %q", image, actual, expected)
		}
	}
}

func TestVeleroDigestRefreshesWithoutTemplateChange(t *testing.T) {
	ctx := context.Background()
	serverLabels := map[string]string{"app": "velero"}
	nodeLabels := map[string]string{"app": "node-agent"}
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: "hypercdr-agent"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: serverLabels}}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: nodeLabels}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1}},
		readyPod("server", serverLabels, "velero", "registry/velero@sha256:old"),
		readyPod("node", nodeLabels, "node-agent", "registry/velero@sha256:target"),
	)
	manager := &KubernetesAgentRuntime{client: client}
	first, err := manager.VeleroRuntimeStatus(ctx, "hypercdr-agent")
	if err != nil || first.ImageDigest != "sha256:old" {
		t.Fatalf("initial observation: %+v, %v", first, err)
	}
	pod, err := client.CoreV1().Pods("hypercdr-agent").Get(ctx, "server", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.Status.ContainerStatuses[0].ImageID = "registry/velero@sha256:target"
	if _, err := client.CoreV1().Pods("hypercdr-agent").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	second, err := manager.VeleroRuntimeStatus(ctx, "hypercdr-agent")
	if err != nil || second.ImageDigest != "sha256:target" || second.NodeAgentImageDigest != "sha256:target" {
		t.Fatalf("cached observation hid rollout completion: %+v, %v", second, err)
	}
}

func TestVeleroDigestIgnoresTerminatingOldPods(t *testing.T) {
	oldPod := readyPod("old", nil, "velero", "registry/velero@sha256:old")
	now := metav1.Now()
	oldPod.DeletionTimestamp = &now
	newPod := readyPod("new", nil, "velero", "registry/velero@sha256:target")
	if actual := commonReadyPodDigest([]corev1.Pod{*oldPod, *newPod}, "velero", 1); actual != "sha256:target" {
		t.Fatalf("terminating pod interfered with target digest: %q", actual)
	}
}
