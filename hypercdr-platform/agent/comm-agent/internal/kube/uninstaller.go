package kube

import (
	"context"
	"encoding/json"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type UninstallOptions struct {
	Namespace       string
	DeleteVelero    bool
	DeleteNamespace bool
}

type Uninstaller interface {
	Uninstall(ctx context.Context, options UninstallOptions) error
}

type KubernetesUninstaller struct {
	client        kubernetes.Interface
	dynamicClient dynamic.Interface
}

func NewKubernetesUninstaller(kubeconfigPath string) (*KubernetesUninstaller, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubernetesUninstaller{client: client, dynamicClient: dynamicClient}, nil
}

func NewKubernetesUninstallerWithClient(client kubernetes.Interface) *KubernetesUninstaller {
	return &KubernetesUninstaller{client: client}
}

func NewKubernetesUninstallerWithClients(client kubernetes.Interface, dynamicClient dynamic.Interface) *KubernetesUninstaller {
	return &KubernetesUninstaller{client: client, dynamicClient: dynamicClient}
}

func (u *KubernetesUninstaller) Uninstall(ctx context.Context, options UninstallOptions) error {
	if options.Namespace == "" {
		options.Namespace = "hypercdr-agent"
	}

	// Self-removal must be the final phase. If any cluster-scoped or Velero
	// cleanup fails, keep the agent namespace alive so a retry can finish the job.
	var preSelfRemovalErrs []error
	if options.DeleteVelero {
		if err := u.deleteVeleroNamespacedResources(ctx, options.Namespace); err != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	if !options.DeleteNamespace {
		if err := u.deleteNamespacedAgentResources(ctx, options); err != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	for _, name := range uninstallExternalClusterRBACNames(options.DeleteVelero) {
		if err := u.client.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
		if err := u.client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	if options.DeleteVelero {
		if err := u.deleteVeleroCRDs(ctx, options.Namespace); err != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	if err := errors.Join(preSelfRemovalErrs...); err != nil {
		return err
	}

	var errs []error
	if options.DeleteNamespace {
		if err := u.attachAgentRBACOwnerReferences(ctx, options.Namespace); err != nil {
			return err
		}
		if err := u.client.CoreV1().Namespaces().Delete(ctx, options.Namespace, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	} else {
		for _, name := range uninstallAgentClusterRBACNames() {
			if err := u.client.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
			if err := u.client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (u *KubernetesUninstaller) attachAgentRBACOwnerReferences(ctx context.Context, namespace string) error {
	ns, err := u.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if ignoreNotFound(err) != nil {
		return err
	}
	if err != nil {
		return nil
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"ownerReferences": []map[string]any{{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"name":       ns.Name,
				"uid":        string(ns.UID),
			}},
		},
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range uninstallAgentClusterRBACNames() {
		if _, err := u.client.RbacV1().ClusterRoles().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
		if _, err := u.client.RbacV1().ClusterRoleBindings().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (u *KubernetesUninstaller) deleteVeleroNamespacedResources(ctx context.Context, namespace string) error {
	if u.dynamicClient == nil {
		return nil
	}
	var errs []error
	for _, gvr := range veleroNamespacedResources() {
		resource := u.dynamicClient.Resource(gvr).Namespace(namespace)
		list, err := resource.List(ctx, metav1.ListOptions{})
		if ignoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			name := item.GetName()
			if len(item.GetFinalizers()) > 0 {
				_, err := resource.Patch(ctx, name, types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`), metav1.PatchOptions{})
				if ignoreNotFound(err) != nil {
					errs = append(errs, err)
				}
			}
			if err := resource.Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (u *KubernetesUninstaller) deleteNamespacedAgentResources(ctx context.Context, options UninstallOptions) error {
	var errs []error
	namespace := options.Namespace
	if options.DeleteVelero {
		for _, name := range []string{"velero"} {
			if err := u.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
		for _, name := range []string{"node-agent"} {
			if err := u.client.AppsV1().DaemonSets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
		for _, name := range []string{"node-agent-config"} {
			if err := u.client.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
		for _, name := range []string{"velero", "hypercdr-velero"} {
			if err := u.client.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, name := range []string{"hypercdr-agent", "hypercdr-agent-bootstrap", "hypercdr-agent-credential"} {
		if err := u.client.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range []string{"hypercdr-agent"} {
		if err := u.client.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range []string{"hypercdr-comm-agent"} {
		if err := u.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func veleroNamespacedResources() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{
		{Group: "velero.io", Version: "v1", Resource: "backups"},
		{Group: "velero.io", Version: "v1", Resource: "backuprepositories"},
		{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"},
		{Group: "velero.io", Version: "v2alpha1", Resource: "datadownloads"},
		{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"},
		{Group: "velero.io", Version: "v1", Resource: "deletebackuprequests"},
		{Group: "velero.io", Version: "v1", Resource: "downloadrequests"},
		{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"},
		{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"},
		{Group: "velero.io", Version: "v1", Resource: "restores"},
		{Group: "velero.io", Version: "v1", Resource: "schedules"},
		{Group: "velero.io", Version: "v1", Resource: "serverstatusrequests"},
		{Group: "velero.io", Version: "v1", Resource: "volumesnapshotlocations"},
	}
}

func (u *KubernetesUninstaller) deleteVeleroCRDs(ctx context.Context, namespace string) error {
	if u.dynamicClient == nil {
		return nil
	}
	hasExternal, err := u.hasVeleroResourcesOutsideNamespace(ctx, namespace)
	if err != nil {
		return err
	}
	if hasExternal {
		return nil
	}
	var errs []error
	crds := u.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	})
	for _, name := range veleroCRDNames() {
		if err := crds.Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (u *KubernetesUninstaller) hasVeleroResourcesOutsideNamespace(ctx context.Context, namespace string) (bool, error) {
	for _, gvr := range veleroNamespacedResources() {
		list, err := u.dynamicClient.Resource(gvr).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if ignoreNotFound(err) != nil {
			return false, err
		}
		if err != nil {
			continue
		}
		for _, item := range list.Items {
			if item.GetNamespace() != "" && item.GetNamespace() != namespace {
				return true, nil
			}
		}
	}
	return false, nil
}

func veleroCRDNames() []string {
	return []string{
		"backuprepositories.velero.io",
		"backups.velero.io",
		"backupstoragelocations.velero.io",
		"datadownloads.velero.io",
		"datauploads.velero.io",
		"deletebackuprequests.velero.io",
		"downloadrequests.velero.io",
		"podvolumebackups.velero.io",
		"podvolumerestores.velero.io",
		"restores.velero.io",
		"schedules.velero.io",
		"serverstatusrequests.velero.io",
		"volumesnapshotlocations.velero.io",
	}
}

func uninstallExternalClusterRBACNames(deleteVelero bool) []string {
	if deleteVelero {
		return []string{"hypercdr-velero"}
	}
	return nil
}

func uninstallAgentClusterRBACNames() []string {
	return []string{"hypercdr-agent"}
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
