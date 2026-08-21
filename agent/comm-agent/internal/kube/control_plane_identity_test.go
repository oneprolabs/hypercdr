package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDetectControlPlaneIdentityPrefersSortedControlPlaneNode(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-01"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "master-02", Labels: map[string]string{"node-role.kubernetes.io/master": ""}}, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.168.7.132"}}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "master-01", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}}, Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.168.7.131"}}}},
	)
	reader := NewKubernetesClusterReaderWithClients(client, nil, nil)
	identity, err := reader.DetectControlPlaneIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Name != "master-01" || identity.InternalIP != "192.168.7.131" {
		t.Fatalf("identity = %#v", identity)
	}
}
