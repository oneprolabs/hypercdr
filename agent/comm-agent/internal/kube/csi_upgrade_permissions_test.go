package kube

import (
	"context"
	"reflect"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCSIUpgradePermissions(t *testing.T) {
	for _, scenario := range []string{"complete", "wildcard", "missing-create", "missing-delete", "restricted-name", "split-rules"} {
		t.Run(scenario, func(t *testing.T) {
			role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: scopedRBACName("hypercdr-agent", "enterprise-agent")}, Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses"}, Verbs: []string{"get", "list", "watch", "create"}},
				{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"csidrivers"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{"snapshot.storage.k8s.io"}, Resources: []string{"volumesnapshotclasses"}, Verbs: []string{"get", "list", "watch"}},
				{APIGroups: []string{"snapshot.storage.k8s.io"}, Resources: []string{"volumesnapshots"}, Verbs: []string{"get", "list", "watch", "update", "delete"}},
			}}
			switch scenario {
			case "wildcard":
				role.Rules = []rbacv1.PolicyRule{{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}}}
			case "missing-create":
				role.Rules[0].Verbs = []string{"get", "list", "watch"}
			case "missing-delete":
				role.Rules[3].Verbs = []string{"get", "list", "watch", "update"}
			case "restricted-name":
				role.Rules[0].ResourceNames = []string{"business"}
			case "split-rules":
				role.Rules[0].Verbs = []string{"get", "list", "watch"}
				role.Rules = append(role.Rules, rbacv1.PolicyRule{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"storageclasses"}, Verbs: []string{"create"}})
			}
			client := fake.NewSimpleClientset(role)
			manager := &KubernetesAgentRuntime{client: client}
			err := manager.ensureCSIUpgradePermissions(context.Background(), "enterprise-agent")
			wantSuccess := scenario == "complete" || scenario == "wildcard" || scenario == "split-rules"
			if (err == nil) != wantSuccess {
				t.Fatalf("permission validation: %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "upgrade-cluster-csi-rbac.sh --namespace enterprise-agent --apply") {
				t.Fatalf("missing administrator repair instructions: %v", err)
			}
			updated, err := client.RbacV1().ClusterRoles().Get(context.Background(), role.Name, metav1.GetOptions{})
			if err != nil || !reflect.DeepEqual(role.Rules, updated.Rules) {
				t.Fatalf("preflight changed RBAC: %v", err)
			}
		})
	}
}

func TestCSIUpgradeRejectsLegacyRBACBeforeChangingWorkloads(t *testing.T) {
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "hypercdr-agent"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "list"}}}}
	client := fake.NewSimpleClientset(role)
	manager := &KubernetesAgentRuntime{client: client}
	err := manager.UpgradeVelero(context.Background(), VeleroUpgradeOptions{Image: "registry/velero:v1.18.2-hcdr.8-backup-volume-policy-v1-source-isolation-cache-v1"})
	if err == nil || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("expected actionable permission failure: %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("failed preflight mutated cluster: %#v", action)
		}
	}
}

func TestSourceAddressedVeleroCapabilities(t *testing.T) {
	image := "registry/velero:v1.18.2-hcdr.8-backup-volume-policy-v1-" + strings.Repeat("a", 64) + "-isolation-cache-v1"
	for _, reference := range []string{image, image + "@sha256:" + strings.Repeat("b", 64)} {
		if !supportsBackupPVCRefreshImage(reference) || !supportsPersistentCache(reference) || !supportsCacheIsolationImage(reference) || !supportsOnDemandImage(reference) {
			t.Fatalf("release capabilities missing for %s", reference)
		}
	}
}
