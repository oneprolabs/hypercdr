package httpserver

import (
	"reflect"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestStorageClassesFromCatalogScopesAndDeduplicates(t *testing.T) {
	resources := []protocol.BackupResourceSummary{
		{Kind: "PersistentVolumeClaim", Namespace: "app", StorageClasses: []string{"longhorn"}},
		{Kind: "PersistentVolumeClaim", Namespace: "app", StorageClasses: []string{" longhorn ", "fast"}},
		{Kind: "PersistentVolumeClaim", Namespace: "other", StorageClasses: []string{"other-sc"}},
		{Kind: "StorageClass", ClusterScoped: true, StorageClasses: []string{"cluster-sc"}},
		{Kind: "Pod", Namespace: "app"},
	}
	if got, want := storageClassesFromCatalog(resources, []string{"app"}), []string{"fast", "longhorn"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("storageClassesFromCatalog() = %v, want %v", got, want)
	}
}

func TestStorageClassesFromCatalogReturnsEmptyWithoutPersistentStorage(t *testing.T) {
	resources := []protocol.BackupResourceSummary{
		{Kind: "Pod", Namespace: "default"},
		{Kind: "Service", Namespace: "default"},
	}
	if got := storageClassesFromCatalog(resources, []string{"default"}); len(got) != 0 {
		t.Fatalf("storageClassesFromCatalog() = %v, want empty", got)
	}
}
