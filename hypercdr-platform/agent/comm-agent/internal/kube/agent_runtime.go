package kube

import (
	"context"
	"encoding/json"
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
