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
	if enabled {
		mounted := false
		for _, mount := range container.VolumeMounts {
			if mount.MountPath != cacheRoot || mount.ReadOnly {
				continue
			}
			for _, volume := range daemon.Spec.Template.Spec.Volumes {
				if volume.Name == mount.Name && volume.HostPath != nil && volume.HostPath.Path == "/var/lib/hypercdr/kopia-cache" {
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
	}
	return state
}

func supportsPersistentCache(image string) bool {
	image = strings.SplitN(image, "@", 2)[0]
	return strings.HasSuffix(image, "-cache-v1") || image == "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5" || image == "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6"
}

func supportsOnDemandImage(image string) bool {
	image = strings.SplitN(image, "@", 2)[0]
	return strings.HasSuffix(image, "-on-demand-cache-v1") || image == "registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.6"
}

func (r *KubernetesAgentRuntime) ConfigureCache(ctx context.Context, namespace string, enabled bool, maxGiB int, clean bool) error {
	if maxGiB < 1 || maxGiB > 1024 {
		return fmt.Errorf("invalid node cache budget")
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
		if daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] != "v1" {
			if !supportsPersistentCache(container.Image) {
				return fmt.Errorf("installed Velero does not support cache persistence; upgrade Velero first")
			}
			if daemon.Spec.Template.Annotations == nil {
				daemon.Spec.Template.Annotations = map[string]string{}
			}
			daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-supported"] = "v1"
			daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] = container.Image
		}
		if daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != container.Image {
			return fmt.Errorf("cache capability does not match installed image")
		}
		for _, setting := range []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: strconv.FormatBool(enabled)}, {Name: "HCDR_CACHE_BUDGET_BYTES", Value: strconv.FormatInt(int64(maxGiB)<<30, 10)}} {
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
		found := false
		for _, volume := range daemon.Spec.Template.Spec.Volumes {
			if volume.Name == "hypercdr-kopia-cache" {
				if volume.HostPath == nil || volume.HostPath.Path != "/var/lib/hypercdr/kopia-cache" {
					return fmt.Errorf("unexpected cache volume")
				}
				found = true
			}
		}
		if !found {
			daemon.Spec.Template.Spec.Volumes = append(daemon.Spec.Template.Spec.Volumes, corev1.Volume{Name: "hypercdr-kopia-cache", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/hypercdr/kopia-cache", Type: &typeDirectory}}})
		}
		found = false
		for _, mount := range container.VolumeMounts {
			if mount.Name == "hypercdr-kopia-cache" {
				if mount.MountPath != cacheRoot {
					return fmt.Errorf("unexpected cache mount")
				}
				found = true
			}
		}
		if !found {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "hypercdr-kopia-cache", MountPath: cacheRoot})
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
		automount := false
		runAsUser := int64(0)
		job, err := r.client.BatchV1().Jobs(namespace).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{GenerateName: "hypercdr-cache-clean-", Namespace: namespace}, Spec: batchv1.JobSpec{ActiveDeadlineSeconds: &deadline, TTLSecondsAfterFinished: &ttl, BackoffLimit: &backoff, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{RunAsUser: &runAsUser}, AutomountServiceAccountToken: &automount, NodeName: pod.Spec.NodeName, RestartPolicy: corev1.RestartPolicyNever, ImagePullSecrets: pod.Spec.ImagePullSecrets, Containers: []corev1.Container{{Name: "clean", Image: pod.Spec.Containers[0].Image, Command: []string{"/velero", "cache-clean"}, VolumeMounts: []corev1.VolumeMount{{Name: "cache", MountPath: cacheRoot}}}}, Volumes: []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/hypercdr/kopia-cache", Type: &mountType}}}}}}}}, metav1.CreateOptions{})
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
