package cacheusage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func allocatedBytes(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Blocks * 512
}

func TestScanCountsAllocatedBytesAndDoesNotFollowSymlinksOrCountHardLinksTwice(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "data")
	if err := os.WriteFile(file, make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(directory, "hard-link")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside-data"), make([]byte, 32768), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	usage, err := Scan(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	expected := allocatedBytes(t, directory) + allocatedBytes(t, file) + allocatedBytes(t, link)
	if !usage.Exists || usage.UsedBytes != expected {
		t.Fatalf("usage = %#v, expected %d", usage, expected)
	}
}

func TestScanDoesNotTreatSparseLogicalSizeAsDiskUsage(t *testing.T) {
	directory := t.TempDir()
	file, err := os.Create(filepath.Join(directory, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	usage, err := Scan(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if usage.UsedBytes >= 1<<30 {
		t.Fatal("logical file length was counted instead of allocated blocks")
	}
}

func TestScanMissingAndCancelledDirectories(t *testing.T) {
	usage, err := Scan(context.Background(), filepath.Join(t.TempDir(), "not-created"))
	if err != nil || usage.Exists || usage.UsedBytes != 0 {
		t.Fatalf("missing directory: %#v %v", usage, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := Scan(context.Background(), "/"); err == nil {
		t.Fatal("root filesystem scan was allowed")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(context.Background(), link); err == nil {
		t.Fatal("root symlink was followed")
	}
}
