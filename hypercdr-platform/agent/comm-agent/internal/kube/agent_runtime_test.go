package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestVeleroRuntimeStatusRequiresEveryNodeAgentDigest(t *testing.T) {
	labels := map[string]string{"app": "velero"}
	nodeLabels := map[string]string{"app": "node-agent"}
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: "hypercdr-agent"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "velero", Image: "registry/velero:v1.17.1-hcdr.1"}}}}}, Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: nodeLabels}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 2, UpdatedNumberScheduled: 2}},
		readyPod("velero-1", labels, "velero", "registry/velero@sha256:target"),
		readyPod("node-agent-1", nodeLabels, "node-agent", "registry/velero@sha256:target"),
		readyPod("node-agent-2", nodeLabels, "node-agent", "registry/velero@sha256:target"),
	)
	runtime := &KubernetesAgentRuntime{client: client}
	status, err := runtime.VeleroRuntimeStatus(context.Background(), "hypercdr-agent")
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != "v1.17.1" {
		t.Fatalf("unexpected version %q", status.Version)
	}
	if status.ImageDigest != "sha256:target" || status.NodeAgentImageDigest != "sha256:target" {
		t.Fatalf("unexpected digests: %#v", status)
	}
	if !status.ServerReady || status.NodeAgentReady != 2 || status.NodeAgentDesired != 2 {
		t.Fatalf("unexpected readiness: %#v", status)
	}
}

func readyPod(name string, labels map[string]string, container string, imageID string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "hypercdr-agent", Labels: labels}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: container, Ready: true, ImageID: imageID}}}}
}
