package cacheusage

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

type Usage struct {
	Exists    bool  `json:"exists"`
	UsedBytes int64 `json:"usedBytes"`
}

type fileIdentity struct{ device, inode uint64 }

func Scan(ctx context.Context, directory string) (Usage, error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || directory == "/" {
		return Usage{}, fmt.Errorf("cache directory must be an absolute non-root path")
	}
	if err := ctx.Err(); err != nil {
		return Usage{}, err
	}
	root, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return Usage{}, nil
	}
	if err != nil {
		return Usage{}, err
	}
	if !root.IsDir() {
		return Usage{}, fmt.Errorf("cache path is not a directory")
	}
	usage := Usage{Exists: true}
	seen := map[fileIdentity]struct{}{}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("allocated disk usage is unavailable for %s", path)
		}
		identity := fileIdentity{uint64(stat.Dev), stat.Ino}
		if _, exists := seen[identity]; exists {
			return nil
		}
		seen[identity] = struct{}{}
		usage.UsedBytes += stat.Blocks * 512
		return nil
	})
	if err != nil {
		return Usage{}, err
	}
	return usage, nil
}
