package kube

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var ErrCacheDirectoryStageFailed = errors.New("cache directory stage reached terminal failure")

func (r *KubernetesAgentRuntime) ValidateCacheDirectoryTransition(ctx context.Context, namespace, isolationID string, paths, nodes []string) error {
	if len(paths) == 0 || len(nodes) == 0 {
		return fmt.Errorf("cache transition requires directories and nodes")
	}
	for index, path := range paths {
		for _, other := range paths[index+1:] {
			if path == other || strings.HasPrefix(path, other+"/") || strings.HasPrefix(other, path+"/") {
				return fmt.Errorf("cache transition directories must not overlap: %s and %s", path, other)
			}
		}
	}
	daemon, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(daemon.Spec.Template.Spec.Containers) != 1 || daemon.Spec.Template.Annotations["hypercdr.io/cache-isolation-supported"] != "v1" || daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != daemon.Spec.Template.Spec.Containers[0].Image {
		return fmt.Errorf("installed node-agent image does not support cache directory isolation")
	}
	for _, path := range paths {
		for _, node := range nodes {
			if _, err := cacheDirectoryStageJob(namespace, node, daemon.Spec.Template.Spec.Containers[0].Image, "isolate", isolationID, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func cacheDirectoryStageJob(namespace, nodeName, image, operation, isolationID, hostPath string) (*batchv1.Job, error) {
	if operation != "isolate" && operation != "rollback" && operation != "delete-isolated" {
		return nil, fmt.Errorf("unsupported cache directory operation")
	}
	if len(isolationID) != 64 || strings.Trim(isolationID, "0123456789abcdef") != "" {
		return nil, fmt.Errorf("invalid cache isolation ID")
	}
	if !filepath.IsAbs(hostPath) || filepath.Clean(hostPath) != hostPath || filepath.Dir(hostPath) == "/" || nodeName == "" {
		return nil, fmt.Errorf("unsafe cache directory stage target")
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(operation+"\x00"+isolationID+"\x00"+nodeName+"\x00"+hostPath)))[:32]
	backoff := int32(0)
	deadline := int64(300)
	if operation == "delete-isolated" {
		backoff = 5
		deadline = 24 * 60 * 60
	}
	mountType := corev1.HostPathDirectory
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-cache-stage-" + key, Namespace: namespace, Labels: map[string]string{"hypercdr.io/cache-stage": operation}},
		Spec: batchv1.JobSpec{BackoffLimit: &backoff, ActiveDeadlineSeconds: &deadline, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			NodeName: nodeName, RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: int64Ptr(0)}, AutomountServiceAccountToken: ptr(false),
			Containers: []corev1.Container{{Name: "cache-stage", Image: image, Command: []string{"/velero", "cache-" + operation, "/cache-parent/" + filepath.Base(hostPath), isolationID}, VolumeMounts: []corev1.VolumeMount{{Name: "cache-parent", MountPath: "/cache-parent"}}}},
			Volumes:    []corev1.Volume{{Name: "cache-parent", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: filepath.Dir(hostPath), Type: &mountType}}}},
		}}},
	}, nil
}

func (r *KubernetesAgentRuntime) RunCacheDirectoryStage(ctx context.Context, namespace, operation, isolationID string, paths, nodes []string) error {
	return r.RunCacheDirectoryStageWithProgress(ctx, namespace, operation, isolationID, paths, nodes, "", nil)
}

func (r *KubernetesAgentRuntime) RunCacheDirectoryStageWithProgress(ctx context.Context, namespace, operation, isolationID string, paths, nodes []string, taskID string, progress func(int, int, int)) error {
	if len(paths) == 0 {
		return nil
	}
	if len(nodes) == 0 {
		return fmt.Errorf("cache isolation requires a persisted node list")
	}
	daemon, err := r.client.AppsV1().DaemonSets(namespace).Get(ctx, "node-agent", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(daemon.Spec.Template.Spec.Containers) != 1 || daemon.Spec.Template.Annotations["hypercdr.io/cache-isolation-supported"] != "v1" || daemon.Spec.Template.Annotations["hypercdr.io/cache-persistence-image"] != daemon.Spec.Template.Spec.Containers[0].Image {
		return fmt.Errorf("installed node-agent image does not support cache directory isolation")
	}
	jobs := make(map[string]*batchv1.Job)
	for _, hostPath := range paths {
		for _, nodeName := range nodes {
			job, err := cacheDirectoryStageJob(namespace, nodeName, daemon.Spec.Template.Spec.Containers[0].Image, operation, isolationID, hostPath)
			if err != nil {
				return err
			}
			if operation == "delete-isolated" && taskID != "" {
				job.Name = cacheDirectoryCleanupJobName(job.Name, taskID)
			}
			job.Spec.Template.Spec.ImagePullSecrets = daemon.Spec.Template.Spec.ImagePullSecrets
			jobs[job.Name] = job
		}
	}
	if operation == "isolate" {
		for _, hostPath := range paths {
			if validationErr := validateCacheCleanupTarget(hostPath, cacheHostEnvironment(daemon)); validationErr != nil {
				for name := range jobs {
					_, lookupErr := r.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
					if !apierrors.IsNotFound(lookupErr) {
						return fmt.Errorf("cache isolation preflight requires recovery: %w", validationErr)
					}
				}
				return errors.Join(ErrCacheDirectoryStageFailed, validationErr)
			}
		}
	}
	for _, job := range jobs {
		if _, err := r.client.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		observations := make([]*batchv1.Job, 0, len(jobs))
		completed, failedAttempts := 0, 0
		for name := range jobs {
			job, err := r.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			observations = append(observations, job)
			failedAttempts += int(job.Status.Failed)
			for _, condition := range job.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					completed++
				}
			}
		}
		if progress != nil {
			progress(completed, len(jobs), failedAttempts)
		}
		return cacheDirectoryStageResult(operation, observations)
	})
}

func cacheDirectoryCleanupJobName(stageName, taskID string) string {
	return fmt.Sprintf("hypercdr-cache-stage-%x", sha256.Sum256([]byte(stageName+"\x00"+taskID)))[:53]
}

func cacheDirectoryStageResult(operation string, jobs []*batchv1.Job) (bool, error) {
	complete := len(jobs) > 0
	var failures []error
	for _, job := range jobs {
		terminal := false
		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
				terminal = true
			}
			if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
				terminal = true
				failures = append(failures, fmt.Errorf("cache %s failed on node %s: %s: %s", operation, job.Spec.Template.Spec.NodeName, condition.Reason, condition.Message))
			}
		}
		if !terminal {
			complete = false
		}
	}
	if !complete {
		return false, nil
	}
	if err := errors.Join(failures...); err != nil {
		return false, errors.Join(ErrCacheDirectoryStageFailed, err)
	}
	return true, nil
}
