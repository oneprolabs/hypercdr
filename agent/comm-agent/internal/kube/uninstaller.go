package kube

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	Progress        func(stage, message string)
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
		reportUninstallProgress(options, "velero_resources_cleaning", "deleting Velero resources and waiting for finalizers")
		if err := u.deleteVeleroNamespacedResources(ctx, options.Namespace); err != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	if !options.DeleteNamespace {
		if err := u.deleteNamespacedAgentResources(ctx, options); err != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	for _, name := range uninstallExternalClusterRBACNames(options.Namespace, options.DeleteVelero) {
		reportUninstallProgress(options, "external_rbac_cleaning", "deleting Velero cluster RBAC")
		if err := u.client.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
		if err := u.client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	if options.DeleteVelero {
		reportUninstallProgress(options, "velero_crd_check", "checking whether Velero CRDs are shared")
		if err := u.deleteVeleroCRDs(ctx, options.Namespace); err != nil {
			preSelfRemovalErrs = append(preSelfRemovalErrs, err)
		}
	}
	if err := errors.Join(preSelfRemovalErrs...); err != nil {
		return err
	}
	if options.Namespace == "openshift-adp" && u.dynamicClient != nil {
		// CatalogSource is outside the dedicated namespace and therefore is not
		// garbage-collected with the Subscription/DPA. Remove only HyperCDR's
		// qualified catalog; never touch Red Hat's shared catalogs.
		catalogs := u.dynamicClient.Resource(schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "catalogsources"}).Namespace("openshift-marketplace")
		if err := catalogs.Delete(ctx, "hypercdr-oadp", metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			return fmt.Errorf("delete HyperCDR OADP CatalogSource: %w", err)
		}
	}

	var errs []error
	if options.DeleteNamespace {
		reportUninstallProgress(options, "namespace_deleting", "deleting the dedicated Agent namespace")
		if err := u.client.CoreV1().Namespaces().Delete(ctx, options.Namespace, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
		}
		if err := u.deleteAgentClusterRBACWithOwnerCascade(ctx, options.Namespace); err != nil {
			errs = append(errs, err)
		}
	} else {
		for _, name := range uninstallAgentClusterRBACNames(options.Namespace) {
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

func reportUninstallProgress(options UninstallOptions, stage, message string) {
	if options.Progress != nil {
		options.Progress(stage, message)
	}
}

func (u *KubernetesUninstaller) deleteAgentClusterRBACWithOwnerCascade(ctx context.Context, namespace string) error {
	var errs []error
	for _, name := range uninstallAgentClusterRBACNames(namespace) {
		role, err := u.client.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if ignoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		if err != nil {
			continue
		}
		patch, marshalErr := json.Marshal(map[string]any{"metadata": map[string]any{"ownerReferences": []map[string]any{{
			"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole", "name": role.Name, "uid": string(role.UID),
		}}}})
		if marshalErr != nil {
			errs = append(errs, marshalErr)
			continue
		}
		if _, err = u.client.RbacV1().ClusterRoleBindings().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); ignoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		if err = u.client.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
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
	gvrs := veleroNamespacedResources()
	// First request deletion for every Velero kind. Some finalizers depend on
	// related objects, so waiting one kind at a time can deadlock the natural
	// controller cleanup order.
	for _, gvr := range gvrs {
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
			if err := resource.Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, err)
			}
		}
	}
	// Give the still-running Velero controller one shared grace period to
	// perform normal finalization before applying the namespace-scoped fallback.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		remainingCount := 0
		for _, gvr := range gvrs {
			remaining, err := u.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
			if err == nil {
				remainingCount += len(remaining.Items)
			} else if !apierrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("wait for %s cleanup: %w", gvr.Resource, err))
			}
		}
		if remainingCount == 0 {
			return errors.Join(errs...)
		}
		select {
		case <-ctx.Done():
			return errors.Join(append(errs, ctx.Err())...)
		case <-time.After(500 * time.Millisecond):
		}
	}
	for _, gvr := range gvrs {
		resource := u.dynamicClient.Resource(gvr).Namespace(namespace)
		remaining, err := resource.List(ctx, metav1.ListOptions{})
		if ignoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("inspect remaining %s: %w", gvr.Resource, err))
			continue
		}
		if err != nil {
			continue
		}
		for _, item := range remaining.Items {
			if len(item.GetFinalizers()) == 0 {
				continue
			}
			if _, err := resource.Patch(ctx, item.GetName(), types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`), metav1.PatchOptions{}); ignoreNotFound(err) != nil {
				errs = append(errs, fmt.Errorf("remove stale finalizers from %s/%s: %w", gvr.Resource, item.GetName(), err))
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
		for _, name := range []string{"node-agent-config", "backup-repository-config"} {
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
	deployments, err := u.client.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, deployment := range deployments.Items {
		if deployment.Namespace != namespace && (deployment.Name == "velero" || deployment.Name == "hypercdr-comm-agent") {
			return true, nil
		}
	}
	daemonSets, err := u.client.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, daemonSet := range daemonSets.Items {
		if daemonSet.Namespace != namespace && daemonSet.Name == "node-agent" {
			return true, nil
		}
	}
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

func uninstallExternalClusterRBACNames(namespace string, deleteVelero bool) []string {
	if deleteVelero {
		return []string{scopedRBACName("hypercdr-velero", namespace)}
	}
	return nil
}

func uninstallAgentClusterRBACNames(namespace string) []string {
	return []string{scopedRBACName("hypercdr-agent", namespace)}
}

func scopedRBACName(baseName, namespace string) string {
	if namespace == "" || namespace == "hypercdr-agent" {
		return baseName
	}
	digest := sha256.Sum256([]byte(namespace))
	return fmt.Sprintf("%s-%x", baseName, digest[:4])
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
