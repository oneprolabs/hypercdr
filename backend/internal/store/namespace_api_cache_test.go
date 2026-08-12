package store

import "testing"

func TestMergeNamespaceAPIsReplacesOnlyScannedNamespace(t *testing.T) {
	existing := []ClusterNamespaceAPI{
		{Namespace: "app-a", Resource: "pods"},
		{Namespace: "app-b", Resource: "services"},
	}
	scanned := []ClusterNamespaceAPI{{Namespace: "app-a", Resource: "deployments"}}
	got := mergeNamespaceAPIs(existing, scanned, "app-a")
	if len(got) != 2 || got[0].Namespace != "app-b" || got[1].Resource != "deployments" {
		t.Fatalf("unexpected merged namespace API cache: %#v", got)
	}
}

func TestMergeNamespaceAPIsClearsStaleEntriesForEmptyScan(t *testing.T) {
	existing := []ClusterNamespaceAPI{{Namespace: "app-a", Resource: "pods"}, {Namespace: "app-b", Resource: "services"}}
	got := mergeNamespaceAPIs(existing, nil, "app-a")
	if len(got) != 1 || got[0].Namespace != "app-b" {
		t.Fatalf("unexpected cache after empty scan: %#v", got)
	}
}

func TestMergeNamespaceAPIsReplacesClusterScopeAlongsideNamespace(t *testing.T) {
	existing := []ClusterNamespaceAPI{
		{Scope: "namespace", Namespace: "app-a", Resource: "pods"},
		{Scope: "namespace", Namespace: "app-b", Resource: "services"},
		{Scope: "cluster", Namespace: "app-a", Resource: "nodes"},
		{Scope: "cluster", Namespace: "app-b", Resource: "clusterroles"},
	}
	scanned := []ClusterNamespaceAPI{
		{Scope: "namespace", Namespace: "app-a", Resource: "deployments"},
		{Scope: "cluster", Namespace: "app-a", Resource: "storageclasses"},
	}
	got := mergeNamespaceAPIs(existing, scanned, "app-a")
	if len(got) != 4 || got[0].Resource != "services" || got[1].Resource != "clusterroles" || got[2].Resource != "deployments" || got[3].Resource != "storageclasses" {
		t.Fatalf("unexpected merged scoped API cache: %#v", got)
	}
}

func TestMergeNamespaceAPIsClearsStaleClusterScopeWhenScanFindsNone(t *testing.T) {
	existing := []ClusterNamespaceAPI{
		{Scope: "namespace", Namespace: "app-a", Resource: "pods"},
		{Scope: "namespace", Namespace: "app-b", Resource: "services"},
		{Scope: "cluster", Namespace: "app-a", Resource: "customresourcedefinitions"},
	}
	got := mergeNamespaceAPIs(existing, []ClusterNamespaceAPI{{Scope: "namespace", Namespace: "app-a", Resource: "deployments"}}, "app-a")
	if len(got) != 2 || got[0].Namespace != "app-b" || got[1].Resource != "deployments" {
		t.Fatalf("stale cluster scope retained after complete empty scan: %#v", got)
	}
}

func TestMergeNamespaceAPIsDropsLegacyUnownedClusterScope(t *testing.T) {
	existing := []ClusterNamespaceAPI{
		{Scope: "namespace", Namespace: "app-b", Resource: "services"},
		{Scope: "cluster", Resource: "persistentvolumes"},
	}
	got := mergeNamespaceAPIs(existing, []ClusterNamespaceAPI{{Scope: "namespace", Namespace: "app-a", Resource: "deployments"}}, "app-a")
	if len(got) != 2 || got[0].Resource != "services" || got[1].Resource != "deployments" {
		t.Fatalf("legacy cluster scope leaked after merge: %#v", got)
	}
}
