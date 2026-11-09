package maintenance

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/util/logging"
)

func TestMaintenanceJobPersistentCacheCapability(t *testing.T) {
	for _, scenario := range []string{"enabled", "disabled", "image-mismatch", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, appsv1.AddToScheme(scheme))
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "velero", Namespace: "agent"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "velero", Image: "velero:test"}}}}}}
			daemon := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "agent"}, Spec: appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"hypercdr.io/cache-persistence-supported": "v1", "hypercdr.io/cache-persistence-image": "velero:test"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "node-agent", Image: "velero:test", Env: []corev1.EnvVar{{Name: "HCDR_CACHE_PERSISTENCE", Value: "true"}, {Name: "HCDR_CACHE_BUDGET_BYTES", Value: "5368709120"}}, VolumeMounts: []corev1.VolumeMount{{Name: "hypercdr-kopia-cache", MountPath: "/var/cache/hypercdr"}}}}, Volumes: []corev1.Volume{{Name: "hypercdr-kopia-cache", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/hypercdr/kopia-cache"}}}}},
			}}}
			switch scenario {
			case "disabled":
				daemon.Spec.Template.Spec.Containers[0].Env[0].Value = "false"
			case "image-mismatch":
				daemon.Spec.Template.Spec.Containers[0].Image = "velero:other"
			case "unsupported":
				daemon.Spec.Template.Annotations = nil
			}
			cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deployment, daemon).Build()
			repo := &velerov1.BackupRepository{ObjectMeta: metav1.ObjectMeta{Name: "repo", Namespace: "agent"}}
			job, err := buildJob(cli, t.Context(), repo, "store", nil, logrus.InfoLevel, logging.NewFormatFlag(), logrus.New())
			require.NoError(t, err)
			if scenario == "enabled" {
				require.NotNil(t, job.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser)
				require.Zero(t, *job.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser)
				require.False(t, *job.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot)
				require.Contains(t, job.Spec.Template.Spec.Volumes, daemon.Spec.Template.Spec.Volumes[0])
				require.Contains(t, job.Spec.Template.Spec.Containers[0].VolumeMounts, daemon.Spec.Template.Spec.Containers[0].VolumeMounts[0])
				require.Contains(t, job.Spec.Template.Spec.Containers[0].Env, daemon.Spec.Template.Spec.Containers[0].Env[1])
			} else {
				require.Empty(t, job.Spec.Template.Spec.Volumes)
				require.Empty(t, job.Spec.Template.Spec.Containers[0].Env)
			}
		})
	}
}
