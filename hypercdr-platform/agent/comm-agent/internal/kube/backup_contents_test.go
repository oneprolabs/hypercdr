package kube

import (
	"reflect"
	"testing"
)

func TestSplitVeleroGroupResource(t *testing.T) {
	group, resource := splitVeleroGroupResource("deployments.apps")
	if group != "apps" || resource != "deployments" {
		t.Fatalf("got %q/%q", group, resource)
	}
	group, resource = splitVeleroGroupResource("persistentvolumeclaims")
	if group != "" || resource != "persistentvolumeclaims" {
		t.Fatalf("core got %q/%q", group, resource)
	}
	group, resource = splitVeleroGroupResource("backupactions.actions.kio.kasten.io")
	if group != "actions.kio.kasten.io" || resource != "backupactions" {
		t.Fatalf("custom resource got %q/%q", group, resource)
	}
}

func TestClassifyVeleroArchivePath(t *testing.T) {
	tests := []struct {
		name      string
		role      veleroArchiveRole
		group     string
		namespace string
		cluster   bool
		version   string
		preferred bool
	}{
		{"resources/deployments.apps/namespaces/demo/web.json", veleroArchiveCanonicalResource, "deployments.apps", "demo", false, "", false},
		{"resources/customresourcedefinitions.apiextensions.k8s.io/cluster/widgets.example.io.json", veleroArchiveCanonicalResource, "customresourcedefinitions.apiextensions.k8s.io", "", true, "", false},
		{"resources/widgets.example.io/v1/namespaces/demo/widget.json", veleroArchiveVersionRepresentation, "widgets.example.io", "demo", false, "v1", false},
		{"resources/widgets.example.io/v1beta1-preferredversion/namespaces/demo/widget.json", veleroArchiveVersionRepresentation, "widgets.example.io", "demo", false, "v1beta1", true},
		{"metadata/version", veleroArchiveOther, "", "", false, "", false},
		{"resources/pods/itemoperations/demo.json", veleroArchiveOther, "", "", false, "", false},
		{"resources/pods/namespaces/demo/pod.yaml", veleroArchiveOther, "", "", false, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item, role := classifyVeleroArchivePath(test.name)
			if role != test.role || item.groupResource != test.group || item.namespace != test.namespace || item.clusterScoped != test.cluster || item.version != test.version || item.preferred != test.preferred {
				t.Fatalf("item=%+v role=%v", item, role)
			}
		})
	}
}

func TestResourceReferencesDoesNotExposeSecretData(t *testing.T) {
	images, storage := resourceReferences(map[string]any{
		"data":   map[string]any{"password": "sensitive"},
		"spec":   map[string]any{"storageClassName": "fast", "template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"image": "registry.example/app:v1"}}}}},
		"status": map[string]any{"containerStatuses": []any{map[string]any{"image": "runtime.example/app:v1", "imageID": "runtime.example/app@sha256:abc"}}},
	})
	if !reflect.DeepEqual(images, []string{"registry.example/app:v1"}) || !reflect.DeepEqual(storage, []string{"fast"}) {
		t.Fatalf("images=%v storage=%v", images, storage)
	}
}
