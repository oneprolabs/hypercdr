package persistentcache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func isolationDirectory(root, isolationID string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return "", fmt.Errorf("invalid cache isolation path")
	}
	if len(isolationID) != 64 || strings.Trim(isolationID, "0123456789abcdef") != "" {
		return "", fmt.Errorf("invalid cache isolation ID")
	}
	return root + ".delete-" + isolationID, nil
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("not a cache directory: %s", path)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func Isolate(root, isolationID string) error {
	staging, err := isolationDirectory(root, isolationID)
	if err != nil {
		return err
	}
	if err := os.Mkdir(staging, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := requireDirectory(staging); err != nil {
		return err
	}
	guard, err := lock(filepath.Join(staging, "lock"), false)
	if err != nil {
		return err
	}
	defer guard.Close()
	if _, err := os.Lstat(filepath.Join(staging, "rollback-started")); err == nil {
		return fmt.Errorf("cache isolation was rolled back")
	} else if !os.IsNotExist(err) {
		return err
	}
	receipt := filepath.Join(staging, "isolated")
	if info, err := os.Lstat(receipt); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("invalid cache isolation receipt")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	dataPath := filepath.Join(staging, "data")
	if err := requireDirectory(dataPath); os.IsNotExist(err) {
		if err := requireDirectory(root); err == nil {
			if err := unix.Renameat2(unix.AT_FDCWD, root, unix.AT_FDCWD, dataPath, unix.RENAME_NOREPLACE); err != nil {
				return fmt.Errorf("isolate cache directory: %w", err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(root)); err != nil {
		return err
	}
	if err := syncDirectory(staging); err != nil {
		return err
	}
	marker, err := os.OpenFile(receipt, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	syncErr := marker.Sync()
	closeErr := marker.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDirectory(staging)
}

func DeleteIsolated(root, isolationID string) error {
	staging, err := isolationDirectory(root, isolationID)
	if err != nil {
		return err
	}
	if err := requireDirectory(staging); err != nil {
		return err
	}
	guard, err := lock(filepath.Join(staging, "lock"), false)
	if err != nil {
		return err
	}
	defer guard.Close()
	if _, err := os.Lstat(filepath.Join(staging, "rollback-started")); err == nil {
		return fmt.Errorf("cache isolation was rolled back")
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err := os.Lstat(filepath.Join(staging, "isolated"))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid cache isolation receipt")
	}
	dataPath := filepath.Join(staging, "data")
	if err := requireDirectory(dataPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.RemoveAll(dataPath); err != nil {
		return err
	}
	return syncDirectory(staging)
}

func RestoreIsolated(root, isolationID string) error {
	staging, err := isolationDirectory(root, isolationID)
	if err != nil {
		return err
	}
	if err := requireDirectory(staging); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	guard, err := lock(filepath.Join(staging, "lock"), false)
	if err != nil {
		return err
	}
	defer guard.Close()
	restored := filepath.Join(staging, "restored")
	if info, err := os.Lstat(restored); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("invalid rollback receipt")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	dataPath := filepath.Join(staging, "data")
	rollbackMarker, err := os.OpenFile(filepath.Join(staging, "rollback-started"), os.O_CREATE|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	markerSyncErr := rollbackMarker.Sync()
	markerCloseErr := rollbackMarker.Close()
	if markerSyncErr != nil {
		return markerSyncErr
	}
	if markerCloseErr != nil {
		return markerCloseErr
	}
	if err := syncDirectory(staging); err != nil {
		return err
	}
	if err := requireDirectory(dataPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("isolated cache unavailable for rollback: %w", err)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, dataPath, unix.AT_FDCWD, root, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("restore isolated cache without overwriting current directory: %w", err)
	}
	if err := syncDirectory(filepath.Dir(root)); err != nil {
		return err
	}
	marker, err := os.OpenFile(restored, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	syncErr := marker.Sync()
	closeErr := marker.Close()
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDirectory(staging)
}
