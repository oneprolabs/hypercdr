package kube

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

const cacheRoot = "/var/cache/hypercdr"
const defaultCacheHostRoot = "/var/lib/hypercdr/cache"

func observedCacheState(daemon *appsv1.DaemonSet) map[string]any {
	state := map[string]any{"phase": "unknown"}
	if len(daemon.Spec.Template.Spec.Containers) != 1 {
		return state
	}
	container := daemon.Spec.Template.Spec.Containers[0]
	values := map[string]string{}
	for _, env := range container.Env {
		if env.ValueFrom == nil {
			values[env.Name] = env.Value
		}
	}
	enabled, err := strconv.ParseBool(values["HCDR_CACHE_PERSISTENCE"])
	if err != nil {
		state["phase"] = "unconfigured"
		return state
	}
	budget, err := strconv.ParseInt(values["HCDR_CACHE_BUDGET_BYTES"], 10, 64)
	if err != nil || budget < 1<<30 || budget > 1024<<30 || budget%(1<<30) != 0 {
		return state
	}
	drillEnabled, _ := strconv.ParseBool(values["HCDR_DRILL_DATA_CACHE"])
	drillBudget, _ := strconv.ParseInt(values["HCDR_DRILL_DATA_CACHE_BUDGET_BYTES"], 10, 64)
	if enabled {
		mounted := false
		for _, mount := range container.VolumeMounts {
			if mount.MountPath != cacheRoot || mount.ReadOnly {
				continue
			}
			for _, volume := range daemon.Spec.Template.Spec.Volumes {
				if volume.Name == mount.Name && volume.HostPath != nil && volume.HostPath.Path != "" {
					mounted = true
				}
			}
		}
		if !mounted || daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] != "v1" || daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != container.Image {
			return state
		}
	}
	state["phase"] = "applying"
	if daemon.Status.ObservedGeneration >= daemon.Generation && daemon.Status.DesiredNumberScheduled > 0 && daemon.Status.UpdatedNumberScheduled == daemon.Status.DesiredNumberScheduled && daemon.Status.NumberReady == daemon.Status.DesiredNumberScheduled && daemon.Status.NumberAvailable == daemon.Status.DesiredNumberScheduled {
		state["phase"] = "applied"
		state["enabled"] = enabled
		state["maxGiB"] = int(budget >> 30)
		state["rootPath"] = values["HCDR_CACHE_HOST_ROOT"]
		state["indexPath"] = values["HCDR_CACHE_HOST_ROOT"]
		if values["HCDR_CACHE_INDEX_PATH"] != "" {
			state["indexPath"] = values["HCDR_CACHE_INDEX_PATH"]
		}
		state["indexEnabled"] = enabled
		state["indexMaxGiB"] = int(budget >> 30)
		state["drillEnabled"] = drillEnabled
		if drillBudget > 0 {
			state["drillMaxGiB"] = int(drillBudget >> 30)
		}
		state["drillPath"] = values["HCDR_DRILL_DATA_CACHE_PATH"]
	}
	return state
}

func supportsPersistentCache(image string) bool {
	image = strings.SplitN(image, "@", 2)[0]
	return strings.HasSuffix(image, "-cache-v1") || strings.Contains(image, "drill-cache-") || image == "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5" || image == "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6"
}

func supportsOnDemandImage(image string) bool {
	image = strings.SplitN(image, "@", 2)[0]
	return strings.HasSuffix(image, "-on-demand-cache-v1") || strings.Contains(image, "drill-cache-") || image == "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6"
}

func (r *KubernetesAgentRuntime) ConfigureCache(ctx context.Context, namespace, rootPath, indexPath, drillPath string, indexEnabled bool, indexMaxGiB int, drillEnabled bool, drillMaxGiB int, clean bool) error {
	if indexMaxGiB < 1 || indexMaxGiB > 1024 || drillMaxGiB < 1 || drillMaxGiB > 4096 {
		return fmt.Errorf("invalid node cache budget")
	}
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		rootPath = defaultCacheHostRoot
	}
	if !strings.HasPrefix(rootPath, "/") || strings.Contains(rootPath, "..") {
		return fmt.Errorf("cache root path must be an absolute local path")
	}
	indexPath = firstNonEmpty(strings.TrimSpace(indexPath), rootPath+"/index")
	drillPath = firstNonEmpty(strings.TrimSpace(drillPath), rootPath+"/drill-data")
	for _, path := range []string{indexPath, drillPath} {
		if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return fmt.Errorf("cache paths must be absolute local paths")
		}
	}
	namespace = firstNonEmpty(namespace, "hypercdr-agent")
	if clean {
		return r.cleanCache(ctx, namespace)
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		daemon, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
		if err != nil {
			return err
		}
		before := daemon.DeepCopy()
		if len(daemon.Spec.Template.Spec.Containers) != 1 {
			return fmt.Errorf("unexpected node-agent template")
		}
		container := &daemon.Spec.Template.Spec.Containers[0]
		if !supportsPersistentCache(container.Image) {
			return fmt.Errorf("installed Velero does not support cache persistence; upgrade Velero first")
		}
		if daemon.Spec.Template.Annotations == nil {
			daemon.Spec.Template.Annotations = map[string]string{}
		}
		if daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] != "v1" || daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != container.Image {
			daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] = "v1"
			daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] = container.Image
		}
		if daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != container.Image {
			return fmt.Errorf("cache capability does not match installed image")
		}
		for _, setting := range []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: strconv.FormatBool(indexEnabled)}, {Name: "HCDR_CACHE_BUDGET_BYTES", Value: strconv.FormatInt(int64(indexMaxGiB)<<30, 10)}, {Name: "HCDR_CACHE_ROOT", Value: cacheRoot}, {Name: "HCDR_CACHE_HOST_ROOT", Value: rootPath}, {Name: "HCDR_CACHE_INDEX_PATH", Value: cacheRoot}, {Name: "HCDR_DRILL_DATA_CACHE", Value: strconv.FormatBool(drillEnabled)}, {Name: "HCDR_DRILL_DATA_CACHE_BUDGET_BYTES", Value: strconv.FormatInt(int64(drillMaxGiB)<<30, 10)}, {Name: "HCDR_DRILL_DATA_CACHE_PATH", Value: cacheRoot + "/drill-data"}} {
			found := false
			for index := range container.Env {
				if container.Env[index].Name == setting.Name {
					container.Env[index] = setting
					found = true
				}
			}
			if !found {
				container.Env = append(container.Env, setting)
			}
		}
		typeDirectory := corev1.HostPathDirectoryOrCreate
		for _, volumeSpec := range []struct{ name, path, mount string }{{"hypercdr-kopia-cache", indexPath, cacheRoot}, {"hypercdr-drill-data-cache", drillPath, cacheRoot + "/drill-data"}} {
			found := false
			for _, volume := range daemon.Spec.Template.Spec.Volumes {
				if volume.Name == volumeSpec.name {
					if volume.HostPath == nil {
						return fmt.Errorf("unexpected cache volume")
					}
					volume.HostPath.Path = volumeSpec.path
					found = true
				}
			}
			if !found {
				daemon.Spec.Template.Spec.Volumes = append(daemon.Spec.Template.Spec.Volumes, corev1.Volume{Name: volumeSpec.name, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: volumeSpec.path, Type: &typeDirectory}}})
			}
			found = false
			for _, mount := range container.VolumeMounts {
				if mount.Name == volumeSpec.name {
					if mount.MountPath != volumeSpec.mount {
						return fmt.Errorf("unexpected cache mount")
					}
					found = true
				}
			}
			if !found {
				container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: volumeSpec.name, MountPath: volumeSpec.mount})
			}
		}
		if reflect.DeepEqual(before.Spec, daemon.Spec) {
			return nil
		}
		_, err = r.client.AppsV1().DaemonSets(namespace).Update(ctx, daemon, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		daemon, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return daemon.Status.ObservedGeneration >= daemon.Generation && daemon.Status.DesiredNumberScheduled > 0 && daemon.Status.UpdatedNumberScheduled == daemon.Status.DesiredNumberScheduled && daemon.Status.NumberReady == daemon.Status.DesiredNumberScheduled, nil
	})
}

func (r *KubernetesAgentRuntime) cleanCache(ctx context.Context, namespace string) error {
	daemon, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] != "v1" {
		return fmt.Errorf("installed Velero does not support cache cleanup")
	}
	if len(daemon.Spec.Template.Spec.Containers) != 1 || daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != daemon.Spec.Template.Spec.Containers[0].Image {
		return fmt.Errorf("cache capability does not match installed image")
	}
	pods, err := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(daemon.Spec.Selector)})
	if err != nil {
		return err
	}
	nodes := map[string]bool{}
	var jobs []string
	defer func() {
		for _, name := range jobs {
			policy := metav1.DeletePropagationBackground
			_ = r.client.BatchV1().Jobs(namespace).Delete(context.Background(), name, metav1.DeleteOptions{PropagationPolicy: &policy})
		}
	}()
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" || nodes[pod.Spec.NodeName] {
			continue
		}
		nodes[pod.Spec.NodeName] = true
		deadline, ttl, backoff := int64(120), int32(300), int32(0)
		mountType := corev1.HostPathDirectoryOrCreate
		indexHostPath := defaultCacheHostRoot + "/index"
		for _, volume := range daemon.Spec.Template.Spec.Volumes {
			if volume.Name == "hypercdr-kopia-cache" && volume.HostPath != nil && volume.HostPath.Path != "" {
				indexHostPath = volume.HostPath.Path
			}
		}
		automount := false
		runAsUser := int64(0)
		job, err := r.client.BatchV1().Jobs(namespace).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{GenerateName: "hypercdr-cache-clean-", Namespace: namespace}, Spec: batchv1.JobSpec{ActiveDeadlineSeconds: &deadline, TTLSecondsAfterFinished: &ttl, BackoffLimit: &backoff, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{RunAsUser: &runAsUser}, AutomountServiceAccountToken: &automount, NodeName: pod.Spec.NodeName, RestartPolicy: corev1.RestartPolicyNever, ImagePullSecrets: pod.Spec.ImagePullSecrets, Containers: []corev1.Container{{Name: "clean", Image: pod.Spec.Containers[0].Image, Command: []string{"/velero", "cache-clean"}, VolumeMounts: []corev1.VolumeMount{{Name: "cache", MountPath: cacheRoot}}}}, Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: indexHostPath, Type: &mountType}}}}}}}}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		jobs = append(jobs, job.Name)
	}
	if len(jobs) == 0 {
		return fmt.Errorf("no node-agent nodes available for cache cleanup")
	}
	for _, name := range jobs {
		if err := wait.PollUntilContextTimeout(ctx, time.Second, 150*time.Second, true, func(ctx context.Context) (bool, error) {
			job, err := r.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			if job.Status.Failed > 0 {
				return false, fmt.Errorf("cache cleanup failed on a node")
			}
			return job.Status.Succeeded > 0, nil
		}); err != nil {
			return err
		}
	}
	return nil
}
