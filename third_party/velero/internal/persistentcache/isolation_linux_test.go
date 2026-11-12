package persistentcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolationRetriesNeverRemoveRecreatedCache(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	isolationID := strings.Repeat("a", 64)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old-data"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Isolate(root, isolationID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("original directory still exists: %v", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(root, "new-data")
	if err := os.WriteFile(newFile, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := Isolate(root, isolationID); err != nil {
			t.Fatal(err)
		}
		if err := DeleteIsolated(root, isolationID); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(newFile); err != nil || string(data) != "new" {
			t.Fatalf("new cache affected on retry %d: %v", attempt, err)
		}
	}
}

func TestIsolationRecoversRenameBeforeReceipt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	isolationID := strings.Repeat("b", 64)
	staging, err := isolationDirectory(root, isolationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Isolate(root, isolationID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteIsolated(root, isolationID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("recreated root was touched during recovery: %v", err)
	}
}

func TestIsolationRejectsUnsafeSourcesAndMissingReceipt(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "cache")
	isolationID := strings.Repeat("c", 64)
	if err := os.Symlink(parent, root); err != nil {
		t.Fatal(err)
	}
	if err := Isolate(root, isolationID); err == nil {
		t.Fatal("accepted symlink cache root")
	}
	if err := DeleteIsolated(root, isolationID); err == nil {
		t.Fatal("deleted without isolation receipt")
	}
	for _, unsafeRoot := range []string{"/", "relative", parent + "/../cache"} {
		if err := Isolate(unsafeRoot, isolationID); err == nil {
			t.Fatalf("accepted unsafe path %s", unsafeRoot)
		}
	}
}

func TestRollbackDoesNotOverwriteRecreatedDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	isolationID := strings.Repeat("d", 64)
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Isolate(root, isolationID); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RestoreIsolated(root, isolationID); err == nil {
		t.Fatal("rollback overwrote an existing directory")
	}
	if err := DeleteIsolated(root, isolationID); err == nil {
		t.Fatal("cleanup allowed after rollback started")
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := RestoreIsolated(root, isolationID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	if err := RestoreIsolated(root, isolationID); err != nil {
		t.Fatal(err)
	}
	if err := Isolate(root, isolationID); err == nil {
		t.Fatal("rolled back isolation ID was reused")
	}
}
