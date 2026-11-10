package persistentcache

import (
	"os"
	"path/filepath"
	"testing"
)

func testAcquire(root, identity string, budget int64) (*Lease, error) {
	return acquire(root, identity, budget, func(string) error { return nil })
}

func TestLeaseReuseIsolationAndIdleCleanup(t *testing.T) {
	root := t.TempDir()
	first, err := testAcquire(root, "repo/pvc-one", 5<<30)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := testAcquire(root, "repo/pvc-one", 5<<30)
	if err != nil {
		t.Fatal("active shard was not shareable", err)
	}
	second, err := testAcquire(root, "repo/pvc-two", 5<<30)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatal("different identities share cache")
	}
	marker := filepath.Join(first.Path, "index")
	if err := os.WriteFile(marker, []byte("cached"), 0600); err != nil {
		t.Fatal(err)
	}
	first.Close()
	reused, err := testAcquire(root, "repo/pvc-one", 5<<30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("cache not reused", err)
	}
	if err := Clean(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("active cache removed", err)
	}
	reused.Close()
	shared.Close()
	if err := Clean(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("idle cache not cleared", err)
	}
	second.Close()
}

func TestBudgetAndSymlinkFallback(t *testing.T) {
	root := t.TempDir()
	if _, err := testAcquire(root, "small", Allocation-1); err == nil {
		t.Fatal("invalid budget accepted")
	}
	first, err := testAcquire(root, "first", Allocation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testAcquire(root, "second", Allocation); err == nil {
		t.Fatal("active cache exceeds reservation budget")
	}
	first.Close()
	second, err := testAcquire(root, "second", Allocation)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := os.Stat(first.Path); !os.IsNotExist(err) {
		t.Fatal("idle shard not evicted")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(second.Path, "external")); err != nil {
		t.Fatal(err)
	}
	if _, err := testAcquire(root, "third", 5<<30); err == nil {
		t.Fatal("symbolic link accepted")
	}
}

func TestCleanRejectsLinkedRoot(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(target, linked); err != nil {
		t.Fatal(err)
	}
	if err := Clean(linked); err == nil {
		t.Fatal("linked cache root accepted")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("linked target changed", err)
	}
}
