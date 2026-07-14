package httpserver

import "testing"

func TestDefaultExcludedResourcesAreEmpty(t *testing.T) {
	for _, namespace := range []string{"demo-mysql-csi", "kasten-io", ""} {
		if got := defaultExcludedResources(namespace); len(got) != 0 {
			t.Fatalf("defaultExcludedResources(%q) = %v, want empty", namespace, got)
		}
	}
}

func TestDefaultExcludedResourcesForNamespacesKeepsExplicitDefaultsEmpty(t *testing.T) {
	got := defaultExcludedResourcesForNamespaces([]string{"dev-test", "kasten-io"})
	if len(got) != 0 {
		t.Fatalf("defaultExcludedResourcesForNamespaces = %v, want empty", got)
	}
}
