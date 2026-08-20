package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesClusterReaderReadsCoreInventory(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-a",
				Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
				NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.30.1"},
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"team": "platform"}},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 2},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
			Spec:       appsv1.StatefulSetSpec{Replicas: int32Ptr(1)},
			Status:     appsv1.StatefulSetStatus{ReadyReplicas: 1},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "default"},
			Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 1},
		},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "default"}},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "default"},
			Status: corev1.PersistentVolumeClaimStatus{
				Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			},
		},
	)
	clientset.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.30.1"}

	reader := NewKubernetesClusterReaderWithClients(clientset, nil, nil)
	state, err := reader.ReadCluster(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if state.KubeVersion != "v1.30.1" {
		t.Fatalf("unexpected kube version: %s", state.KubeVersion)
	}
	if len(state.Nodes) != 1 || !state.Nodes[0].Ready || state.Nodes[0].KubeletVersion != "v1.30.1" {
		t.Fatalf("unexpected nodes: %#v", state.Nodes)
	}
	if len(state.Namespaces) != 1 || state.Namespaces[0].Name != "default" {
		t.Fatalf("unexpected namespaces: %#v", state.Namespaces)
	}
	if len(state.Workloads) != 3 {
		t.Fatalf("expected 3 workloads, got %#v", state.Workloads)
	}
	if len(state.Services) != 1 || len(state.Ingresses) != 1 || len(state.ConfigMaps) != 1 || len(state.Secrets) != 1 {
		t.Fatalf("unexpected namespaced resources: %#v", state)
	}
	if len(state.PVCs) != 1 || state.PVCs[0].CapacityBytes != 10*1024*1024*1024 {
		t.Fatalf("unexpected pvcs: %#v", state.PVCs)
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func TestReadNamespaceAPIsReturnsOnlyRestorableNamespaceAndRelatedClusterTypes(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	discoveryClient := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	discoveryClient.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "actions.kio.kasten.io/v1alpha1",
			APIResources: []metav1.APIResource{
				{Name: "backupactions", Kind: "BackupAction", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "create", "delete"}},
				{Name: "emptyactions", Kind: "EmptyAction", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}},
				{Name: "clusteractions", Kind: "ClusterAction", Namespaced: false, Verbs: metav1.Verbs{"get", "list", "create", "delete"}},
			},
		},
	}
	backupAction := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "actions.kio.kasten.io/v1alpha1",
		"kind":       "BackupAction",
		"metadata": map[string]any{
			"name":      "manualbackup-test",
			"namespace": "backup-test-no-pvc",
		},
	}}
	clusterAction := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "actions.kio.kasten.io/v1alpha1",
		"kind":       "ClusterAction",
		"metadata": map[string]any{
			"name": "cluster-action",
		},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: "actions.kio.kasten.io", Version: "v1alpha1", Resource: "backupactions"}:  "BackupActionList",
			{Group: "actions.kio.kasten.io", Version: "v1alpha1", Resource: "emptyactions"}:   "EmptyActionList",
			{Group: "actions.kio.kasten.io", Version: "v1alpha1", Resource: "clusteractions"}: "ClusterActionList",
		},
		backupAction, clusterAction,
	)

	reader := NewKubernetesClusterReaderWithClients(clientset, dynamicClient, discoveryClient)
	items, err := reader.ReadNamespaceAPIs(context.Background(), "backup-test-no-pvc")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only the present restorable namespace type, got %#v", items)
	}
	byResource := map[string]NamespaceAPI{}
	for _, item := range items {
		byResource[item.Resource] = item
	}
	if item := byResource["backupactions"]; item.Scope != "namespace" || item.Namespace != "backup-test-no-pvc" || item.Kind != "BackupAction" || item.Count != 1 {
		t.Fatalf("unexpected discovered namespace API: %#v", item)
	}
	if _, ok := byResource["clusteractions"]; ok {
		t.Fatalf("unrelated cluster API leaked into namespace catalog: %#v", items)
	}
}

func TestRestorablePreferredResourcesFiltersReadOnlyAndCohabitatingEvents(t *testing.T) {
	verbs := []string{"get", "list", "create", "delete"}
	got := restorablePreferredResources([]APIResource{
		{Resource: "events", Verbs: verbs},
		{Group: "events.k8s.io", Resource: "events", Verbs: verbs},
		{Group: "apps.kio.kasten.io", Resource: "applications", Verbs: []string{"get", "list"}},
		{Group: "apps", Resource: "deployments", Verbs: verbs},
	})
	if len(got) != 2 || resourceKey(got[0]) != "events" || resourceKey(got[1]) != "deployments.apps" {
		t.Fatalf("unexpected preferred restorable resources: %#v", got)
	}
}

func TestNamespaceCatalogExcludesClusterScopedResources(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	discoveryClient := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	discoveryClient.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{Name: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "create", "delete"}},
			{Name: "persistentvolumes", Kind: "PersistentVolume", Namespaced: false, Verbs: metav1.Verbs{"get", "list", "create", "delete"}},
		},
	}}
	pvc := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": "data", "namespace": "app-a"}, "spec": map[string]any{"volumeName": "pv-a"}}}
	pv := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]any{"name": "pv-a"}}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "persistentvolumeclaims"}: "PersistentVolumeClaimList",
		{Version: "v1", Resource: "persistentvolumes"}:      "PersistentVolumeList",
	}, pvc, pv)
	reader := NewKubernetesClusterReaderWithClients(clientset, dynamicClient, discoveryClient)
	items, err := reader.ReadNamespaceAPIs(context.Background(), "app-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Scope == "cluster" || item.Resource == "persistentvolumes" {
			t.Fatalf("cluster-scoped resource leaked into namespace catalog: %#v", item)
		}
	}
}

func TestEnsureResourceDiscoveryPermissionAddsReadOnlyWildcardRule(t *testing.T) {
	clientset := fake.NewSimpleClientset(&rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"},
		Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get", "list"}}},
	})
	reader := NewKubernetesClusterReaderWithClients(clientset, nil, nil)
	if err := reader.EnsureResourceDiscoveryPermission(context.Background()); err != nil {
		t.Fatal(err)
	}
	role, err := clientset.RbacV1().ClusterRoles().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Rules) != 2 || !containsString(role.Rules[1].APIGroups, "*") || !containsString(role.Rules[1].Resources, "*") || !containsString(role.Rules[1].Verbs, "get") || !containsString(role.Rules[1].Verbs, "list") {
		t.Fatalf("read-only discovery rule missing: %#v", role.Rules)
	}
}

func TestEnsureResourceDiscoveryPermissionUsesNamespaceScopedRBAC(t *testing.T) {
	const namespace = "hypercdr-enterprise-agent"
	clientset := fake.NewSimpleClientset(
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: scopedRBACName("hypercdr-agent", namespace)}},
	)
	reader := NewKubernetesClusterReaderWithClients(clientset, nil, nil)
	reader.namespace = namespace
	if err := reader.EnsureResourceDiscoveryPermission(context.Background()); err != nil {
		t.Fatal(err)
	}
	communityRole, err := clientset.RbacV1().ClusterRoles().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(communityRole.Rules) != 0 {
		t.Fatalf("enterprise discovery modified community RBAC: %#v", communityRole.Rules)
	}
	enterpriseRole, err := clientset.RbacV1().ClusterRoles().Get(context.Background(), scopedRBACName("hypercdr-agent", namespace), metav1.GetOptions{})
	if err != nil || len(enterpriseRole.Rules) != 1 {
		t.Fatalf("enterprise scoped discovery rule missing: role=%#v err=%v", enterpriseRole, err)
	}
}
