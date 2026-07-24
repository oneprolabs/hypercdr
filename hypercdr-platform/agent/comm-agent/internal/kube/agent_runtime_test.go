package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEnsureLogCollectionPermission(t *testing.T) {
	client := fake.NewSimpleClientset(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}})
	runtime := &KubernetesAgentRuntime{client: client}
	if err := runtime.ensureLogCollectionPermission(context.Background()); err != nil {
		t.Fatal(err)
	}
	role, err := client.RbacV1().ClusterRoles().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rule := range role.Rules {
		if containsString(rule.Resources, "pods/log") && containsString(rule.Verbs, "get") {
			found = true
		}
	}
	if !found {
		t.Fatalf("pods/log permission was not added: %#v", role.Rules)
	}
}

func TestVeleroRuntimeStatusRequiresEveryNodeAgentDigest(t *testing.T) {
	labels := map[string]string{"app": "velero"}
	nodeLabels := map[string]string{"app": "node-agent"}
	client := fake.NewSimpleClientset(
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"daemonsets"}, Verbs: []string{"get", "patch", "update"}}}},
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

func TestUpgradeVeleroReconcilesProviderPlugins(t *testing.T) {
	labels := map[string]string{"app": "velero"}
	nodeLabels := map[string]string{"app": "node-agent"}
	client := fake.NewSimpleClientset(
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"daemonsets"}, Verbs: []string{"get", "patch", "update"}}}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: "hypercdr-agent"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "velero", Image: "old"}}, InitContainers: []corev1.Container{{Name: "velero-plugin-for-aws", Image: "old-plugin"}}}}}, Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "hypercdr-agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: nodeLabels}, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "old"}}}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1}},
	)
	runtime := &KubernetesAgentRuntime{client: client}
	err := runtime.UpgradeVelero(context.Background(), VeleroUpgradeOptions{Namespace: "hypercdr-agent", Image: "registry/velero:new", AWSPluginImage: "registry/aws:new", AzurePluginImage: "registry/azure:new", GCPPluginImage: "registry/gcp:new"})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := client.AppsV1().Deployments("hypercdr-agent").Get(context.Background(), "velero", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	images := map[string]string{}
	for _, container := range deployment.Spec.Template.Spec.InitContainers {
		images[container.Name] = container.Image
	}
	for name, expected := range map[string]string{"velero-plugin-for-aws": "registry/aws:new", "velero-plugin-for-microsoft-azure": "registry/azure:new", "velero-plugin-for-gcp": "registry/gcp:new"} {
		if images[name] != expected {
			t.Fatalf("plugin %s image = %q, want %q", name, images[name], expected)
		}
	}
}

func readyPod(name string, labels map[string]string, container string, imageID string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "hypercdr-agent", Labels: labels}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: container, Ready: true, ImageID: imageID}}}}
}
