package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/cacheusage"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func cacheUsageConfiguration(pod corev1.Pod, drill bool) cacheusage.Cache {
	values := map[string]string{}
	for _, container := range pod.Spec.Containers {
		if container.Name != "node-agent" {
			continue
		}
		for _, variable := range container.Env {
			if variable.ValueFrom == nil {
				values[variable.Name] = variable.Value
			}
		}
	}
	enabledKey, sizeKey, pathKey, mountPath := "HCDR_CACHE_PERSISTENCE", "HCDR_CACHE_BUDGET_BYTES", "HCDR_CACHE_INDEX_PATH", cacheRoot
	if drill {
		enabledKey, sizeKey, pathKey, mountPath = "HCDR_DRILL_DATA_CACHE", "HCDR_DRILL_DATA_CACHE_BUDGET_BYTES", "HCDR_DRILL_DATA_CACHE_PATH", cacheRoot+"/drill-data"
	}
	result := cacheusage.Cache{Path: values[pathKey]}
	if !drill && result.Path == "" {
		result.Path = values["HCDR_CACHE_HOST_ROOT"]
	}
	for _, container := range pod.Spec.Containers {
		if container.Name != "node-agent" {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if mount.MountPath != mountPath {
				continue
			}
			for _, volume := range pod.Spec.Volumes {
				if volume.Name == mount.Name && volume.HostPath != nil {
					result.Path = volume.HostPath.Path
				}
			}
		}
	}
	enabled, err := strconv.ParseBool(values[enabledKey])
	if err != nil {
		result.Error = "Node cache configuration is unavailable"
		return result
	}
	result.Enabled = &enabled
	result.MaxBytes, err = strconv.ParseInt(values[sizeKey], 10, 64)
	if err != nil || result.MaxBytes <= 0 {
		result.Error = "Node cache size is unavailable"
	}
	if !safeUsageDirectory(result.Path) {
		result.Error = "Node cache directory is unavailable or unsafe"
	}
	return result
}

func safeUsageDirectory(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Dir(path) != "/"
}

func cacheUsageJob(namespace, nodeName, image string, secrets []corev1.LocalObjectReference, paths []string) (*batchv1.Job, []string, error) {
	if len(paths) < 1 || len(paths) > 2 || nodeName == "" || image == "" {
		return nil, nil, fmt.Errorf("cache usage job requires a node, image and one or two directories")
	}
	deadline, ttl, backoff := int64(240), int32(300), int32(0)
	readOnly := true
	rootUser := int64(0)
	automount := false
	directoryType := corev1.HostPathDirectory
	container := corev1.Container{Name: "usage", Image: image, Command: []string{"/comm-agent", "cache-usage"}, SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly}}
	container.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}
	volumes := []corev1.Volume{}
	localPaths := []string{}
	for index, path := range paths {
		if !safeUsageDirectory(path) {
			return nil, nil, fmt.Errorf("unsafe cache usage directory")
		}
		name := fmt.Sprintf("cache-%d", index)
		mountPath := "/scan/" + name
		volumes = append(volumes, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: filepath.Dir(path), Type: &directoryType}}})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: name, MountPath: mountPath, ReadOnly: true})
		localPaths = append(localPaths, mountPath+"/"+filepath.Base(path))
	}
	container.Command = append(container.Command, localPaths...)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{GenerateName: "hypercdr-cache-usage-", Namespace: namespace, Labels: map[string]string{"hypercdr.io/cache-usage": "true"}}, Spec: batchv1.JobSpec{ActiveDeadlineSeconds: &deadline, TTLSecondsAfterFinished: &ttl, BackoffLimit: &backoff, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeName: nodeName, RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: &automount, ImagePullSecrets: secrets, SecurityContext: &corev1.PodSecurityContext{RunAsUser: &rootUser}, Containers: []corev1.Container{container}, Volumes: volumes}}}}
	return job, localPaths, nil
}

func nodeAgentReady(pod corev1.Pod) bool {
	if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *KubernetesAgentRuntime) CollectNodeCacheUsage(ctx context.Context, namespace, agentPodName string) ([]cacheusage.Node, error) {
	nodes, err := r.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	daemon, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(daemon.Spec.Selector)})
	if err != nil {
		return nil, err
	}
	agentPod, err := r.client.CoreV1().Pods(namespace).Get(ctx, agentPodName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	image := workloadContainerImage(agentPod.Spec.Containers, "comm-agent")
	byNode := map[string]corev1.Pod{}
	for _, pod := range pods.Items {
		if !nodeAgentReady(pod) {
			continue
		}
		previous, exists := byNode[pod.Spec.NodeName]
		if !exists || pod.CreationTimestamp.After(previous.CreationTimestamp.Time) {
			byNode[pod.Spec.NodeName] = pod
		}
	}
	results := make([]cacheusage.Node, len(nodes.Items))
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for index, node := range nodes.Items {
		results[index].Name = node.Name
		pod, ready := byNode[node.Name]
		nodeReady := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				nodeReady = true
			}
		}
		if !ready || !nodeReady {
			results[index].Index.Error = "Node agent is not ready; showing the last observation"
			results[index].Drill.Error = results[index].Index.Error
			continue
		}
		results[index].Index = cacheUsageConfiguration(pod, false)
		results[index].Drill = cacheUsageConfiguration(pod, true)
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				results[index].Index.Error = ctx.Err().Error()
				results[index].Drill.Error = ctx.Err().Error()
				return
			}
			r.collectNodeUsage(ctx, namespace, image, agentPod.Spec.ImagePullSecrets, &results[index])
		}(index)
	}
	workers.Wait()
	return results, nil
}

func (r *KubernetesAgentRuntime) collectNodeUsage(ctx context.Context, namespace, image string, secrets []corev1.LocalObjectReference, node *cacheusage.Node) {
	paths := []string{}
	caches := []*cacheusage.Cache{}
	for _, cache := range []*cacheusage.Cache{&node.Index, &node.Drill} {
		if cache.Error == "" {
			paths = append(paths, cache.Path)
			caches = append(caches, cache)
		}
	}
	if len(paths) == 0 {
		return
	}
	samples, err := r.runCacheUsageJob(ctx, namespace, node.Name, image, secrets, paths)
	for index, cache := range caches {
		if err != nil {
			cache.Error = err.Error()
			continue
		}
		sample := samples[index]
		if sample.Error != "" {
			cache.Error = sample.Error
			continue
		}
		if sample.Usage == nil || sample.Usage.UsedBytes < 0 {
			cache.Error = "Node returned invalid cache usage"
			continue
		}
		bytes := sample.Usage.UsedBytes
		cache.UsedBytes, cache.ObservedAt = &bytes, sample.ObservedAt
	}
}

func (r *KubernetesAgentRuntime) runCacheUsageJob(ctx context.Context, namespace, nodeName, image string, secrets []corev1.LocalObjectReference, paths []string) ([]cacheusage.PathUsage, error) {
	job, localPaths, err := cacheUsageJob(namespace, nodeName, image, secrets, paths)
	if err != nil {
		return nil, err
	}
	created, err := r.client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		policy := metav1.DeletePropagationBackground
		_ = r.client.BatchV1().Jobs(namespace).Delete(cleanupCtx, created.Name, metav1.DeleteOptions{PropagationPolicy: &policy})
	}()
	err = wait.PollUntilContextTimeout(ctx, time.Second, 4*time.Minute, true, func(ctx context.Context) (bool, error) {
		current, err := r.client.BatchV1().Jobs(namespace).Get(ctx, created.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
				return false, fmt.Errorf("cache usage collection failed on node %s: %s", nodeName, condition.Reason)
			}
		}
		return current.Status.Succeeded > 0, nil
	})
	if err != nil {
		return nil, err
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + created.Name})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodSucceeded {
			continue
		}
		maximum := int64(1 << 20)
		stream, err := r.client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "usage", LimitBytes: &maximum}).Stream(ctx)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(stream, maximum))
		_ = stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		var samples []cacheusage.PathUsage
		if err := json.Unmarshal(body, &samples); err != nil {
			return nil, err
		}
		if len(samples) != len(localPaths) {
			return nil, fmt.Errorf("node returned incomplete cache usage")
		}
		for index, sample := range samples {
			if sample.Path != localPaths[index] {
				return nil, fmt.Errorf("node returned cache usage for a different directory")
			}
			if sample.Usage != nil {
				if _, err := time.Parse(time.RFC3339Nano, sample.ObservedAt); err != nil {
					return nil, fmt.Errorf("node cache usage timestamp is invalid")
				}
			}
		}
		return samples, nil
	}
	return nil, fmt.Errorf("completed cache usage pod was not found")
}
