package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestKubernetesUninstallerDeletesNamespaceAfterPreparingAgentRBACForGarbageCollection(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent", UID: types.UID("namespace-uid")}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-velero"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-velero"}},
	)
	uninstaller := NewKubernetesUninstallerWithClient(client)

	if err := uninstaller.Uninstall(context.Background(), UninstallOptions{
		Namespace:       "hypercdr-agent",
		DeleteVelero:    true,
		DeleteNamespace: true,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := client.CoreV1().Namespaces().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected namespace deleted, got %v", err)
	}
	assertPatchedOwnerReference(t, client.Actions(), schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, "hypercdr-agent")
	assertPatchedOwnerReference(t, client.Actions(), schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, "hypercdr-agent")
	for _, name := range []string{"hypercdr-velero"} {
		if _, err := client.RbacV1().ClusterRoles().Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatalf("expected cluster role %s deleted, got %v", name, err)
		}
		if _, err := client.RbacV1().ClusterRoleBindings().Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			t.Fatalf("expected cluster role binding %s deleted, got %v", name, err)
		}
	}
}

func assertPatchedOwnerReference(t *testing.T, actions []k8stesting.Action, gvr schema.GroupVersionResource, name string) {
	t.Helper()
	for _, action := range actions {
		patch, ok := action.(k8stesting.PatchAction)
		if !ok || patch.GetResource() != gvr || patch.GetName() != name {
			continue
		}
		if patch.GetPatchType() != types.MergePatchType {
			t.Fatalf("expected merge patch for %s, got %s", name, patch.GetPatchType())
		}
		if !strings.Contains(string(patch.GetPatch()), `"kind":"Namespace"`) ||
			!strings.Contains(string(patch.GetPatch()), `"uid":"namespace-uid"`) {
			t.Fatalf("unexpected owner reference patch for %s: %s", name, string(patch.GetPatch()))
		}
		return
	}
	t.Fatalf("expected owner reference patch for %s", name)
}

func TestKubernetesUninstallerKeepsAgentNamespaceWhenPreCleanupFails(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-velero"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-velero"}},
	)
	client.Fake.PrependReactor("delete", "clusterrolebindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleteAction := action.(k8stesting.DeleteAction)
		if deleteAction.GetName() == "hypercdr-velero" {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Group: "rbac.authorization.k8s.io", Resource: "clusterrolebindings"},
				"hypercdr-velero",
				context.Canceled,
			)
		}
		return false, nil, nil
	})
	uninstaller := NewKubernetesUninstallerWithClient(client)

	if err := uninstaller.Uninstall(context.Background(), UninstallOptions{
		Namespace:       "hypercdr-agent",
		DeleteVelero:    true,
		DeleteNamespace: true,
	}); err == nil {
		t.Fatal("expected pre-cleanup error")
	}

	if _, err := client.CoreV1().Namespaces().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected agent namespace retained, got %v", err)
	}
	if _, err := client.RbacV1().ClusterRoles().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected agent cluster role retained, got %v", err)
	}
	if _, err := client.RbacV1().ClusterRoleBindings().Get(context.Background(), "hypercdr-agent", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected agent cluster role binding retained, got %v", err)
	}
}

func TestKubernetesUninstallerDeletesVeleroCRDsWhenExclusive(t *testing.T) {
	client := fake.NewSimpleClientset()
	listKinds := map[schema.GroupVersionResource]string{}
	for _, gvr := range veleroNamespacedResources() {
		listKinds[gvr] = "UnstructuredList"
	}
	crdGVR := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	listKinds[crdGVR] = "UnstructuredList"
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		listKinds,
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "backups.velero.io",
			},
		}},
	)
	uninstaller := NewKubernetesUninstallerWithClients(client, dynamicClient)

	if err := uninstaller.Uninstall(context.Background(), UninstallOptions{
		Namespace:       "hypercdr-agent",
		DeleteVelero:    true,
		DeleteNamespace: false,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := dynamicClient.Resource(crdGVR).Get(context.Background(), "backups.velero.io", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected velero CRD deleted, got %v", err)
	}
}

func TestKubernetesUninstallerKeepsVeleroCRDsWhenExternalResourcesExist(t *testing.T) {
	client := fake.NewSimpleClientset()
	backupGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}
	crdGVR := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	listKinds := map[schema.GroupVersionResource]string{}
	for _, gvr := range veleroNamespacedResources() {
		listKinds[gvr] = "UnstructuredList"
	}
	listKinds[crdGVR] = "UnstructuredList"
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		listKinds,
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "backups.velero.io",
			},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v1",
			"kind":       "Backup",
			"metadata": map[string]any{
				"name":      "external-backup",
				"namespace": "velero",
			},
		}},
	)
	uninstaller := NewKubernetesUninstallerWithClients(client, dynamicClient)

	if err := uninstaller.Uninstall(context.Background(), UninstallOptions{
		Namespace:       "hypercdr-agent",
		DeleteVelero:    true,
		DeleteNamespace: false,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := dynamicClient.Resource(crdGVR).Get(context.Background(), "backups.velero.io", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected velero CRD retained, got %v", err)
	}
	if _, err := dynamicClient.Resource(backupGVR).Namespace("velero").Get(context.Background(), "external-backup", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected external backup retained, got %v", err)
	}
}

func TestKubernetesUninstallerDeletesVeleroCRsBeforeNamespace(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-velero"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-velero"}},
	)
	restoreGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}
	pvbGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}
	listKinds := map[schema.GroupVersionResource]string{}
	for _, gvr := range veleroNamespacedResources() {
		listKinds[gvr] = "UnstructuredList"
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		listKinds,
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v1",
			"kind":       "Restore",
			"metadata": map[string]any{
				"name":       "stale-restore",
				"namespace":  "hypercdr-agent",
				"finalizers": []any{"velero.io/restore-finalizer"},
			},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v1",
			"kind":       "PodVolumeBackup",
			"metadata": map[string]any{
				"name":       "stale-pvb",
				"namespace":  "hypercdr-agent",
				"finalizers": []any{"velero.io/pod-volume-backup-finalizer"},
			},
		}},
	)
	uninstaller := NewKubernetesUninstallerWithClients(client, dynamicClient)

	if err := uninstaller.Uninstall(context.Background(), UninstallOptions{
		Namespace:       "hypercdr-agent",
		DeleteVelero:    true,
		DeleteNamespace: true,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := dynamicClient.Resource(restoreGVR).Namespace("hypercdr-agent").Get(context.Background(), "stale-restore", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected restore deleted, got %v", err)
	}
	if _, err := dynamicClient.Resource(pvbGVR).Namespace("hypercdr-agent").Get(context.Background(), "stale-pvb", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected pod volume backup deleted, got %v", err)
	}
	assertPatchedFinalizers(t, dynamicClient.Actions(), restoreGVR, "stale-restore")
	assertPatchedFinalizers(t, dynamicClient.Actions(), pvbGVR, "stale-pvb")
}

func assertPatchedFinalizers(t *testing.T, actions []k8stesting.Action, gvr schema.GroupVersionResource, name string) {
	t.Helper()
	for _, action := range actions {
		patch, ok := action.(k8stesting.PatchAction)
		if !ok || patch.GetResource() != gvr || patch.GetName() != name {
			continue
		}
		if patch.GetPatchType() != types.MergePatchType {
			t.Fatalf("expected merge patch for %s, got %s", name, patch.GetPatchType())
		}
		if string(patch.GetPatch()) != `{"metadata":{"finalizers":[]}}` {
			t.Fatalf("unexpected finalizer patch for %s: %s", name, patch.GetPatch())
		}
		return
	}
	t.Fatalf("expected finalizer patch for %s", name)
}
