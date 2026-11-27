package httpserver

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInstallerPreauthorizesOptionalManagedFeatures(t *testing.T) {
	_, role, found := strings.Cut(installScriptTemplate, "kind: ClusterRole\nmetadata:\n  name: ${AGENT_RBAC_NAME}\n")
	if !found {
		t.Fatal("managed Agent role missing")
	}
	role, _, found = strings.Cut(role, "${AGENT_PLATFORM_RBAC_RULES}")
	if !found {
		t.Fatal("managed Agent role boundary missing")
	}
	var manifest struct {
		Metadata struct {
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
		Rules []struct {
			APIGroups     []string `yaml:"apiGroups"`
			Resources     []string `yaml:"resources"`
			Verbs         []string `yaml:"verbs"`
			ResourceNames []string `yaml:"resourceNames"`
		} `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte("metadata:\n"+role), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Metadata.Annotations["hypercdr.io/permission-profile"] != "managed-cluster-v1" {
		t.Fatal("installer permission profile missing")
	}
	for _, required := range []struct {
		group, resource, verbs string
	}{
		{"storage.k8s.io", "storageclasses", "get list watch create"},
		{"storage.k8s.io", "csidrivers", "get list watch"},
		{"snapshot.storage.k8s.io", "volumesnapshotclasses", "get list watch"},
		{"snapshot.storage.k8s.io", "volumesnapshots", "get list watch update delete"},
		{"apps", "deployments", "get list watch patch update"},
		{"apps", "daemonsets", "get list watch patch update"},
		{"", "secrets", "get list watch create patch update delete"},
		{"", "configmaps", "get list watch create patch update"},
		{"apiextensions.k8s.io", "customresourcedefinitions", "get list watch create patch update delete"},
		{"velero.io", "backups", "get list watch create patch update delete"},
		{"velero.io", "restores", "get list watch create patch update delete"},
		{"velero.io", "datauploads", "get list watch create patch update delete"},
		{"velero.io", "datadownloads", "get list watch create patch update delete"},
	} {
		for _, verb := range strings.Fields(required.verbs) {
			allowed := false
			for _, rule := range manifest.Rules {
				if len(rule.ResourceNames) == 0 && slices.Contains(rule.APIGroups, required.group) && slices.Contains(rule.Resources, required.resource) && slices.Contains(rule.Verbs, verb) {
					allowed = true
				}
			}
			if !allowed {
				t.Errorf("first install must preauthorize %s %s/%s without enabling the feature first", verb, required.group, required.resource)
			}
		}
	}
	for _, rule := range manifest.Rules {
		for _, verb := range rule.Verbs {
			if slices.Contains([]string{"*", "escalate", "bind", "impersonate"}, verb) {
				t.Errorf("Agent must not receive unrestricted privilege: %s", verb)
			}
		}
	}
}
