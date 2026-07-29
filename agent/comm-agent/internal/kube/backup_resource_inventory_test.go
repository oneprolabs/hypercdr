package kube

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"
)

func TestReadBackupResourceArchiveIndexesEveryNamespacedCR(t *testing.T) {
	objects := map[string]map[string]any{
		"resources/deployments.apps/namespaces/demo/demo.json":                      {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"namespace": "demo", "name": "demo"}},
		"resources/backupactions.actions.kio.kasten.io/namespaces/demo/action.json": {"apiVersion": "actions.kio.kasten.io/v1alpha1", "kind": "BackupAction", "metadata": map[string]any{"namespace": "demo", "name": "action"}},
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, object := range objects {
		data, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := readBackupResourceArchive(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("objects=%d, want 2", len(items))
	}
	foundKasten := false
	for _, item := range items {
		if item.APIVersion == "actions.kio.kasten.io/v1alpha1" && item.Kind == "BackupAction" && item.Namespace == "demo" {
			foundKasten = true
		}
	}
	if !foundKasten {
		t.Fatalf("Kasten namespaced CR was omitted: %#v", items)
	}
}
