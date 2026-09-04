package kube

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type AgentRuntimeReader interface {
	PodImageStatus(ctx context.Context, namespace string, podName string, containerName string) (image string, imageID string, digest string, err error)
}

type AgentUpgrader interface {
	UpgradeAgent(ctx context.Context, options AgentUpgradeOptions) error
}

type VeleroRuntimeManager interface {
	VeleroRuntimeStatus(ctx context.Context, namespace string) (VeleroRuntimeStatus, error)
	PrepareVeleroUpgrade(ctx context.Context, namespace string) error
	UpgradeVelero(ctx context.Context, options VeleroUpgradeOptions) error
	UpgradeOADP(ctx context.Context, options OADPUpgradeOptions) error
}

type ComponentLogEntry struct {
	Timestamp          time.Time
	Pod, Node, Message string
}
type ComponentLogCollector interface {
	CollectComponentLogs(ctx context.Context, namespace, component string, since time.Time, tailLines int64) ([]ComponentLogEntry, bool, error)
}

type RestoreDataPathFailure struct {
	Code      string
	Message   string
	Pod       string
	Node      string
	LogDetail string
}

// RestoreDataPathFailureReader provides a fallback source of truth when a
// Kubernetes API/etcd outage prevents Velero from persisting a failed
// PodVolumeRestore status. The restore hosting pod has already written the
// authoritative Kopia error to its log in that situation.
type RestoreDataPathFailureReader interface {
	FindRestoreDataPathFailure(ctx context.Context, namespace, restoreName string, since time.Time) (RestoreDataPathFailure, bool, error)
}

type VeleroRuntimeStatus struct {
	Version              string
	Image                string
	ImageDigest          string
	ServerReady          bool
	NodeAgentDesired     int32
	NodeAgentReady       int32
	NodeAgentImageDigest string
}

type RestoreCachePreflightResult struct {
	Enabled             bool
	Usable              bool
	StorageClass        string
	ResidentThresholdMB int64
	CacheLimitMB        int64
	Message             string
}

type RestoreCachePreflighter interface {
	PreflightRestoreCache(ctx context.Context, namespace string) (RestoreCachePreflightResult, error)
}

type VeleroUpgradeOptions struct {
	Namespace                string
	Image                    string
	DeploymentName           string
	DaemonSetName            string
	AWSPluginImage           string
	AzurePluginImage         string
	GCPPluginImage           string
	ConcurrentBackups        int
	NodeAgentConcurrency     int
	PrepareQueueLength       int
	CacheStorageClass        string
	CacheResidentThresholdMB int
	CacheLimitMB             int
}

type OADPUpgradeOptions struct {
	Namespace, CatalogImage, Package, Channel, TargetCSV string
}

type AgentUpgradeOptions struct {
	Namespace         string
	DeploymentName    string
	ContainerName     string
	Image             string
	Version           string
	RolloutAnnotation string
}

type KubernetesAgentRuntime struct {
	client  kubernetes.Interface
	dynamic dynamic.Interface
}

func (r *KubernetesAgentRuntime) PreflightRestoreCache(ctx context.Context, namespace string) (RestoreCachePreflightResult, error) {
	namespace = firstNonEmpty(namespace, "hypercdr-agent")
	result := RestoreCachePreflightResult{Usable: true, Message: "Restore cache is not enabled; Velero will use data mover ephemeral storage."}
	configMap, err := r.client.CoreV1().ConfigMaps(namespace).Get(ctx, "node-agent-config", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read node-agent cache configuration: %w", err)
	}
	var config struct {
		CachePVC *struct {
			StorageClass          string `json:"storageClass"`
			ResidentThresholdInMB int64  `json:"residentThresholdInMB"`
		} `json:"cachePVC"`
	}
	if err := json.Unmarshal([]byte(configMap.Data["node-agent-config.json"]), &config); err != nil {
		return result, fmt.Errorf("parse node-agent cache configuration: %w", err)
	}
	if config.CachePVC == nil || strings.TrimSpace(config.CachePVC.StorageClass) == "" {
		return result, nil
	}
	result.Enabled = true
	result.Usable = false
	result.StorageClass = strings.TrimSpace(config.CachePVC.StorageClass)
	result.ResidentThresholdMB = config.CachePVC.ResidentThresholdInMB
	storageClass, err := r.client.StorageV1().StorageClasses().Get(ctx, result.StorageClass, metav1.GetOptions{})
	if err != nil {
		return result, fmt.Errorf("restore cache StorageClass %q is unavailable: %w", result.StorageClass, err)
	}
	if strings.TrimSpace(storageClass.Provisioner) == "" {
		return result, fmt.Errorf("restore cache StorageClass %q has no dynamic provisioner", result.StorageClass)
	}
	if storageClass.ReclaimPolicy != nil && *storageClass.ReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
		return result, fmt.Errorf("restore cache StorageClass %q must use reclaimPolicy Delete", result.StorageClass)
	}
	repositoryConfig, err := r.client.CoreV1().ConfigMaps(namespace).Get(ctx, "backup-repository-config", metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return result, fmt.Errorf("read backup repository cache configuration: %w", err)
	}
	result.CacheLimitMB = 5120
	if err == nil {
		var repository struct {
			CacheLimitMB int64 `json:"cacheLimitMB"`
		}
		if raw := strings.TrimSpace(repositoryConfig.Data["kopia"]); raw != "" {
			if err := json.Unmarshal([]byte(raw), &repository); err != nil {
				return result, fmt.Errorf("parse Kopia cache configuration: %w", err)
			}
			if repository.CacheLimitMB > 0 {
				result.CacheLimitMB = repository.CacheLimitMB
			}
		}
	}
	result.Usable = true
	result.Message = fmt.Sprintf("Restore cache is ready with StorageClass %s (threshold %d MiB, cache limit %d MiB).", result.StorageClass, result.ResidentThresholdMB, result.CacheLimitMB)
	return result, nil
}

func NewKubernetesAgentRuntime(kubeconfigPath string) (*KubernetesAgentRuntime, error) {
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
	return &KubernetesAgentRuntime{client: client, dynamic: dynamicClient}, nil
}

func (r *KubernetesAgentRuntime) UpgradeOADP(ctx context.Context, options OADPUpgradeOptions) error {
	if r.dynamic == nil {
		return errors.New("dynamic Kubernetes client is unavailable")
	}
	namespace := firstNonEmpty(options.Namespace, "openshift-adp")
	if strings.TrimSpace(options.CatalogImage) == "" {
		return errors.New("OADP catalog image is required")
	}
	catalogs := r.dynamic.Resource(schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "catalogsources"}).Namespace("openshift-marketplace")
	catalog, err := catalogs.Get(ctx, "hypercdr-oadp", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read OADP CatalogSource: %w", err)
	}
	if err = unstructured.SetNestedField(catalog.Object, options.CatalogImage, "spec", "image"); err != nil {
		return err
	}
	if _, err = catalogs.Update(ctx, catalog, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update OADP CatalogSource: %w", err)
	}
	subscriptions := r.dynamic.Resource(schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "subscriptions"}).Namespace(namespace)
	subscription, err := subscriptions.Get(ctx, "hypercdr-oadp-operator", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read OADP Subscription: %w", err)
	}
	_ = unstructured.SetNestedField(subscription.Object, firstNonEmpty(options.Package, "oadp-operator"), "spec", "name")
	_ = unstructured.SetNestedField(subscription.Object, firstNonEmpty(options.Channel, "stable-1.3"), "spec", "channel")
	if _, err = subscriptions.Update(ctx, subscription, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update OADP Subscription: %w", err)
	}
	csvs := r.dynamic.Resource(schema.GroupVersionResource{Group: "operators.coreos.com", Version: "v1alpha1", Resource: "clusterserviceversions"}).Namespace(namespace)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		current, getErr := subscriptions.Get(ctx, "hypercdr-oadp-operator", metav1.GetOptions{})
		if getErr == nil {
			csvName, _, _ := unstructured.NestedString(current.Object, "status", "installedCSV")
			if csvName != "" && (strings.TrimSpace(options.TargetCSV) == "" || csvName == options.TargetCSV) {
				csv, csvErr := csvs.Get(ctx, csvName, metav1.GetOptions{})
				if csvErr == nil {
					phase, _, _ := unstructured.NestedString(csv.Object, "status", "phase")
					if phase == "Succeeded" {
						break
					}
					if phase == "Failed" {
						return fmt.Errorf("OADP CSV %s entered phase %s", csvName, phase)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for OADP CSV: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	return r.waitForVeleroRollout(ctx, namespace, "velero", "node-agent")
}

func (r *KubernetesAgentRuntime) waitForVeleroRollout(ctx context.Context, namespace, deploymentName, daemonSetName string) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		deployment, depErr := r.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		daemonSet, dsErr := r.client.AppsV1().DaemonSets(namespace).Get(ctx, daemonSetName, metav1.GetOptions{})
		if depErr == nil && dsErr == nil && deployment.Status.Replicas > 0 && deployment.Status.UpdatedReplicas == deployment.Status.Replicas && deployment.Status.AvailableReplicas == deployment.Status.Replicas && daemonSet.Status.DesiredNumberScheduled > 0 && daemonSet.Status.UpdatedNumberScheduled == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.NumberUnavailable == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for OADP Velero rollout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *KubernetesAgentRuntime) PodImageStatus(ctx context.Context, namespace string, podName string, containerName string) (string, string, string, error) {
	if namespace == "" || podName == "" {
		return "", "", "", nil
	}
	pod, err := r.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", err
	}
	image := containerImage(pod, containerName)
	imageID := containerImageID(pod, containerName)
	return image, imageID, digestFromImageID(imageID), nil
}

func (r *KubernetesAgentRuntime) CollectComponentLogs(ctx context.Context, namespace, component string, since time.Time, tailLines int64) ([]ComponentLogEntry, bool, error) {
	containers := map[string]string{"comm-agent": "comm-agent", "velero": "velero", "node-agent": "node-agent"}
	container, ok := containers[component]
	if !ok {
		return nil, false, fmt.Errorf("unsupported log component %q", component)
	}
	if tailLines <= 0 || tailLines > 2000 {
		tailLines = 1000
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, false, err
	}
	result := make([]ComponentLogEntry, 0)
	truncated := false
	for _, pod := range pods.Items {
		found := false
		for _, candidate := range pod.Spec.Containers {
			if candidate.Name == container {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		options := &corev1.PodLogOptions{Container: container, Timestamps: true, TailLines: &tailLines}
		if !since.IsZero() {
			value := metav1.NewTime(since)
			options.SinceTime = &value
		}
		stream, err := r.client.CoreV1().Pods(namespace).GetLogs(pod.Name, options).Stream(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("read %s logs from %s: %w", component, pod.Name, err)
		}
		scanner := bufio.NewScanner(io.LimitReader(stream, 10<<20))
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		count := int64(0)
		for scanner.Scan() {
			count++
			line := scanner.Text()
			timestamp, message := splitKubernetesLogLine(line)
			result = append(result, ComponentLogEntry{Timestamp: timestamp, Pod: pod.Name, Node: pod.Spec.NodeName, Message: message})
		}
		_ = stream.Close()
		if err := scanner.Err(); err != nil {
			return nil, false, err
		}
		if count >= tailLines {
			truncated = true
		}
	}
	return result, truncated, nil
}

func (r *KubernetesAgentRuntime) FindRestoreDataPathFailure(ctx context.Context, namespace, restoreName string, since time.Time) (RestoreDataPathFailure, bool, error) {
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return RestoreDataPathFailure{}, false, err
	}
	for _, pod := range pods.Items {
		if !strings.HasPrefix(pod.Name, restoreName+"-") {
			continue
		}
		for _, container := range pod.Spec.Containers {
			tailLines := int64(300)
			options := &corev1.PodLogOptions{Container: container.Name, Timestamps: true, TailLines: &tailLines}
			if !since.IsZero() {
				value := metav1.NewTime(since)
				options.SinceTime = &value
			}
			stream, err := r.client.CoreV1().Pods(namespace).GetLogs(pod.Name, options).Stream(ctx)
			if err != nil {
				continue
			}
			raw, readErr := io.ReadAll(io.LimitReader(stream, 2<<20))
			_ = stream.Close()
			if readErr != nil {
				continue
			}
			if failure, ok := parseRestoreDataPathFailure(string(raw)); ok {
				failure.Pod = pod.Name
				failure.Node = pod.Spec.NodeName
				return failure, true, nil
			}
		}
	}
	return RestoreDataPathFailure{}, false, nil
}

func parseRestoreDataPathFailure(logText string) (RestoreDataPathFailure, bool) {
	var detail string
	for _, line := range strings.Split(logText, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "restore data path failed") && !strings.Contains(lower, "async fs restore was not completed") {
			continue
		}
		detail = strings.TrimSpace(line)
		if strings.Contains(lower, "read-only file system") {
			return RestoreDataPathFailure{
				Code:      "RESTORE_VOLUME_FILESYSTEM_READ_ONLY",
				Message:   extractRestoreLogError(detail, "Persistent volume restoration failed because the target filesystem became read-only."),
				LogDetail: detail,
			}, true
		}
	}
	if detail != "" {
		return RestoreDataPathFailure{
			Code:      "RESTORE_VOLUME_DATA_PATH_FAILED",
			Message:   extractRestoreLogError(detail, "Persistent volume data restoration failed."),
			LogDetail: detail,
		}, true
	}
	return RestoreDataPathFailure{}, false
}

func extractRestoreLogError(line, fallback string) string {
	for _, marker := range []string{`error="`, `error=`} {
		if index := strings.Index(line, marker); index >= 0 {
			value := line[index+len(marker):]
			if marker == `error="` {
				if end := strings.Index(value, `"`); end >= 0 {
					value = value[:end]
				}
			}
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return fallback
}

func splitKubernetesLogLine(line string) (time.Time, string) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 {
		if value, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
			return value.UTC(), parts[1]
		}
	}
	return time.Now().UTC(), line
}

func (r *KubernetesAgentRuntime) UpgradeAgent(ctx context.Context, options AgentUpgradeOptions) error {
	namespace := strings.TrimSpace(options.Namespace)
	if namespace == "" {
		namespace = "hypercdr-agent"
	}
	deploymentName := strings.TrimSpace(options.DeploymentName)
	if deploymentName == "" {
		deploymentName = "hypercdr-comm-agent"
	}
	containerName := strings.TrimSpace(options.ContainerName)
	if containerName == "" {
		containerName = "comm-agent"
	}
	annotationValue := strings.TrimSpace(options.RolloutAnnotation)
	if annotationValue == "" {
		annotationValue = time.Now().UTC().Format(time.RFC3339Nano)
	}
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"hypercdr.io/agent-upgrade-at": annotationValue,
					},
				},
				"spec": map[string]any{
					"containers": []map[string]any{
						{
							"name":  containerName,
							"image": options.Image,
							"env": []map[string]string{
								{"name": "HCDR_AGENT_IMAGE", "value": options.Image},
								{"name": "HCDR_AGENT_VERSION", "value": options.Version},
							},
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = r.client.AppsV1().Deployments(namespace).Patch(ctx, deploymentName, types.StrategicMergePatchType, raw, metav1.PatchOptions{})
	return err
}

func (r *KubernetesAgentRuntime) VeleroRuntimeStatus(ctx context.Context, namespace string) (VeleroRuntimeStatus, error) {
	namespace = firstNonEmpty(namespace, "hypercdr-agent")
	deployment, err := r.client.AppsV1().Deployments(namespace).Get(ctx, "velero", metav1.GetOptions{})
	if err != nil {
		return VeleroRuntimeStatus{}, err
	}
	daemonSet, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
	if err != nil {
		return VeleroRuntimeStatus{}, err
	}
	status := VeleroRuntimeStatus{
		Image:            workloadContainerImage(deployment.Spec.Template.Spec.Containers, "velero"),
		ServerReady:      deployment.Status.Replicas > 0 && deployment.Status.AvailableReplicas == deployment.Status.Replicas && deployment.Status.UpdatedReplicas == deployment.Status.Replicas,
		NodeAgentDesired: daemonSet.Status.DesiredNumberScheduled,
		NodeAgentReady:   daemonSet.Status.NumberReady,
	}
	status.Version = imageTag(status.Image)
	serverPods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector)})
	if err != nil {
		return VeleroRuntimeStatus{}, err
	}
	status.ImageDigest = commonReadyPodDigest(serverPods.Items, "velero", 1)
	nodePods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(daemonSet.Spec.Selector)})
	if err != nil {
		return VeleroRuntimeStatus{}, err
	}
	status.NodeAgentImageDigest = commonReadyPodDigest(nodePods.Items, "node-agent", int(status.NodeAgentDesired))
	return status, nil
}

func (r *KubernetesAgentRuntime) UpgradeVelero(ctx context.Context, options VeleroUpgradeOptions) error {
	namespace := firstNonEmpty(options.Namespace, "hypercdr-agent")
	deploymentName := firstNonEmpty(options.DeploymentName, "velero")
	daemonSetName := firstNonEmpty(options.DaemonSetName, "node-agent")
	if strings.TrimSpace(options.Image) == "" {
		return fmt.Errorf("velero image is required")
	}
	if err := r.PrepareVeleroUpgrade(ctx, namespace); err != nil {
		return err
	}
	if err := r.ensureVeleroPerformanceConfig(ctx, namespace, options); err != nil {
		return fmt.Errorf("configure velero performance options: %w", err)
	}
	initContainers := []map[string]any{}
	for _, plugin := range []struct{ name, image string }{
		{"velero-plugin-for-aws", options.AWSPluginImage},
		{"velero-plugin-for-microsoft-azure", options.AzurePluginImage},
		{"velero-plugin-for-gcp", options.GCPPluginImage},
	} {
		if strings.TrimSpace(plugin.image) != "" {
			initContainers = append(initContainers, map[string]any{"name": plugin.name, "image": plugin.image, "imagePullPolicy": "Always"})
		}
	}
	serverContainer := map[string]any{"name": "velero", "image": options.Image, "imagePullPolicy": "Always"}
	if options.ConcurrentBackups > 0 {
		serverContainer["args"] = []string{"server", "--default-volumes-to-fs-backup", fmt.Sprintf("--concurrent-backups=%d", options.ConcurrentBackups), "--plugin-dir=/plugins"}
	}
	podSpec := map[string]any{"containers": []map[string]any{serverContainer}}
	if len(initContainers) > 0 {
		podSpec["initContainers"] = initContainers
	}
	deploymentPatch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"hypercdr.io/velero-upgrade-at": time.Now().UTC().Format(time.RFC3339Nano)}}, "spec": podSpec}}})
	if _, err := r.client.AppsV1().Deployments(namespace).Patch(ctx, deploymentName, types.StrategicMergePatchType, deploymentPatch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("update velero deployment: %w", err)
	}
	nodeAgentContainer := map[string]any{"name": "node-agent", "image": options.Image, "imagePullPolicy": "Always"}
	if options.NodeAgentConcurrency > 0 {
		nodeAgentContainer["args"] = []string{"node-agent", "server", "--node-agent-configmap=node-agent-config", "--backup-repository-configmap=backup-repository-config"}
	}
	daemonSetPatch, _ := json.Marshal(map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"hypercdr.io/velero-upgrade-at": time.Now().UTC().Format(time.RFC3339Nano)}}, "spec": map[string]any{"containers": []map[string]any{nodeAgentContainer}}}}})
	if _, err := r.client.AppsV1().DaemonSets(namespace).Patch(ctx, daemonSetName, types.StrategicMergePatchType, daemonSetPatch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("update node-agent daemonset: %w", err)
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		deployment, err := r.client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		daemonSet, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, daemonSetName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		serverReady := deployment.Status.Replicas > 0 && deployment.Status.UpdatedReplicas == deployment.Status.Replicas && deployment.Status.AvailableReplicas == deployment.Status.Replicas
		nodesReady := daemonSet.Status.DesiredNumberScheduled > 0 && daemonSet.Status.UpdatedNumberScheduled == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled && daemonSet.Status.NumberUnavailable == 0
		if serverReady && nodesReady {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for velero rollout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *KubernetesAgentRuntime) PrepareVeleroUpgrade(ctx context.Context, namespace string) error {
	namespace = firstNonEmpty(namespace, "hypercdr-agent")
	if err := r.ensureDaemonSetUpgradePermission(ctx, namespace); err != nil {
		return fmt.Errorf("ensure node-agent upgrade permission: %w", err)
	}
	if err := r.ensureVeleroCRDUpgradePermission(ctx, namespace); err != nil {
		return fmt.Errorf("ensure Velero CRD upgrade permission: %w", err)
	}
	return nil
}

func (r *KubernetesAgentRuntime) ensureVeleroPerformanceConfig(ctx context.Context, namespace string, options VeleroUpgradeOptions) error {
	if options.NodeAgentConcurrency <= 0 {
		return nil
	}
	config := map[string]any{
		"loadConcurrency": map[string]any{
			"globalConfig":       options.NodeAgentConcurrency,
			"prepareQueueLength": options.PrepareQueueLength,
		},
	}
	if strings.TrimSpace(options.CacheStorageClass) != "" {
		storageClass, err := r.client.StorageV1().StorageClasses().Get(ctx, options.CacheStorageClass, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("cache storage class %q is unavailable: %w", options.CacheStorageClass, err)
		}
		if strings.TrimSpace(storageClass.Provisioner) == "" {
			return fmt.Errorf("cache storage class %q does not define a dynamic provisioner", options.CacheStorageClass)
		}
		if storageClass.ReclaimPolicy != nil && *storageClass.ReclaimPolicy != corev1.PersistentVolumeReclaimDelete {
			return fmt.Errorf("cache storage class %q must use reclaimPolicy Delete", options.CacheStorageClass)
		}
		config["cachePVC"] = map[string]any{
			"storageClass":          options.CacheStorageClass,
			"residentThresholdInMB": options.CacheResidentThresholdMB,
		}
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	configMaps := r.client.CoreV1().ConfigMaps(namespace)
	current, err := configMaps.Get(ctx, "node-agent-config", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = configMaps.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "node-agent-config", Namespace: namespace},
			Data:       map[string]string{"node-agent-config.json": string(data)},
		}, metav1.CreateOptions{})
	} else if err != nil {
		return err
	} else {
		current.Data = map[string]string{"node-agent-config.json": string(data)}
		_, err = configMaps.Update(ctx, current, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}
	cacheLimitMB := options.CacheLimitMB
	if cacheLimitMB <= 0 {
		cacheLimitMB = 5120
	}
	repositoryData, err := json.Marshal(map[string]any{"cacheLimitMB": cacheLimitMB})
	if err != nil {
		return err
	}
	repositoryConfig, err := configMaps.Get(ctx, "backup-repository-config", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = configMaps.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "backup-repository-config", Namespace: namespace}, Data: map[string]string{"kopia": string(repositoryData)}}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	repositoryConfig.Data = map[string]string{"kopia": string(repositoryData)}
	_, err = configMaps.Update(ctx, repositoryConfig, metav1.UpdateOptions{})
	return err
}

func (r *KubernetesAgentRuntime) ensureDaemonSetUpgradePermission(ctx context.Context, namespace string) error {
	roleName := scopedRBACName("hypercdr-agent", namespace)
	role, err := r.client.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i := range role.Rules {
		rule := &role.Rules[i]
		if !containsExactString(rule.APIGroups, "apps") || !containsExactString(rule.Resources, "daemonsets") {
			continue
		}
		if containsString(rule.Verbs, "patch") && containsString(rule.Verbs, "update") {
			return nil
		}
		return fmt.Errorf("%s ClusterRole apps/daemonsets rule requires patch and update", roleName)
	}
	return fmt.Errorf("%s ClusterRole has no apps/daemonsets rule", roleName)
}

func (r *KubernetesAgentRuntime) ensureVeleroCRDUpgradePermission(ctx context.Context, namespace string) error {
	roleName := scopedRBACName("hypercdr-agent", namespace)
	role, err := r.client.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i := range role.Rules {
		rule := &role.Rules[i]
		if !containsExactString(rule.APIGroups, "apiextensions.k8s.io") || !containsExactString(rule.Resources, "customresourcedefinitions") {
			continue
		}
		complete := true
		for _, verb := range []string{"create", "update", "patch"} {
			complete = complete && containsString(rule.Verbs, verb)
		}
		if complete {
			return nil
		}
		return fmt.Errorf("%s ClusterRole apiextensions.k8s.io/customresourcedefinitions rule requires create, patch, and update", roleName)
	}
	return fmt.Errorf("%s ClusterRole has no apiextensions.k8s.io/customresourcedefinitions rule", roleName)
}

func containsExactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target || value == "*" {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func workloadContainerImage(containers []corev1.Container, name string) string {
	for _, container := range containers {
		if container.Name == name {
			return container.Image
		}
	}
	return ""
}

func commonReadyPodDigest(pods []corev1.Pod, containerName string, expected int) string {
	digest := ""
	count := 0
	for i := range pods {
		for _, container := range pods[i].Status.ContainerStatuses {
			if container.Name != containerName || !container.Ready {
				continue
			}
			current := digestFromImageID(container.ImageID)
			if current == "" || (digest != "" && current != digest) {
				return ""
			}
			digest = current
			count++
		}
	}
	if expected > 0 && count < expected {
		return ""
	}
	return digest
}

func imageTag(image string) string {
	image = strings.TrimSpace(image)
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	if slash, colon := strings.LastIndex(image, "/"), strings.LastIndex(image, ":"); colon > slash {
		tag := image[colon+1:]
		if strings.HasPrefix(tag, "v") {
			if suffix := strings.Index(tag, "-"); suffix > 0 {
				return tag[:suffix]
			}
		}
		return tag
	}
	return ""
}

func containerImage(pod *corev1.Pod, containerName string) string {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName || containerName == "" {
			return container.Image
		}
	}
	return ""
}

func containerImageID(pod *corev1.Pod, containerName string) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName || containerName == "" {
			return status.ImageID
		}
	}
	return ""
}

func digestFromImageID(imageID string) string {
	imageID = strings.TrimSpace(imageID)
	if imageID == "" {
		return ""
	}
	if idx := strings.LastIndex(imageID, "@"); idx >= 0 && idx+1 < len(imageID) {
		return imageID[idx+1:]
	}
	if strings.HasPrefix(imageID, "docker-pullable://") {
		trimmed := strings.TrimPrefix(imageID, "docker-pullable://")
		if idx := strings.LastIndex(trimmed, "@"); idx >= 0 && idx+1 < len(trimmed) {
			return trimmed[idx+1:]
		}
	}
	return ""
}

var _ AgentRuntimeReader = (*KubernetesAgentRuntime)(nil)
var _ AgentUpgrader = (*KubernetesAgentRuntime)(nil)
