package kube

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestConsistencyWorkloadInventoryUsesCurrentScopedIdentity(t *testing.T) {
	replicas := int32(1)
	controller := true
	now := metav1.Now()
	metadata := metav1.ObjectMeta{Namespace: "app", Name: "writer", UID: "current-uid", ResourceVersion: "12", Generation: 3, Annotations: map[string]string{"hypercdr.io/allow-offline-backup": "true"}}
	deployment := &appsv1.Deployment{ObjectMeta: metadata, Spec: appsv1.DeploymentSpec{Replicas: &replicas}}
	for _, scenario := range []string{"current", "empty", "list-error", "missing-uid", "missing-version", "deleting", "managed", "wrong-scope"} {
		t.Run(scenario, func(t *testing.T) {
			current := deployment.DeepCopy()
			switch scenario {
			case "missing-uid":
				current.UID = ""
			case "missing-version":
				current.ResourceVersion = ""
			case "deleting":
				current.DeletionTimestamp = &now
			case "managed":
				current.OwnerReferences = []metav1.OwnerReference{{Kind: "Database", Controller: &controller}}
			case "wrong-scope":
				current.Namespace = "other"
			}
			client := fake.NewSimpleClientset(current, &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "outside"}})
			if scenario == "list-error" {
				client.PrependReactor("list", "statefulsets", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("forbidden")
				})
			}
			namespace := "app"
			if scenario == "empty" {
				namespace = "*"
			}
			reader := &KubernetesClusterReader{clientset: client}
			items, err := reader.ReadConsistencyWorkloadInventory(context.Background(), namespace)
			wantError := scenario == "empty" || scenario == "list-error" || scenario == "missing-uid" || scenario == "missing-version"
			if (err != nil) != wantError {
				t.Fatalf("unexpected inventory error: %v", err)
			}
			if wantError || scenario == "deleting" || scenario == "wrong-scope" {
				if len(items) != 0 {
					t.Fatal("partial, deleting or out-of-scope identities were exposed")
				}
				return
			}
			if len(items) != 1 || items[0].Fields["uid"] != "current-uid" || items[0].Fields["generation"] != "3" || items[0].Fields["resourceVersion"] != "12" || items[0].Fields["offlineOptIn"] != "true" || items[0].Fields["kind"] != "Deployment" {
				t.Fatalf("current identity lost: %+v", items)
			}
			if (items[0].Fields["controllerManaged"] == "true") != (scenario == "managed") {
				t.Fatal("controller ownership was hidden")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "list" || action.GetNamespace() != "app" {
					t.Fatalf("inventory performed unexpected access: %v", action)
				}
			}
		})
	}
}
