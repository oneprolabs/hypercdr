package inventory

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type KubernetesCollector struct {
	cfg    config.Config
	reader kube.ClusterReader
}

func NewKubernetesCollector(cfg config.Config, reader kube.ClusterReader) *KubernetesCollector {
	return &KubernetesCollector{cfg: cfg, reader: reader}
}

func (c *KubernetesCollector) Collect() (Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := c.reader.ReadCluster(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if state.CollectedAt.IsZero() {
		state.CollectedAt = time.Now().UTC()
	}
	if state.Name == "" {
		state.Name = c.cfg.ClusterName
	}
	if state.KubeVersion == "" {
		state.KubeVersion = "unknown"
	}

	report := protocol.InventoryReportPayload{
		Full:        true,
		CollectedAt: state.CollectedAt,
		Cluster: protocol.ClusterSummary{
			Name:           state.Name,
			KubeVersion:    state.KubeVersion,
			NodeCount:      len(state.Nodes),
			NamespaceCount: len(state.Namespaces),
		},
		Nodes:                buildNodeInventory(state.Nodes),
		StorageClasses:       buildStorageClassInventory(state.StorageClasses),
		APIResources:         buildAPIResourceInventory(state.APIResources),
		Capabilities:         buildNamedCapabilityInventory(state.Capabilities),
		CapabilitiesComplete: state.CapabilitiesComplete,
		Apps:                 buildApplicationInventory(state),
		Velero:               buildVeleroInventory(state.Velero),
	}

	hash, err := hashReport(report)
	if err != nil {
		return Snapshot{}, err
	}
	report.InventoryHash = hash
	return Snapshot{Report: report, Hash: hash}, nil
}

func buildNamedCapabilityInventory(capabilities []kube.NamedCapability) []protocol.NamedCapabilityInventory {
	items := make([]protocol.NamedCapabilityInventory, 0, len(capabilities))
	for _, capability := range capabilities {
		items = append(items, protocol.NamedCapabilityInventory{Type: capability.Type, Name: capability.Name, Driver: capability.Driver, Fields: capability.Fields})
	}
	return items
}

func (c *KubernetesCollector) CollectCapabilities(namespace string) (Snapshot, error) {
	snapshot, err := c.Collect()
	if err != nil {
		return Snapshot{}, err
	}
	reader, ok := c.reader.(kube.CapabilityReader)
	if !ok {
		return snapshot, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if preparer, ok := c.reader.(kube.ResourceDiscoveryPreparer); ok {
		if err := preparer.EnsureResourceDiscoveryPermission(ctx); err != nil {
			return Snapshot{}, err
		}
	}
	resources, err := reader.ReadNamespaceAPIs(ctx, namespace)
	if err != nil {
		return Snapshot{}, err
	}
	for _, resource := range resources {
		snapshot.Report.NamespaceAPIs = append(snapshot.Report.NamespaceAPIs, protocol.NamespaceAPIInventory{
			Scope: resource.Scope, Namespace: resource.Namespace, Group: resource.Group, Version: resource.Version,
			Resource: resource.Resource, Kind: resource.Kind, Count: resource.Count,
		})
	}
	snapshot.Report.Scope = "capabilities"
	snapshot.Report.Namespace = namespace
	return snapshot, nil
}

func buildAPIResourceInventory(resources []kube.APIResource) []protocol.APIResourceInventory {
	items := make([]protocol.APIResourceInventory, 0, len(resources))
	for _, resource := range resources {
		items = append(items, protocol.APIResourceInventory{
			Group: resource.Group, Version: resource.Version, Resource: resource.Resource,
			Kind: resource.Kind, Namespaced: resource.Namespaced,
		})
	}
	return items
}

func buildNodeInventory(nodes []kube.Node) []protocol.NodeInventory {
	items := make([]protocol.NodeInventory, 0, len(nodes))
	for _, node := range nodes {
		status := "notReady"
		if node.Ready {
			status = "ready"
		}
		items = append(items, protocol.NodeInventory{
			Name:           node.Name,
			Role:           nodeRole(node.Labels),
			Status:         status,
			KubeletVersion: node.KubeletVersion,
			Capacity:       cloneStringMap(node.Capacity),
			AgeSeconds:     node.AgeSeconds,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func buildStorageClassInventory(storageClasses []kube.StorageClass) []protocol.StorageClassInventory {
	items := make([]protocol.StorageClassInventory, 0, len(storageClasses))
	for _, storageClass := range storageClasses {
		items = append(items, protocol.StorageClassInventory{
			Name:                 storageClass.Name,
			Provisioner:          storageClass.Provisioner,
			ReclaimPolicy:        storageClass.ReclaimPolicy,
			VolumeBindingMode:    storageClass.VolumeBindingMode,
			AllowVolumeExpansion: storageClass.AllowVolumeExpansion,
			Default:              storageClass.Default,
			AgeSeconds:           storageClass.AgeSeconds,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Default != items[j].Default {
			return items[i].Default
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func buildApplicationInventory(state kube.ClusterState) []protocol.ApplicationInventory {
	byNamespace := map[string]*protocol.ApplicationInventory{}
	for _, ns := range state.Namespaces {
		phase := strings.ToLower(ns.Phase)
		if phase == "" {
			phase = "unknown"
		}
		byNamespace[ns.Name] = &protocol.ApplicationInventory{
			Namespace:  ns.Name,
			Status:     phase,
			Labels:     cloneStringMap(ns.Labels),
			AgeSeconds: ns.AgeSeconds,
			Resources:  protocol.ResourceSummary{},
		}
	}

	ensureApp := func(namespace string) *protocol.ApplicationInventory {
		app, ok := byNamespace[namespace]
		if ok {
			return app
		}
		app = &protocol.ApplicationInventory{
			Namespace: namespace,
			Status:    "unknown",
			Labels:    map[string]string{},
			Resources: protocol.ResourceSummary{},
		}
		byNamespace[namespace] = app
		return app
	}

	for _, workload := range state.Workloads {
		resources := &ensureApp(workload.Namespace).Resources
		switch strings.ToLower(workload.Kind) {
		case "deployment", "deployments":
			resources.Deployments++
		case "statefulset", "statefulsets":
			resources.StatefulSets++
		case "daemonset", "daemonsets":
			resources.DaemonSets++
		case "job", "jobs":
			resources.Jobs++
		case "cronjob", "cronjobs":
			resources.CronJobs++
		}
		addWorkloadResource(resources, workload)
	}
	for _, item := range state.Services {
		resources := &ensureApp(item.Namespace).Resources
		resources.Services++
		addResourceWithFields(resources, "network", "Network", "Service", "SVC", item.Namespace, item.Name, "v1", item.Labels, item.AgeSeconds, item.Fields)
	}
	for _, item := range state.Ingresses {
		resources := &ensureApp(item.Namespace).Resources
		resources.Ingresses++
		addResourceWithFields(resources, "network", "Network", "Ingress", "ING", item.Namespace, item.Name, "networking.k8s.io/v1", item.Labels, item.AgeSeconds, item.Fields)
	}
	for _, item := range state.NetworkPolicies {
		resources := &ensureApp(item.Namespace).Resources
		resources.NetworkPolicies++
		addResourceWithFields(resources, "network", "Network", "NetworkPolicy", "NP", item.Namespace, item.Name, "networking.k8s.io/v1", item.Labels, item.AgeSeconds, item.Fields)
	}
	for _, item := range state.ConfigMaps {
		resources := &ensureApp(item.Namespace).Resources
		resources.ConfigMaps++
		addResourceWithFields(resources, "config", "Config", "ConfigMap", "CM", item.Namespace, item.Name, "v1", item.Labels, item.AgeSeconds, item.Fields)
	}
	for _, item := range state.Secrets {
		resources := &ensureApp(item.Namespace).Resources
		resources.Secrets++
		addResourceWithFields(resources, "config", "Config", "Secret", "SEC", item.Namespace, item.Name, "v1", item.Labels, item.AgeSeconds, item.Fields)
	}
	for _, item := range state.ServiceAccounts {
		resources := &ensureApp(item.Namespace).Resources
		resources.ServiceAccounts++
		addResourceWithFields(resources, "config", "Config", "ServiceAccount", "SA", item.Namespace, item.Name, "v1", item.Labels, item.AgeSeconds, item.Fields)
	}
	for _, pvc := range state.PVCs {
		resources := &ensureApp(pvc.Namespace).Resources
		resources.PVCs++
		resources.PVCapacityBytes += pvc.CapacityBytes
		applyDRSupportCheck(resources, pvc)
		addResourceWithFields(resources, "storage", "Storage", "PersistentVolumeClaim", "PVC", pvc.Namespace, pvc.Name, "v1", pvc.Labels, pvc.AgeSeconds, pvc.Fields)
	}
	for _, item := range state.OtherResources {
		resources := &ensureApp(item.Namespace).Resources
		addResourceWithFields(resources, "other", "Other Objects", item.Kind, item.ShortName, item.Namespace, item.Name, item.APIVersion, item.Labels, item.AgeSeconds, item.Fields)
	}

	items := make([]protocol.ApplicationInventory, 0, len(byNamespace))
	for _, app := range byNamespace {
		if app.Resources.DRSupport == nil {
			app.Resources.DRSupport = &protocol.DRSupportSummary{
				Status: "supported",
				Reason: "Namespace has no persistent volumes that require storage portability checks.",
			}
		}
		normalizeResourceCategories(&app.Resources)
		items = append(items, *app)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Namespace < items[j].Namespace
	})
	return items
}

func applyDRSupportCheck(summary *protocol.ResourceSummary, pvc kube.PVC) {
	if summary.DRSupport == nil {
		summary.DRSupport = &protocol.DRSupportSummary{Status: "supported", Reason: "Namespace has no unsupported persistent volumes."}
	}
	fields := pvc.Fields
	storageClass := strings.TrimSpace(fields["STORAGECLASS"])
	provisioner := strings.TrimSpace(fields["PROVISIONER"])
	volumeType := strings.TrimSpace(fields["PV TYPE"])
	check := protocol.DRSupportCheck{
		Kind:         "PersistentVolumeClaim",
		Name:         pvc.Name,
		Status:       "supported",
		StorageClass: storageClass,
		Provisioner:  provisioner,
		Volume:       fields["VOLUME"],
		VolumeType:   volumeType,
	}
	if reason := drUnsupportedPVCReason(storageClass, provisioner, volumeType); reason != "" {
		check.Status = "unsupported"
		check.Reason = reason
		summary.DRSupport.Status = "unsupported"
		summary.DRSupport.Reason = reason
	}
	summary.DRSupport.Checks = append(summary.DRSupport.Checks, check)
}

func drUnsupportedPVCReason(storageClass string, provisioner string, volumeType string) string {
	normalizedSC := strings.ToLower(strings.TrimSpace(storageClass))
	normalizedProvisioner := strings.ToLower(strings.TrimSpace(provisioner))
	normalizedType := strings.ToLower(strings.TrimSpace(volumeType))
	switch {
	case normalizedProvisioner == "rancher.io/local-path" || normalizedSC == "local-path":
		return "PVC uses local-path storage. Velero file-system backup cannot reliably protect host-local volume data for DR."
	case normalizedType == "hostpath":
		return "PVC is bound to a hostPath persistent volume. Host-local paths are not portable across clusters."
	case normalizedType == "local":
		return "PVC is bound to a local persistent volume. Local volumes are tied to a node and are not portable across clusters."
	default:
		return ""
	}
}

func addResource(summary *protocol.ResourceSummary, categoryKey string, categoryLabel string, kind string, shortName string, namespace string, name string, apiVersion string) {
	addResourceWithFields(summary, categoryKey, categoryLabel, kind, shortName, namespace, name, apiVersion, nil, 0, nil)
}

func addResourceWithFields(summary *protocol.ResourceSummary, categoryKey string, categoryLabel string, kind string, shortName string, namespace string, name string, apiVersion string, labels map[string]string, ageSeconds int64, fields map[string]string) {
	if summary == nil || name == "" {
		return
	}
	category := ensureResourceCategory(summary, categoryKey, categoryLabel)
	item := ensureResourceKind(category, kind, shortName)
	item.Count++
	item.Resources = append(item.Resources, protocol.ResourceRef{
		Name:       name,
		Namespace:  namespace,
		Kind:       kind,
		APIVersion: apiVersion,
		Labels:     cloneStringMap(labels),
		AgeSeconds: ageSeconds,
		Fields:     cloneStringMap(fields),
	})
	category.Total++
}

func addWorkloadResource(summary *protocol.ResourceSummary, workload kube.Workload) {
	if summary == nil || workload.Name == "" {
		return
	}
	category := ensureResourceCategory(summary, "workloads", "Workloads")
	item := ensureResourceKind(category, workload.Kind, shortResourceName(workload.Kind))
	item.Count++
	ref := protocol.ResourceRef{
		Name:       workload.Name,
		Namespace:  workload.Namespace,
		Kind:       workload.Kind,
		APIVersion: apiVersionForKind(workload.Kind),
		Labels:     cloneStringMap(workload.Labels),
		AgeSeconds: workload.AgeSeconds,
		Fields:     cloneStringMap(workload.Fields),
	}
	if strings.EqualFold(workload.Kind, "Deployment") {
		ref.Ready = deploymentReadyText(workload.ReadyReplicas, workload.DesiredReplicas)
		ref.DesiredReplicas = workload.DesiredReplicas
		ref.ReadyReplicas = workload.ReadyReplicas
		ref.UpdatedReplicas = workload.UpdatedReplicas
		ref.AvailableReplicas = workload.AvailableReplicas
		ref.AgeSeconds = workload.AgeSeconds
		ref.Containers = append([]string(nil), workload.Containers...)
		ref.Images = append([]string(nil), workload.Images...)
		ref.Selector = workload.Selector
	}
	item.Resources = append(item.Resources, ref)
	category.Total++
}

func deploymentReadyText(ready int32, desired int32) string {
	if desired < 0 {
		desired = 0
	}
	return strconv.FormatInt(int64(ready), 10) + "/" + strconv.FormatInt(int64(desired), 10)
}

func ensureResourceCategory(summary *protocol.ResourceSummary, key string, label string) *protocol.ResourceCategory {
	for i := range summary.Categories {
		if summary.Categories[i].Key == key {
			return &summary.Categories[i]
		}
	}
	summary.Categories = append(summary.Categories, protocol.ResourceCategory{Key: key, Label: label})
	return &summary.Categories[len(summary.Categories)-1]
}

func ensureResourceKind(category *protocol.ResourceCategory, kind string, shortName string) *protocol.ResourceKindSummary {
	for i := range category.Items {
		if category.Items[i].Kind == kind {
			return &category.Items[i]
		}
	}
	category.Items = append(category.Items, protocol.ResourceKindSummary{Kind: kind, ShortName: shortName})
	return &category.Items[len(category.Items)-1]
}

func normalizeResourceCategories(summary *protocol.ResourceSummary) {
	order := map[string]int{"workloads": 0, "storage": 1, "network": 2, "config": 3, "other": 4}
	sort.Slice(summary.Categories, func(i, j int) bool {
		return order[summary.Categories[i].Key] < order[summary.Categories[j].Key]
	})
	for i := range summary.Categories {
		sort.Slice(summary.Categories[i].Items, func(left, right int) bool {
			return summary.Categories[i].Items[left].Kind < summary.Categories[i].Items[right].Kind
		})
		for j := range summary.Categories[i].Items {
			sort.Slice(summary.Categories[i].Items[j].Resources, func(left, right int) bool {
				return summary.Categories[i].Items[j].Resources[left].Name < summary.Categories[i].Items[j].Resources[right].Name
			})
		}
	}
}

func shortResourceName(kind string) string {
	switch strings.ToLower(kind) {
	case "deployment":
		return "DEP"
	case "statefulset":
		return "STS"
	case "daemonset":
		return "DS"
	case "job":
		return "JOB"
	case "cronjob":
		return "CJ"
	default:
		return strings.ToUpper(kind)
	}
}

func apiVersionForKind(kind string) string {
	switch strings.ToLower(kind) {
	case "deployment", "statefulset", "daemonset":
		return "apps/v1"
	case "job", "cronjob":
		return "batch/v1"
	default:
		return ""
	}
}

func buildVeleroInventory(state kube.VeleroState) protocol.VeleroInventory {
	status := state.Status
	if status == "" {
		status = "unknown"
	}
	return protocol.VeleroInventory{
		Status:                  status,
		BackupStorageLocations:  cloneMapSlice(state.BackupStorageLocations),
		VolumeSnapshotLocations: cloneMapSlice(state.VolumeSnapshotLocations),
		RecentBackups:           cloneMapSlice(state.RecentBackups),
		RecentRestores:          cloneMapSlice(state.RecentRestores),
	}
}

func nodeRole(labels map[string]string) string {
	roles := make([]string, 0, 2)
	for key := range labels {
		if !strings.HasPrefix(key, "node-role.kubernetes.io/") {
			continue
		}
		role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
		if role == "" {
			continue
		}
		if role == "master" {
			role = "control-plane"
		}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return "<none>"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneMapSlice(input []map[string]any) []map[string]any {
	if len(input) == 0 {
		return []map[string]any{}
	}
	output := make([]map[string]any, 0, len(input))
	for _, item := range input {
		cloned := make(map[string]any, len(item))
		for key, value := range item {
			cloned[key] = value
		}
		output = append(output, cloned)
	}
	return output
}
