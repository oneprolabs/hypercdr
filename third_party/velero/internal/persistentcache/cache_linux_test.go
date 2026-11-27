package persistentcache

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSizeDuringCacheFileReplacement(t *testing.T) {
	root := t.TempDir()
	active, err := testAcquire(root, "repo/active", 5<<30)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	stable := filepath.Join(active.Path, "stable")
	if err := os.WriteFile(stable, []byte("cached"), 0600); err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	writers.Add(1)
	stop := make(chan struct{})
	defer func() { close(stop); writers.Wait() }()
	go func() {
		defer writers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			path := filepath.Join(active.Path, "kopia.blobcfg")
			if err := os.WriteFile(path, []byte("temporary"), 0600); err != nil {
				t.Error(err)
				return
			}
			if err := os.Remove(path); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for attempt := 0; attempt < 1000; attempt++ {
		bytes, err := size(active.Path)
		if err != nil || bytes < 6 {
			t.Fatalf("replacement interrupted scan: bytes=%d error=%v", bytes, err)
		}
		lease, err := testAcquire(root, "repo/other", 5<<30)
		if err != nil {
			t.Fatalf("active cache replacement disabled another volume's cache: %v", err)
		}
		lease.Close()
	}
}

func TestConcurrentAcquisitionRespectsBudget(t *testing.T) {
	root := t.TempDir()
	var workers sync.WaitGroup
	leases := make(chan *Lease, 16)
	for worker := 0; worker < cap(leases); worker++ {
		workers.Add(1)
		go func(identity int) {
			defer workers.Done()
			lease, err := testAcquire(root, fmt.Sprintf("repo/%d", identity), Allocation)
			if err == nil {
				leases <- lease
			}
		}(worker)
	}
	workers.Wait()
	close(leases)
	count := 0
	for lease := range leases {
		count++
		lease.Close()
	}
	if count != 1 {
		t.Fatalf("single-shard budget admitted %d simultaneous leases", count)
	}
}

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
