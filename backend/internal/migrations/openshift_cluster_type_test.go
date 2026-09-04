package migrations

import (
	"strings"
	"testing"
)

func TestOpenShiftClusterTypeMigrationUpdatesTokenAndClusterConstraints(t *testing.T) {
	raw, err := migrationFiles.ReadFile("sql/000038_openshift_cluster_type.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"agent_tokens_cluster_type_check",
		"clusters_cluster_type_check",
		"'native-kubernetes'",
		"'huaweicloud-cce'",
		"'openshift'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("OpenShift cluster type migration is missing %q", required)
		}
	}
}
