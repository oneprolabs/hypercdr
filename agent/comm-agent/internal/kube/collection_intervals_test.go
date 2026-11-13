package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func resourceListCount(client *fake.Clientset, resource string) int {
	count := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == resource {
			count++
		}
	}
	return count
}

func TestClusterInventoryUsesNodeResourceAndStaticIntervals(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.30.1"}
	reader := NewKubernetesClusterReaderWithClients(client, nil, nil)
	ctx := context.Background()
	if _, err := reader.ReadClusterPeriodic(ctx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if _, err := reader.ReadClusterPeriodic(ctx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if resourceListCount(client, "nodes") != 1 || resourceListCount(client, "deployments") != 0 || resourceListCount(client, "storageclasses") != 0 {
		t.Fatalf("fast node observation performed full inventory: %#v", client.Actions())
	}
	reader.inventoryState.CollectedAt = time.Now().Add(-5 * time.Minute)
	client.ClearActions()
	if _, err := reader.ReadClusterPeriodic(ctx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if resourceListCount(client, "deployments") != 1 || resourceListCount(client, "storageclasses") != 0 {
		t.Fatal("five-minute collection did not preserve hourly static inventory")
	}
	reader.staticInventoryAt = time.Now().Add(-time.Hour)
	client.ClearActions()
	if _, err := reader.ReadClusterPeriodic(ctx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if resourceListCount(client, "storageclasses") != 1 {
		t.Fatal("hourly static inventory was not refreshed")
	}
	client.ClearActions()
	if _, err := reader.ReadCluster(ctx); err != nil {
		t.Fatal(err)
	}
	if resourceListCount(client, "deployments") != 1 || resourceListCount(client, "storageclasses") != 1 {
		t.Fatal("manual refresh reused cached inventory")
	}
}

func TestFailedResourceCollectionRetainsLastSuccessfulSnapshot(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.30.1"}
	reader := NewKubernetesClusterReaderWithClients(client, nil, nil)
	if _, err := reader.ReadClusterPeriodic(context.Background(), time.Minute); err != nil {
		t.Fatal(err)
	}
	reader.inventoryState.CollectedAt = time.Now().Add(-time.Hour)
	previous := reader.inventoryState.CollectedAt
	client.PrependReactor("list", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("unavailable") })
	if _, err := reader.ReadClusterPeriodic(context.Background(), time.Minute); err == nil {
		t.Fatal("collection failure was hidden")
	}
	if !reader.inventoryState.CollectedAt.Equal(previous) {
		t.Fatal("failed collection replaced the last successful snapshot")
	}
}

func TestRuntimeDetailsAreCachedButReadinessIsAlwaysFresh(t *testing.T) {
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: "agent"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "velero"}}}, Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "agent"}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-agent"}}}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1}},
	)
	manager := &KubernetesAgentRuntime{client: client}
	ctx := context.Background()
	if _, err := manager.VeleroRuntimeStatus(ctx, "agent"); err != nil {
		t.Fatal(err)
	}
	deployment, _ := client.AppsV1().Deployments("agent").Get(ctx, "velero", metav1.GetOptions{})
	deployment.Status.AvailableReplicas = 0
	if _, err := client.AppsV1().Deployments("agent").UpdateStatus(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	status, err := manager.VeleroRuntimeStatus(ctx, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if status.ServerReady {
		t.Fatal("readiness used an old observation")
	}
	if resourceListCount(client, "pods") != 0 || resourceListCount(client, "jobs") != 0 {
		t.Fatal("cached detail observation still listed pods or cleanup jobs")
	}
	manager.InvalidateRuntimeObservations()
	client.ClearActions()
	if _, err := manager.VeleroRuntimeStatus(ctx, "agent"); err != nil {
		t.Fatal(err)
	}
	if resourceListCount(client, "pods") != 2 {
		t.Fatal("forced refresh did not refresh component digests")
	}
}
