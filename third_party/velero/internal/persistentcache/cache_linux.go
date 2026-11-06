package persistentcache

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const Root = "/var/cache/hypercdr"
const Allocation = int64(256 << 20)

type Lease struct {
	Path string
	lock *os.File
}

func lock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func size(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache contains a symbolic link")
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func Acquire(root, identity string, budget int64) (*Lease, error) {
	return acquire(root, identity, budget, checkDiskSpace)
}

func checkDiskSpace(root string) error {
	var disk syscall.Statfs_t
	if err := syscall.Statfs(root, &disk); err != nil {
		return err
	}
	free := uint64(disk.Bavail) * uint64(disk.Bsize)
	minimum := max(uint64(5<<30), uint64(disk.Blocks)*uint64(disk.Bsize)/10)
	if free < minimum+uint64(Allocation) {
		return fmt.Errorf("insufficient free disk space for persistent cache")
	}
	return nil
}

func acquire(root, identity string, budget int64, checkSpace func(string) error) (*Lease, error) {
	if budget < Allocation {
		return nil, fmt.Errorf("cache budget is too small")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid cache root")
	}
	if err := checkSpace(root); err != nil {
		return nil, err
	}
	guard, err := lock(filepath.Join(root, ".guard"))
	if err != nil {
		return nil, err
	}
	defer guard.Close()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
	leaseLock, err := lock(filepath.Join(root, key+".lock"))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, key)
	entries, err := os.ReadDir(root)
	if err != nil {
		leaseLock.Close()
		return nil, err
	}
	type candidate struct {
		path     string
		key      string
		modified time.Time
		bytes    int64
	}
	var candidates []candidate
	total := Allocation
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 64 || strings.Trim(entry.Name(), "0123456789abcdef") != "" {
			continue
		}
		entryPath := filepath.Join(root, entry.Name())
		bytes, err := size(entryPath)
		if err != nil {
			leaseLock.Close()
			return nil, err
		}
		if entry.Name() == key {
			total += max(int64(0), bytes-Allocation)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			leaseLock.Close()
			return nil, err
		}
		reserved := max(Allocation, bytes)
		total += reserved
		candidates = append(candidates, candidate{entryPath, entry.Name(), info.ModTime(), reserved})
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].modified.Before(candidates[right].modified) })
	for _, entry := range candidates {
		if total <= budget && time.Since(entry.modified) < 7*24*time.Hour {
			continue
		}
		entryLock, err := lock(filepath.Join(root, entry.key+".lock"))
		if err != nil {
			continue
		}
		err = os.RemoveAll(entry.path)
		entryLock.Close()
		if err == nil {
			total -= entry.bytes
		}
	}
	if total > budget {
		leaseLock.Close()
		return nil, fmt.Errorf("node cache budget is reserved by active tasks")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		leaseLock.Close()
		return nil, err
	}
	return &Lease{Path: path, lock: leaseLock}, nil
}

func (lease *Lease) Close() {
	if lease == nil || lease.lock == nil {
		return
	}
	now := time.Now()
	_ = os.Chtimes(lease.Path, now, now)
	_ = lease.lock.Close()
	lease.lock = nil
}

func Clean(root string) error {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid cache root")
	}
	guard, err := lock(filepath.Join(root, ".guard"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer guard.Close()
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 64 || strings.Trim(entry.Name(), "0123456789abcdef") != "" {
			continue
		}
		entryLock, err := lock(filepath.Join(root, entry.Name()+".lock"))
		if err != nil {
			continue
		}
		err = os.RemoveAll(filepath.Join(root, entry.Name()))
		entryLock.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
