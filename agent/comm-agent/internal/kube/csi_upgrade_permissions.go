package kube

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *KubernetesAgentRuntime) ensureCSIUpgradePermissions(ctx context.Context, namespace string) error {
	roleName := scopedRBACName("hypercdr-agent", namespace)
	role, err := r.client.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for _, need := range []struct {
		group, resource string
		verbs           []string
	}{
		{"storage.k8s.io", "storageclasses", []string{"get", "list", "watch", "create"}},
		{"storage.k8s.io", "csidrivers", []string{"get", "list", "watch"}},
		{"snapshot.storage.k8s.io", "volumesnapshotclasses", []string{"get", "list", "watch"}},
		{"snapshot.storage.k8s.io", "volumesnapshots", []string{"get", "list", "watch", "update", "delete"}},
	} {
		for _, verb := range need.verbs {
			allowed := false
			for _, rule := range role.Rules {
				if len(rule.ResourceNames) == 0 && containsString(rule.APIGroups, need.group) && containsString(rule.Resources, need.resource) && containsString(rule.Verbs, verb) {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("CSI upgrade requires %s on %s/%s in ClusterRole %s; a cluster administrator must run upgrade-cluster-csi-rbac.sh --namespace %s --apply from the release package before retrying; Agent will not escalate its own permissions", verb, need.group, need.resource, roleName, namespace)
			}
		}
	}
	return nil
}
