package kube

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	PrepareVeleroUpgrade(ctx context.Context) error
	UpgradeVelero(ctx context.Context, options VeleroUpgradeOptions) error
}

type ComponentLogEntry struct {
	Timestamp          time.Time
	Pod, Node, Message string
}
type ComponentLogCollector interface {
	CollectComponentLogs(ctx context.Context, namespace, component string, since time.Time, tailLines int64) ([]ComponentLogEntry, bool, error)
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
	client kubernetes.Interface
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
	return &KubernetesAgentRuntime{client: client}, nil
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
	if err := r.PrepareVeleroUpgrade(ctx); err != nil {
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
			initContainers = append(initContainers, map[string]any{"name": plugin.name, "image": plugin.image})
		}
	}
	serverContainer := map[string]any{"name": "velero", "image": options.Image}
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
	nodeAgentContainer := map[string]any{"name": "node-agent", "image": options.Image}
	if options.NodeAgentConcurrency > 0 {
		nodeAgentContainer["args"] = []string{"node-agent", "server", "--node-agent-configmap=node-agent-config"}
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

func (r *KubernetesAgentRuntime) PrepareVeleroUpgrade(ctx context.Context) error {
	if err := r.ensureDaemonSetUpgradePermission(ctx); err != nil {
		return fmt.Errorf("ensure node-agent upgrade permission: %w", err)
	}
	if err := r.ensureVeleroCRDUpgradePermission(ctx); err != nil {
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
		if _, err := r.client.StorageV1().StorageClasses().Get(ctx, options.CacheStorageClass, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("cache storage class %q is unavailable: %w", options.CacheStorageClass, err)
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
		return err
	}
	if err != nil {
		return err
	}
	current.Data = map[string]string{"node-agent-config.json": string(data)}
	_, err = configMaps.Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func (r *KubernetesAgentRuntime) ensureDaemonSetUpgradePermission(ctx context.Context) error {
	role, err := r.client.RbacV1().ClusterRoles().Get(ctx, "hypercdr-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i := range role.Rules {
		rule := &role.Rules[i]
		if !containsString(rule.APIGroups, "apps") || !containsString(rule.Resources, "daemonsets") {
			continue
		}
		if !containsString(rule.Verbs, "patch") {
			rule.Verbs = append(rule.Verbs, "patch")
		}
		if !containsString(rule.Verbs, "update") {
			rule.Verbs = append(rule.Verbs, "update")
		}
		_, err = r.client.RbacV1().ClusterRoles().Update(ctx, role, metav1.UpdateOptions{})
		return err
	}
	return fmt.Errorf("hypercdr-agent ClusterRole has no apps/daemonsets rule")
}

func (r *KubernetesAgentRuntime) ensureVeleroCRDUpgradePermission(ctx context.Context) error {
	role, err := r.client.RbacV1().ClusterRoles().Get(ctx, "hypercdr-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	for i := range role.Rules {
		rule := &role.Rules[i]
		if !containsString(rule.APIGroups, "apiextensions.k8s.io") || !containsString(rule.Resources, "customresourcedefinitions") {
			continue
		}
		for _, verb := range []string{"create", "update", "patch"} {
			if !containsString(rule.Verbs, verb) {
				rule.Verbs = append(rule.Verbs, verb)
			}
		}
		_, err = r.client.RbacV1().ClusterRoles().Update(ctx, role, metav1.UpdateOptions{})
		return err
	}
	return fmt.Errorf("hypercdr-agent ClusterRole has no apiextensions.k8s.io/customresourcedefinitions rule")
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
