package httpserver

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentInstallerGrantsScopedSnapshotCleanupVerbs(t *testing.T) {
	_, role, found := strings.Cut(installScriptTemplate, "kind: ClusterRole\nmetadata:\n  name: ${AGENT_RBAC_NAME}\n")
	if !found {
		t.Fatal("agent ClusterRole template is missing")
	}
	role, _, found = strings.Cut(role, "${AGENT_PLATFORM_RBAC_RULES}")
	if !found {
		t.Fatal("agent ClusterRole rules boundary is missing")
	}
	var manifest struct {
		Rules []struct {
			APIGroups []string `yaml:"apiGroups"`
			Resources []string `yaml:"resources"`
			Verbs     []string `yaml:"verbs"`
		} `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte("metadata:\n"+role), &manifest); err != nil {
		t.Fatal(err)
	}
	var snapshotVerbs []string
	for _, rule := range manifest.Rules {
		if !slices.Contains(rule.APIGroups, "snapshot.storage.k8s.io") {
			continue
		}
		if slices.Contains(rule.Resources, "volumesnapshots") {
			snapshotVerbs = append(snapshotVerbs, rule.Verbs...)
		}
		if slices.Contains(rule.Resources, "volumesnapshotcontents") || slices.Contains(rule.Resources, "volumesnapshotclasses") || slices.Contains(rule.Resources, "*") {
			for _, verb := range rule.Verbs {
				if !slices.Contains([]string{"get", "list", "watch"}, verb) {
					t.Fatalf("snapshot cleanup must not grant %s on snapshot contents/classes", verb)
				}
			}
		}
	}
	for _, verb := range []string{"get", "list", "watch", "update", "delete"} {
		if !slices.Contains(snapshotVerbs, verb) {
			t.Fatalf("agent installer lacks volumesnapshots %s permission", verb)
		}
	}
	if slices.Contains(snapshotVerbs, "create") || slices.Contains(snapshotVerbs, "*") {
		t.Fatal("Agent cleanup should not create snapshots or grant wildcard mutation")
	}
}
