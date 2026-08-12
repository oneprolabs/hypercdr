package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubernetesClusterReader struct {
	clientset kubernetes.Interface
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	namespace string
}

func NewKubernetesClusterReader(kubeconfigPath string, namespace ...string) (*KubernetesClusterReader, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	reader, err := NewKubernetesClusterReaderForConfig(cfg)
	if err != nil {
		return nil, err
	}
	if len(namespace) > 0 {
		reader.namespace = namespace[0]
	}
	return reader, nil
}

func NewKubernetesClusterReaderForConfig(cfg *rest.Config) (*KubernetesClusterReader, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubernetesClusterReader{
		clientset: clientset,
		dynamic:   dynamicClient,
		discovery: clientset.Discovery(),
	}, nil
}

func NewKubernetesClusterReaderWithClients(clientset kubernetes.Interface, dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface) *KubernetesClusterReader {
	if discoveryClient == nil && clientset != nil {
		discoveryClient = clientset.Discovery()
	}
	return &KubernetesClusterReader{clientset: clientset, dynamic: dynamicClient, discovery: discoveryClient}
}

func (r *KubernetesClusterReader) ReadCluster(ctx context.Context) (ClusterState, error) {
	state := ClusterState{CollectedAt: time.Now().UTC()}

	version, err := r.discovery.ServerVersion()
	if err != nil {
		return ClusterState{}, err
	}
	state.KubeVersion = version.GitVersion
	state.APIResources = r.readAPIResources()
	state.Capabilities, state.CapabilitiesComplete = r.readNamedCapabilities(ctx)

	nodes, err := r.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ClusterState{}, err
	}
	for _, node := range nodes.Items {
		state.Nodes = append(state.Nodes, nodeState(node))
	}

	storageClasses, err := r.clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ClusterState{}, err
	}
	for _, item := range storageClasses.Items {
		state.StorageClasses = append(state.StorageClasses, storageClassState(item))
	}

	namespaces, err := r.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ClusterState{}, err
	}
	for _, ns := range namespaces.Items {
		state.Namespaces = append(state.Namespaces, Namespace{
			Name:       ns.Name,
			Phase:      string(ns.Status.Phase),
			Labels:     cloneLabels(ns.Labels),
			AgeSeconds: ageSeconds(ns.CreationTimestamp),
		})
	}

	if err := r.readNamespacedResources(ctx, &state); err != nil {
		return ClusterState{}, err
	}
	state.Velero = r.readVeleroState(ctx)
	return state, nil
}

func (r *KubernetesClusterReader) readNamedCapabilities(ctx context.Context) ([]NamedCapability, bool) {
	items := make([]NamedCapability, 0)
	complete := true
	if drivers, err := r.clientset.StorageV1().CSIDrivers().List(ctx, metav1.ListOptions{}); err == nil {
		for _, item := range drivers.Items {
			items = append(items, NamedCapability{Type: "CSIDriver", Name: item.Name})
		}
	} else {
		complete = false
	}
	if classes, err := r.clientset.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{}); err == nil {
		for _, item := range classes.Items {
			items = append(items, NamedCapability{Type: "IngressClass", Name: item.Name, Driver: item.Spec.Controller})
		}
	} else {
		complete = false
	}
	if classes, err := r.clientset.NodeV1().RuntimeClasses().List(ctx, metav1.ListOptions{}); err == nil {
		for _, item := range classes.Items {
			items = append(items, NamedCapability{Type: "RuntimeClass", Name: item.Name, Driver: item.Handler})
		}
	} else {
		complete = false
	}
	if classes, err := r.clientset.SchedulingV1().PriorityClasses().List(ctx, metav1.ListOptions{}); err == nil {
		for _, item := range classes.Items {
			items = append(items, NamedCapability{Type: "PriorityClass", Name: item.Name, Fields: map[string]string{"value": strconv.FormatInt(int64(item.Value), 10)}})
		}
	} else {
		complete = false
	}
	items = append(items, r.readDynamicNamedCapabilities(ctx, schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}, "VolumeSnapshotClass", "driver")...)
	items = append(items, r.readDynamicNamedCapabilities(ctx, schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}, "ClusterIssuer", "")...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type < items[j].Type
		}
		return items[i].Name < items[j].Name
	})
	return items, complete
}

func (r *KubernetesClusterReader) readDynamicNamedCapabilities(ctx context.Context, gvr schema.GroupVersionResource, capabilityType, driverField string) []NamedCapability {
	if r.dynamic == nil {
		return nil
	}
	list, err := r.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	items := make([]NamedCapability, 0, len(list.Items))
	for _, item := range list.Items {
		driver := ""
		if driverField != "" {
			driver, _, _ = unstructured.NestedString(item.Object, driverField)
		}
		items = append(items, NamedCapability{Type: capabilityType, Name: item.GetName(), Driver: driver})
	}
	return items
}

func (r *KubernetesClusterReader) readAPIResources() []APIResource {
	if r.discovery == nil {
		return nil
	}
	lists, err := r.discovery.ServerPreferredResources()
	if len(lists) == 0 {
		// Some discovery implementations (including lightweight proxies and
		// test clients) do not calculate a preferred-resource view. The full
		// discovery result is still sufficient because the key includes version.
		_, allLists, allErr := r.discovery.ServerGroupsAndResources()
		if len(allLists) > 0 {
			lists = allLists
			err = allErr
		}
	}
	if err != nil && len(lists) == 0 {
		return nil
	}
	items := make([]APIResource, 0)
	seen := map[string]bool{}
	for _, list := range lists {
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			continue
		}
		for _, resource := range list.APIResources {
			// Subresources such as pods/log are not independently restorable.
			if strings.Contains(resource.Name, "/") {
				continue
			}
			key := gv.Group + "\x00" + gv.Version + "\x00" + resource.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, APIResource{
				Group: gv.Group, Version: gv.Version, Resource: resource.Name,
				Kind: resource.Kind, Namespaced: resource.Namespaced,
				Verbs: append([]string(nil), resource.Verbs...),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		if items[i].Version != items[j].Version {
			return items[i].Version < items[j].Version
		}
		return items[i].Resource < items[j].Resource
	})
	return items
}

// ReadNamespaceAPIs builds the custom-backup catalog for one namespace.
// Namespaced entries must be restorable by Velero and have objects in the
// requested namespace. Cluster-scoped entries are limited to dependency types
// implied by those namespaced objects; exposing every type present in the
// cluster would invite a namespace backup to capture unrelated shared state.
func (r *KubernetesClusterReader) ReadNamespaceAPIs(ctx context.Context, namespace string) ([]NamespaceAPI, error) {
	if r.dynamic == nil || r.discovery == nil || namespace == "" {
		return nil, nil
	}
	resources := restorablePreferredResources(r.readAPIResources())
	items := make([]NamespaceAPI, 0)
	var itemsMu sync.Mutex
	var workers sync.WaitGroup
	limit := make(chan struct{}, 8)
	for _, resource := range resources {
		if !resource.Namespaced {
			continue
		}
		resource := resource
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				return
			}
			client := r.dynamic.Resource(schema.GroupVersionResource{Group: resource.Group, Version: resource.Version, Resource: resource.Resource})
			var list *unstructured.UnstructuredList
			var listErr error
			list, listErr = client.Namespace(namespace).List(ctx, metav1.ListOptions{})
			if listErr != nil {
				// Some aggregated endpoints expose create-only resources or can be
				// temporarily unavailable. Skip them rather than failing the catalog.
				return
			}
			if len(list.Items) > 0 {
				itemsMu.Lock()
				items = append(items, NamespaceAPI{Scope: "namespace", Namespace: namespace, Group: resource.Group, Version: resource.Version, Resource: resource.Resource, Kind: resource.Kind, Count: len(list.Items)})
				itemsMu.Unlock()
			}
		}()
	}
	workers.Wait()
	sort.Slice(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		if items[i].Version != items[j].Version {
			return items[i].Version < items[j].Version
		}
		return items[i].Resource < items[j].Resource
	})
	return items, nil
}

func resourceKey(resource APIResource) string {
	if resource.Group == "" {
		return resource.Resource
	}
	return resource.Resource + "." + resource.Group
}

func hasAllVerbs(resource APIResource, required ...string) bool {
	available := make(map[string]bool, len(resource.Verbs))
	for _, verb := range resource.Verbs {
		available[verb] = true
	}
	for _, verb := range required {
		if !available[verb] {
			return false
		}
	}
	return true
}

func restorablePreferredResources(resources []APIResource) []APIResource {
	coreEvents := false
	for _, resource := range resources {
		if resource.Group == "" && resource.Resource == "events" && hasAllVerbs(resource, "get", "list", "create", "delete") {
			coreEvents = true
		}
	}
	items := make([]APIResource, 0, len(resources))
	for _, resource := range resources {
		if !hasAllVerbs(resource, "get", "list", "create", "delete") {
			continue
		}
		// Kubernetes serves the same Event objects through core/v1 and
		// events.k8s.io/v1. Velero cohabitation keeps only one representation.
		if coreEvents && resource.Group == "events.k8s.io" && resource.Resource == "events" {
			continue
		}
		items = append(items, resource)
	}
	return items
}

// EnsureResourceDiscoveryPermission adds a read-only wildcard rule so the
// agent can count objects for APIs unknown when HyperCDR was installed. The
// existing role already permits the agent to update its ClusterRole during
// rolling component upgrades.
func (r *KubernetesClusterReader) EnsureResourceDiscoveryPermission(ctx context.Context) error {
	if r.clientset == nil {
		return nil
	}
	roles := r.clientset.RbacV1().ClusterRoles()
	role, err := roles.Get(ctx, "hypercdr-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	for _, rule := range role.Rules {
		if containsString(rule.APIGroups, "*") && containsString(rule.Resources, "*") && containsString(rule.Verbs, "get") && containsString(rule.Verbs, "list") {
			return nil
		}
	}
	role.Rules = append(role.Rules, rbacv1.PolicyRule{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "list"}})
	_, err = roles.Update(ctx, role, metav1.UpdateOptions{})
	return err
}

func (r *KubernetesClusterReader) readNamespacedResources(ctx context.Context, state *ClusterState) error {
	deployments, err := r.clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range deployments.Items {
		state.Workloads = append(state.Workloads, deploymentWorkloadState(item))
	}

	statefulSets, err := r.clientset.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range statefulSets.Items {
		ready := fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, desiredReplicas(item.Spec.Replicas))
		state.Workloads = append(state.Workloads, workloadState(item.Namespace, item.Name, "StatefulSet", item.Labels, statefulSetReady(item), ageSeconds(item.CreationTimestamp), map[string]string{
			"READY": ready,
			"AGE":   formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}))
	}

	daemonSets, err := r.clientset.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range daemonSets.Items {
		state.Workloads = append(state.Workloads, workloadState(item.Namespace, item.Name, "DaemonSet", item.Labels, daemonSetReady(item), ageSeconds(item.CreationTimestamp), map[string]string{
			"DESIRED":       strconv.Itoa(int(item.Status.DesiredNumberScheduled)),
			"CURRENT":       strconv.Itoa(int(item.Status.CurrentNumberScheduled)),
			"READY":         strconv.Itoa(int(item.Status.NumberReady)),
			"UP-TO-DATE":    strconv.Itoa(int(item.Status.UpdatedNumberScheduled)),
			"AVAILABLE":     strconv.Itoa(int(item.Status.NumberAvailable)),
			"NODE SELECTOR": labelsToSelector(item.Spec.Template.Spec.NodeSelector),
			"AGE":           formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}))
	}

	jobs, err := r.clientset.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range jobs.Items {
		completions := "1"
		if item.Spec.Completions != nil {
			completions = strconv.Itoa(int(*item.Spec.Completions))
		}
		state.Workloads = append(state.Workloads, workloadState(item.Namespace, item.Name, "Job", item.Labels, item.Status.Succeeded > 0, ageSeconds(item.CreationTimestamp), map[string]string{
			"COMPLETIONS": fmt.Sprintf("%d/%s", item.Status.Succeeded, completions),
			"DURATION":    jobDuration(item),
			"AGE":         formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}))
	}

	cronJobs, err := r.clientset.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range cronJobs.Items {
		lastSchedule := "<none>"
		if item.Status.LastScheduleTime != nil {
			lastSchedule = formatDurationAge(int64(time.Since(item.Status.LastScheduleTime.Time).Seconds()))
		}
		timeZone := "<none>"
		if item.Spec.TimeZone != nil && *item.Spec.TimeZone != "" {
			timeZone = *item.Spec.TimeZone
		}
		state.Workloads = append(state.Workloads, workloadState(item.Namespace, item.Name, "CronJob", item.Labels, true, ageSeconds(item.CreationTimestamp), map[string]string{
			"SCHEDULE":      item.Spec.Schedule,
			"TIMEZONE":      timeZone,
			"SUSPEND":       strconv.FormatBool(item.Spec.Suspend != nil && *item.Spec.Suspend),
			"ACTIVE":        strconv.Itoa(len(item.Status.Active)),
			"LAST SCHEDULE": lastSchedule,
			"AGE":           formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}))
	}

	services, err := r.clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range services.Items {
		state.Services = append(state.Services, NamespacedResource{Namespace: item.Namespace, Name: item.Name, Labels: cloneLabels(item.Labels), AgeSeconds: ageSeconds(item.CreationTimestamp), Fields: map[string]string{
			"TYPE":        string(item.Spec.Type),
			"CLUSTER-IP":  item.Spec.ClusterIP,
			"EXTERNAL-IP": serviceExternalIP(item),
			"PORT(S)":     servicePorts(item),
			"AGE":         formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}})
	}

	ingresses, err := r.clientset.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range ingresses.Items {
		className := "<none>"
		if item.Spec.IngressClassName != nil && *item.Spec.IngressClassName != "" {
			className = *item.Spec.IngressClassName
		}
		state.Ingresses = append(state.Ingresses, NamespacedResource{Namespace: item.Namespace, Name: item.Name, Labels: cloneLabels(item.Labels), AgeSeconds: ageSeconds(item.CreationTimestamp), Fields: map[string]string{
			"CLASS":   className,
			"HOSTS":   ingressHosts(item),
			"ADDRESS": ingressAddress(item),
			"PORTS":   ingressPorts(item),
			"AGE":     formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}})
	}

	networkPolicies, err := r.clientset.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range networkPolicies.Items {
		state.NetworkPolicies = append(state.NetworkPolicies, NamespacedResource{Namespace: item.Namespace, Name: item.Name, Labels: cloneLabels(item.Labels), AgeSeconds: ageSeconds(item.CreationTimestamp), Fields: map[string]string{
			"POD-SELECTOR": metav1.FormatLabelSelector(&item.Spec.PodSelector),
			"AGE":          formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}})
	}

	configMaps, err := r.clientset.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range configMaps.Items {
		state.ConfigMaps = append(state.ConfigMaps, NamespacedResource{Namespace: item.Namespace, Name: item.Name, Labels: cloneLabels(item.Labels), AgeSeconds: ageSeconds(item.CreationTimestamp), Fields: map[string]string{
			"DATA": strconv.Itoa(len(item.Data) + len(item.BinaryData)),
			"AGE":  formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}})
	}

	secrets, err := r.clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range secrets.Items {
		state.Secrets = append(state.Secrets, NamespacedResource{Namespace: item.Namespace, Name: item.Name, Labels: cloneLabels(item.Labels), AgeSeconds: ageSeconds(item.CreationTimestamp), Fields: map[string]string{
			"TYPE": string(item.Type),
			"DATA": strconv.Itoa(len(item.Data)),
			"AGE":  formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}})
	}

	serviceAccounts, err := r.clientset.CoreV1().ServiceAccounts("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range serviceAccounts.Items {
		state.ServiceAccounts = append(state.ServiceAccounts, NamespacedResource{Namespace: item.Namespace, Name: item.Name, Labels: cloneLabels(item.Labels), AgeSeconds: ageSeconds(item.CreationTimestamp), Fields: map[string]string{
			"SECRETS": strconv.Itoa(len(item.Secrets)),
			"AGE":     formatDurationAge(ageSeconds(item.CreationTimestamp)),
		}})
	}

	storageClassProvisioners := make(map[string]string, len(state.StorageClasses))
	for _, item := range state.StorageClasses {
		storageClassProvisioners[item.Name] = item.Provisioner
	}

	pvs, err := r.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	pvInfos := make(map[string]PVInfo, len(pvs.Items))
	for _, item := range pvs.Items {
		pvInfos[item.Name] = PVInfo{VolumeType: persistentVolumeType(item)}
	}

	pvcs, err := r.clientset.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range pvcs.Items {
		scName := storageClassName(item.Spec.StorageClassName)
		pvInfo := pvInfos[item.Spec.VolumeName]
		state.PVCs = append(state.PVCs, PVC{
			Namespace:     item.Namespace,
			Name:          item.Name,
			Labels:        cloneLabels(item.Labels),
			CapacityBytes: pvcCapacityBytes(item),
			AgeSeconds:    ageSeconds(item.CreationTimestamp),
			Fields: map[string]string{
				"STATUS":                string(item.Status.Phase),
				"VOLUME":                item.Spec.VolumeName,
				"CAPACITY":              pvcCapacityText(item),
				"ACCESS MODES":          accessModes(item.Spec.AccessModes),
				"STORAGECLASS":          scName,
				"PROVISIONER":           storageClassProvisioners[scName],
				"PV TYPE":               pvInfo.VolumeType,
				"VOLUMEATTRIBUTESCLASS": "<unset>",
				"AGE":                   formatDurationAge(ageSeconds(item.CreationTimestamp)),
			},
		})
	}

	roles, err := r.clientset.RbacV1().Roles("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range roles.Items {
		state.OtherResources = append(state.OtherResources, typedResource(item.Namespace, item.Name, "Role", "rbac.authorization.k8s.io/v1", "ROLE", item.Labels, ageSeconds(item.CreationTimestamp), map[string]string{"AGE": formatDurationAge(ageSeconds(item.CreationTimestamp))}))
	}

	roleBindings, err := r.clientset.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range roleBindings.Items {
		state.OtherResources = append(state.OtherResources, typedResource(item.Namespace, item.Name, "RoleBinding", "rbac.authorization.k8s.io/v1", "RB", item.Labels, ageSeconds(item.CreationTimestamp), map[string]string{"ROLE": item.RoleRef.Kind + "/" + item.RoleRef.Name, "AGE": formatDurationAge(ageSeconds(item.CreationTimestamp))}))
	}

	hpas, err := r.clientset.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range hpas.Items {
		targets := "<unknown>"
		if len(item.Status.CurrentMetrics) > 0 {
			targets = strconv.Itoa(len(item.Status.CurrentMetrics))
		}
		state.OtherResources = append(state.OtherResources, typedResource(item.Namespace, item.Name, "HorizontalPodAutoscaler", "autoscaling/v2", "HPA", item.Labels, ageSeconds(item.CreationTimestamp), map[string]string{"REFERENCE": item.Spec.ScaleTargetRef.Kind + "/" + item.Spec.ScaleTargetRef.Name, "TARGETS": targets, "MINPODS": int32PtrText(item.Spec.MinReplicas, "1"), "MAXPODS": strconv.Itoa(int(item.Spec.MaxReplicas)), "REPLICAS": strconv.Itoa(int(item.Status.CurrentReplicas)), "AGE": formatDurationAge(ageSeconds(item.CreationTimestamp))}))
	}

	pdbs, err := r.clientset.PolicyV1().PodDisruptionBudgets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range pdbs.Items {
		state.OtherResources = append(state.OtherResources, typedResource(item.Namespace, item.Name, "PodDisruptionBudget", "policy/v1", "PDB", item.Labels, ageSeconds(item.CreationTimestamp), map[string]string{"MIN AVAILABLE": intstrPtrText(item.Spec.MinAvailable), "MAX UNAVAILABLE": intstrPtrText(item.Spec.MaxUnavailable), "ALLOWED DISRUPTIONS": strconv.Itoa(int(item.Status.DisruptionsAllowed)), "AGE": formatDurationAge(ageSeconds(item.CreationTimestamp))}))
	}

	resourceQuotas, err := r.clientset.CoreV1().ResourceQuotas("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range resourceQuotas.Items {
		state.OtherResources = append(state.OtherResources, typedResource(item.Namespace, item.Name, "ResourceQuota", "v1", "RQ", item.Labels, ageSeconds(item.CreationTimestamp), map[string]string{"AGE": formatDurationAge(ageSeconds(item.CreationTimestamp))}))
	}

	limitRanges, err := r.clientset.CoreV1().LimitRanges("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, item := range limitRanges.Items {
		state.OtherResources = append(state.OtherResources, typedResource(item.Namespace, item.Name, "LimitRange", "v1", "LIMIT", item.Labels, ageSeconds(item.CreationTimestamp), map[string]string{"AGE": formatDurationAge(ageSeconds(item.CreationTimestamp))}))
	}
	return nil
}

func (r *KubernetesClusterReader) readVeleroState(ctx context.Context) VeleroState {
	return VeleroState{
		Status:                  r.readVeleroDeploymentStatus(ctx),
		BackupStorageLocations:  r.readVeleroList(ctx, schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"}, 20),
		VolumeSnapshotLocations: r.readVeleroList(ctx, schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "volumesnapshotlocations"}, 20),
		RecentBackups:           r.readVeleroList(ctx, schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}, 200),
		RecentRestores:          r.readVeleroList(ctx, schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}, 10),
	}
}

func (r *KubernetesClusterReader) readVeleroDeploymentStatus(ctx context.Context) string {
	if r.clientset == nil {
		return "unknown"
	}
	namespace := r.namespace
	if namespace == "" {
		namespace = "velero"
	}
	deploy, err := r.clientset.AppsV1().Deployments(namespace).Get(ctx, "velero", metav1.GetOptions{})
	if err != nil {
		return "unknown"
	}
	if deploymentReady(*deploy) {
		return "ready"
	}
	return "degraded"
}

func (r *KubernetesClusterReader) readVeleroList(ctx context.Context, gvr schema.GroupVersionResource, limit int) []map[string]any {
	if r.dynamic == nil {
		return []map[string]any{}
	}
	list, err := r.dynamic.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return []map[string]any{}
	}
	items := make([]unstructured.Unstructured, 0, len(list.Items))
	items = append(items, list.Items...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetCreationTimestamp().After(items[j].GetCreationTimestamp().Time)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	summaries := make([]map[string]any, 0, len(items))
	enrichedBackupCount := 0
	for _, item := range items {
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		if phase == "" {
			phase, _, _ = unstructured.NestedString(item.Object, "status", "status")
		}
		summary := map[string]any{
			"name":      item.GetName(),
			"namespace": item.GetNamespace(),
			"phase":     phase,
			"createdAt": item.GetCreationTimestamp().Time,
			"labels":    item.GetLabels(),
		}
		if storageLocation, _, _ := unstructured.NestedString(item.Object, "spec", "storageLocation"); storageLocation != "" {
			summary["storageLocation"] = storageLocation
		}
		if includedNamespaces, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "includedNamespaces"); len(includedNamespaces) > 0 {
			summary["includedNamespaces"] = includedNamespaces
		}
		if startedAt, _, _ := unstructured.NestedString(item.Object, "status", "startTimestamp"); startedAt != "" {
			summary["startedAt"] = startedAt
		}
		if completedAt, _, _ := unstructured.NestedString(item.Object, "status", "completionTimestamp"); completedAt != "" {
			summary["completedAt"] = completedAt
		}
		if errors, _, _ := unstructured.NestedInt64(item.Object, "status", "errors"); errors > 0 {
			summary["errors"] = errors
		}
		if warnings, _, _ := unstructured.NestedInt64(item.Object, "status", "warnings"); warnings > 0 {
			summary["warnings"] = warnings
		}
		if gvr.Group == "velero.io" && gvr.Version == "v1" && gvr.Resource == "backups" && shouldEnrichBackupInventory(summary) && enrichedBackupCount < 25 {
			r.enrichBackupInventory(ctx, summary)
			enrichedBackupCount++
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func shouldEnrichBackupInventory(summary map[string]any) bool {
	if summary["phase"] != "Completed" {
		return false
	}
	labels, _ := summary["labels"].(map[string]string)
	return labels["hypercdr.io/managed-by"] == "hypercdr" || labels["hypercdr.io/plan-id"] != ""
}

func (r *KubernetesClusterReader) enrichBackupInventory(ctx context.Context, summary map[string]any) {
	name, _ := summary["name"].(string)
	namespace, _ := summary["namespace"].(string)
	storageLocation, _ := summary["storageLocation"].(string)
	if name == "" || namespace == "" {
		return
	}
	applier := NewDynamicManifestApplierWithClient(r.dynamic)
	volumeProgress, _ := applier.GetBackupVolumeProgress(ctx, namespace, name)
	if len(volumeProgress.Items) > 0 {
		summary["volumeProgress"] = backupInventoryVolumeProgressPayload(volumeProgress)
	}

	var objectStats BackupObjectStats
	var objectStatsErr string
	var volumeInfoStats BackupVolumeInfoStats
	var volumeInfoStatsErr string
	if storageLocation != "" {
		stats, err := applier.GetBackupObjectStats(ctx, namespace, storageLocation, name)
		if err != nil {
			objectStatsErr = err.Error()
		} else {
			objectStats = stats
		}
		infoStats, err := applier.GetBackupVolumeInfoStats(ctx, namespace, storageLocation, name)
		if err != nil {
			volumeInfoStatsErr = err.Error()
		} else {
			volumeInfoStats = infoStats
		}
	}

	volumeBytes, volumeAccuracy, volumeSource := inventoryFinalVolumeBytes(volumeProgress)
	if volumeInfoStats.Accurate {
		volumeBytes = volumeInfoStats.VolumeBytes
		volumeAccuracy = "accurate"
		volumeSource = "veleroBackupVolumeInfo"
	}
	metadataBytes := objectStats.MetadataPackageBytes
	uploadedMetadataBytes := metadataBytes
	uploadedVolumeBytes := volumeBytes
	uploadedVolumeSource := "estimatedFromVolumeBytes"
	if volumeProgress.IncrementalCount > 0 {
		uploadedVolumeBytes = volumeProgress.IncrementalBytes
		uploadedVolumeSource = "veleroIncrementalBytes"
	}
	totalBytes := metadataBytes + volumeBytes
	uploadedBytes := uploadedMetadataBytes + uploadedVolumeBytes
	sizeStatus := "complete"
	if objectStatsErr != "" || volumeAccuracy != "accurate" {
		sizeStatus = "partial"
	}
	size := map[string]any{
		"sizeStatus":             sizeStatus,
		"totalBytes":             totalBytes,
		"metadataBytes":          metadataBytes,
		"volumeBytes":            volumeBytes,
		"uploadedBytes":          uploadedBytes,
		"uploadedMetadataBytes":  uploadedMetadataBytes,
		"uploadedVolumeBytes":    uploadedVolumeBytes,
		"incrementalVolumeBytes": volumeProgress.IncrementalBytes,
		"incrementalVolumeCount": volumeProgress.IncrementalCount,
		"sources": map[string]any{
			"metadataBytes":         "objectStoreBackupArtifacts",
			"volumeBytes":           volumeSource,
			"uploadedMetadataBytes": "objectStoreBackupArtifacts",
			"uploadedVolumeBytes":   uploadedVolumeSource,
		},
		"accuracy": map[string]any{
			"totalBytes":            "mixed",
			"metadataBytes":         "accurate",
			"volumeBytes":           volumeAccuracy,
			"uploadedBytes":         "mixed",
			"uploadedMetadataBytes": "accurate",
			"uploadedVolumeBytes":   "estimated",
		},
		"metadataObjectCount": objectStats.ObjectCount,
		"metadataPrefix":      objectStats.Prefix,
		"volumeInfoCount":     volumeInfoStats.VolumeCount,
		"volumeInfoKey":       volumeInfoStats.Key,
		"storageLocation":     storageLocation,
	}
	if objectStatsErr != "" {
		size["metadataStatsError"] = objectStatsErr
	}
	if volumeInfoStatsErr != "" {
		size["volumeInfoStatsError"] = volumeInfoStatsErr
	}
	summary["size"] = size
	summary["sizeBytes"] = totalBytes
}

func backupInventoryVolumeProgressPayload(progress VolumeProgress) map[string]any {
	items := make([]map[string]any, 0, len(progress.Items))
	for _, item := range progress.Items {
		items = append(items, map[string]any{
			"kind":             item.Kind,
			"name":             item.Name,
			"phase":            item.Phase,
			"bytesDone":        item.BytesDone,
			"totalBytes":       item.TotalBytes,
			"incrementalBytes": item.IncrementalBytes,
			"incrementalKnown": item.IncrementalKnown,
			"knownTotal":       item.KnownTotal,
			"message":          item.Message,
		})
	}
	return map[string]any{
		"operation":         "backup",
		"bytesDone":         progress.BytesDone,
		"totalBytes":        progress.TotalBytes,
		"incrementalBytes":  progress.IncrementalBytes,
		"incrementalCount":  progress.IncrementalCount,
		"knownTotal":        progress.KnownTotal,
		"allTotalsKnown":    progress.AllTotalsKnown,
		"knownTotalCount":   progress.KnownTotalCount,
		"unknownTotalCount": progress.UnknownTotalCount,
		"itemCount":         len(progress.Items),
		"runningCount":      progress.RunningCount,
		"completedCount":    progress.Completed,
		"failedCount":       progress.FailedCount,
		"items":             items,
	}
}

func backupInventoryVolumeAccuracy(progress VolumeProgress) string {
	if len(progress.Items) == 0 {
		return "unknown"
	}
	if progress.AllTotalsKnown {
		return "accurate"
	}
	if progress.KnownTotal {
		return "partial"
	}
	return "estimated"
}

func inventoryFinalVolumeBytes(progress VolumeProgress) (int64, string, string) {
	if progress.AllTotalsKnown && progress.TotalBytes > 0 {
		return progress.TotalBytes, "accurate", "veleroVolumeProgress"
	}
	return 0, backupInventoryVolumeAccuracy(progress), "veleroVolumeProgress"
}

func nodeState(node corev1.Node) Node {
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	return Node{
		Name:           node.Name,
		Labels:         cloneLabels(node.Labels),
		Ready:          ready,
		KubeletVersion: node.Status.NodeInfo.KubeletVersion,
		AgeSeconds:     ageSeconds(node.CreationTimestamp),
		Capacity: map[string]string{
			"cpu":    node.Status.Capacity.Cpu().String(),
			"memory": node.Status.Capacity.Memory().String(),
		},
	}
}

func storageClassState(item storagev1.StorageClass) StorageClass {
	return StorageClass{
		Name:                 item.Name,
		Provisioner:          item.Provisioner,
		ReclaimPolicy:        storageClassReclaimPolicy(item.ReclaimPolicy),
		VolumeBindingMode:    storageClassVolumeBindingMode(item.VolumeBindingMode),
		AllowVolumeExpansion: storageClassAllowExpansion(item.AllowVolumeExpansion),
		Default:              isDefaultStorageClass(item.Annotations),
		AgeSeconds:           ageSeconds(item.CreationTimestamp),
	}
}

func workloadState(namespace string, name string, kind string, labels map[string]string, ready bool, age int64, fields map[string]string) Workload {
	return Workload{Namespace: namespace, Name: name, Kind: kind, Labels: cloneLabels(labels), Ready: ready, AgeSeconds: age, Fields: fields}
}

func deploymentWorkloadState(item appsv1.Deployment) Workload {
	containers := make([]string, 0, len(item.Spec.Template.Spec.Containers))
	images := make([]string, 0, len(item.Spec.Template.Spec.Containers))
	for _, container := range item.Spec.Template.Spec.Containers {
		containers = append(containers, container.Name)
		images = append(images, container.Image)
	}
	return Workload{
		Namespace:         item.Namespace,
		Name:              item.Name,
		Kind:              "Deployment",
		Labels:            cloneLabels(item.Labels),
		Ready:             deploymentReady(item),
		DesiredReplicas:   desiredReplicas(item.Spec.Replicas),
		ReadyReplicas:     item.Status.ReadyReplicas,
		UpdatedReplicas:   item.Status.UpdatedReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds:        int64(time.Since(item.CreationTimestamp.Time).Seconds()),
		Containers:        containers,
		Images:            images,
		Selector:          metav1.FormatLabelSelector(item.Spec.Selector),
		Fields: map[string]string{
			"READY":      fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, desiredReplicas(item.Spec.Replicas)),
			"UP-TO-DATE": strconv.Itoa(int(item.Status.UpdatedReplicas)),
			"AVAILABLE":  strconv.Itoa(int(item.Status.AvailableReplicas)),
			"AGE":        formatDurationAge(ageSeconds(item.CreationTimestamp)),
			"CONTAINERS": strings.Join(containers, ","),
			"IMAGES":     strings.Join(images, ","),
			"SELECTOR":   metav1.FormatLabelSelector(item.Spec.Selector),
		},
	}
}

func typedResource(namespace string, name string, kind string, apiVersion string, shortName string, labels map[string]string, age int64, fields map[string]string) TypedNamespacedResource {
	return TypedNamespacedResource{Namespace: namespace, Name: name, Kind: kind, APIVersion: apiVersion, ShortName: shortName, Labels: cloneLabels(labels), AgeSeconds: age, Fields: fields}
}

func deploymentReady(item appsv1.Deployment) bool {
	return item.Status.ReadyReplicas >= desiredReplicas(item.Spec.Replicas)
}

func statefulSetReady(item appsv1.StatefulSet) bool {
	return item.Status.ReadyReplicas >= desiredReplicas(item.Spec.Replicas)
}

func daemonSetReady(item appsv1.DaemonSet) bool {
	return item.Status.NumberReady >= item.Status.DesiredNumberScheduled
}

func desiredReplicas(value *int32) int32 {
	if value == nil {
		return 1
	}
	return *value
}

func ageSeconds(ts metav1.Time) int64 {
	return int64(time.Since(ts.Time).Seconds())
}

func storageClassReclaimPolicy(value *corev1.PersistentVolumeReclaimPolicy) string {
	if value == nil || *value == "" {
		return "Delete"
	}
	return string(*value)
}

func storageClassVolumeBindingMode(value *storagev1.VolumeBindingMode) string {
	if value == nil || *value == "" {
		return "Immediate"
	}
	return string(*value)
}

func storageClassAllowExpansion(value *bool) string {
	if value == nil {
		return "false"
	}
	return strconv.FormatBool(*value)
}

func isDefaultStorageClass(annotations map[string]string) bool {
	return annotations["storageclass.kubernetes.io/is-default-class"] == "true" ||
		annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true"
}

func formatDurationAge(seconds int64) string {
	if seconds < 0 {
		return "-"
	}
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24
	switch {
	case days > 0:
		return strconv.FormatInt(days, 10) + "d"
	case hours > 0:
		return strconv.FormatInt(hours, 10) + "h"
	case minutes > 0:
		return strconv.FormatInt(minutes, 10) + "m"
	default:
		return strconv.FormatInt(seconds, 10) + "s"
	}
}

func labelsToSelector(labels map[string]string) string {
	if len(labels) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(labels))
	for key, value := range labels {
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func serviceExternalIP(item corev1.Service) string {
	values := append([]string{}, item.Spec.ExternalIPs...)
	for _, ingress := range item.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			values = append(values, ingress.IP)
		}
		if ingress.Hostname != "" {
			values = append(values, ingress.Hostname)
		}
	}
	if len(values) == 0 {
		return "<none>"
	}
	return strings.Join(values, ",")
}

func servicePorts(item corev1.Service) string {
	ports := make([]string, 0, len(item.Spec.Ports))
	for _, port := range item.Spec.Ports {
		value := strconv.Itoa(int(port.Port)) + "/" + string(port.Protocol)
		if port.NodePort > 0 {
			value = strconv.Itoa(int(port.Port)) + ":" + strconv.Itoa(int(port.NodePort)) + "/" + string(port.Protocol)
		}
		ports = append(ports, value)
	}
	return strings.Join(ports, ",")
}

func ingressHosts(item networkingv1.Ingress) string {
	hosts := make([]string, 0, len(item.Spec.Rules))
	for _, rule := range item.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	if len(hosts) == 0 {
		return "*"
	}
	return strings.Join(hosts, ",")
}

func ingressAddress(item networkingv1.Ingress) string {
	values := make([]string, 0, len(item.Status.LoadBalancer.Ingress))
	for _, ingress := range item.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			values = append(values, ingress.IP)
		}
		if ingress.Hostname != "" {
			values = append(values, ingress.Hostname)
		}
	}
	return strings.Join(values, ",")
}

func ingressPorts(item networkingv1.Ingress) string {
	hasTLS := len(item.Spec.TLS) > 0
	if hasTLS {
		return "80, 443"
	}
	return "80"
}

func jobDuration(item batchv1.Job) string {
	if item.Status.StartTime == nil {
		return "-"
	}
	end := time.Now()
	if item.Status.CompletionTime != nil {
		end = item.Status.CompletionTime.Time
	}
	return formatDurationAge(int64(end.Sub(item.Status.StartTime.Time).Seconds()))
}

func accessModes(modes []corev1.PersistentVolumeAccessMode) string {
	if len(modes) == 0 {
		return "<none>"
	}
	values := make([]string, 0, len(modes))
	for _, mode := range modes {
		switch mode {
		case corev1.ReadWriteOnce:
			values = append(values, "RWO")
		case corev1.ReadOnlyMany:
			values = append(values, "ROX")
		case corev1.ReadWriteMany:
			values = append(values, "RWX")
		case corev1.ReadWriteOncePod:
			values = append(values, "RWOP")
		default:
			values = append(values, string(mode))
		}
	}
	return strings.Join(values, ",")
}

func storageClassName(value *string) string {
	if value == nil || *value == "" {
		return "<none>"
	}
	return *value
}

func persistentVolumeType(item corev1.PersistentVolume) string {
	source := item.Spec.PersistentVolumeSource
	switch {
	case source.CSI != nil:
		return "CSI"
	case source.HostPath != nil:
		return "HostPath"
	case source.Local != nil:
		return "Local"
	case source.NFS != nil:
		return "NFS"
	case source.RBD != nil:
		return "RBD"
	case source.CephFS != nil:
		return "CephFS"
	case source.ISCSI != nil:
		return "iSCSI"
	case source.AWSElasticBlockStore != nil:
		return "AWS EBS"
	case source.AzureDisk != nil:
		return "Azure Disk"
	case source.GCEPersistentDisk != nil:
		return "GCE PD"
	default:
		return ""
	}
}

func pvcCapacityText(item corev1.PersistentVolumeClaim) string {
	quantity, ok := item.Status.Capacity[corev1.ResourceStorage]
	if !ok {
		return ""
	}
	return quantity.String()
}

func int32PtrText(value *int32, fallback string) string {
	if value == nil {
		return fallback
	}
	return strconv.Itoa(int(*value))
}

func intstrPtrText(value *intstr.IntOrString) string {
	if value == nil {
		return "<unset>"
	}
	return value.String()
}

func pvcCapacityBytes(item corev1.PersistentVolumeClaim) int64 {
	quantity, ok := item.Status.Capacity[corev1.ResourceStorage]
	if !ok {
		return 0
	}
	return quantity.Value()
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func (r *KubernetesClusterReader) String() string {
	return fmt.Sprintf("KubernetesClusterReader{%T}", r.clientset)
}
