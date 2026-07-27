package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSecretCredentialStoreSaveLoadAndUpdate(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}})
	store := NewSecretCredentialStoreWithClient(client, "hypercdr-agent", "hypercdr-agent-credential")

	if _, ok, err := store.Load(context.Background()); err != nil || ok {
		t.Fatalf("expected empty credential before save, ok=%v err=%v", ok, err)
	}

	first := AgentCredential{ClusterID: "cluster-a", Credential: "cred-a"}
	if err := store.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded != first {
		t.Fatalf("unexpected loaded credential: %#v ok=%v", loaded, ok)
	}

	second := AgentCredential{ClusterID: "cluster-b", Credential: "cred-b"}
	if err := store.Save(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded != second {
		t.Fatalf("unexpected updated credential: %#v ok=%v", loaded, ok)
	}
}
